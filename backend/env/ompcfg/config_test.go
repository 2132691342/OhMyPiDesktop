package ompcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 配置写错的表现是「用户设的东西下次打开就没了」，而且不会报错。
// 所以这里只测三件事：只改自己掌管的键、密钥打码、坏文件不覆盖。

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

func TestReadModelsParsesProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	if err := os.WriteFile(path, []byte(sampleModels), 0o644); err != nil {
		t.Fatal(err)
	}
	mf, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Providers) != 1 {
		t.Fatalf("服务商数 = %d", len(mf.Providers))
	}
	p, ok := mf.Providers["router"]
	if !ok {
		t.Fatal("没读到 router")
	}
	if p.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("baseUrl = %q", p.BaseURL)
	}
	if len(p.Models) != 1 || p.Models[0].ID != "fast" {
		t.Fatalf("模型 = %+v", p.Models)
	}
	if p.Models[0].ContextWindow != 100000 {
		t.Errorf("contextWindow = %d", p.Models[0].ContextWindow)
	}
}

// 缺文件是正常状态（用户还没配过），不该报错。
func TestReadModelsMissingFileIsEmpty(t *testing.T) {
	mf, err := ReadModels(filepath.Join(t.TempDir(), "nope.yml"))
	if err != nil {
		t.Fatalf("缺文件不该报错: %v", err)
	}
	if len(mf.Providers) != 0 {
		t.Fatalf("期望空表，得到 %+v", mf.Providers)
	}
}

// 写入一个服务商时必须保留文件里别的键与注释：整体重写会抹掉用户的 modelRoles。
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

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "router") {
		t.Error("原有服务商被抹掉了")
	}
	if !strings.Contains(body, "modelRoles") {
		t.Error("文件里别的键被抹掉了")
	}
	if !strings.Contains(body, "我自己的服务商") {
		t.Error("注释被抹掉了")
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
}

func TestDeleteProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	if err := os.WriteFile(path, []byte(sampleModels), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProvider(path, "router"); err != nil {
		t.Fatal(err)
	}
	mf, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mf.Providers["router"]; ok {
		t.Error("router 还在")
	}
	if err := DeleteProvider(path, "router"); err == nil {
		t.Error("删不存在的服务商应当报错")
	}
}

// 坏 YAML 必须原样保留、报错，绝不覆盖：那是用户的数据。
func TestEditRefusesBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	broken := "providers:\n  bad: [unclosed\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	err := UpsertProvider(path, "x", Provider{BaseURL: "http://x"})
	if err == nil {
		t.Fatal("坏文件应当报错")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != broken {
		t.Errorf("坏文件被改写了: %q", string(raw))
	}
}

// 密钥打码：既要能看出「有没有配」，又不能把全文显示在屏幕上。
func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"short":   "•••••",
		"$ENV":    "$ENV",
		"!secret": "!secret",
	}
	for input, want := range cases {
		if got := MaskKey(input); got != want {
			t.Errorf("MaskKey(%q) = %q，期望 %q", input, got, want)
		}
	}
	long := "sk-abcdefghijklmnopqrst"
	masked := MaskKey(long)
	if strings.Contains(masked, "ghijklmn") {
		t.Errorf("打码后仍含明文: %q", masked)
	}
	if !strings.HasPrefix(masked, "sk-abc") || !strings.HasSuffix(masked, "qrst") {
		t.Errorf("打码应保留首尾便于辨识: %q", masked)
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

// 整体替换 provider 节点时，界面表达不了的字段（auth / headers）与空密钥必须由调用方继承。
// 这里就锁「替换会丢字段」这个事实：改一个字段后，原节点的其余字段不该自己冒出来。
func TestUpsertReplacesWholeProviderNode(t *testing.T) {
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
	// 只改地址、其余留空：这一步会丢掉 auth / headers / apiKey
	if err := UpsertProvider(path, "keep", Provider{BaseURL: "http://b/v1", API: "openai-completions", Models: []ModelEntry{{ID: "m1"}}}); err != nil {
		t.Fatal(err)
	}
	mf, err := ReadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	got := mf.Providers["keep"]
	if got.APIKey == "sk-original" {
		t.Fatal("假设不成立：替换节点居然保留了密钥，service 层的继承逻辑就多余了")
	}
	if got.Auth != "" || got.Headers != nil {
		t.Fatalf("auth / headers 也应被替换掉，实际: %+v", got)
	}
}

// service 层靠 MergeProvider 兜住「界面表达不了的字段」。
func TestMergeProviderKeepsUntouchedFields(t *testing.T) {
	prev := Provider{
		BaseURL: "http://a/v1",
		APIKey:  "sk-original",
		Auth:    "oauth",
		Headers: map[string]string{"X-Org": "acme"},
		Models:  []ModelEntry{{ID: "m1"}},
	}
	// 界面只改了地址：其余全是零值
	next := Provider{BaseURL: "http://b/v1", API: "openai-completions", Models: []ModelEntry{{ID: "m1"}}}
	got := MergeProvider(prev, next)

	if got.APIKey != "sk-original" {
		t.Errorf("密钥被抹掉了: %q", got.APIKey)
	}
	if got.Auth != "oauth" {
		t.Errorf("auth 被抹掉了: %q", got.Auth)
	}
	if got.Headers["X-Org"] != "acme" {
		t.Errorf("headers 被抹掉了: %v", got.Headers)
	}
	if got.BaseURL != "http://b/v1" {
		t.Errorf("用户改的地址没生效: %q", got.BaseURL)
	}

	// 用户显式填了新值时必须采用新值，不能被旧值盖回去
	explicit := Provider{BaseURL: "http://b/v1", APIKey: "sk-new", Auth: "apiKey", Headers: map[string]string{"X-Org": "other"}}
	merged := MergeProvider(prev, explicit)
	if merged.APIKey != "sk-new" || merged.Auth != "apiKey" || merged.Headers["X-Org"] != "other" {
		t.Errorf("显式填写被旧值覆盖: %+v", merged)
	}
}
