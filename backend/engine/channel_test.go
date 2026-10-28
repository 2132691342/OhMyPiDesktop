package engine

// 渠道判定是「卸载按钮存不存在」的唯一依据，判错就是两类事故：
// 给了一个删不掉的按钮，或者把一个用户自己装的东西认成我们的。
//
// 测的是判据本身（纯函数），不测文件系统之外的任何东西：
// 这些用例的目录布局都取自真实机器上见过的形状。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectChannel(t *testing.T) {
	home := `C:\Users\me`
	local := `C:\Users\me\AppData\Local`

	cases := []struct {
		name   string
		kind   Kind
		exe    string
		want   Channel
		reason string
	}{
		{
			// npm 的 Windows 全局 shim：<prefix>\pi.cmd，包在 <prefix>\node_modules。
			// 这条最容易被「只看 node_modules 路径段」的实现漏掉。
			name: "npm 全局 shim 与包目录同级",
			kind: KindPi, exe: `C:\nvm\node_global\pi.cmd`,
			want: ChannelUnknown, // 该目录在测试环境里没有 node_modules，所以判不出
		},
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
			got := DetectChannel(tc.kind, tc.exe, home, local)
			if got != tc.want {
				t.Fatalf("DetectChannel(%s, %q) = %q, 想要 %q", tc.kind, tc.exe, got, tc.want)
			}
		})
	}
}

// TestDetectChannelSiblingModules 造一个真的全局安装目录，验证 shim 判定。
func TestDetectChannelSiblingModules(t *testing.T) {
	root := t.TempDir()
	// 模拟 npm 的 Windows 全局布局：prefix\pi.cmd + prefix\node_modules\<包>。
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

// TestCanUninstallMatchesCommand 是「有命令才有按钮」这条不变量的守卫。
//
// 两处判断一旦漂移，界面会出现一个点了报错的按钮——那是本文件最想防的事。
func TestCanUninstallMatchesCommand(t *testing.T) {
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

// TestPackageNameIsReal 防的是包名拼错。
//
// @oh-my-pi 与 @ohmy-pi 差一个连字符，而那种错只会在用户点安装时暴露。
func TestPackageNameIsReal(t *testing.T) {
	want := map[Kind]string{
		KindOMP: "@oh-my-pi/pi-coding-agent",
		KindPi:  "@earendil-works/pi-coding-agent",
	}
	for _, kind := range AllKinds() {
		if got := PackageName(kind); got != want[kind] {
			t.Fatalf("%s 的包名 = %q，想要 %q", kind, got, want[kind])
		}
	}
}
