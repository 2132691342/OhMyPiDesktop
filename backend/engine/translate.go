package engine

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/domain"
)

// 事件翻译：把引擎帧归一成界面事件。
// 纪律：正文 / 思考 / 工具调用按引擎吐出的原始顺序保留（blocks），不按字段类型分组——分组会把工具卡顶到正文前面，顺序一丢就读不出先干了什么。
// 两个引擎的帧名高度重合，所以翻译只有一份；差异集中在三个开关：用哪个 contentIndex、结束判据、工具流帧名。

// BlockKind 是渲染块的种类。
const (
	BlockText     = "text"
	BlockThinking = "thinking"
	BlockTool     = "tool"
)

// Block 是界面渲染块。工具块的 output 由 tool_execution_end 回填。
type Block struct {
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Intent     string          `json:"intent,omitempty"`
	Output     string          `json:"output,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Status     string          `json:"status,omitempty"` // pending / running / done / error
	Streaming  bool            `json:"streaming,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	// FilePath 被改动的文件路径；只有改文件的工具才有
	FilePath string `json:"file_path,omitempty"`
	// Diff 差异行；只有改文件的工具才有
	Diff []domain.DiffLineVO `json:"diff,omitempty"`
}

// Turn 是一轮对话在界面上的投影：一次「提问 → 回答」。
type Turn struct {
	RunID      string  `json:"run_id"`
	UserMsg    string  `json:"user_message"`
	Blocks     []Block `json:"blocks"`
	Model      string  `json:"model,omitempty"`
	Error      string  `json:"error,omitempty"`
	StopReason string  `json:"stop_reason,omitempty"`
}

// Update 描述一帧带来的可见变化。
// 两种形态：delta（只是某块尾部增长，前端追加即可）与 block（结构变了，前端按 index 替换/插入那一块）；
// 全量重发 blocks 会让一轮长回答的流量是 O(字数²)。
type Update struct {
	Mode   string // delta / block / blocks
	Index  int
	Kind   string // text / thinking / tool
	Delta  string
	Block  *Block
	Blocks []Block // Mode == blocks 时整表（换回合 / 回填工具结果）
}

// Translator 维护「当前流式消息 → 块」的映射。
// 它被两个协程同时碰：请求协程在发 prompt 前 BeginTurn，读协程在收帧时 Apply；
// 所以内部状态一律走 mu，对外只交出副本——交实时指针等于把正在被追加的 slice 递给 JSON 序列化，竞态且会读出半截数据。
type Translator struct {
	mu       sync.Mutex
	turn     *Turn
	textIdx  map[int]int // contentIndex → blocks 下标
	thinkIdx map[int]int
	toolIdx  map[int]int
	toolByID map[string]int
	curMsgID string
	curRole  string
	// toolStart 记每个工具开始的时刻，用来算耗时。
	// 自己算的理由：pi 在帧顶层给 durationMs，omp 没有任何现成字段可读，
	// 而界面上「这个工具跑了多久」对两个引擎都得有。
	toolStart map[string]time.Time
}

// NewTranslator 构造翻译器。
func NewTranslator() *Translator {
	t := &Translator{}
	t.Reset()
	return t
}

// BeginTurn 开一轮新对话。返回的是副本：调用方要把它发出去。
func (t *Translator) BeginTurn(runID, userMessage, model string) *Turn {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turn = &Turn{RunID: runID, UserMsg: userMessage, Model: model, Blocks: []Block{}}
	t.textIdx = map[int]int{}
	t.thinkIdx = map[int]int{}
	t.toolIdx = map[int]int{}
	t.toolByID = map[string]int{}
	t.toolStart = map[string]time.Time{}
	t.curMsgID = ""
	return t.snapshotLocked()
}

// Reset 清掉流式状态（新会话 / 切会话时）。
func (t *Translator) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turn = nil
	t.textIdx = map[int]int{}
	t.thinkIdx = map[int]int{}
	t.toolIdx = map[int]int{}
	t.toolByID = map[string]int{}
	t.toolStart = map[string]time.Time{}
	t.curMsgID = ""
}

// Current 返回当前轮的副本，可能为 nil。
func (t *Translator) Current() *Turn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

// CurrentRunID 返回当前轮 id（没有轮次时为空）。
func (t *Translator) CurrentRunID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.turn == nil {
		return ""
	}
	return t.turn.RunID
}

func (t *Translator) snapshotLocked() *Turn {
	if t.turn == nil {
		return nil
	}
	cp := *t.turn
	cp.Blocks = append([]Block(nil), t.turn.Blocks...)
	return &cp
}

