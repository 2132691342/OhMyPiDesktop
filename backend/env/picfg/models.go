// Package picfg 读写 pi 的 models.json。与 omp 的 models.yml 是两个完全不同的 schema，没有共用字段
// （pi 用 JSONC、模型条目内嵌 thinking 与 inputLimits），合成一套「通用 provider 模型」只会两头都对不上，两个包并列存在，谁也不认识谁。
// 纪律：只改我们掌管的键（providers.<name> 的 baseUrl/apiKey/api/models），顶层 $schema、未知 provider 字段、modelOverrides 一律原样保留。
package picfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/pkg"
)

// File 是 models.json 的解码结果。
type File struct {
	Schema    string              `json:"$schema,omitempty"`
	Providers map[string]Provider `json:"providers"`
	// Rest 保存我们不认识的顶层键，写回时原样带上。
	Rest map[string]json.RawMessage `json:"-"`
}

// Provider 是一个服务商。
type Provider struct {
	Name           string                     `json:"name,omitempty"`
	BaseURL        string                     `json:"baseUrl,omitempty"`
	APIKey         string                     `json:"apiKey,omitempty"`
	API            string                     `json:"api,omitempty"`
	OAuth          string                     `json:"oauth,omitempty"`
	Headers        map[string]string          `json:"headers,omitempty"`
	AuthHeader     *bool                      `json:"authHeader,omitempty"`
	Models         []Model                    `json:"models,omitempty"`
	ModelOverrides map[string]json.RawMessage `json:"modelOverrides,omitempty"`
}

// Model 是一个模型条目。
type Model struct {
	ID               string          `json:"id"`
	Name             string          `json:"name,omitempty"`
	API              string          `json:"api,omitempty"`
	BaseURL          string          `json:"baseUrl,omitempty"`
	Reasoning        bool            `json:"reasoning,omitempty"`
	Input            []string        `json:"input,omitempty"`
	InputLimits      json.RawMessage `json:"inputLimits,omitempty"`
	ContextWindow    int             `json:"contextWindow,omitempty"`
	MaxTokens        int             `json:"maxTokens,omitempty"`
	Cost             *Cost           `json:"cost,omitempty"`
	ThinkingLevelMap json.RawMessage `json:"thinkingLevelMap,omitempty"`
}

// Cost 是每百万 token 的价格。
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Read 读 models.json。文件不存在时返回空表；格式有误时返回错误且不改盘。
func Read(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Providers: map[string]Provider{}}, nil
		}
		return nil, pkg.Wrap(pkg.CodeInternal, "读不到 models.json", err)
	}
	body := StripJSONC(raw)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, pkg.Wrap(pkg.CodeModelInvalid, "models.json 格式有误", err)
	}
	f := &File{Providers: map[string]Provider{}, Rest: map[string]json.RawMessage{}}
	for k, v := range top {
		switch k {
		case "$schema":
			_ = json.Unmarshal(v, &f.Schema)
		case "providers":
			if err := json.Unmarshal(v, &f.Providers); err != nil {
				return nil, pkg.Wrap(pkg.CodeModelInvalid, "providers 段无法解析", err)
			}
		default:
			f.Rest[k] = v
		}
	}
	if f.Providers == nil {
		f.Providers = map[string]Provider{}
	}
	return f, nil
}

