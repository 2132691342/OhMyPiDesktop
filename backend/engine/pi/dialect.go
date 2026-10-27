// Package pi 是 Pi 引擎与公共层之间的接缝：启动参数、无握手、私有帧。
//
// 与 omp 的差别只有三处，其余全部复用 engine：没有 ready 帧、没有协议 v2 分片、
// 结束判据看 agent_settled 而不是 yielded。
package pi

import (
	"context"
	"encoding/json"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// Kind 是本引擎的种类。
const Kind = engine.KindPi

// Caps 是 pi 的能力表：没有子代理、目标、待办、OAuth 向导、分片与排队编辑。
func Caps() engine.Caps {
	return engine.Caps{
		Subagents:      false,
		Goals:          false,
		Todos:          false,
		Login:          false,
		Fragments:      false,
		QueueEdit:      false,
		QueueClear:     true, // pi 只有一次性 clear_queue，没有逐条移除
		LiveSteerCount: false,
		SkillToggle:    false,
		Extensions:     true,
		ExportHTML:     true,
		SlashCommands:  false, // pi 不主动推，只能拉
	}
}

// Dialect 实现 engine.Dialect。
type Dialect struct{}

// Kind 返回引擎种类。
func (Dialect) Kind() engine.Kind { return Kind }

// Caps 返回能力表。
func (Dialect) Caps() engine.Caps { return Caps() }

// Args 拼出 CLI 参数。
//
// pi 不认 --cwd：工作目录完全由进程 cwd 决定。多给一个 --cwd 会让它直接
// 以「Unknown option」退出，所以这里只给 --mode。
func (Dialect) Args(_ string, extra []string) []string {
	args := []string{"--mode", "rpc"}
	return append(args, extra...)
}

// Handshake 等第一帧或超时。
//
// pi 没有 ready 帧：第一帧可能是扩展的 extension_ui_request，也可能是我们
// 主动发命令的回包。所以「就绪」只能是「已经能读到东西」+ 一个短超时，
// 不能无限期等——否则界面会永久停在启动中。
func (Dialect) Handshake(ctx context.Context, p *engine.Proc) error {
	// 主动问一次状态：回包到了就证明读循环与写路径都通了。
	// 这一步把「等引擎自己说话」变成「等一次往返」——pi 没有 ready 帧，
	// 不主动问就只能靠超时猜。25 秒是因为 Node 冷启动在这台机器上要 3~4 秒，
	// 装着扩展时更久；再长就没有意义了（真起不来应该是错误）。
	var out json.RawMessage
	err := p.Call(ctx, "get_state", nil, &out, 25*time.Second)
	if err == nil {
		p.SetReady(1, 0)
		return nil
	}
	select {
	case <-p.Closed():
		return pkg.Wrap(pkg.CodeEngineDown, "pi 启动后立即退出", p.Err())
	default:
	}
	return pkg.Wrap(pkg.CodeEngineTimeout, "pi 启动超时", err)
}

// Consume 处理 pi 私有帧。
func (Dialect) Consume(e *engine.Engine, f engine.Frame) bool {
	switch f.Type {
	case "agent_end":
		// pi 没有 prompt_result，也没有 yielded；willRetry 为真表示还要再来一轮。
		// 这一帧只更新投影（取 stopReason），**不收尾整轮**：重试与压缩都挂在
		// 它之后，提前发 done 会让界面在还在跑的时候显示完成。
		// 整轮收尾统一在 agent_settled 做。
		e.Translator().Apply(f.Type, f.Raw)
		return true

	case "agent_settled":
		// pi 的整段静默：这一帧才是「这一轮真的结束了」。
		var head struct {
			Aborted bool `json:"aborted"`
		}
		_ = json.Unmarshal(f.Raw, &head)
		runID := e.RunID()
		if runID == "" {
			return true // 已经收过尾，重复的 settled 不重发
		}
		e.Settle(runID, map[string]any{"aborted": head.Aborted})
		if head.Aborted {
			e.Emit("", "chat:error", map[string]any{"run_id": runID, "message": "这一轮被中止了"})
		}
		e.FinishFromTurn()
		return true

	case "compaction_start":
		var head struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(f.Raw, &head)
		e.Emit("", "chat:compaction", map[string]any{"state": "start", "reason": head.Reason})
		return true

	case "compaction_end":
		var head struct {
			Reason  string `json:"reason"`
			Aborted bool   `json:"aborted"`
		}
		_ = json.Unmarshal(f.Raw, &head)
		e.Emit("", "chat:compaction", map[string]any{"state": "end", "reason": head.Reason, "aborted": head.Aborted})
		return true

	case "session_info_changed":
		var up struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(f.Raw, &up)
		e.Emit("", "session:title", map[string]any{"title": up.Name})
		return true

	case "thinking_level_changed":
		var up struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(f.Raw, &up)
		e.Emit("", "engine:config_changed", map[string]any{"type": f.Type, "level": up.Level})
		return true

	case "bash_execution_update":
		// pi 只给 bash 一条流；工具块按 toolCallId 定位（回包里有 id）。
		var up struct {
			ID    string `json:"id"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal(f.Raw, &up) != nil || up.ID == "" {
			return true
		}
		e.PushUpdate(e.Translator().ToolDelta(up.ID, up.Delta, true))
		return true
	}
	return false
}
