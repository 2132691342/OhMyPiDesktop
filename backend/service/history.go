package service

import (
	"encoding/json"
	"strings"

	"OhMyPiDesktop/backend/engine"
)

// 历史投影：把引擎的 messages 数组翻成界面直接能渲染的轮次。
//
// 为什么在后端做：投影规则（块顺序、工具结果按 toolCallId 配对）只该有一份实现。
// 前端再写一遍就一定会跟后端漂移，而漂移的表现是「刷新之后消息顺序变了」。
type HistoryTurn struct {
	Role   string         `json:"role"`
	Blocks []engine.Block `json:"blocks"`
	Model  string         `json:"model,omitempty"`
	// Usage 这条助手消息的用量（输入 / 输出 / 缓存命中 / 缓存写入）。
	//
	// 为什么带在轮次上：引擎每条助手消息都带一份，而界面要在每条回复下摆读数。
	// 不带的话，读数只能靠流式帧现取——刷新一下就没了。
	Usage *TurnUsageVO `json:"usage,omitempty"`
}

// TurnUsageVO 是界面上的用量读数。
//
// 缓存命中与缓存写入分开：命中率是用户最想看的那件事（为什么这次这么快），
// 而「写进去多少」是另一回事（缓存要重建时的代价）。
type TurnUsageVO struct {
	// InputTokens 输入 token（不含缓存命中与写入）
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 token
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens 缓存命中的 token
	CacheReadTokens int `json:"cache_read_tokens"`
	// CacheWriteTokens 写入缓存的 token
	CacheWriteTokens int `json:"cache_write_tokens"`
	// CostUSD 这一条花了多少美元（没有报价时是 0）
	CostUSD float64 `json:"cost_usd"`
	// ElapsedMS 这一轮从提问到答完的毫秒数；会话文件里推不出来时是 0。
	//
	// 它是「这次回答花了多久」的唯一来源：刷新页面之后流式那一边的计时已经没了，
	// 而用户判断「这次是不是变慢了」靠的就是这个数字。
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
}

// ProjectHistory 把消息列表投影成轮次。
//
// 一次回答 = 一轮：正文 / 思考 / 工具调用按原始顺序摆在同一份 blocks 里；
// 工具结果不挂在块上，而是回填到同 toolCallId 的工具块里——界面据此显示输出。
//
// 连续的多条助手消息要合并成一轮：引擎会把一次回答拆成「工具调用前一条、
// 工具调用后一条」，每一轮回答都自带一份用量。不合并的话，界面上一次回答
// 会出两次角色头、两行读数，而直播那一轮也会因为块结构不同对不上历史
// 而被重复渲染一遍。
func ProjectHistory(messages []json.RawMessage) []HistoryTurn {
	turns := make([]HistoryTurn, 0, len(messages))
	// toolBlock 记录 toolCallId → (轮次下标, 块下标)，用于回填结果
	index := map[string][2]int{}
	// open 是「正在续写的助手轮」下标；-1 表示当前没有可续写的轮。
	open := -1
	// askedAt 是当前这一题提问的时刻（毫秒），用来算这一轮花了多久。
	var askedAt int64

	for _, raw := range messages {
		var msg engine.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Role {
		case "user":
			open = -1
			askedAt = msg.Timestamp
			turns = append(turns, HistoryTurn{
				Role:   "user",
				Blocks: []engine.Block{{Kind: engine.BlockText, Text: userText(msg.Content)}},
			})

		case "assistant":
			blocks := assistantBlocks(msg.Content)
			if len(blocks) == 0 {
				continue
			}
			if open >= 0 {
				turn := &turns[open]
				from := len(turn.Blocks)
				turn.Blocks = append(turn.Blocks, blocks...)
				if model := modelLabel(msg); model != "" {
					turn.Model = model
				}
				if usage := usageOf(msg); usage != nil {
					turn.Usage = usage
					turn.Usage.ElapsedMS = elapsed(askedAt, msg.Timestamp)
				}
				registerTools(index, open, turn.Blocks, from)
				continue
			}
			turn := HistoryTurn{Role: "assistant", Blocks: blocks, Model: modelLabel(msg), Usage: usageOf(msg)}
			if turn.Usage != nil {
				turn.Usage.ElapsedMS = elapsed(askedAt, msg.Timestamp)
			}
			turns = append(turns, turn)
			open = len(turns) - 1
			registerTools(index, open, turns[open].Blocks, 0)

		case "toolResult", "bashExecution", "pythonExecution":
			text := joinBlocks(msg.Content)
			if pos, ok := index[msg.ToolCallID]; ok {
				// 结果回填到它自己的那一块，轮次边界不变：工具是这一轮回答的一部分。
				open = pos[0]
				block := &turns[pos[0]].Blocks[pos[1]]
				block.Output = text
				block.IsError = msg.IsError
				block.Status = "done"
				block.DurationMS = msg.DurationMs
				// 历史里的工具结果也带 details，差异同样要从那里取。
				// 不取的话：刚聊完能看到差异，刷新一下就没了——
				// 用户会以为「引擎把改动撤销了」。
				if diffText, filePath := engine.ExtractToolDiff(msg.Details); diffText != "" {
					block.Diff = engine.ParseToolDiff(diffText)
					if filePath == "" {
						filePath = engine.ToolFilePath(block.Args)
					}
					if filePath != "" {
						block.FilePath = filePath
					}
				}
				if msg.IsError {
					block.Status = "error"
				}
				continue
			}
			// 找不到配对（历史截断、结果被压缩掉）：单独成一轮，不静默丢弃
			open = -1
			name := msg.ToolName
			if name == "" {
				name = msg.Role
			}
			turns = append(turns, HistoryTurn{
				Role: "tool",
				Blocks: []engine.Block{{
					Kind:     engine.BlockTool,
					ToolName: name,
					Output:   text,
					IsError:  msg.IsError,
					Status:   "done",
				}},
			})
		}
	}
	// 收尾：仍是 pending 的工具（历史里没有结果）标成 done，避免界面一直转圈
	for i := range turns {
		for j := range turns[i].Blocks {
			if turns[i].Blocks[j].Kind == engine.BlockTool && turns[i].Blocks[j].Status == "pending" {
				turns[i].Blocks[j].Status = "done"
			}
		}
	}
	return turns
}

