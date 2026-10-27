package ompcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/pkg"
	"gopkg.in/yaml.v3"
)

// 配置读写：models.yml（服务商与模型）、config.yml（其余设置）。
//
// 纪律：
//   - 只改自己掌管的键，绝不整体重写。omp 与用户还在这些文件里写别的键
//     （modelRoles / presets / 项目级覆盖），整体重写会把它们抹掉。
//   - YAML 的注释与顺序尽量保留：用 yaml.Node 改键值，而不是先解成 map 再序列化。

// ModelsFile 是 models.yml 的解码结果。
type ModelsFile struct {
	Providers map[string]Provider `yaml:"providers"`
}

// Provider 是一个服务商。
//
// yaml 标签对齐 models.yml；json 标签对齐界面契约。两者必须同时给：
// 只给 yaml 的话，前端发来的 base_url 解不到 BaseURL 上（Go 的 JSON 解码不认下划线）。
type Provider struct {
	BaseURL string            `yaml:"baseUrl,omitempty" json:"base_url"`
	APIKey  string            `yaml:"apiKey,omitempty" json:"api_key"`
	API     string            `yaml:"api,omitempty" json:"api"`
	Auth    string            `yaml:"auth,omitempty" json:"auth"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Models  []ModelEntry      `yaml:"models,omitempty" json:"models"`
}

// ModelEntry 是一个模型条目。
type ModelEntry struct {
	ID            string     `yaml:"id" json:"id"`
	Name          string     `yaml:"name,omitempty" json:"name,omitempty"`
	API           string     `yaml:"api,omitempty" json:"api,omitempty"`
	Reasoning     bool       `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	Input         []string   `yaml:"input,omitempty" json:"input,omitempty"`
	ContextWindow int        `yaml:"contextWindow,omitempty" json:"contextWindow,omitempty"`
	MaxTokens     int        `yaml:"maxTokens,omitempty" json:"maxTokens,omitempty"`
	Cost          *ModelCost `yaml:"cost,omitempty" json:"cost,omitempty"`
}

// ModelCost 是每百万 token 的价格。
type ModelCost struct {
	Input      float64 `yaml:"input" json:"input"`
	Output     float64 `yaml:"output" json:"output"`
	CacheRead  float64 `yaml:"cacheRead" json:"cacheRead"`
	CacheWrite float64 `yaml:"cacheWrite" json:"cacheWrite"`
}

// ReadModels 读 models.yml。文件不存在时返回空表而不是错误。
func ReadModels(path string) (*ModelsFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ModelsFile{Providers: map[string]Provider{}}, nil
		}
		return nil, pkg.Wrap(pkg.CodeInternal, "读不到 models.yml", err)
	}
	var mf ModelsFile
	if err := yaml.Unmarshal(raw, &mf); err != nil {
		return nil, pkg.Wrap(pkg.CodeModelInvalid, "models.yml 格式有误", err)
	}
	if mf.Providers == nil {
		mf.Providers = map[string]Provider{}
	}
	return &mf, nil
}