// Apply 喂一帧，返回可见变化（nil 表示无变化）。
// 只处理两引擎共有的帧形态：message_*、tool_execution_*、turn_end；
// 私有帧（omp 的 tool_stream_update、pi 的 bash_execution_update）由 Dialect 转成共有形态后再进来，翻译表只有一份。
func (t *Translator) Apply(typeName string, raw json.RawMessage) *Update {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch typeName {
	case "message_start":
		var f struct {
			Message   Message `json:"message"`
			MessageID string  `json:"messageId"`
		}
		if json.Unmarshal(raw, &f) != nil {
			return nil
		}
		t.curMsgID = f.MessageID
		t.curRole = f.Message.Role
		if t.turn == nil {
			return nil
		}
		if f.Message.Model != "" {
			t.turn.Model = f.Message.Model
		}
		// 助手消息开新块表：一条助手消息就是一个渲染单元。
		if f.Message.Role == "assistant" {
			t.textIdx = map[int]int{}
			t.thinkIdx = map[int]int{}
			t.toolIdx = map[int]int{}
			return &Update{Mode: "blocks", Blocks: append([]Block(nil), t.turn.Blocks...)}
		}
		return nil

	case "message_update":
		var f struct {
			Message   Message `json:"message"`
			MessageID string  `json:"messageId"`
			Event     struct {
				Type         string `json:"type"`
				ContentIndex int    `json:"contentIndex"`
				Delta        string `json:"delta"`
				Content      string `json:"content"`
				ToolCall     *struct {
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"toolCall"`
			} `json:"assistantMessageEvent"`
		}
		if json.Unmarshal(raw, &f) != nil || t.turn == nil {
			return nil
		}
		if f.MessageID != "" {
			t.curMsgID = f.MessageID
		}
		switch f.Event.Type {
		case "text_start":
			return t.addBlock(&t.textIdx, f.Event.ContentIndex, Block{Kind: BlockText, Streaming: true})
		case "text_delta":
			return t.appendDelta(&t.textIdx, f.Event.ContentIndex, BlockText, f.Event.Delta)
		case "text_end":
			if idx, ok := t.textIdx[f.Event.ContentIndex]; ok {
				if f.Event.Content != "" {
					t.turn.Blocks[idx].Text = f.Event.Content
				}
				t.turn.Blocks[idx].Streaming = false
				return t.blockUpdate(idx)
			}
			return nil
		case "thinking_start":
			return t.addBlock(&t.thinkIdx, f.Event.ContentIndex, Block{Kind: BlockThinking, Streaming: true})
		case "thinking_delta":
			return t.appendDelta(&t.thinkIdx, f.Event.ContentIndex, BlockThinking, f.Event.Delta)
		case "thinking_end":
			if idx, ok := t.thinkIdx[f.Event.ContentIndex]; ok {
				if f.Event.Content != "" {
					t.turn.Blocks[idx].Text = f.Event.Content
				}
				t.turn.Blocks[idx].Streaming = false
				return t.blockUpdate(idx)
			}
			return nil
		case "toolcall_start":
			return t.addBlock(&t.toolIdx, f.Event.ContentIndex, Block{Kind: BlockTool, Status: "pending"})
		case "toolcall_end":
			if f.Event.ToolCall == nil || f.Event.ToolCall.ID == "" {
				return nil
			}
			idx, ok := t.toolIdx[f.Event.ContentIndex]
			if !ok {
				idx = len(t.turn.Blocks)
				t.toolIdx[f.Event.ContentIndex] = idx
				t.turn.Blocks = append(t.turn.Blocks, Block{Kind: BlockTool, Status: "pending"})
			}
			t.toolByID[f.Event.ToolCall.ID] = idx
			t.turn.Blocks[idx].ToolCallID = f.Event.ToolCall.ID
			t.turn.Blocks[idx].ToolName = f.Event.ToolCall.Name
			t.turn.Blocks[idx].Args = f.Event.ToolCall.Arguments
			return t.blockUpdate(idx)
		}
		return nil

	case "tool_execution_start":
		var f struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
			Intent     string          `json:"intent"`
			// StartedAt omp 给的起始时刻（ISO 8601）
			StartedAt string `json:"startedAt"`
		}
		if json.Unmarshal(raw, &f) != nil || t.turn == nil {
			return nil
		}
		idx, ok := t.toolByID[f.ToolCallID]
		if !ok {
			idx = len(t.turn.Blocks)
			t.toolByID[f.ToolCallID] = idx
			t.turn.Blocks = append(t.turn.Blocks, Block{Kind: BlockTool, ToolCallID: f.ToolCallID, ToolName: f.ToolName})
		}
		if f.ToolName != "" {
			t.turn.Blocks[idx].ToolName = f.ToolName
		}
		if len(f.Args) > 0 && string(f.Args) != "null" {
			t.turn.Blocks[idx].Args = f.Args
		}
		if f.Intent != "" {
			t.turn.Blocks[idx].Intent = f.Intent
		}
		t.turn.Blocks[idx].Status = "running"
		// 记下开始的时刻：omp 结束帧里没有任何耗时字段，只能自己量。
		// 有引擎给的 startedAt 就用它（更准，跨进程时钟一致）。
		start := time.Now()
		if ts, err := time.Parse(time.RFC3339, f.StartedAt); err == nil {
			start = ts
		}
		t.toolStart[f.ToolCallID] = start
		return t.blockUpdate(idx)

	case "tool_execution_update":
		var f struct {
			ToolCallID    string `json:"toolCallId"`
			PartialResult struct {
				Content []ContentBlock `json:"content"`
			} `json:"partialResult"`
		}
		if json.Unmarshal(raw, &f) != nil || t.turn == nil {
			return nil
		}
		idx, ok := t.toolByID[f.ToolCallID]
		if !ok {
			return nil
		}
		t.turn.Blocks[idx].Output = JoinText(f.PartialResult.Content)
		return t.blockUpdate(idx)

	case "tool_execution_end":
		var f struct {
			ToolCallID string          `json:"toolCallId"`
			Result     json.RawMessage `json:"result"`
			IsError    bool            `json:"isError"`
			// DurationMS 在帧的顶层，不在 result 里。
			// pi 有这个字段，omp 没有——omp 的耗时在 result.details 里，形状又不一样，
			// 所以两处都读，读到哪个算哪个。
			DurationMS int64 `json:"durationMs"`
		}
		if json.Unmarshal(raw, &f) != nil || t.turn == nil {
			return nil
		}
		idx, ok := t.toolByID[f.ToolCallID]
		if !ok {
			return nil
		}
		var res struct {
			Content []ContentBlock `json:"content"`
			Details struct {
				WallTimeMs float64 `json:"wallTimeMs"`
			} `json:"details"`
		}
		_ = json.Unmarshal(f.Result, &res)
		t.turn.Blocks[idx].Output = JoinText(res.Content)
		t.turn.Blocks[idx].IsError = f.IsError
		t.turn.Blocks[idx].DurationMS = f.DurationMS
		if t.turn.Blocks[idx].DurationMS == 0 && res.Details.WallTimeMs > 0 {
			t.turn.Blocks[idx].DurationMS = int64(res.Details.WallTimeMs)
		}
		// 引擎没给耗时就自己量：pi 给 durationMs，omp 一个字段都没有，
		// 而界面上「这个工具跑了多久」对两个引擎都得有。
		if t.turn.Blocks[idx].DurationMS == 0 {
			if started, ok := t.toolStart[f.ToolCallID]; ok {
				t.turn.Blocks[idx].DurationMS = time.Since(started).Milliseconds()
			}
		}
		delete(t.toolStart, f.ToolCallID)
		// 差异在 result.details 里，不在 content 里：content 是给模型看的一句人话（"Successfully replaced 3 blocks"），
		// 真正的改动在 details.diff / details.patch；漏掉这一步，界面上就永远看不到「引擎改了什么」。
		diffText, filePath := ExtractToolDiff(f.Result)
		if diffText != "" {
			t.turn.Blocks[idx].Diff = ParseToolDiff(diffText)
		}
		// details 里没有路径时从参数里找：pi 的 edit 把路径放在参数里，
		// 而 omp 放在 details.path。两条路都要覆盖。
		if filePath == "" {
			filePath = ToolFilePath(t.turn.Blocks[idx].Args)
		}
		if filePath != "" {
			t.turn.Blocks[idx].FilePath = filePath
		}
		t.turn.Blocks[idx].Status = "done"
		if f.IsError {
			t.turn.Blocks[idx].Status = "error"
		}
		return t.blockUpdate(idx)

	case "turn_end":
		if t.turn == nil {
			return nil
		}
		var f struct {
			Message Message `json:"message"`
		}
		if json.Unmarshal(raw, &f) == nil && f.Message.StopReason != "" {
			t.turn.StopReason = f.Message.StopReason
		}
		return nil
	}
	return nil
}

// ToolDelta 把「工具执行的流式文本」写成工具块的输出（两个引擎的私有帧都归到这里）。
func (t *Translator) ToolDelta(toolCallID, delta string, appendMode bool) *Update {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.turn == nil || toolCallID == "" {
		return nil
	}
	idx, ok := t.toolByID[toolCallID]
	if !ok {
		return nil
	}
	if appendMode {
		t.turn.Blocks[idx].Output += delta
	} else {
		t.turn.Blocks[idx].Output = delta
	}
	return t.blockUpdate(idx)
}

func (t *Translator) addBlock(idxMap *map[int]int, contentIndex int, block Block) *Update {
	if t.turn == nil {
		return nil
	}
	idx := len(t.turn.Blocks)
	(*idxMap)[contentIndex] = idx
	t.turn.Blocks = append(t.turn.Blocks, block)
	return t.blockUpdate(idx)
}

func (t *Translator) appendDelta(idxMap *map[int]int, contentIndex int, kind, delta string) *Update {
	if t.turn == nil {
		return nil
	}
	idx, ok := (*idxMap)[contentIndex]
	if !ok {
		idx = len(t.turn.Blocks)
		(*idxMap)[contentIndex] = idx
		t.turn.Blocks = append(t.turn.Blocks, Block{Kind: kind, Streaming: true})
	}
	t.turn.Blocks[idx].Text += delta
	return &Update{Mode: "delta", Index: idx, Kind: kind, Delta: delta}
}

func (t *Translator) blockUpdate(idx int) *Update {
	if t.turn == nil || idx < 0 || idx >= len(t.turn.Blocks) {
		return nil
	}
	block := t.turn.Blocks[idx]
	return &Update{Mode: "block", Index: idx, Kind: block.Kind, Block: &block}
}

// JoinText 把内容块拼成纯文本。
func JoinText(blocks []ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Text != "" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
