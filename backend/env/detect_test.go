package env

// 官方安装器落点的探测：这一条是「装了却说没装」这类错的唯一防线。
//
// 两个引擎的官方安装器都不往 PATH 里写东西。只看 PATH 的应用会在一台
// 明明装好的机器上给出「未安装」+ 安装命令，而用户看到的是
// 「这软件连装没装都看不出来」。这一组用例钉住「落点推得对不对」。

import (
	"os"
	"path/filepath"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// omp 的落点顺序：PI_INSTALL_DIR → %LOCALAPPDATA%\omp → ~\.omp\bin。
func TestInstallCandidatesOMP(t *testing.T) {
	local := t.TempDir()
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("PI_INSTALL_DIR", "")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	got := InstallCandidates(engine.KindOMP)
	want := []string{
		filepath.Join(local, "omp", "omp.exe"),
		filepath.Join(home, ".omp", "bin", "omp.exe"),
	}
	if len(got) != len(want) {
		t.Fatalf("候选数 %d，期望 %d：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个候选 %q，期望 %q", i, got[i], want[i])
		}
	}

	// PI_INSTALL_DIR 是 omp 安装器自己的覆盖点，必须排在最前。
	custom := filepath.Join(t.TempDir(), "omp")
	t.Setenv("PI_INSTALL_DIR", custom)
	if first := InstallCandidates(engine.KindOMP)[0]; first != filepath.Join(custom, "omp.exe") {
		t.Errorf("PI_INSTALL_DIR 应排第一，得到 %q", first)
	}
}

// pi 的托管布局：current-version 指向哪个 release 就要探哪个，
// 指不到时回退到目录里版本号最大的那个。
func TestInstallCandidatesPiManaged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_MANAGED_INSTALL_ROOT", root)
	t.Setenv("LOCALAPPDATA", t.TempDir())

	for _, version := range []string{"1.3.0", "1.4.0"} {
		if err := os.MkdirAll(filepath.Join(root, "releases", version, "node_modules", ".bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 有 current-version：它说的那个排第一，回退项排第二。
	if err := os.WriteFile(filepath.Join(root, "current-version"), []byte("1.3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := InstallCandidates(engine.KindPi)
	if len(got) < 2 {
		t.Fatalf("应给出 current-version 与回退两项，实际 %v", got)
	}
	if want := filepath.Join(root, "releases", "1.3.0", "node_modules", ".bin", "pi.cmd"); got[0] != want {
		t.Errorf("current-version 应排第一：%q，期望 %q", got[0], want)
	}
	if want := filepath.Join(root, "releases", "1.4.0", "node_modules", ".bin", "pi.cmd"); got[1] != want {
		t.Errorf("回退项应是最大版本：%q，期望 %q", got[1], want)
	}
}
