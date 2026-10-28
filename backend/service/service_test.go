package service

// 用例层的测试：只测「界面表达不了、错了也不报错」的那些判据。
//
// 判据分三类：
//   1. 命令原文与真正执行的命令必须一致（用户会从屏幕上复制它）；
//   2. 版本比较（数字比，不是字符串比）；
//   3. 会话键的拆分（拆错会打开另一个引擎的会话文件，肉眼发现不了）。
//
// 其余（"能不能解一个 map"）不会错，不测。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- 应用管理 ----------------

// 版本比较是「有没有新版本」的唯一判据，判错就是两种坏体验：
// 明明最新还提示升级，或者真有新版却不说。
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
				t.Fatalf("compareVersion(%q, %q) = %d，期望 %d（%s）", tc.a, tc.b, got, tc.want, tc.why)
			}
		})
	}
}

// 界面展示的命令与实际执行的命令必须一致。
//
// 界面把 installCommand 当原文给用户复制；如果它与 actionCommand 拼出的
// 命令不是同一行，用户照着抄就会得到不一样的结果——那是这个功能最坏的一种错。
func TestInstallCommandTextMatchesExecuted(t *testing.T) {
	app := &App{}
	for _, kind := range engine.AllKinds() {
		shown := installCommandText(kind)
		argv, err := app.actionCommand(kind, ActionInstall)
		if err != nil {
			// 这台机器缺包管理器时不算失败：那正是「命令不发出去」的正确行为。
			continue
		}
		executed := strings.Join(argv, " ")
		if argv[0] == "powershell" {
			// 官方安装器用 -Command 传脚本，展示形态带引号，只比较脚本本体。
			if !strings.Contains(executed, "irm https://omp.sh/install.ps1 | iex") {
				t.Fatalf("%s 执行的命令里没有安装脚本本体：%s", kind, executed)
			}
			continue
		}
		if shown != executed {
			t.Fatalf("%s 展示的命令与执行的不一致\n  展示: %s\n  执行: %s", kind, shown, executed)
		}
	}
}

// 「没有卸载按钮」必须永远有一句解释：
// 一个装着的应用没有卸载按钮，用户只会以为漏了。
func TestUninstallBlockerCoversEveryChannel(t *testing.T) {
	channels := []engine.Channel{
		engine.ChannelOMPInstaller, engine.ChannelPiInstaller,
		engine.ChannelUnknown, engine.ChannelNPM, "",
	}
	for _, ch := range channels {
		if msg := uninstallBlocker(ch); msg == "" {
			t.Fatalf("渠道 %q 没有解释文案：界面会出现一个装着的应用却没有卸载按钮，也没说为什么", ch)
		}
	}
}

// 命令不存在就不发出去（AGENTS.md §3）：缺包管理器时要直接说缺什么，
// 而不是让用户看到一条 "exec: npm: executable file not found"。
func TestInstallUnavailableSaysWhy(t *testing.T) {
	// 清空 PATH 造出「什么都没有」的环境。
	t.Setenv("PATH", t.TempDir())

	for _, kind := range engine.AllKinds() {
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
}

// ---------------- 会话键 ----------------

// 会话键是「引擎 + 路径」拼出来的一个字符串。它一旦拆错，
// 「启动时恢复上次会话」就会打开另一个引擎的会话文件——
// 这类错误在界面上表现为「点开之后历史对不上」，不可能靠肉眼发现。
func TestSplitSessionKey(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantKind string
		wantPath string
	}{
		{
			name: "带引擎前缀", raw: "omp\x00C:/Users/x/.omp/agent/sessions/s1.jsonl",
			wantKind: "omp", wantPath: "C:/Users/x/.omp/agent/sessions/s1.jsonl",
		},
		{
			name: "pi", raw: "pi\x00C:/Users/x/.pi/agent/sessions/s2.jsonl",
			wantKind: "pi", wantPath: "C:/Users/x/.pi/agent/sessions/s2.jsonl",
		},
		{name: "无分隔符", raw: "C:/only/path.jsonl", wantPath: "C:/only/path.jsonl"},
		{name: "空", raw: "", wantPath: ""},
		{
			// Windows 路径里的反斜杠与冒号不该被误当成键分隔符。
			name: "Windows 路径", raw: "pi\x00" + `C:\Users\x\.pi\agent\sessions\s.jsonl`,
			wantKind: "pi", wantPath: `C:\Users\x\.pi\agent\sessions\s.jsonl`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, path := splitSessionKey(c.raw)
			if string(kind) != c.wantKind {
				t.Fatalf("kind = %q, want %q", kind, c.wantKind)
			}
			if path != c.wantPath {
				t.Fatalf("path = %q, want %q", path, c.wantPath)
			}
		})
	}
}

