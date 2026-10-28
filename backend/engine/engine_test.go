package engine

// 引擎识别、安装渠道与能力表漂移的测试。
//
// 这三处错了都不会报错，表现是「界面按错的能力表渲染」或「给了一个删不掉的按钮」：
//   - 判种：把 omp 认成 pi，界面按错的能力渲染，控件点了没反应；
//   - 渠道：判错就装错渠道的卸载命令，或把「不确定」当成「能卸」；
//   - 能力表漂移：探测期与运行期不一致，按钮先出现点下去报不支持。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ---------------- 判种 ----------------

// 各引擎帮助首部的真实形状（按各自的渲染逻辑截取）。
const (
	helpSampleOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	helpSamplePi  = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...] [messages...]\n\nCommands:\n"
)

// 判种决策表：每一行都是一种真实存在的引擎输出组合。
func TestMatchKind(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		version string
		help    string
		want    bool
	}{
		{"omp 标准版本号", KindOMP, "omp/18.13.1", helpSampleOMP, true},
		{"omp 改了 bin 名", KindOMP, "custom/2.0.0", helpSampleOMP, true},
		{"omp 改了 bin 名但帮助是 pi", KindOMP, "custom/2.0.0", helpSamplePi, false},
		{"pi 的版本号不是 omp 形状", KindOMP, "pi/1.4.0", helpSamplePi, false},
		{"裸版本号不是 omp", KindOMP, "1.4.0", helpSamplePi, false},

		{"pi 裸版本号", KindPi, "1.4.0", helpSamplePi, true},
		{"pi 带预发布后缀", KindPi, "1.4.0-beta.2", helpSamplePi, true},
		{"omp 遗留分支打裸版本号", KindPi, "1.4.0", helpSampleOMP, false},
		{"pi 不可能是带前缀的版本", KindPi, "omp/18.13.1", helpSampleOMP, false},
		{"帮助跑不出来就不认", KindPi, "1.4.0", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchKindWithHelp(c.kind, c.version, c.help); got != c.want {
				t.Errorf("matchKindWithHelp(%s, %q) = %v，期望 %v", c.kind, c.version, got, c.want)
			}
		})
	}
}

// 单独守「不靠子串 'omp' 判种」这条纪律：
// pi 的帮助里 'omp' 出现十几次（completion、compile 等词），子串匹配必然误判。
// 它在上面的决策表里已经被盖到一半，这里补的是「空帮助」这个边界。
func TestHelpIsOMP(t *testing.T) {
	if helpIsOMP("") {
		t.Error("空帮助不该被判为 omp")
	}
}

// ---------------- 能力表漂移 ----------------

// 能力开关与引擎真实命令表必须一一对得上。
//
// 发出去一条引擎不认识的命令，引擎只回 success:false 或干脆静默丢弃，
// 界面表现为「点了没反应」。所以这里把「哪条命令属于谁」钉死。
func TestCapsMatchCommandTable(t *testing.T) {
	// 队列相关的两条命令：omp 只有逐条移除，pi 只有一次性清空。
	cases := []struct {
		kind Kind
		cmd  string
		want bool
	}{
		{KindOMP, "remove_queued_message", true},
		{KindOMP, "clear_queue", false},
		{KindPi, "remove_queued_message", false},
		{KindPi, "clear_queue", true},
	}
	for _, c := range cases {
		caps := CapsFor(c.kind)
		got := caps.QueueEdit
		if c.cmd == "clear_queue" {
			got = caps.QueueClear
		}
		if got != c.want {
			t.Errorf("%s: 命令 %q 存在与否 = %v，期望 %v", c.kind, c.cmd, got, c.want)
		}
	}
}

// 参数名写错是静默失败：引擎把它当成缺参，回 success:false 或干脆不动，
// 界面上就是「点了没反应」。所以对着引擎的命令表钉死参数名。
func TestRPCParamNames(t *testing.T) {
	cases := []struct {
		cmd    string
		params map[string]any
		want   []string
		ban    []string
	}{
		{"steer_subagent", SteerSubagentParams("a", "m"), []string{"subagentId", "message"}, []string{"id"}},
		{"cancel_subagent", CancelSubagentParams("a"), []string{"subagentId"}, []string{"id"}},
		{"get_subagent_messages", SubagentMessagesParams("a", 7), []string{"subagentId", "fromByte"}, []string{"id", "from"}},
		{"login", LoginParams("anthropic"), []string{"providerId"}, []string{"provider"}},
	}
	for _, c := range cases {
		for _, k := range c.want {
			if _, ok := c.params[k]; !ok {
				t.Errorf("%s 缺参数 %q（实际 %v）", c.cmd, k, c.params)
			}
		}
		for _, k := range c.ban {
			if _, bad := c.params[k]; bad {
				t.Errorf("%s 不该用参数名 %q", c.cmd, k)
			}
		}
	}
}

