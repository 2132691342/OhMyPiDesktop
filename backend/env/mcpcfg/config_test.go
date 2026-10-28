package mcpcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sameJSON 语义比较两个 JSON 片段。
//
// 不能比字节：写回时 MarshalIndent 会重排缩进，未知字段的内容不变但空白变了。
// 关心的是「键与值还在」，不是「空白没动」。
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return jsonEqual(x, y)
}

func jsonEqual(x, y any) bool {
	switch xv := x.(type) {
	case map[string]any:
		yv, ok := y.(map[string]any)
		if !ok || len(xv) != len(yv) {
			return false
		}
		for k, v := range xv {
			if !jsonEqual(v, yv[k]) {
				return false
			}
		}
		return true
	case []any:
		yv, ok := y.([]any)
		if !ok || len(xv) != len(yv) {
			return false
		}
		for i := range xv {
			if !jsonEqual(xv[i], yv[i]) {
				return false
			}
		}
		return true
	default:
		return x == y
	}
}

// 一个包一份测试，同一主题合成一条表驱动：这里的主题是
// 「读写 mcp.json 时不抹掉别人的键」——写错了不会报错，只会在下次引擎
// 启动时静默丢配置，所以必须测。

func TestReadWritePreservesUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := `{
  "$schema": "https://example.com/schema.json",
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem"],
      "headers": {"X-Trace": "1"},
      "auth": {"type": "oauth", "credentialId": "abc"},
      "timeout": 120000
    },
    "docs": {
      "type": "http",
      "url": "https://example.com/mcp",
      "enabled": false
    }
  },
  "disabledServers": ["docs"],
  "somethingFuture": {"nested": true}
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	verbatim := []struct {
		name string
		got  any
		want any
	}{
		{"服务器数量", len(file.Servers), 2},
		{"停用名单读出", len(file.Disabled), 1},
		{"stdio 推断", file.Servers["filesystem"].Transport(), "stdio"},
		{"显式 type 优先", file.Servers["docs"].Transport(), "http"},
	}
	for _, tc := range verbatim {
		if tc.got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, tc.got, tc.want)
		}
	}

	preserved := []struct {
		name string
		got  string
		want string
	}{
		{"未知顶层键", string(file.Rest["somethingFuture"]), `{"nested":true}`},
		{"schema", string(file.Rest["$schema"]), `"https://example.com/schema.json"`},
		{"条目 headers", string(file.Servers["filesystem"].Rest["headers"]), `{"X-Trace":"1"}`},
		{"条目 auth", string(file.Servers["filesystem"].Rest["auth"]), `{"type":"oauth","credentialId":"abc"}`},
		{"条目 timeout", string(file.Servers["filesystem"].Rest["timeout"]), `120000`},
	}
	for _, tc := range preserved {
		if !sameJSON(tc.got, tc.want) {
			t.Errorf("%s 保留失败: got %s want %s", tc.name, tc.got, tc.want)
		}
	}

	// 改一个不相关的键再写回，未知字段必须还在
	file.Servers["docs"] = Entry{URL: "https://other.example.com/mcp", Type: "http"}
	if err := Write(path, file); err != nil {
		t.Fatalf("Write: %v", err)
	}
	again, err := Read(path)
	if err != nil {
		t.Fatalf("Read again: %v", err)
	}
	if !sameJSON(string(again.Servers["filesystem"].Rest["auth"]), `{"type":"oauth","credentialId":"abc"}`) {
		t.Errorf("写回后 auth 变了: %s", again.Servers["filesystem"].Rest["auth"])
	}
	if again.Rest["somethingFuture"] == nil {
		t.Error("写回后未知顶层键丢了")
	}
	if again.Servers["docs"].URL != "https://other.example.com/mcp" {
		t.Errorf("docs 没改成: %+v", again.Servers["docs"])
	}
}

// 启用状态的优先级：停用名单 > 强制启用名单 > 条目 enabled > 默认开。
// 排错顺序的后果是「界面显示已启用、引擎根本不连」，所以单独一条。
func TestIsEnabledPrecedence(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name       string
		file       *File
		wantOn     bool
		wantByDeny bool
	}{
		{"默认开", &File{Servers: map[string]Entry{"a": {}}}, true, false},
		{"条目 enabled=false", &File{Servers: map[string]Entry{"a": {Enabled: &no}}}, false, false},
		{"条目 enabled=true", &File{Servers: map[string]Entry{"a": {Enabled: &yes}}}, true, false},
		{"强制启用压过条目 false", &File{Servers: map[string]Entry{"a": {Enabled: &no}}, Enabled: []string{"a"}}, true, false},
		{"停用名单压过强制启用", &File{Servers: map[string]Entry{"a": {Enabled: &yes}}, Enabled: []string{"a"}, Disabled: []string{"a"}}, false, true},
		{"停用名单压过条目 true", &File{Servers: map[string]Entry{"a": {Enabled: &yes}}, Disabled: []string{"a"}}, false, true},
	}
	for _, tc := range cases {
		on, byDeny := tc.file.IsEnabled("a")
		if on != tc.wantOn || byDeny != tc.wantByDeny {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", tc.name, on, byDeny, tc.wantOn, tc.wantByDeny)
		}
	}
}

func TestSetEnabledAndRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := Upsert(path, "fs", Entry{Command: "npx", Args: []string{"-y", "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(path, "remote", Entry{URL: "https://e.com/mcp", Type: "http"}); err != nil {
		t.Fatal(err)
	}

	// omp 走顶层名单
	if err := SetEnabled(path, "fs", false, true); err != nil {
		t.Fatal(err)
	}
	file, _ := Read(path)
	if _, byDeny := file.IsEnabled("fs"); !byDeny {
		t.Error("omp 模式停用后应落进停用名单")
	}
	if err := SetEnabled(path, "fs", true, true); err != nil {
		t.Fatal(err)
	}
	file, _ = Read(path)
	if on, _ := file.IsEnabled("fs"); !on {
		t.Error("重新启用后应为开")
	}

	// pi 写条目上的 enabled
	if err := SetEnabled(path, "remote", false, false); err != nil {
		t.Fatal(err)
	}
	file, _ = Read(path)
	if on, _ := file.IsEnabled("remote"); on {
		t.Error("pi 模式停用后应为关")
	}
	if len(file.Disabled) != 0 {
		t.Errorf("pi 模式不该动顶层名单: %v", file.Disabled)
	}

	// 删除要连带清掉名单里的同名残留
	if err := SetEnabled(path, "remote", false, true); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path, "remote"); err != nil {
		t.Fatal(err)
	}
	file, _ = Read(path)
	if _, ok := file.Servers["remote"]; ok {
		t.Error("服务器没删掉")
	}
	if len(file.Disabled) != 0 {
		t.Errorf("删掉后名单里还留着同名项: %v", file.Disabled)
	}
	if err := Remove(path, "不存在"); err == nil {
		t.Error("删不存在的服务器应当报错")
	}
}

func TestReadMissingAndEmpty(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"missing.json"} {
		file, err := Read(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s 不存在不该报错: %v", name, err)
		}
		if len(file.Servers) != 0 {
			t.Errorf("%s 应为空表", name)
		}
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(empty); err != nil {
		t.Errorf("空文件不该报错: %v", err)
	}
}