// Write 写回 models.json：未知顶层键与未知 provider 字段全部保留。
// 先解析成 map[string]json.RawMessage 再改我们那一段，而不是解成 struct 再序列化——
// 后者会把 pi 明天新增的字段静默删掉。
func Write(path string, f *File) error {
	top := map[string]json.RawMessage{}
	for k, v := range f.Rest {
		if len(v) > 0 {
			top[k] = v
		}
	}
	if f.Schema != "" {
		b, _ := json.Marshal(f.Schema)
		top["$schema"] = b
	}
	// providers 先按磁盘上的原样取回来，只覆盖我们改过的名字。
	existing := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		var cur map[string]json.RawMessage
		if json.Unmarshal(StripJSONC(raw), &cur) == nil {
			if p, ok := cur["providers"]; ok {
				var m map[string]json.RawMessage
				if json.Unmarshal(p, &m) == nil {
					existing = m
				}
			}
		}
	}
	names := make([]string, 0, len(f.Providers))
	for name := range f.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := json.MarshalIndent(f.Providers[name], "", "  ")
		if err != nil {
			return pkg.Wrap(pkg.CodeModelInvalid, "服务商 "+name+" 无法编码", err)
		}
		existing[name] = body
	}
	providers, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "providers 段无法编码", err)
	}
	top["providers"] = providers

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "models.json 无法编码", err)
	}
	return pkg.WriteFileAtomic(path, out, 0o644)
}

// Upsert 写入或更新一个服务商。
func Upsert(path, name string, p Provider) error {
	f, err := Read(path)
	if err != nil {
		return err
	}
	f.Providers[name] = p
	return Write(path, f)
}

// Delete 删掉一个服务商。
func Delete(path, name string) error {
	f, err := Read(path)
	if err != nil {
		return err
	}
	if _, ok := f.Providers[name]; !ok {
		return pkg.New(pkg.CodeNotFound, "没有这个服务商："+name, "")
	}
	delete(f.Providers, name)
	if err := Write(path, f); err != nil {
		return err
	}
	// 删名字这件事必须显式做一次：Write 只覆盖「还在表里」的，删掉的会留在盘上。
	return removeProviderKey(path, name)
}

// removeProviderKey 从磁盘文件里真正摘掉一个 provider 键。
func removeProviderKey(path, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "models.json 不存在", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(StripJSONC(raw), &top); err != nil {
		return pkg.Wrap(pkg.CodeModelInvalid, "models.json 格式有误", err)
	}
	var providers map[string]json.RawMessage
	if p, ok := top["providers"]; ok {
		if err := json.Unmarshal(p, &providers); err != nil {
			return pkg.Wrap(pkg.CodeModelInvalid, "providers 段无法解析", err)
		}
	}
	delete(providers, name)
	merged, err := json.MarshalIndent(providers, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "providers 段无法编码", err)
	}
	top["providers"] = merged
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "models.json 无法编码", err)
	}
	return pkg.WriteFileAtomic(path, out, 0o644)
}

// Names 返回排好序的服务商名。
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Providers))
	for k := range f.Providers {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// MaskKey 把密钥打码（与 omp 侧同一规则，界面不必区分引擎）。
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "!") || strings.HasPrefix(key, "$") {
		return key
	}
	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return key[:6] + strings.Repeat("•", 8) + key[len(key)-4:]
}

// Merge 把界面提交的字段合并到磁盘上已有的条目。
// 密钥在界面上是打码的，空值不能当成「删除」，否则「什么都没改只点保存」
// 就把能用的凭据弄坏了。
func Merge(prev, next Provider) Provider {
	if strings.TrimSpace(next.APIKey) == "" {
		next.APIKey = prev.APIKey
	}
	if next.OAuth == "" {
		next.OAuth = prev.OAuth
	}
	if len(next.Headers) == 0 {
		next.Headers = prev.Headers
	}
	if next.ModelOverrides == nil {
		next.ModelOverrides = prev.ModelOverrides
	}
	return next
}

// StripJSONC 去掉 JSONC 的注释与尾逗号，让 encoding/json 能解。
// 手写扫描而不是正则：字符串里的 // 与 /* 必须保留，正则会把 baseUrl 里的
// 路径切断（https://… 里的 // 就长得像注释）。
func StripJSONC(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escape := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			out = append(out, c)
			if escape {
				escape = false
				continue
			}
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			// 尾逗号：后面除了空白就只有 } 或 ]
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\n' || raw[j] == '\r') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// PathIn 返回某目录下的 models.json（错误信息里给路径用）。
func PathIn(dir string) string { return filepath.Join(dir, "models.json") }