// ---------------- 安装渠道 ----------------

// 渠道判定是「卸载按钮存不存在」的唯一依据，判错就是两类事故：
// 给了一个删不掉的按钮，或者把一个用户自己装的东西认成我们的。
func TestDetectChannel(t *testing.T) {
	home := `C:\Users\me`
	local := `C:\Users\me\AppData\Local`

	cases := []struct {
		name string
		kind Kind
		exe  string
		want Channel
	}{
		{
			name: "直接命中 node_modules",
			kind: KindPi, exe: `C:\nvm\node_global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`,
			want: ChannelNPM,
		},
		{
			name: "pnpm 的 node_modules",
			kind: KindPi, exe: `C:\Users\me\AppData\Local\pnpm\node_modules\.pnpm\@earendil-works+pi\dist\cli.js`,
			want: ChannelPNPM,
		},
		{
			name: "bun 的全局安装",
			kind: KindPi, exe: `C:\Users\me\.bun\install\global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`,
			want: ChannelBun,
		},
		{
			name: "omp 官方安装器落点",
			kind: KindOMP, exe: `C:\Users\me\AppData\Local\omp\omp.exe`,
			want: ChannelOMPInstaller,
		},
		{
			// 同名目录但不是 LOCALAPPDATA 下那一份：不能认成官方安装器。
			name: "别处的 omp 目录不算官方安装器",
			kind: KindOMP, exe: `D:\tools\omp\omp.exe`,
			want: ChannelUnknown,
		},
		{
			name: "bun 的 bin 目录",
			kind: KindPi, exe: `C:\Users\me\.bun\bin\pi.exe`,
			want: ChannelBun,
		},
		{
			name: "空路径",
			kind: KindOMP, exe: "",
			want: ChannelUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectChannel(tc.kind, tc.exe, home, local); got != tc.want {
				t.Fatalf("DetectChannel(%s, %q) = %q，期望 %q", tc.kind, tc.exe, got, tc.want)
			}
		})
	}
}

// 造一个真的全局安装目录，验证 shim 判定：
// Windows 的全局安装是 <prefix>\pi.cmd + <prefix>\node_modules\<包>，
// 只看路径段会全判成未知——而这恰恰是最常见的一种。
func TestDetectChannelSiblingModules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "pi.cmd")

	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelNPM {
		t.Fatalf("同级 node_modules 的 shim 应判为 npm，得到 %q", got)
	}

	// pnpm 的全局目录里有 .pnpm 存储，靠它区分。
	if err := os.MkdirAll(filepath.Join(root, "node_modules", ".pnpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelPNPM {
		t.Fatalf("带 .pnpm 存储的应判为 pnpm，得到 %q", got)
	}
}

// 「有命令才有按钮」这条不变量：两处判断一旦漂移，
// 界面会出现一个点了报错的按钮——那是最想防的事。
func TestUninstallCommandMatchesChannel(t *testing.T) {
	channels := []Channel{
		ChannelNPM, ChannelPNPM, ChannelBun,
		ChannelOMPInstaller, ChannelPiInstaller, ChannelUnknown,
	}
	for _, kind := range AllKinds() {
		for _, ch := range channels {
			hasCmd := len(UninstallCommand(kind, ch)) > 0
			if hasCmd != CanUninstall(ch) {
				t.Fatalf("%s/%s: CanUninstall=%v 但命令存在=%v", kind, ch, CanUninstall(ch), hasCmd)
			}
		}
	}
}

// 包名拼错是静默失败：命令会失败，但错误信息指向「网络」或「权限」。
func TestPackageNameIsReal(t *testing.T) {
	want := map[Kind]string{
		KindOMP: "@oh-my-pi/pi-coding-agent",
		KindPi:  "@earendil-works/pi-coding-agent",
	}
	for _, kind := range AllKinds() {
		if got := PackageName(kind); got != want[kind] {
			t.Fatalf("%s 的包名 = %q，期望 %q", kind, got, want[kind])
		}
	}
}

// ---------------- 测试替身 ----------------

// fakeDialect 只用于回答能力表；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }
