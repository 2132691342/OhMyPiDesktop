package env

import (
	"os"
	"path/filepath"
	"testing"
)

// 插件 source 字符串形状有好几种，切错了只会在卸载时匹配不到——按表驱动全测一遍。

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
		got := ParsePackageSource(tc.source)
		if got.Name != tc.name || got.Version != tc.version {
			t.Errorf("%q: got (%q,%q) want (%q,%q)", tc.source, got.Name, got.Version, tc.name, tc.version)
		}
	}
}

// Windows 盘符不能被当协议前缀切掉，否则 C:\dev\plugin 会被切成 dev\plugin 而找不到。
func TestParsePackageSourceWindowsPath(t *testing.T) {
	got := ParsePackageSource(`C:\dev\my-plugin`)
	if got.Name != `C:\dev\my-plugin` {
		t.Errorf("本地路径被切坏了: %q", got.Name)
	}
}

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
}

// 文件不存在不是错误：干净机器上那份文件根本不存在，
// 「没装插件」是正常状态。
func TestReadPluginEntriesMissing(t *testing.T) {
	dir := t.TempDir()
	entries, _, err := ReadPluginEntries("pi", Layout{Settings: filepath.Join(dir, "nope.json")})
	if err != nil {
		t.Fatalf("文件不存在不该报错: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("应为空，实际 %d", len(entries))
	}
}

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
}

// scope 包在盘上是 node_modules/@scope/pkg，名字要按斜杠拼路径。
func TestReadPluginDetailScoped(t *testing.T) {
	dir := t.TempDir()
	modules := filepath.Join(dir, "node_modules")
	pkgDir := filepath.Join(modules, "@juicesharp", "rpiv-todo")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@juicesharp/rpiv-todo","version":"2.15.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	detail := ReadPluginDetail(modules, PluginEntry{Name: "@juicesharp/rpiv-todo"})
	if detail.Path != pkgDir {
		t.Errorf("scoped path: %q want %q", detail.Path, pkgDir)
	}
	if detail.Version != "2.15.0" {
		t.Errorf("scoped version: %q", detail.Version)
	}
}
