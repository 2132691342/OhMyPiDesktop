package service

// 健康计数的可信度：计数没读到时，界面显示 0。
//
// 0 是一个断言（「有 0 个会话」），而实际情况是「没读到」——
// 磁盘上可能躺着九十个会话。这种错不会崩、不会报错、日志也是绿的，
// 用户只会觉得「这个软件数不准」，然后不再信它说的任何一个数字。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// ---------------- 健康计数的可信度 ----------------

// 计数没读到时，界面显示 0。
//
// 0 是一个断言（「有 0 个会话」），而实际情况是「没读到」——
// 磁盘上可能躺着九十个会话。这种错不会崩、不会报错、日志也是绿的，
// 用户只会觉得「这个软件数不准」，然后不再信它说的任何一个数字。
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
		// Health 会顺手触发一轮后台探测（把结果落盘）。不等它跑完就退出的话，
		// 那颗 goroutine 会在 t.TempDir() 回收之后才去写文件，清理因此失败。
		app.waitDetect(t)
	})
}
