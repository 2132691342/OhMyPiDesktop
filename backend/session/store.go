package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// 会话索引：直接读引擎写的 JSONL 会话文件。
//
// 为什么读文件而不是问引擎：列表要能列出「别的终端里聊过的会话」，
// 那些会话没有进程在跑。文件是唯一同时覆盖「这里聊的」与「那边聊的」的来源。
// 引擎自己的 get_messages 只覆盖当前进程里的那一个会话，用它做列表会漏。
//
// 读的纪律：
//   - 只读不写。格式是引擎的私有契约，改名 / 换模型一律走 RPC。
//   - 列表只要头几行 + 尾部的用量，不整文件解析（一个会话可能几 MB）。
//
// 两个引擎的会话文件同源不同代，一套代码靠三点适配同时读：
//  1. omp 首行是 256B 固定宽度的 title 槽位，pi 首行就是 session 头；
//  2. 未知条目跳过而不报错（各有十几种私有类型）；
//  3. model_change 的字段不同：omp `{model:"provider/id"}`，pi `{provider, modelId}`。

// Session 是会话列表项。
type Session struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	CWD       string    `json:"cwd"`
	WorkDir   string    `json:"work_dir"` // 目录 slug 还原出的路径
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at"`
	Size      int64     `json:"size"`
	Messages  int       `json:"messages"`
	Tokens    int       `json:"tokens"`
	Cost      float64   `json:"cost"`
}

// Detail 是会话详情：元信息 + 投影消息。
type Detail struct {
	Session
	Thinking string    `json:"thinking"`
	Items    []Item    `json:"items"`
}

// Item 是一条可渲染条目。
type Item struct {
	Kind      string          `json:"kind"` // user / assistant / tool / system
	Role      string          `json:"role"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Blocks    []json.RawMessage `json:"blocks,omitempty"`
	Model     string          `json:"model,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolCall  string          `json:"tool_call_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Cost      float64         `json:"cost,omitempty"`
	Tokens    int             `json:"tokens,omitempty"`
}

// Index 是会话文件的读取器，带一层 (size+mtime) 缓存。
type Index struct {
	SessionsDir string
	Kind        engine.Kind

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	size  int64
	mtime time.Time
	meta  Session
}

// NewIndex 构造索引器。Kind 决定标题来源与目录编码的解读方式。
func NewIndex(sessionsDir string, kind engine.Kind) *Index {
	return &Index{SessionsDir: sessionsDir, Kind: kind, cache: map[string]cacheEntry{}}
}

