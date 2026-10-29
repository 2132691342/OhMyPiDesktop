package service

// 健康卡计数的「可信度」语义。
//
// 这一组测的是一类很具体的错：计数没读到时，界面显示 0。
// 0 是一个断言（「有 0 个会话」），而实际情况是「没读到」——
// 磁盘上可能躺着九十个会话。这种错不会崩、不会报错、日志也是绿的，
// 用户只会觉得「这个软件数不准」，然后不再信它说的任何一个数字。
// 会悄无声息错，所以值得单独一组表驱动。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// 只有一个引擎装了、另一个没装的机器上，没装那个的会话目录多半根本不存在。
// 那种情况下以前 healthOf 会显示「已安装 · 0 个会话」——而真实的答案是「没读到」。
func TestCountSessionsReportsUnknownWhenMissing(t *testing.T) {
	cases := []struct {
		name    string
		makeDir bool
		files   int
		wantN   int
		wantOK  bool
	}{
		{name: "目录不存在 —— 没读到，不是 0 个", makeDir: false, wantN: 0, wantOK: false},
		{name: "空目录 —— 真的 0 个", makeDir: true, files: 0, wantN: 0, wantOK: true},
		{name: "有会话 —— 真值", makeDir: true, files: 3, wantN: 3, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			// 把 pi 的会话目录挪到本用例控制的临时目录上。
			// countSessions 只认「布局给的会话目录」，所以改环境变量即可。
			dir := filepath.Join(t.TempDir(), "agent")
			if tc.makeDir {
				sessions := filepath.Join(dir, "sessions", "proj-a")
				if err := os.MkdirAll(sessions, 0o755); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < tc.files; i++ {
					name := filepath.Join(sessions, "s"+string(rune('a'+i))+".jsonl")
					if err := os.WriteFile(name, []byte("{}\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("PI_CODING_AGENT_DIR", dir)

			got, ok := app.countSessions(engine.KindPi)
			if got != tc.wantN {
				t.Fatalf("countSessions 数到 %d，期望 %d", got, tc.wantN)
			}
			if ok != tc.wantOK {
				t.Fatalf("countSessions 的 ok=%v，期望 %v（这决定界面显示 0 还是「—」）", ok, tc.wantOK)
			}
		})
	}
}

// known 标记必须一路传到 health 的响应里，中间不能断。
//
// 断在任一环上，界面就又回到「拿 0 当 0」——而这个 bug 全程不报错。
func TestHealthPropagatesKnownFlags(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ context.Context, _ string, k engine.Kind) (engine.Identity, string) {
		return engine.Identity{Kind: k, Exe: "x.exe", Version: "1.0.0", Caps: engine.CapsFor(k)}, ""
	})
	defer restore()

	// pi 那份扫到了真值；omp 那份什么都没扫到。
	app.storeCounts(engine.KindPi, healthCounts{
		sessions: 42, skills: 3, plugins: 2, mcp: 1,
		at:    time.Now(),
		known: knownFlags{sessions: true, skills: true, plugins: true, mcp: true},
	})
	app.storeCounts(engine.KindOMP, healthCounts{at: time.Now()})

	row := app.healthOf(engine.KindPi, engine.RunRow{}, engine.Latest{})
	if !row.SessionCountKnown || !row.SkillCountKnown || !row.PluginCountKnown || !row.McpCountKnown {
		t.Fatalf("扫到的那一格必须报可信，实际 %+v", row)
	}
	if row.SessionCount != 42 {
		t.Fatalf("session_count = %d，期望 42", row.SessionCount)
	}

	missing := app.healthOf(engine.KindOMP, engine.RunRow{}, engine.Latest{})
	if missing.SessionCountKnown || missing.SkillCountKnown || missing.PluginCountKnown || missing.McpCountKnown {
		t.Fatalf("没扫到的那一格必须报未知，实际 %+v", missing)
	}
}

// 合计里只要有一格没读到，totals_partial 必须是真。
//
// 否则「共 12 个技能」少了 pi 那一份也没人说，读的人会当它数全了。
func TestHealthTotalsPartial(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ context.Context, _ string, k engine.Kind) (engine.Identity, string) {
		return engine.Identity{Kind: k, Exe: "x.exe", Caps: engine.CapsFor(k)}, ""
	})
	defer restore()

	// pi 有真值，omp 全没读到。
	app.storeCounts(engine.KindPi, healthCounts{sessions: 5, at: time.Now(), known: knownFlags{sessions: true}})
	app.storeCounts(engine.KindOMP, healthCounts{at: time.Now()})

	health, err := app.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !health.TotalsPartial {
		t.Fatal("有一格没读到时 totals_partial 应为真")
	}
	if health.TotalSessions != 5 {
		t.Fatalf("合计应是读到的那 5 个（未知的算 0，所以它是偏小的真话），实际 %d", health.TotalSessions)
	}
	// Health 会顺手触发一轮后台探测（把结果落盘）。不等它跑完就退出的话，
	// 那颗 goroutine 会在 t.TempDir() 回收之后才去写文件，清理因此失败。
	app.waitDetect(t)
}

// 探测没给出结论时，原因要能说清「下一步干什么」。
//
// 三种原因的下一步完全不同，混成一句「未安装」会让用户去装一个
// 其实已经装好的引擎，或者反复重试一条根本不可能成的路径。
func TestProbeUnknownReasonDifferentiates(t *testing.T) {
	app := newTestApp(t)

	// 显式路径配了一个不存在的文件 —— 该去改路径，不是去安装。
	app.mu.Lock()
	app.probeReason = map[engine.Kind]string{engine.KindPi: "bad-path"}
	app.mu.Unlock()
	if got := app.ProbeReason(engine.KindPi); got != "bad-path" {
		t.Fatalf("没探到时应报出原因，实际 %q", got)
	}

	// 探到了就没什么要解释的：结论可信。
	app.mu.Lock()
	app.ids[engine.KindPi] = engine.Identity{Kind: engine.KindPi, Exe: "pi.exe", Caps: engine.CapsFor(engine.KindPi)}
	app.mu.Unlock()
	if got := app.ProbeReason(engine.KindPi); got != "" {
		t.Fatalf("探到了就不该给原因，实际 %q", got)
	}
}