// ---------------- 配置原文 ----------------

// 读原文只接受清单里的路径：否则就成了任意文件读取接口。
// 凭据文件也不在清单里——界面上不该有「一键看密钥」的入口。
func TestConfigCandidates(t *testing.T) {
	omp := configCandidates(engine.KindOMP, env.Layout{
		Models:   `C:\u\.omp\agent\models.yml`,
		Settings: `C:\u\.omp\agent\config.yml`,
		Agent:    `C:\u\.omp\agent`,
	})
	if len(omp) != 3 {
		t.Fatalf("omp 候选数 = %d, want 3", len(omp))
	}
	if filepathBase(omp[0].path) != "models.yml" {
		t.Fatalf("omp 主配置应当是 models.yml，得到 %s", omp[0].path)
	}

	pi := configCandidates(engine.KindPi, env.Layout{
		Models:   `C:\u\.pi\agent\models.json`,
		Settings: `C:\u\.pi\agent\settings.json`,
		Agent:    `C:\u\.pi\agent`,
		AuthFile: `C:\u\.pi\agent\auth.json`,
	})
	if filepathBase(pi[0].path) != "models.json" {
		t.Fatalf("pi 主配置应当是 models.json，得到 %s", pi[0].path)
	}
	for _, cand := range pi {
		if filepathBase(cand.path) == "auth.json" {
			t.Fatal("凭据文件不该出现在可查看清单里")
		}
	}
}

// 大小写不同的同一路径必须被接受（Windows 大小写不敏感），
// 清单外的路径必须被拒绝。
func TestInCandidates(t *testing.T) {
	allowed := []configFile{
		{path: `C:\u\.omp\agent\models.yml`, label: "models.yml"},
		{path: `C:\u\.omp\agent\config.yml`, label: "config.yml"},
	}
	if !inCandidates(`c:\U\.omp\agent\MODELS.yml`, allowed) {
		t.Fatal("大小写不同的同一路径应当被接受")
	}
	if inCandidates(`C:\u\.omp\agent\auth.json`, allowed) {
		t.Fatal("清单外的路径必须被拒绝")
	}
	if inCandidates(`C:\Windows\win.ini`, allowed) {
		t.Fatal("工作区外的路径必须被拒绝")
	}
}

// 读原文的两条边界：文件不存在时不算错误（干净机器上首次打开设置页
// 不该先红一片）；超过上限要被截断（一个几十 MB 的文件会把界面卡死）。
func TestReadConfigText(t *testing.T) {
	dir := t.TempDir()

	vo, err := readConfigText(filepath.Join(dir, "nope.yml"))
	if err != nil {
		t.Fatalf("不存在的文件不该报错：%v", err)
	}
	if vo.Found || vo.Text != "" {
		t.Fatalf("不存在时应 Found=false 且 Text 为空，得到 %+v", vo)
	}

	big := make([]byte, configTextLimit+16)
	for i := range big {
		big[i] = 'x'
	}
	path := filepath.Join(dir, "big.json")
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	vo, err = readConfigText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !vo.Truncated {
		t.Fatal("超过上限应当被标记为截断")
	}
	if len(vo.Text) != configTextLimit {
		t.Fatalf("截断长度 = %d，期望 %d", len(vo.Text), configTextLimit)
	}
}

// ---------------- 测试替身 ----------------

// filepathBase 取路径的文件名部分（避免为一处调用多引一个包）。
func filepathBase(path string) string {
	idx := strings.LastIndexAny(path, `\/`)
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