// List 列出全部会话，按更新时间倒序。workspace 非空时只留该目录的。
func (ix *Index) List(workspace string) ([]Session, error) {
	entries, err := os.ReadDir(ix.SessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, pkg.Wrap(pkg.CodeInternal, "读不到会话目录", err)
	}
	want := normalizeDir(workspace)
	out := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(ix.SessionsDir, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}
			meta := ix.metaOf(path, info.Size(), info.ModTime())
			if want != "" && normalizeDir(meta.CWD) != want {
				continue
			}
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// metaOf 读会话头与尾部统计，命中缓存则直接返回。
func (ix *Index) metaOf(path string, size int64, mtime time.Time) Session {
	ix.mu.Lock()
	if c, ok := ix.cache[path]; ok && c.size == size && c.mtime.Equal(mtime) {
		ix.mu.Unlock()
		return c.meta
	}
	ix.mu.Unlock()

	meta := Session{Path: path, Size: size, UpdatedAt: mtime}
	meta.ID = idFromName(filepath.Base(path))
	meta.WorkDir = slugToPath(filepath.Base(filepath.Dir(path)))

	f, err := os.Open(path)
	if err != nil {
		return meta
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	count := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var head struct {
			Type      string `json:"type"`
			Title     string `json:"title"`
			CWD       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Role  string `json:"role"`
				Model string `json:"model"`
				Usage *struct {
					TotalTokens int `json:"totalTokens"`
					Cost        struct {
						Total float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
			Model     string  `json:"model"`
			Provider  string  `json:"provider"`
			ModelID   string  `json:"modelId"`
			Name      *string `json:"name"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		switch head.Type {
		case "title":
			// omp 的固定宽度标题槽位：取到就覆盖，然后继续读下一行当头部。
			if head.Title != "" {
				meta.Title = head.Title
			}
		case "session":
			// 两个引擎的第一条真实头部；cwd 是「会话归属目录」的唯一可靠来源。
			if head.CWD != "" {
				meta.CWD = head.CWD
			}
		case "session_info":
			// pi 的标题存在 session_info 条目里，取最新一条（空串 = 清除）。
			if head.Name != nil {
				meta.Title = *head.Name
			}
		case "message":
			if head.Message.Usage != nil {
				meta.Tokens += head.Message.Usage.TotalTokens
				meta.Cost += head.Message.Usage.Cost.Total
				count++
			}
			if head.Message.Model != "" {
				meta.Model = head.Message.Model
			}
		case "model_change":
			// omp: {model:"provider/id"}；pi: {provider, modelId}。
			switch {
			case head.Model != "":
				meta.Model = head.Model
			case head.Provider != "" && head.ModelID != "":
				meta.Model = head.Provider + "/" + head.ModelID
			case head.ModelID != "":
				meta.Model = head.ModelID
			}
		case "usage":
			// pi 的额外计费条目（与 message 内的 usage 不重叠）。
			if head.Message.Usage != nil {
				meta.Tokens += head.Message.Usage.TotalTokens
				meta.Cost += head.Message.Usage.Cost.Total
			}
		}
	}
	meta.Messages = count
	if meta.CWD == "" {
		meta.CWD = meta.WorkDir
	}
	if meta.Title == "" {
		meta.Title = defaultTitle(meta)
	}

	ix.mu.Lock()
	ix.cache[path] = cacheEntry{size: size, mtime: mtime, meta: meta}
	ix.mu.Unlock()
	return meta
}

// Detail 读一个会话的完整内容。
func (ix *Index) Detail(path string) (*Detail, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNotFound, "会话文件不存在", err)
	}
	meta := ix.metaOf(path, st.Size(), st.ModTime())
	d := &Detail{Session: meta, Items: []Item{}}

	f, err := os.Open(path)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeInternal, "打不开会话文件", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Type string `json:"type"`
			Message *struct {
				Role      string          `json:"role"`
				Content   json.RawMessage `json:"content"`
				Model     string          `json:"model"`
				Timestamp int64           `json:"timestamp"`
				ToolName  string          `json:"toolName"`
				ToolCallID string         `json:"toolCallId"`
				IsError   bool            `json:"isError"`
				Usage     *struct {
					TotalTokens int `json:"totalTokens"`
					Cost        struct {
						Total float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
			ThinkingLevel string  `json:"thinkingLevel"`
			Name          *string `json:"name"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		switch entry.Type {
		case "thinking_level_change":
			if entry.ThinkingLevel != "" {
				d.Thinking = entry.ThinkingLevel
			}
			continue
		case "session_info":
			// pi 的标题条目：最新一条就是当前标题（空串表示清除）。
			if entry.Name != nil {
				d.Title = *entry.Name
			}
			continue
		case "message":
			// 只有 message 条目进投影；其余十几种私有类型一律跳过。
		default:
			continue
		}
		if entry.Message == nil {
			continue
		}
		item := projectMessage(entry.Message.Role, entry.Message.Content, entry.Message.Model,
			entry.Message.ToolName, entry.Message.ToolCallID, entry.Message.IsError, entry.Message.Timestamp)
		if item == nil {
			continue
		}
		if entry.Message.Usage != nil {
			item.Tokens = entry.Message.Usage.TotalTokens
			item.Cost = entry.Message.Usage.Cost.Total
		}
		d.Items = append(d.Items, *item)
	}
	if d.Title == "" {
		d.Title = defaultTitle(d.Session)
	}
	return d, nil
}

// projectMessage 把一条 omp 消息投影成界面条目。
//
// 用户消息压成一行文本；助手消息保留块原文，交给前端按原始顺序渲染
// （正文 / 思考 / 工具调用的先后关系一丢就读不出先干了什么）。
func projectMessage(role string, raw json.RawMessage, model, toolName, toolCallID string, isError bool, ts int64) *Item {
	blocks, text := splitContent(raw)
	switch role {
	case "user":
		return &Item{Kind: "user", Role: role, Text: text, Timestamp: ts, Blocks: blocks}
	case "assistant":
		return &Item{Kind: "assistant", Role: role, Text: text, Model: model, Timestamp: ts, Blocks: blocks}
	case "toolResult":
		return &Item{Kind: "tool", Role: role, Text: text, ToolName: toolName, ToolCall: toolCallID, IsError: isError, Timestamp: ts, Blocks: blocks}
	case "bashExecution", "pythonExecution":
		return &Item{Kind: "tool", Role: role, Text: text, ToolName: role, Timestamp: ts, Blocks: blocks}
	}
	if text == "" && len(blocks) == 0 {
		return nil
	}
	return &Item{Kind: "system", Role: role, Text: text, Timestamp: ts, Blocks: blocks}
}

// splitContent 拆 content：字符串或块数组都接受，返回块原文与纯文本拼接。
func splitContent(raw json.RawMessage) ([]json.RawMessage, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return nil, s
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return nil, ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		var one struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		}
		if json.Unmarshal(b, &one) != nil {
			continue
		}
		switch one.Type {
		case "text":
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(one.Text)
		}
	}
	return blocks, sb.String()
}

// idFromName 从「时间戳_id.jsonl」里取出 id。
func idFromName(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	if i := strings.LastIndex(base, "_"); i >= 0 && i+1 < len(base) {
		return base[i+1:]
	}
	return base
}

// slugToPath 把 omp 的目录 slug 还原成路径（-D-edgeFiles- → D:\edgeFiles）。
// slug 有歧义（真实路径里也可能有 -），所以只用于展示；真实路径从文件头的 cwd 取。
func slugToPath(slug string) string {
	s := slug
	s = strings.TrimPrefix(s, "--")
	s = strings.TrimSuffix(s, "--")
	if len(s) >= 3 && s[0] == '-' {
		drive := s[1:2]
		rest := s[3:]
		return strings.ToUpper(drive) + ":\\" + strings.ReplaceAll(rest, "-", "\\")
	}
	return strings.ReplaceAll(s, "-", "/")
}

func normalizeDir(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return strings.TrimRight(strings.ToLower(abs), `\/`)
}

func defaultTitle(s Session) string {
	if s.Model != "" {
		return "会话 · " + s.Model
	}
	return "未命名会话"
}
