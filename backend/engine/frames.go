package engine

import (
	"context"
	"encoding/json"

	"OhMyPiDesktop/backend/pkg"
)

// onFrame 是读协程的帧回调。禁止阻塞：这里只做内存投影与事件投递。
func (e *Engine) onFrame(f Frame) {
	runID := e.RunID()

	// 私有帧先交给引擎自己：omp 的 agent_end/prompt_result、pi 的 agent_settled
	// 语义不同，公共层不猜。
	if e.dialect.Consume(e, f) {
		return
	}

	switch f.Type {
	case "agent_start":
		e.send("", "chat:agent_start", map[string]any{"run_id": runID})

	case "message_start", "message_update", "message_end":
		e.onMessage(f)

	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		e.pushUpdate(e.tr.Apply(f.Type, f.Raw))

	case "turn_end":
		e.onTurnEnd(f, runID)

	case "queue_update":
		e.SetQueue(f.Raw)

	case "auto_retry_start":
		e.onRetryStart(f)

	case "auto_retry_end":
		e.onRetryEnd(f)

	case "model_changed", "thinking_level_changed", "config_update":
		e.send("", "engine:config_changed", json.RawMessage(f.Raw))

	case "extension_ui_request":
		var req UIRequest
		if json.Unmarshal(f.Raw, &req) != nil {
			return
		}
		e.handleUI(req)

	case "extension_error":
		var ex struct {
			Path  string `json:"extensionPath"`
			Event string `json:"event"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(f.Raw, &ex)
		e.send("", "engine:extension_error", ex)

	case "notice":
		var n struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Source  string `json:"source"`
		}
		_ = json.Unmarshal(f.Raw, &n)
		e.send("", "chat:notice", n)

	case "command_output":
		var co struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(f.Raw, &co)
		e.send("", "chat:command_output", co)

	case "rpc_frame_error":
		e.send("", "chat:gap", map[string]any{"reason": "引擎单帧超出传输上限"})

	case "entry_appended":
		e.send("", "session:entry", json.RawMessage(f.Raw))

	case "goal_updated":
		if e.Caps().Goals {
			e.send("", "chat:goal", json.RawMessage(f.Raw))
		}
	}
}

// onMessage 处理消息三帧。用户消息跳过：前端已本地回显，回灌会重复一条。
func (e *Engine) onMessage(f Frame) {
	var head struct {
		Message struct {
			Role string `json:"role"`
		} `json:"message"`
	}
	_ = json.Unmarshal(f.Raw, &head)
	if head.Message.Role == "user" {
		return
	}
	e.pushUpdate(e.tr.Apply(f.Type, f.Raw))
}

// onTurnEnd 收一回合：用量跟着 turn_end 一起发，界面少一次往返。
func (e *Engine) onTurnEnd(f Frame, runID string) {
	var head struct {
		Message struct {
			Usage *Usage `json:"usage"`
		} `json:"message"`
	}
	_ = json.Unmarshal(f.Raw, &head)
	e.tr.Apply(f.Type, f.Raw)
	payload := map[string]any{"run_id": runID}
	if head.Message.Usage != nil {
		payload["usage"] = head.Message.Usage
	}
	e.send("", "chat:turn_end", payload)
}

// onRetryStart 报告自动重试：用户要看见「卡了一下」而不是一个转圈。
func (e *Engine) onRetryStart(f Frame) {
	var r struct {
		Attempt     int `json:"attempt"`
		MaxAttempts int `json:"maxAttempts"`
		DelayMS     int `json:"delayMs"`
	}
	_ = json.Unmarshal(f.Raw, &r)
	e.send("", "chat:retry", map[string]any{"attempt": r.Attempt, "max": r.MaxAttempts, "delay_ms": r.DelayMS})
}

// onRetryEnd 报告重试结果。
func (e *Engine) onRetryEnd(f Frame) {
	var r struct {
		Success bool `json:"success"`
	}
	_ = json.Unmarshal(f.Raw, &r)
	e.send("", "chat:retry_end", map[string]any{"success": r.Success})
}

// Translator 返回本实例的投影器（Dialect 用它处理私有帧的共用形态）。
func (e *Engine) Translator() *Translator { return e.tr }

// PushUpdate 把翻译结果投成事件（Dialect 用）。
func (e *Engine) PushUpdate(up *Update) { e.pushUpdate(up) }

// pushUpdate 把翻译结果投成事件。
func (e *Engine) pushUpdate(up *Update) {
	if up == nil {
		return
	}
	turn := e.tr.Current()
	if turn == nil {
		return
	}
	payload := map[string]any{"run_id": turn.RunID, "mode": up.Mode, "index": up.Index, "kind": up.Kind}
	switch up.Mode {
	case "delta":
		payload["delta"] = up.Delta
		e.send("", "chat:patch", payload)
	case "block":
		payload["block"] = up.Block
		e.send("", "chat:patch", payload)
	default:
		payload["blocks"] = up.Blocks
		e.send("", "chat:blocks", payload)
	}
}

// SetQueue 广播排队快照。
func (e *Engine) SetQueue(raw json.RawMessage) {
	var q QueuedMessages
	if json.Unmarshal(raw, &q) != nil {
		return
	}
	if q.Steering == nil {
		q.Steering = []string{}
	}
	if q.FollowUp == nil {
		q.FollowUp = []string{}
	}
	e.send("", "chat:queue", q)
}

// FinishRun 收尾一轮：清 runID 并发 chat:done。
//
// 只有 Dialect 能判断「这一轮到底结束了没」——omp 看 agent_end.yielded，
// pi 看 agent_settled；重试与压缩都挂在 agent_end 之后，提前收尾会让界面自相矛盾。
func (e *Engine) FinishRun(runID, stopReason string) {
	e.mu.Lock()
	current := e.runID
	if current != "" && runID == "" {
		runID = current
	}
	e.runID = ""
	e.mu.Unlock()
	if current == "" {
		return // 已经收过尾，重复的结束帧不该再发一次 done
	}
	e.send("", "chat:done", map[string]any{"run_id": runID, "stop_reason": stopReason})
}

// EmitTurn 把当前轮整块推一次（工具结果回填后让界面拿到一致快照）。
func (e *Engine) EmitTurn() {
	if turn := e.tr.Current(); turn != nil {
		e.send("", "chat:turn", turn)
	}
}

// FinishFromTurn 用当前轮的 StopReason 收尾。
func (e *Engine) FinishFromTurn() {
	turn := e.tr.Current()
	stop := ""
	if turn != nil {
		stop = turn.StopReason
	}
	e.EmitTurn()
	e.FinishRun("", stop)
}

// Settle 发整段静默事件。
func (e *Engine) Settle(runID string, extra map[string]any) {
	payload := map[string]any{"run_id": runID}
	for k, v := range extra {
		payload[k] = v
	}
	e.send("", "chat:settled", payload)
}

// Answer 回答一次扩展提问。
func (e *Engine) Answer(ctx context.Context, resp UIResponse) error {
	e.mu.Lock()
	p := e.proc
	e.mu.Unlock()
	if p == nil {
		return pkg.New(pkg.CodeEngineDown, "引擎没有在运行", "")
	}
	resp.Type = "extension_ui_response"
	return p.Send(resp)
}

// handleUI 处理扩展要界面配合的请求。
//
// 提问类（confirm/select/input/editor/ask）转成界面卡片，等用户回答；
// 展示类（notify/setStatus/setWidget/setTitle/set_editor_text/open_url）只读转发。
func (e *Engine) handleUI(req UIRequest) {
	switch req.Method {
	case "confirm", "select", "input", "editor", "ask", "cancel":
		e.send("", "chat:ask", req)
	case "notify":
		e.send("", "chat:notify", map[string]any{"level": firstNonEmpty(req.NotifyType, "info"), "message": req.Message})
	case "setStatus":
		e.send("", "chat:status", map[string]any{"key": req.StatusKey, "text": deref(req.StatusText)})
	case "setWidget":
		e.send("", "chat:widget", map[string]any{"key": req.WidgetKey, "lines": req.WidgetLines, "placement": req.WidgetPlacement})
	case "setTitle":
		e.send("", "session:title", map[string]any{"title": req.Text})
	case "set_editor_text":
		e.send("", "chat:editor_text", map[string]any{"text": req.Text})
	case "open_url":
		if e.Caps().Login {
			e.send("", "chat:open_url", map[string]any{
				"url": firstNonEmpty(req.LaunchURL, req.URL), "instructions": req.Instructions,
			})
		}
	}
}
