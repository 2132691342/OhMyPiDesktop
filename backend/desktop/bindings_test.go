package desktop

// 绑定契约回归门：前端生成物的绑定路径与每个绑定 id 都必须与后端运行时一致。
//
// 为什么：Wails v3 的绑定 id 由 FNV32a(导入路径.类型.方法) 算出。挪包或改绑定面会让
// 前端那份生成物里的 id 变成死数字——编译、类型检查、单元测试全绿，只有真在 WebView
// 里点一下才发现「按钮没反应」。所以这里从「后端现在的样子」推出路径与 id，不写死。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// wailsImportPattern 抓前端绑定层里的 import：
//
//	import * as App from '@wails/OhMyPiDesktop/backend/desktop/app'
//
// 只认 `@wails/` 前缀——那是 vite 里 aliases 指向 frontend/bindings 的入口。
var wailsImportPattern = regexp.MustCompile(`from '@wails/([^']+)/app'`)

// byIDPattern 抓生成物里的 `$Call.ByID(<数字>)`。
var byIDPattern = regexp.MustCompile(`\$Call\.ByID\((\d+)`)

// exportFnPattern 抓生成物里的 `export function <名字>(`。
var exportFnPattern = regexp.MustCompile(`export function (\w+)\(`)

// fnv32a 是 Wails 用的那个 FNV-1a 32 位哈希（internal/hash/fnv.go）。
//
// 这里重写一遍而不是 import 内部包：internal 包对外不可用，
// 而这个哈希的算法本身是稳定的一行，照抄比绕路更省事。
func fnv32a(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// repoRoot 从测试所在目录往上找仓库根（go.mod 在那里）。
//
// 测试跑在包目录下，而绑定生成物在仓库根的 frontend/ 下。
// 逐级上溯比写死相对路径稳：go test 可能改变工作目录。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到仓库根（没有 go.mod）")
		}
		dir = parent
	}
}

// servicePkgPath 是这个包在模块内的相对路径。
//
// 它跟着包的位置走：desktop 在仓库根时是 "desktop"，
// 挪到 backend/ 下就是 "backend/desktop"。
// 挪包时这道门会失败——那正是它该失败的时候。
const servicePkgPath = "backend/desktop"

// frontendBindingImport 读前端绑定层，算出它 import 的那个绑定目录。
//
// 返回值形如 "OhMyPiDesktop/backend/desktop"——它同时是
//  1. 生成物在 frontend/bindings/ 下的相对目录；
//  2. 后端注册绑定用的导入路径去掉模块名之后的部分。
//
// 前端 import 指向哪里，就是「界面以为绑定在哪」。
// 后端包在哪，就是「绑定真的在哪」。两者对不上就是漂移。
func frontendBindingImport(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	path := filepath.Join(root, "frontend", "src", "api", "wails.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到前端绑定层 %s：%v", path, err)
	}
	m := wailsImportPattern.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s 里没有 `@wails/.../app` 的 import：前端拿不到绑定", path)
	}
	return string(m[1])
}

// readBindings 读前端生成物，抽出「方法名 → 绑定 id」；解析失败报错而非返回空表（空表会假绿）。
func readBindings(t *testing.T, rel string) map[string]uint32 {
	t.Helper()
	path := filepath.Join(repoRoot(t), "frontend", "bindings", filepath.FromSlash(rel), "app.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到前端绑定 %s：%v（生成命令是 `wails3 generate bindings -clean=true -ts -i`）", path, err)
	}
	out := map[string]uint32{}
	current := ""
	for _, line := range splitLines(string(raw)) {
		if m := exportFnPattern.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := byIDPattern.FindStringSubmatch(line)
		if m == nil || current == "" {
			continue
		}
		id, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			t.Fatalf("绑定 id 不是数字：%s", line)
		}
		out[current] = uint32(id)
		current = ""
	}
	if len(out) == 0 {
		t.Fatalf("%s 里一个绑定方法都没有：生成物格式变了，门禁得跟着改", path)
	}
	return out
}

// splitLines 按行切开。
func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// 前端 import 的绑定路径必须等于这个包现在生成出来的路径（挪包后目录跟着挪）。
func TestBindingPathMatchesPackage(t *testing.T) {
	got := frontendBindingImport(t)
	// 前端 import 带模块名（@wails/<模块>/<包路径>/app），这里只比包路径那一段。
	want := servicePkgPath
	if stripModule(got) != want {
		t.Fatalf("前端 import 的绑定路径是 %q，后端这个包生成出来的是 %q。\n"+
			"重新生成绑定并让前端 import 指到新位置：\n"+
			"  wails3 task common:generate:bindings\n"+
			"  然后把 frontend/src/api/wails.ts 的 import 改成 '@wails/%s/app'",
			got, want, want)
	}
}

// stripModule 去掉路径里的模块名那一段。
//
// 前端 import 的路径是 <模块名>/<包路径>（vite 的别名把 @wails 指向 bindings/，
// 所以 @wails/OhMyPiDesktop/backend/desktop 对应 bindings/OhMyPiDesktop/backend/desktop）。
// 比对包路径时要把模块名去掉，否则两边的基准不一样。
func stripModule(p string) string {
	if i := strings.Index(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// 前端生成物里的绑定 id 必须等于后端运行时注册的值（同一条 FNV32a 公式）。
func TestBindingIDsMatchRuntime(t *testing.T) {
	rel := frontendBindingImport(t)
	bindings := readBindings(t, rel)

	// 后端真实暴露给前端的绑定方法。少列一个不会漏，
	// 多列一个会失败——所以这份清单只增不减，删方法时同步删这里。
	methods := []string{
		"ApplyCloseDecision",
		"Endpoint",
		"OpenExternal",
		"PickFiles",
		"PickFolder",
		"Quit",
		"Ready",
		"WindowClose",
		"WindowMinimize",
		"WindowToggleMaximize",
	}

	// 运行时注册用的导入路径：模块名 + 包路径。
	// 前端 import 里的目录名就是包路径（去掉模块名那一段）。
	const moduleName = "OhMyPiDesktop"
	importPath := moduleName + "/" + servicePkgPath

	for _, name := range methods {
		got, ok := bindings[name]
		if !ok {
			t.Errorf("前端绑定里没有方法 %s：重新跑 `wails3 generate bindings -ts -i`", name)
			continue
		}
		want := fnv32a(fmt.Sprintf("%s.%s.%s", importPath, "App", name))
		if got != want {
			t.Errorf("方法 %s 的绑定 id 对不上：前端是 %d，后端注册的是 %d。"+
				"前端调用会静默失败（运行时查不到这个方法）。"+
				"重新生成绑定并提交：wails3 task common:generate:bindings",
				name, got, want)
		}
	}
}