// assistantBlocks 把一条助手消息的内容块翻成渲染块。
func assistantBlocks(content []engine.ContentBlock) []engine.Block {
	blocks := make([]engine.Block, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			if block.Text == "" {
				continue
			}
			blocks = append(blocks, engine.Block{Kind: engine.BlockText, Text: block.Text})
		case "thinking":
			if block.Thinking == "" {
				continue
			}
			blocks = append(blocks, engine.Block{Kind: engine.BlockThinking, Text: block.Thinking})
		case "toolCall":
			blocks = append(blocks, engine.Block{
				Kind:       engine.BlockTool,
				ToolCallID: block.ID,
				ToolName:   block.Name,
				Args:       block.Arguments,
				Intent:     block.Intent,
				Status:     "pending",
			})
		}
	}
	return blocks
}

// elapsed 算「提问到答完」的毫秒数。
//
// 任一端缺失（旧会话文件没有 timestamp）就返回 0：宁可不显示，
// 也不要拿一个错的时长去骗用户。
func elapsed(from, to int64) int64 {
	if from <= 0 || to <= 0 || to <= from {
		return 0
	}
	return to - from
}

// modelLabel 是这一条消息用的模型（有厂商就带上厂商）。
func modelLabel(msg engine.Message) string {
	if msg.Provider != "" && msg.Model != "" {
		return msg.Provider + "/" + msg.Model
	}
	return msg.Model
}

// usageOf 把引擎的用量翻成界面读数；引擎没报用量时返回 nil。
func usageOf(msg engine.Message) *TurnUsageVO {
	if msg.Usage == nil {
		return nil
	}
	out := &TurnUsageVO{
		InputTokens:      msg.Usage.Input,
		OutputTokens:     msg.Usage.Output,
		CacheReadTokens:  msg.Usage.CacheRead,
		CacheWriteTokens: msg.Usage.CacheWrite,
	}
	if msg.Usage.Cost != nil {
		out.CostUSD = msg.Usage.Cost.Total
	}
	return out
}

// registerTools 记下 [from, len(blocks)) 这一段里工具块的位置，供结果回填。
func registerTools(index map[string][2]int, turnIdx int, blocks []engine.Block, from int) {
	for i := from; i < len(blocks); i++ {
		if blocks[i].Kind == engine.BlockTool && blocks[i].ToolCallID != "" {
			index[blocks[i].ToolCallID] = [2]int{turnIdx, i}
		}
	}
}

// userText 取用户消息的正文（多段之间保留换行）。
func userText(blocks []engine.ContentBlock) string {
	return joinBlocks(blocks)
}

func joinBlocks(blocks []engine.ContentBlock) string {
	var sb strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(block.Text)
		case "image":
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString("[图片]")
		}
	}
	return sb.String()
}
