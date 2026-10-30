// Package mcpcfg 读写引擎的 mcp.json。两个引擎在这里**共用同一个文件形状**（`mcpServers` 映射），
// 差别只在「文件放哪」（~/.omp/agent/mcp.json vs ~/.pi/agent/mcp.json）与「停用一个服务器写哪个键」——omp 认顶层 disabledServers 名单，pi 认条目上的 enabled。
// 纪律与 ompcfg / picfg 一致：只改我们掌管的键，未知字段原样带回；引擎的配置项（auth / oauth / headers / timeout …）多得多，整体重写会静默抹掉用户手配的那些。

package mcpcfg

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/pkg"
)

// File 是 mcp.json 的解码结果。
type File struct {
	// Servers 服务器定义，键即服务器名
	Servers map[string]Entry `json:"mcpServers"`
	// Disabled 顶层停用名单（仅 omp 认）
	Disabled []string `json:"disabledServers,omitempty"`
	// Enabled 顶层强制启用名单（仅 omp 认）
	Enabled []string `json:"enabledServers,omitempty"`
	// AutoEnableCodemode 连上 codemode 服务器时自动激活 codemode 工具（仅 pi 认）
	AutoEnableCodemode *bool `json:"autoEnableCodemode,omitempty"`
	// Rest 我们不认识的顶层键，写回时原样带上
	Rest map[string]json.RawMessage
}

