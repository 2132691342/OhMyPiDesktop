package engine

// 判种、渠道判定与能力表：版本号形状、安装器落点、同级 node_modules 识别、Caps 与命令表一致。

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------- 判种 / 渠道 / 能力 ----------------

func TestMatchKind(t *testing.T) {
	const helpOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	const helpPi = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...]\n"
	cases := []struct {
		name, kind, version, help string
		want                      bool
	}{
		{"omp 标准版本号", string(KindOMP), "omp/18.13.1", helpOMP, true},
		{"改了 bin 名但帮助是 pi", string(KindOMP), "custom/2.0.0", helpPi, false},
		{"pi 的版本号不是 omp 形状", string(KindOMP), "pi/1.4.0", helpPi, false},
		{"pi 裸版本号", string(KindPi), "1.4.0", helpPi, true},
		{"pi 带预发布后缀", string(KindPi), "1.4.0-beta.2", helpPi, true},
		{"omp 裸版本号不算 pi", string(KindPi), "1.4.0", helpOMP, false},
		{"pi 不接受带前缀版本", string(KindPi), "omp/18.13.1", helpOMP, false},
		{"帮助跑不出来不认", string(KindPi), "1.4.0", "", false},
	}
	for _, c := range cases {
		if got := matchKindWithHelp(Kind(c.kind), c.version, c.help); got != c.want {
			t.Errorf("%s: matchKindWithHelp = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestDetectChannel(t *testing.T) {
	home := `C:\Users\me`
	local := `C:\Users\me\AppData\Local`
	cases := []struct {
		name, kind, exe string
		want            Channel
	}{
		{"npm 全局", string(KindPi), `C:\nvm\node_global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelNPM},
		{"pnpm 全局", string(KindPi), `C:\Users\me\AppData\Local\pnpm\node_modules\.pnpm\@earendil-works+pi\dist\cli.js`, ChannelPNPM},
		{"bun 全局", string(KindPi), `C:\Users\me\.bun\install\global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelBun},
		{"omp 官方安装器", string(KindOMP), `C:\Users\me\AppData\Local\omp\omp.exe`, ChannelOMPInstaller},
		{"别处的 omp 不是安装器", string(KindOMP), `D:\tools\omp\omp.exe`, ChannelUnknown},
		{"空路径", string(KindOMP), "", ChannelUnknown},
	}
	for _, c := range cases {
		if got := DetectChannel(Kind(c.kind), c.exe, home, local); got != c.want {
			t.Errorf("%s: DetectChannel = %q，期望 %q", c.name, got, c.want)
		}
	}
}

// 同级 node_modules 的 shim 只看路径段会判成未知，必须靠同级目录识别。
func TestDetectChannelSiblingModules(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent"), 0o755)
	exe := filepath.Join(root, "pi.cmd")
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelNPM {
		t.Fatalf("同级 node_modules 的 shim 应判为 npm，得到 %q", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "node_modules", ".pnpm"), 0o755)
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelPNPM {
		t.Fatalf("带 .pnpm 存储的应判为 pnpm，得到 %q", got)
	}
}

// Caps 是按钮是否出现的唯一依据，钉死「哪条命令属于谁」。
func TestCapsMatchCommandTable(t *testing.T) {
	cases := []struct {
		kind, cmd string
		want      bool
	}{
		{string(KindOMP), "remove_queued_message", true},
		{string(KindOMP), "clear_queue", false},
		{string(KindPi), "remove_queued_message", false},
		{string(KindPi), "clear_queue", true},
	}
	for _, c := range cases {
		caps := CapsFor(Kind(c.kind))
		got := caps.QueueEdit
		if c.cmd == "clear_queue" {
			got = caps.QueueClear
		}
		if got != c.want {
			t.Errorf("%s %s 存在性 = %v，期望 %v", c.kind, c.cmd, got, c.want)
		}
	}
}

func TestUninstallCommandMatchesChannel(t *testing.T) {
	channels := []Channel{ChannelNPM, ChannelPNPM, ChannelBun, ChannelOMPInstaller, ChannelPiInstaller, ChannelUnknown}
	for _, kind := range AllKinds() {
		for _, ch := range channels {
			hasCmd := len(UninstallCommand(kind, ch)) > 0
			if hasCmd != CanUninstall(ch) {
				t.Fatalf("%s/%s: CanUninstall=%v 但命令存在=%v", kind, ch, CanUninstall(ch), hasCmd)
			}
		}
	}
}
