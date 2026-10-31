// Package omp 是 OhMyPi 引擎与公共层之间的接缝：启动参数、握手、私有帧。
// 它不做协议传输（那是 engine.Proc），也不做事件命名（那是 engine.Engine），
// 这里只回答「omp 比公共层多什么、少什么」。
package omp

import (
	"context"
	"encoding/json"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// Kind 是本引擎的种类。
const Kind = engine.KindOMP

// Caps 是 omp 的能力表：公共层能表达的能力它全有。
func Caps() engine.Caps {
	return engine.Caps{
		Subagents:      true,
		Goals:          true,
		Todos:          true,
		Login:          true,
		Fragments:      true,
		QueueEdit:      true,
		QueueClear:     false, // omp 没有 clear_queue，只能逐条移除
		LiveSteerCount: true,
		SkillToggle:    true,
		Extensions:     true,
		ExportHTML:     false, // omp 没有导出 HTML 的 RPC，TUI 的 /export 走不了
		SlashCommands:  true,
		SessionClone:   false, // omp 的 /clone 只活在 TUI，RPC 没有 clone 方法
		PluginToggle:   true,
		PluginUpgrade:  true,
	}
}

// Dialect 实现 engine.Dialect。
type Dialect struct{}

// Kind 返回引擎种类。
func (Dialect) Kind() engine.Kind { return Kind }

// Caps 返回能力表。
func (Dialect) Caps() engine.Caps { return Caps() }

// Args 拼出 CLI 参数。
// omp 自己接受 --cwd，也认进程 cwd；两个都给，让「引擎眼里的工作目录」与「进程实际所在目录」不可能不一致。
func (Dialect) Args(workDir string, extra []string) []string {
	args := []string{"--mode", "rpc-ui", "--cwd", workDir}
	return append(args, extra...)
}

// Handshake 等 ready 帧，然后协商协议 v2。
func (Dialect) Handshake(ctx context.Context, p *engine.Proc) error {
	select {
	case <-p.Closed():
		return pkg.Wrap(pkg.CodeEngineDown, "omp 启动后立即退出", p.Err())
	case <-time.After(45 * time.Second):
		return pkg.New(pkg.CodeEngineTimeout, "omp 启动超时", "45 秒内没有收到就绪帧")
	case <-ctx.Done():
		return pkg.Wrap(pkg.CodeEngineDown, "omp 启动被取消", ctx.Err())
	case <-p.Ready():
	}
	// 大消息（长会话历史、整段工具输出）在 v1 下会被截断或报错。
	// ready 的 protocolVersion 是「当前生效版本」（默认 1），能不能升看 supportedProtocolVersions。
	if !p.SupportsV2() {
		return nil
	}
	var out struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := p.Call(ctx, "negotiate_protocol", map[string]any{"protocolVersion": 2}, &out, 10*time.Second); err != nil {
		pkg.Warnf("omp: 协议 v2 协商失败，回退 v1: %v", err)
		return nil
	}
	if out.ProtocolVersion == 2 {
		p.SetReady(2, p.MaxFrame())
	}
	return nil
}

// Consume 处理 omp 私有帧。
func (Dialect) Consume(e *engine.Engine, f engine.Frame) bool {
	switch f.Type {
	case "agent_end":
		var head struct {
			Yielded    *bool `json:"yielded"`
			IsTerminal *bool `json:"isTerminal"`
		}
		_ = json.Unmarshal(f.Raw, &head)
		e.Translator().Apply(f.Type, f.Raw)
		// 重试 / 压缩都挂在 agent_end 之后，提前收尾会让界面自相矛盾。
		// FinishFromTurn 自己会把整轮推一次，这里不单独推——重复的整表
		// 全量推送会让长会话多一次 O(字数) 的序列化。
		if isYield(head.Yielded, head.IsTerminal) {
			e.FinishFromTurn()
		}
		return true

	case "session_settled":
		e.Settle(e.RunID(), nil)
		return true

	case "prompt_result":
		var pr struct {
			Status    string `json:"status"`
			Settled   bool   `json:"sessionSettled"`
			AgentInvo bool   `json:"agentInvoked"`
			Error     *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(f.Raw, &pr)
		runID := e.RunID()
		payload := map[string]any{"run_id": runID, "status": pr.Status, "settled": pr.Settled}
		if pr.Error != nil {
			payload["message"] = pr.Error.Message
			payload["code"] = pr.Error.Code
		}
		e.Emit("", "chat:prompt_result", payload)
		if pr.Status != "completed" && pr.Status != "" {
			e.Emit("", "chat:error", map[string]any{
				"run_id": runID, "message": promptErrText(pr.Status, pr.Error),
			})
		}
		e.FinishRun(runID, "")
		return true

	case "auto_compaction_start":
		e.Emit("", "chat:compaction", map[string]any{"state": "start"})
		return true

	case "auto_compaction_end":
		e.Emit("", "chat:compaction", map[string]any{"state": "end"})
		return true

	case "session_info_update":
		var up struct {
			Title     *string `json:"title"`
			SessionID string  `json:"sessionId"`
		}
		_ = json.Unmarshal(f.Raw, &up)
		if up.SessionID != "" {
			e.SetSessionID(up.SessionID)
		}
		e.Emit("", "session:info", up)
		return true

	case "available_commands_update":
		var up struct {
			Commands []engine.SlashCommand `json:"commands"`
		}
		_ = json.Unmarshal(f.Raw, &up)
		e.Emit("", "engine:commands", up.Commands)
		return true

	case "tool_stream_update":
		// 工具执行的流式文本：omp 用专门帧，pi 用 bash_execution_update。
		var up struct {
			ToolCallID string `json:"toolCallId"`
			Delta      string `json:"delta"`
		}
		if json.Unmarshal(f.Raw, &up) != nil {
			return true
		}
		e.PushUpdate(e.Translator().ToolDelta(up.ToolCallID, up.Delta, true))
		return true

	case "subagent_lifecycle", "subagent_progress", "subagent_event":
		// 同时做两件事：更新 roster（权威索引）与发事件（增量）。
		// 只发事件不更新 roster，界面在下次 get_subagents 之前点不出任何子代理；
		// 只更新 roster 不发事件，进度条就不会动。
		if store := e.Subagents(); store != nil {
			var payload map[string]any
			if json.Unmarshal(f.Raw, &payload) == nil {
				store.ApplyFrame(f.Type, payload)
			}
		}
		switch f.Type {
		case "subagent_lifecycle":
			e.Emit("", "subagent:lifecycle", json.RawMessage(f.Raw))
		case "subagent_progress":
			e.Emit("", "subagent:progress", json.RawMessage(f.Raw))
		default:
			e.Emit("", "subagent:event", json.RawMessage(f.Raw))
		}
		return true
	}
	return false
}

func isYield(yielded, terminal *bool) bool {
	if yielded != nil {
		return *yielded
	}
	if terminal != nil {
		return *terminal
	}
	return true
}

func promptErrText(status string, e *struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}) string {
	switch {
	case e != nil && e.Code == "cost_cap":
		return "已达到本次花费上限，请求已停止：" + e.Message
	case e != nil && e.Message != "":
		return e.Message
	case status == "aborted":
		return "这一轮被中止了"
	case status == "error":
		return "这一轮出错了"
	}
	return ""
}
