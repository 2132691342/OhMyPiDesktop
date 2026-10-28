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
}

// ProjectHistory 把消息列表投影成轮次。
//
// 一条助手消息 = 一个轮次：正文 / 思考 / 工具调用按原始顺序摆在同一份 blocks 里；
// 工具结果不挂在块上，而是回填到同 toolCallId 的工具块里——界面据此显示输出。
func ProjectHistory(messages []json.RawMessage) []HistoryTurn {
	turns := make([]HistoryTurn, 0, len(messages))
	// toolBlock 记录 toolCallId → (轮次下标, 块下标)，用于回填结果
	index := map[string][2]int{}

	for _, raw := range messages {
		var msg engine.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Role {
		case "user":
			turns = append(turns, HistoryTurn{
				Role:   "user",
				Blocks: []engine.Block{{Kind: engine.BlockText, Text: userText(msg.Content)}},
			})

		case "assistant":
			blocks := make([]engine.Block, 0, len(msg.Content))
			for _, block := range msg.Content {
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
			if len(blocks) == 0 {
				continue
			}
			model := msg.Model
			if msg.Provider != "" && msg.Model != "" {
				model = msg.Provider + "/" + msg.Model
			}
			turns = append(turns, HistoryTurn{Role: "assistant", Blocks: blocks, Model: model})
			turnIdx := len(turns) - 1
			for i, block := range turns[turnIdx].Blocks {
				if block.Kind == engine.BlockTool && block.ToolCallID != "" {
					index[block.ToolCallID] = [2]int{turnIdx, i}
				}
			}

		case "toolResult", "bashExecution", "pythonExecution":
			text := joinBlocks(msg.Content)
			if pos, ok := index[msg.ToolCallID]; ok {
				block := &turns[pos[0]].Blocks[pos[1]]
				block.Output = text
				block.IsError = msg.IsError
				block.Status = "done"
				if msg.IsError {
					block.Status = "error"
				}
				continue
			}
			// 找不到配对（历史截断、结果被压缩掉）：单独成一轮，不静默丢弃
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