// ProviderNames 返回排好序的服务商名。
func (m *ModelsFile) ProviderNames() []string {
	names := make([]string, 0, len(m.Providers))
	for k := range m.Providers {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// UpsertProvider 写入或更新一个服务商，保留文件里的其余内容与注释。
func UpsertProvider(path, name string, p Provider) error {
	return editYAMLDoc(path, func(root *yaml.Node) error {
		providers := ensureMapping(root, "providers")
		node, err := toNode(p)
		if err != nil {
			return err
		}
		setMappingValue(providers, name, node)
		return nil
	})
}

// DeleteProvider 删掉一个服务商。
func DeleteProvider(path, name string) error {
	found := false
	err := editYAMLDoc(path, func(root *yaml.Node) error {
		providers := findMapping(root, "providers")
		if providers == nil {
			return nil
		}
		found = deleteMappingValue(providers, name)
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return pkg.New(pkg.CodeNotFound, "没有这个服务商："+name, "")
	}
	return nil
}

// ConfigFile 是 config.yml 的解码结果（只取界面掌管的键）。
type ConfigFile struct {
	ModelRoles        map[string]any `yaml:"modelRoles,omitempty"`
	EnabledModels     []string       `yaml:"enabledModels,omitempty"`
	DisabledProviders []string       `yaml:"disabledProviders,omitempty"`
	ApprovalMode      string         `yaml:"approvalMode,omitempty"`
	ShellPath         string         `yaml:"shellPath,omitempty"`
	Raw               map[string]any `yaml:"-"`
}

// ReadConfig 读 config.yml 原文与解出来的键。
func ReadConfig(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, pkg.Wrap(pkg.CodeInternal, "读不到 config.yml", err)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "config.yml 格式有误", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// SetConfigKeys 批量写 config.yml 的顶层键（nil 值表示删除该键）。
func SetConfigKeys(path string, kv map[string]any) error {
	return editYAMLDoc(path, func(root *yaml.Node) error {
		keys := make([]string, 0, len(kv))
		for k := range kv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if kv[k] == nil {
				deleteMappingValue(root, k)
				continue
			}
			node, err := toNode(kv[k])
			if err != nil {
				return err
			}
			setMappingValue(root, k, node)
		}
		return nil
	})
}

// ------------- YAML 节点级编辑 -------------

func loadOrNewDoc(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, pkg.Wrap(pkg.CodeInternal, "读不到配置文件", err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode}
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{root}
		return doc, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "配置文件格式有误，未做修改", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		doc.Content[0] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return &doc, nil
}

func editYAMLDoc(path string, edit func(root *yaml.Node) error) error {
	doc, err := loadOrNewDoc(path)
	if err != nil {
		return err
	}
	root := doc.Content[0]
	if err := edit(root); err != nil {
		return err
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return pkg.Wrap(pkg.CodeInternal, "配置序列化失败", err)
	}
	enc.Close()
	return pkg.WriteFileAtomic(path, []byte(sb.String()), 0o644)
}

func findMapping(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			if root.Content[i+1].Kind == yaml.MappingNode {
				return root.Content[i+1]
			}
			return nil
		}
	}
	return nil
}

func ensureMapping(root *yaml.Node, key string) *yaml.Node {
	if m := findMapping(root, key); m != nil {
		return m
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(root, key, value)
	return value
}

func setMappingValue(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m.Content = append(m.Content, keyNode, value)
}

func deleteMappingValue(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

func toNode(v any) (*yaml.Node, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "值无法编码", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "值无法编码", err)
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return doc.Content[0], nil
}

// MergeProvider 把「界面提交的字段」合并到「磁盘上已有的条目」上。
//
// 存在的理由：整体替换节点会把界面表达不了的字段一起抹掉。界面上密钥是打码的、
// auth 与 headers 根本不展示，所以这三样在提交时必然是空——空不能当成「删除」，
// 否则用户「什么都没改、只点保存」就把一个能用的服务商弄坏了。
// 反过来，用户真要清掉密钥，填一个空格再删空也有同样的语义歧义；
// 这里选择安全一侧：宁可保留，也不静默毁掉凭据。
func MergeProvider(prev, next Provider) Provider {
	if strings.TrimSpace(next.APIKey) == "" {
		next.APIKey = prev.APIKey
	}
	if strings.TrimSpace(next.Auth) == "" {
		next.Auth = prev.Auth
	}
	if len(next.Headers) == 0 {
		next.Headers = prev.Headers
	}
	return next
}

// ProviderModels 返回某服务商的模型条目。
func (m *ModelsFile) ProviderModels(name string) []ModelEntry {
	p, ok := m.Providers[name]
	if !ok {
		return nil
	}
	return p.Models
}

// MaskKey 把密钥打成可展示的形态：保留前 6 位与后 4 位。
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "!") || strings.HasPrefix(key, "$") {
		return key // 命令 / 环境变量引用，原样展示
	}
	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return key[:6] + strings.Repeat("•", 8) + key[len(key)-4:]
}

// ProvidersView 是给界面的服务商视图：密钥打码，模型条目照原样。
type ProvidersView struct {
	Providers []ProviderView `json:"providers"`
	Path      string         `json:"path"`
}

// ProviderView 是单个服务商的展示形态。
type ProviderView struct {
	Name    string       `json:"name"`
	BaseURL string       `json:"base_url"`
	API     string       `json:"api"`
	Auth    string       `json:"auth"`
	APIKey  string       `json:"api_key"` // 打码
	HasKey  bool         `json:"has_key"`
	Models  []ModelEntry `json:"models"`
}

// View 把 models.yml 转成界面视图。
func (m *ModelsFile) View(path string) ProvidersView {
	out := ProvidersView{Providers: []ProviderView{}, Path: path}
	for _, name := range m.ProviderNames() {
		p := m.Providers[name]
		v := ProviderView{
			Name:    name,
			BaseURL: p.BaseURL,
			API:     p.API,
			Auth:    p.Auth,
			APIKey:  MaskKey(p.APIKey),
			HasKey:  p.APIKey != "",
			Models:  p.Models,
		}
		if v.Models == nil {
			v.Models = []ModelEntry{}
		}
		out.Providers = append(out.Providers, v)
	}
	return out
}

// configPathFor 用于日志与错误信息里给出路径。
func configPathFor(dir, name string) string { return filepath.Join(dir, name) }

// JSONView 便于调试：把配置打成 JSON。
func JSONView(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
