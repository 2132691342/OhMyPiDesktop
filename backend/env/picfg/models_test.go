package picfg

// pi 的 models.json 是 JSONC：允许注释与尾逗号。解析器写错的表现有两个方向：
// 要么把 baseUrl 里的 `//` 当注释吃掉（配置全废），要么尾逗号让它整份读不出来。
//
// 写入侧与 omp 同理：只改我们掌管的键，用户与引擎写的其它键原样保留。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// StripJSONC 的输出必须是合法 JSON：四个方向都要守住。
func TestStripJSONC(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		key  string
		want string
		arr  int // 数组长度，0 表示不比
	}{
		{"URL 里的双斜杠", `{"baseUrl":"https://api.example.com/v1"}`, "baseUrl", "https://api.example.com/v1", 0},
		{"尾逗号", `{"a":[1,2,3,],"b":{"c":1,},}`, "", "", 3},
		{"块注释", "{/* 块\n注释 */ \"a\": \"x\"}", "a", "x", 0},
		{"行注释", "{\n// 顶层\n\"a\": \"x\" // 行尾\n}", "a", "x", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := StripJSONC([]byte(c.raw))
			var v map[string]any
			if err := json.Unmarshal(out, &v); err != nil {
				t.Fatalf("剥离后不是合法 JSON: %v\n%s", err, out)
			}
			if c.arr > 0 {
				a, ok := v["a"].([]any)
				if !ok || len(a) != c.arr {
					t.Fatalf("数组长度不对: %v", v["a"])
				}
				return
			}
			if got, _ := v[c.key].(string); got != c.want {
				t.Errorf("%s = %q，期望 %q", c.key, got, c.want)
			}
		})
	}
}

// 写入必须保留我们不认识的顶层键与其它 provider。
// 抹掉它们，用户看到的现象是「我设的偏好不见了」。
func TestWritePreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	original := `{
  "$schema": "https://example/schema.json",
  "cacheWarming": "idle",
  "providers": {
    "keepme": { "baseUrl": "https://keep.example/v1", "api": "openai-completions" }
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Providers["newone"] = Provider{BaseURL: "https://new.example/v1", API: "openai-completions"}
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}

	after, err := Read(path)
	if err != nil {
		t.Fatalf("写回后读不出来: %v", err)
	}
	if _, ok := after.Providers["keepme"]; !ok {
		t.Fatal("keepme 被抹掉了")
	}
	if _, ok := after.Providers["newone"]; !ok {
		t.Fatal("newone 没写进去")
	}
	if _, ok := after.Rest["cacheWarming"]; !ok {
		t.Fatal("未知顶层键 cacheWarming 被抹掉了")
	}
	if after.Schema == "" {
		t.Fatal("$schema 被抹掉了")
	}

	// 删一个服务商不能连带删掉别的：写盘是整份重写，最容易在这里出事。
	if err := Delete(path, "keepme"); err != nil {
		t.Fatal(err)
	}
	deleted, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := deleted.Providers["keepme"]; ok {
		t.Error("keepme 没被删掉")
	}
	if _, ok := deleted.Providers["newone"]; !ok {
		t.Error("newone 被连带删掉了")
	}
}

// 空密钥不能当成「删除」：界面上密钥是打码的，用户只点保存时传回来必然是空。
// 空即删除会把用户能用的凭据毁掉，而且界面上看不出发生过。
func TestMergeKeepsKeyWhenEmpty(t *testing.T) {
	prev := Provider{APIKey: "sk-real", OAuth: "radius", Headers: map[string]string{"x": "1"}}
	next := Merge(prev, Provider{BaseURL: "https://new/v1"})
	if next.APIKey != "sk-real" {
		t.Fatalf("密钥 = %q，期望保留原值", next.APIKey)
	}
	if next.OAuth != "radius" || len(next.Headers) != 1 {
		t.Fatalf("oauth/headers 没被保留: %+v", next)
	}
	if next.BaseURL != "https://new/v1" {
		t.Fatalf("baseUrl = %q，期望用新值", next.BaseURL)
	}
}
