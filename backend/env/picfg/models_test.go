package picfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// jsonUnmarshal 是测试里的薄封装：StripJSONC 的输出必须是合法 JSON。
func jsonUnmarshal(raw []byte, out any) error { return json.Unmarshal(raw, out) }

// pi 的 models.json 是 JSONC：允许注释与尾逗号。解析器写错的表现是两个方向：
// 要么把 baseUrl 里的 `//` 当注释吃掉（配置全废），要么尾逗号让它整份读不出来。

func TestStripJSONCKeepsURLSlashes(t *testing.T) {
	raw := []byte(`{
  // 顶层注释
  "providers": {
    "router": { "baseUrl": "https://api.example.com/v1" } // 行尾注释
  }
}`)
	out := StripJSONC(raw)
	var f struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
		} `json:"providers"`
	}
	if err := jsonUnmarshal(out, &f); err != nil {
		t.Fatalf("解析失败: %v\n%s", err, out)
	}
	if got := f.Providers["router"].BaseURL; got != "https://api.example.com/v1" {
		t.Fatalf("baseUrl = %q，`//` 被当成注释吃掉了", got)
	}
}

func TestStripJSONCTrailingCommas(t *testing.T) {
	raw := []byte(`{"a":[1,2,3,],"b":{"c":1,},}`)
	out := StripJSONC(raw)
	var v map[string]any
	if err := jsonUnmarshal(out, &v); err != nil {
		t.Fatalf("尾逗号没被剥离: %v\n%s", err, out)
	}
	if len(v["a"].([]any)) != 3 {
		t.Fatalf("数组长度不对: %v", v["a"])
	}
}

func TestStripJSONCBlockComment(t *testing.T) {
	raw := []byte(`{/* 块
注释 */ "a": 1}`)
	var v map[string]any
	if err := jsonUnmarshal(StripJSONC(raw), &v); err != nil {
		t.Fatalf("块注释没被剥离: %v", err)
	}
	if v["a"].(float64) != 1 {
		t.Fatalf("a = %v", v["a"])
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
}

// 空密钥不能当成「删除」：界面上密钥是打码的，用户只点保存时传回来必然是空。
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

func TestDeleteRemovesProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"a":{"baseUrl":"x"},"b":{"baseUrl":"y"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Delete(path, "a"); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Providers["a"]; ok {
		t.Fatal("a 没被删掉")
	}
	if _, ok := f.Providers["b"]; !ok {
		t.Fatal("b 被连带删掉了")
	}
}
