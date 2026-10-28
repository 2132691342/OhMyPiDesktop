package ompcfg

// 引擎配置写错的共同表现是「用户设的东西下次打开就没了」，而且不会报错，
// 所以只测会悄无声息出错的事：只改掌管的键、替换与合并契约相反、坏文件不覆盖。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleModels 是一份真实的 models.yml 形状。
const sampleModels = `# 我自己的服务商
providers:
  router:
    baseUrl: http://127.0.0.1:8080/v1
    api: openai-completions
    apiKey: sk-abcdefghijklmnop
    models:
    - id: fast
      name: fast
      contextWindow: 100000
      maxTokens: 8000
`

// 写一个服务商不能抹掉别的服务商、别的键与注释。
// 删一个服务商不能连带删掉别的，也不能报错说「删了不存在的」。
func TestUpsertProviderPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	original := sampleModels + "modelRoles:\n  default: router/fast\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProvider(path, "local", Provider{
		BaseURL: "http://localhost:11434/v1",
		API:     "openai-completions",
		APIKey:  "none",
		Models:  []ModelEntry{{ID: "qwen", ContextWindow: 32000, MaxTokens: 2048}},
	}); err != nil {
		t.Fatal(err)
	}

	body := string(mustRead(t, path))
	for _, want := range []string{"router", "modelRoles", "我自己的服务商"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q 被抹掉了：\n%s", want, body)
		}
	}

	mf, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Providers) != 2 {
		t.Fatalf("服务商数 = %d，期望 2", len(mf.Providers))
	}
	if mf.Providers["local"].Models[0].ID != "qwen" {
		t.Errorf("新服务商的模型 = %+v", mf.Providers["local"].Models)
	}

	// 删一个服务商不能连带删掉别的，也不能报错说「删了不存在的」。
	if err := DeleteProvider(path, "local"); err != nil {
		t.Fatal(err)
	}
	after, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Providers["local"]; ok {
		t.Error("local 没被删掉")
	}
	if _, ok := after.Providers["router"]; !ok {
		t.Error("router 被连带删掉了")
	}
	if err := DeleteProvider(path, "local"); err == nil {
		t.Error("删不存在的服务商应当报错")
	}
}

// 坏文件不能被改写：解析失败就报错，原文一个字都不动。
// 改写了它，用户在终端里手写的配置就没了，而且界面上看不出发生过。
func TestEditRefusesBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	broken := "providers:\n  bad: [unclosed\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProvider(path, "x", Provider{BaseURL: "http://x"}); err == nil {
		t.Fatal("坏文件应当报错")
	}
	if got := string(mustRead(t, path)); got != broken {
		t.Errorf("坏文件被改写了: %q", got)
	}
}

// SetConfigKeys 只碰给定的键，nil 表示删除。
func TestSetConfigKeysKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("shellPath: C:\\bash.exe\nmodelRoles:\n  default: a/b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetConfigKeys(path, map[string]any{"approvalMode": "write", "shellPath": nil}); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["shellPath"]; ok {
		t.Error("shellPath 应当被删除")
	}
	if cfg["approvalMode"] != "write" {
		t.Errorf("approvalMode = %v", cfg["approvalMode"])
	}
	if _, ok := cfg["modelRoles"]; !ok {
		t.Error("没被点名的键被抹掉了")
	}
}

// 写一个服务商有两条路，契约必须相反且都守住：
//
//	UpsertProvider  整体替换——界面没表达的字段（密钥、auth、headers）会被丢掉
//	MergeProvider   逐字段合并——界面没表达的字段保留，用户显式填的采用新值
//
// service 层靠后者兜住「界面表达不了的字段」。任何一边改错，
// 用户改个地址就可能把能用的凭据弄坏。
func TestProviderReplaceAndMerge(t *testing.T) {
	prev := Provider{
		BaseURL: "http://a/v1",
		APIKey:  "sk-original",
		Auth:    "oauth",
		Headers: map[string]string{"X-Org": "acme"},
		Models:  []ModelEntry{{ID: "m1"}},
	}
	// 界面只改了地址：其余全是零值
	next := Provider{BaseURL: "http://b/v1", API: "openai-completions", Models: []ModelEntry{{ID: "m1"}}}

	// 合并：旧字段全部保留，用户改的地址生效
	got := MergeProvider(prev, next)
	if got.APIKey != "sk-original" {
		t.Errorf("合并后密钥被抹掉了: %q", got.APIKey)
	}
	if got.Auth != "oauth" {
		t.Errorf("合并后 auth 被抹掉了: %q", got.Auth)
	}
	if got.Headers["X-Org"] != "acme" {
		t.Errorf("合并后 headers 被抹掉了: %v", got.Headers)
	}
	if got.BaseURL != "http://b/v1" {
		t.Errorf("合并后用户改的地址没生效: %q", got.BaseURL)
	}

	// 合并：用户显式填了新值时必须采用新值，不能被旧值盖回去
	explicit := Provider{BaseURL: "http://b/v1", APIKey: "sk-new", Auth: "apiKey", Headers: map[string]string{"X-Org": "other"}}
	merged := MergeProvider(prev, explicit)
	if merged.APIKey != "sk-new" || merged.Auth != "apiKey" || merged.Headers["X-Org"] != "other" {
		t.Errorf("合并时显式填写被旧值覆盖: %+v", merged)
	}

	// 替换：整体覆盖，界面没表达的字段确实会丢
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	original := `providers:
  keep:
    baseUrl: http://a/v1
    api: openai-completions
    apiKey: sk-original
    auth: oauth
    headers:
      X-Org: acme
    models:
    - id: m1
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProvider(path, "keep", next); err != nil {
		t.Fatal(err)
	}
	mf, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	replaced := mf.Providers["keep"]
	if replaced.APIKey == "sk-original" {
		t.Fatal("假设不成立：替换节点居然保留了密钥，那么合并逻辑就是多余的")
	}
	if replaced.Auth != "" || replaced.Headers != nil {
		t.Errorf("替换时 auth / headers 也应被覆盖掉，实际: %+v", replaced)
	}
}

// mustRead 读文件，失败就让测试失败。
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