// Entry 是一个服务器定义。已知字段解出来给界面，其余进 Rest。
type Entry struct {
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Description string            `json:"description,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	CWD         string            `json:"cwd,omitempty"`
	// Rest 未知字段（headers / auth / oauth / timeout / toolExposure …）
	Rest map[string]json.RawMessage
}

const (
	serversKey  = "mcpServers"
	disabledKey = "disabledServers"
	enabledKey  = "enabledServers"
	autoKey     = "autoEnableCodemode"
)

// Read 读一份 mcp.json。文件不存在返回空表；格式有误返回错误且不改盘。
func Read(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Servers: map[string]Entry{}, Rest: map[string]json.RawMessage{}}, nil
		}
		return nil, pkg.Wrap(pkg.CodeConfigRead, "读不到 mcp.json", err)
	}
	body := stripBOM(raw)
	if strings.TrimSpace(string(body)) == "" {
		return &File{Servers: map[string]Entry{}, Rest: map[string]json.RawMessage{}}, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, pkg.Wrap(pkg.CodeConfigParse, "mcp.json 格式有误", err)
	}
	f := &File{Servers: map[string]Entry{}, Rest: map[string]json.RawMessage{}}
	for key, value := range top {
		switch key {
		case serversKey:
			var servers map[string]json.RawMessage
			if err := json.Unmarshal(value, &servers); err != nil {
				return nil, pkg.Wrap(pkg.CodeConfigParse, "mcpServers 段无法解析", err)
			}
			for name, body := range servers {
				f.Servers[name] = decodeEntry(body)
			}
		case disabledKey:
			_ = json.Unmarshal(value, &f.Disabled)
		case enabledKey:
			_ = json.Unmarshal(value, &f.Enabled)
		case autoKey:
			var flag bool
			if json.Unmarshal(value, &flag) == nil {
				f.AutoEnableCodemode = &flag
			}
		default:
			f.Rest[key] = value
		}
	}
	return f, nil
}

// decodeEntry 解一个服务器条目：已知字段进结构，未知字段进 Rest。
func decodeEntry(body json.RawMessage) Entry {
	var entry Entry
	if err := json.Unmarshal(body, &entry); err != nil {
		return Entry{Rest: map[string]json.RawMessage{}}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return entry
	}
	known := map[string]bool{
		"type": true, "command": true, "args": true, "url": true,
		"description": true, "enabled": true, "env": true, "cwd": true,
	}
	for key, value := range top {
		if known[key] {
			continue
		}
		if entry.Rest == nil {
			entry.Rest = map[string]json.RawMessage{}
		}
		entry.Rest[key] = value
	}
	return entry
}

// Write 写回 mcp.json。
// 先把盘上现有内容整个读成 map，再只替换我们掌管的那几个键——
// 这样引擎明天新增的顶层键与条目字段都不会被抹掉。
func Write(path string, f *File) error {
	top := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(stripBOM(raw))) != "" {
		var cur map[string]json.RawMessage
		if json.Unmarshal(stripBOM(raw), &cur) == nil {
			top = cur
		}
	}
	for key, value := range f.Rest {
		if len(value) > 0 {
			top[key] = value
		}
	}

	servers := map[string]json.RawMessage{}
	for name, entry := range f.Servers {
		body, err := encodeEntry(entry)
		if err != nil {
			return err
		}
		servers[name] = body
	}
	// 名字按字典序：文件是给人读也给人 diff 的，顺序随 map 迭代漂会造出假变更
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := map[string]json.RawMessage{}
	for _, name := range names {
		ordered[name] = servers[name]
	}
	body, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "mcpServers 段无法编码", err)
	}
	top[serversKey] = body

	writeStringList(top, disabledKey, f.Disabled)
	writeStringList(top, enabledKey, f.Enabled)
	if f.AutoEnableCodemode == nil {
		delete(top, autoKey)
	} else if body, err := json.Marshal(*f.AutoEnableCodemode); err == nil {
		top[autoKey] = body
	}

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "mcp.json 无法编码", err)
	}
	return pkg.WriteFileAtomic(path, append(out, '\n'), 0o644)
}

// writeStringList 写一段字符串数组；空数组写空数组而不是删键。
// 删键与留空数组语义不同：留空数组表示「我明确知道这份名单是空的」，
// 删键则表示「这个引擎不认这个键」。omp 认，所以留空数组。
func writeStringList(top map[string]json.RawMessage, key string, list []string) {
	if len(list) == 0 {
		top[key] = json.RawMessage(`[]`)
		return
	}
	body, err := json.Marshal(list)
	if err == nil {
		top[key] = body
	}
}

// encodeEntry 编码一个条目：已知字段与未知字段合并成一个对象。
func encodeEntry(entry Entry) (json.RawMessage, error) {
	top := map[string]json.RawMessage{}
	for key, value := range entry.Rest {
		if len(value) > 0 {
			top[key] = value
		}
	}
	put := func(key string, value any, keep bool) {
		if !keep {
			delete(top, key)
			return
		}
		body, err := json.Marshal(value)
		if err == nil {
			top[key] = body
		}
	}
	put("type", entry.Type, entry.Type != "")
	put("command", entry.Command, entry.Command != "")
	put("args", entry.Args, len(entry.Args) > 0)
	put("url", entry.URL, entry.URL != "")
	put("description", entry.Description, entry.Description != "")
	put("env", entry.Env, len(entry.Env) > 0)
	put("cwd", entry.CWD, entry.CWD != "")
	if entry.Enabled == nil {
		delete(top, "enabled")
	} else {
		put("enabled", *entry.Enabled, true)
	}
	return json.Marshal(top)
}

// Upsert 写入或覆盖一个服务器，保留该文件里其余的键与条目。
func Upsert(path, name string, entry Entry) error {
	f, err := Read(path)
	if err != nil {
		return err
	}
	f.Servers[name] = entry
	return Write(path, f)
}

// Remove 删掉一个服务器，并把它从停用名单里摘掉（否则同名残留会变成幽灵规则）。
func Remove(path, name string) error {
	f, err := Read(path)
	if err != nil {
		return err
	}
	if _, ok := f.Servers[name]; !ok {
		return pkg.New(pkg.CodeNotFound, "没有这个 MCP 服务器："+name, "")
	}
	delete(f.Servers, name)
	f.Disabled = dropString(f.Disabled, name)
	f.Enabled = dropString(f.Enabled, name)
	return Write(path, f)
}

// SetEnabled 启停一个服务器。
// 两种机制分引擎走：omp 用顶层 disabledServers 名单（它压过条目上的 enabled），其余引擎直接写条目的 enabled；
// 分叉写在这里而不是调用方——界面不该知道「同一个开关在两个引擎上落在不同的键」。
func SetEnabled(path, name string, enabled bool, useDenyList bool) error {
	f, err := Read(path)
	if err != nil {
		return err
	}
	if _, ok := f.Servers[name]; !ok {
		return pkg.New(pkg.CodeNotFound, "没有这个 MCP 服务器："+name, "")
	}
	if useDenyList {
		if enabled {
			f.Disabled = dropString(f.Disabled, name)
			f.Enabled = appendString(f.Enabled, name)
		} else {
			f.Enabled = dropString(f.Enabled, name)
			f.Disabled = appendString(f.Disabled, name)
		}
		return Write(path, f)
	}
	flag := enabled
	f.Servers[name] = withEnabled(f.Servers[name], &flag)
	return Write(path, f)
}

// withEnabled 返回一个改了 enabled 的条目副本（不就地改 map 里的值）。
func withEnabled(entry Entry, flag *bool) Entry {
	next := entry
	next.Enabled = flag
	return next
}

// IsEnabled 算出一个服务器的实际启用状态。
// 优先级（与 omp 的文档一致）：顶层停用名单 > 顶层强制启用名单 > 条目上的 enabled > 默认开。
// 把这个算在一处，界面就不必自己排这三者的顺序。
func (f *File) IsEnabled(name string) (enabled bool, byDeny bool) {
	if hasString(f.Disabled, name) {
		return false, true
	}
	if hasString(f.Enabled, name) {
		return true, false
	}
	entry, ok := f.Servers[name]
	if !ok || entry.Enabled == nil {
		return true, false
	}
	return *entry.Enabled, false
}

// Names 返回排好序的服务器名。
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Servers))
	for name := range f.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Transport 推断连接方式：显式 type 优先，其次有 url 即远程，有 command 即 stdio。
func (e Entry) Transport() string {
	if e.Type != "" {
		return e.Type
	}
	if e.URL != "" {
		return "http"
	}
	if e.Command != "" {
		return "stdio"
	}
	return ""
}

func hasString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func appendString(list []string, value string) []string {
	if hasString(list, value) {
		return list
	}
	return append(list, value)
}

func dropString(list []string, value string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

// stripBOM 去掉 UTF-8 BOM：两个引擎都可能带它，带了解码直接失败。
func stripBOM(raw []byte) []byte {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return raw[3:]
	}
	return raw
}
