package service

// 版本比较是「有没有新版本」的唯一判据，判错就是两种坏体验：
// 明明最新还提示升级，或者真有新版却不说。所以它单独测。
//
// 只测会静默错的形状：前缀（omp 的版本串带 `omp/`）、预发布后缀、
// 位数不一样（1.4 与 1.4.0）、位数对齐（1.10 与 1.9 不能按字符串比）。

import (
	"os"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		why  string
	}{
		{"1.4.0", "1.4.0", 0, "相同"},
		{"1.4.0", "1.4.1", -1, "补丁号落后"},
		{"1.4.1", "1.4.0", 1, "补丁号领先"},
		{"omp/18.13.2", "18.13.2", 0, "omp 的版本串带前缀"},
		{"omp/18.13.2", "18.14.0", -1, "带前缀也要能比"},
		{"1.10.0", "1.9.0", 1, "按数字比，不按字符串比"},
		{"1.4", "1.4.0", 0, "少一段按 0 补齐"},
		{"2.0.0-beta.1", "2.0.0", 0, "预发布后缀一律当同版本"},
		{"1.4.0", "1.4.0+build7", 0, "构建元数据不影响比较"},
	}
	for _, tc := range cases {
		t.Run(tc.a+"__"+tc.b, func(t *testing.T) {
			if got := compareVersion(tc.a, tc.b); got != tc.want {
				t.Fatalf("compareVersion(%q, %q) = %d, 想要 %d（%s）", tc.a, tc.b, got, tc.want, tc.why)
			}
		})
	}
}

// TestVersionParts 守的是解析本身：拿不到数字段时不能返回一堆 0，
// 否则 "abc" 会被判成等于 "0.0.0"，界面显示「已是最新」。
func TestVersionParts(t *testing.T) {
	cases := map[string]int{
		"1.4.0":        3,
		"omp/18.13.2":  3,
		"1.4":          2,
		"2.0.0-beta.1": 3,
		"":             0,
		"abc":          0,
	}
	for in, wantLen := range cases {
		if got := versionParts(in); len(got) != wantLen {
			t.Fatalf("versionParts(%q) = %v（%d 段），想要 %d 段", in, got, len(got), wantLen)
		}
	}
}

// TestInstallCommandTextMatchesExecuted 是「显示的命令 = 执行的命令」这条不变量的守卫。
//
// 界面把 installCommand 当原文展示给用户复制；如果它与 actionCommand 拼出的
// 命令不是同一行，用户照着抄就会得到不一样的结果——那是这个功能最坏的一种错。
func TestInstallCommandTextMatchesExecuted(t *testing.T) {
	app := &App{}
	for _, kind := range allKindsForTest() {
		shown := installCommandText(kind)
		argv, err := app.actionCommand(kind, ActionInstall)
		if err != nil {
			// 这台机器缺包管理器时不算失败：那正是「命令不发出去」的正确行为。
			continue
		}
		executed := joinForTest(argv)
		if "powershell" == argv[0] {
			// 官方安装器用 -Command 传脚本，展示形态带引号，只比较脚本本体。
			if !containsForTest(executed, "irm https://omp.sh/install.ps1 | iex") {
				t.Fatalf("%s 执行的命令里没有安装脚本本体：%s", kind, executed)
			}
			continue
		}
		if shown != executed {
			t.Fatalf("%s 展示的命令与执行的不一致\n  展示: %s\n  执行: %s", kind, shown, executed)
		}
	}
}

// TestUninstallBlockerCoversEveryChannel 保证「没有卸载按钮」永远有一句解释。
func TestUninstallBlockerCoversEveryChannel(t *testing.T) {
	channels := []string{"omp-installer", "pi-installer", "unknown", "", "别的"}
	for _, ch := range channels {
		if msg := uninstallBlocker(channelForTest(ch)); msg == "" {
			t.Fatalf("渠道 %q 没有解释文案：界面会出现一个装着的应用却没有卸载按钮，也没说为什么", ch)
		}
	}
}

// ---------------- 测试用的最小适配 ----------------
// 这几行只是把包内的私有函数摆到测试能看见的位置，
// 不为了让测试好写而改变生产代码的形状。

func allKindsForTest() []engine.Kind { return engine.AllKinds() }

func channelForTest(v string) engine.Channel { return engine.Channel(v) }

func joinForTest(argv []string) string { return strings.Join(argv, " ") }

func containsForTest(s, sub string) bool { return strings.Contains(s, sub) }

// TestInstallUnavailableSaysWhy 守的是「命令不存在就不发出去」这条纪律
// （AGENTS.md §3）：缺包管理器时要直接说缺什么 + 给替代命令，
// 而不是让用户看到一条 "exec: npm: executable file not found"。
func TestInstallUnavailableSaysWhy(t *testing.T) {
	// 清空 PATH 造出「什么都没有」的环境；测完恢复。
	old := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())

	for _, kind := range allKindsForTest() {
		err := installRunnable(kind)
		if err == nil {
			continue // 机器上用绝对路径也能找到（例如系统目录），跳过
		}
		ae := pkg.As(err)
		if ae.Code != pkg.CodeInstallUnavailable {
			t.Fatalf("%s 的错误码 = %s，想要 %s", kind, ae.Code, pkg.CodeInstallUnavailable)
		}
		// 消息要能照着做：要么说装什么，要么给一条可复制的命令。
		if !strings.Contains(ae.Message, kind.Label()) && !strings.Contains(ae.Message, "npm") {
			t.Errorf("%s 的提示既没点名引擎也没提包管理器: %q", kind, ae.Message)
		}
	}
	_ = old
}
