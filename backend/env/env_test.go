package env

// env 包只测会悄无声息出错的地方：官方安装器的落点推得对不对、
// 插件 source 字符串切得对不对。
//
// 这两条各有具体的静默后果：
//   - 落点推错 → 一台明明装好的机器上显示「未安装」+ 安装命令，
//     用户看到的是「这软件连装没装都看不出来」；
//   - source 切错 → 卸载时匹配不到包，插件删不掉。

import (
	"os"
	"path/filepath"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// ---------------- 官方安装器的落点 ----------------

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

// ---------------- 插件登记清单 ----------------

// 插件 source 字符串形状有好几种，切错了只会在卸载时匹配不到。
func TestParsePackageSource(t *testing.T) {
	cases := []struct {
		source  string
		name    string
		version string
	}{
		{"npm:pi-web-access@0.45.0", "pi-web-access", "0.45.0"},
		{"npm:@juicesharp/rpiv-todo@2.15.0", "@juicesharp/rpiv-todo", "2.15.0"},
		{"npm:@ff-labs/pi-fff", "@ff-labs/pi-fff", ""},
		{"pi-context-view", "pi-context-view", ""},
		{"pi-context-view@0.8.0", "pi-context-view", "0.8.0"},
		// git 源：剥掉协议前缀，URL 其余部分（含 https://）必须完整保留
		{"git:https://github.com/a/b.git", "https://github.com/a/b.git", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got := ParsePackageSource(tc.source)
			if got.Name != tc.name || got.Version != tc.version {
				t.Fatalf("%q: got (%q,%q) want (%q,%q)", tc.source, got.Name, got.Version, tc.name, tc.version)
			}
		})
	}

	// Windows 盘符不能被当协议前缀切掉，否则 C:\dev\plugin 会被切成 dev\plugin 而找不到。
	got := ParsePackageSource(`C:\dev\my-plugin`)
	if got.Name != `C:\dev\my-plugin` {
		t.Errorf("本地路径被切坏了: %q", got.Name)
	}
}

// 插件清单：两个引擎读不同的登记文件。
func TestReadPluginEntries(t *testing.T) {
	dir := t.TempDir()

	// pi：settings.json 的 packages 数组
	piSettings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(piSettings, []byte(`{
  "packages": [
    "npm:pi-web-access@0.45.0",
    "npm:@juicesharp/rpiv-todo@2.15.0",
    ""
  ],
  "lastChangelogVersion": "1.4.0"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, path, err := ReadPluginEntries("pi", Layout{Settings: piSettings})
	if err != nil {
		t.Fatalf("pi Read: %v", err)
	}
	if path != piSettings {
		t.Errorf("registry path: got %q want %q", path, piSettings)
	}
	if len(entries) != 2 {
		t.Fatalf("应读到 2 条（空行跳过），实际 %d", len(entries))
	}
	if entries[0].Name != "pi-web-access" || entries[0].Source != "npm:pi-web-access@0.45.0" {
		t.Errorf("第一条: %+v", entries[0])
	}
	if entries[1].Name != "@juicesharp/rpiv-todo" {
		t.Errorf("第二条: %+v", entries[1])
	}

	// omp：plugins/package.json 的 dependencies 映射；source 用裸名
	// （uninstall 认的是包名，拼版本范围会匹配不到）。
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","dependencies":{"a":"^1.0.0","@s/b":"^2.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ompEntries, _, err := ReadPluginEntries("omp", Layout{Plugins: dir})
	if err != nil {
		t.Fatalf("omp Read: %v", err)
	}
	names := map[string]bool{}
	for _, e := range ompEntries {
		names[e.Name] = true
		if e.Source != e.Name {
			t.Errorf("omp source 应为裸名: %+v", e)
		}
	}
	if !names["a"] || !names["@s/b"] {
		t.Errorf("omp 条目: %v", names)
	}

	// 文件不存在不是错误：干净机器上那份文件根本不存在，
	// 「没装插件」是正常状态。
	missing, _, err := ReadPluginEntries("pi", Layout{Settings: filepath.Join(dir, "nope.json")})
	if err != nil {
		t.Fatalf("文件不存在不该报错: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("应为空，实际 %d", len(missing))
	}
}

// 插件详情：按名字去 node_modules 里找包目录。
func TestReadPluginDetail(t *testing.T) {
	dir := t.TempDir()
	modules := filepath.Join(dir, "node_modules")
	pkgDir := filepath.Join(modules, "pi-web-access")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{
  "name": "pi-web-access",
  "version": "0.45.0",
  "description": "Web search for Pi",
  "pi": { "extensions": ["./dist"], "skills": ["./skills"] }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	detail := ReadPluginDetail(modules, PluginEntry{Name: "pi-web-access"})
	if detail.Version != "0.45.0" {
		t.Errorf("version: %q", detail.Version)
	}
	if detail.Desc != "Web search for Pi" {
		t.Errorf("desc: %q", detail.Desc)
	}
	if detail.Path != pkgDir {
		t.Errorf("path: %q want %q", detail.Path, pkgDir)
	}
	if len(detail.Resources) != 2 || detail.Resources[0] != "extensions" {
		t.Errorf("resources: %v", detail.Resources)
	}

	// 目录不在：清单里登记了但包被删掉，返回空而不是报错
	missing := ReadPluginDetail(modules, PluginEntry{Name: "not-there"})
	if missing.Path != "" {
		t.Errorf("不存在的包不该给路径: %+v", missing)
	}

	// scope 包在盘上是 node_modules/@scope/pkg，名字要按斜杠拼路径。
	scopedDir := filepath.Join(modules, "@juicesharp", "rpiv-todo")
	if err := os.MkdirAll(scopedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopedDir, "package.json"), []byte(`{"name":"@juicesharp/rpiv-todo","version":"2.15.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	scoped := ReadPluginDetail(modules, PluginEntry{Name: "@juicesharp/rpiv-todo"})
	if scoped.Path != scopedDir {
		t.Errorf("scoped path: %q want %q", scoped.Path, scopedDir)
	}
	if scoped.Version != "2.15.0" {
		t.Errorf("scoped version: %q", scoped.Version)
	}
}
