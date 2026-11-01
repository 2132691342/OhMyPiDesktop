package service

// 引擎进程明细与「按实例停掉」、健康计数的可信度：两者都是「不崩、不报错」那一类错。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// 引擎进程明细与「按实例停掉」都按 (kind, work_dir) 定位；只凭 kind 停会把另一个目录里正在用的那个一起停掉。
func TestEngineRuns(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(fakeDetector(nil, ""))
	defer restore()

	// 只取句柄不拉进程：两个目录各一个实例，两个都没在跑。
	app.Engine.Engine(engine.KindOMP, `C:\work\a`)
	app.Engine.Engine(engine.KindOMP, `C:\work\b`)

	runs := app.engineRuns()
	if len(runs) != 2 {
		t.Fatalf("实例数 = %d，期望 2（同一引擎在两个目录各一个）", len(runs))
	}
	for _, row := range runs {
		if row.EngineKind != string(engine.KindOMP) || row.EngineLabel != engine.KindOMP.Label() {
			t.Errorf("行身份不对：%+v", row)
		}
		if row.WorkDir == "" {
			t.Errorf("缺工作目录就看不出该停哪一个：%+v", row)
		}
		if row.ProcessID != 0 {
			t.Errorf("没拉进程时 pid 应为 0，实际 %d", row.ProcessID)
		}
	}

	t.Run("按实例停不动另一个目录", func(t *testing.T) {
		stopped, err := app.StopEngine(engine.KindOMP, `C:\work\a`)
		if err != nil {
			t.Fatal(err)
		}
		if !stopped {
			t.Fatal("应该停掉了一条")
		}
		if left := app.engineRuns(); len(left) != 1 || left[0].WorkDir != `C:\work\b` {
			t.Fatalf("剩下的实例 = %+v，期望只剩 C:\\work\\b", left)
		}
	})

	t.Run("停一个不存在的实例不算错", func(t *testing.T) {
		stopped, err := app.StopEngine(engine.KindOMP, `C:\work\gone`)
		if err != nil {
			t.Fatalf("不存在的实例不该报错，实际 %v", err)
		}
		if stopped {
			t.Fatal("不存在的实例不该报「停掉了」")
		}
	})

	t.Run("缺引擎种类是入参错", func(t *testing.T) {
		if _, err := app.StopEngine(engine.Kind("nope"), `C:\work\b`); err == nil {
			t.Fatal("未识别的引擎种类应报错")
		}
	})
}

// 计数没读到时界面显示 0，而 0 是断言（「有 0 个会话」）却实际是「没读到」——磁盘上可能躺着几十个会话；
// 这种错不崩、不报错、日志是绿的，用户只会觉得「这个软件数不准」。
func TestHealthCountConfidence(t *testing.T) {
	t.Run("目录不存在是「没读到」不是「0 个」", func(t *testing.T) {
		app := newTestApp(t)
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
	})

	t.Run("known 标记要一路传到响应里", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, ""))
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
	})

	t.Run("合计里有一格没读到时 totals_partial 为真", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, ""))
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
		// Health 顺手触发后台探测写盘；等它退完，否则 t.TempDir() 回收时撞上还在跑的写入。
		app.waitDetect(t)
	})
}
