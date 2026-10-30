package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// Switch 切到另一个会话文件。引擎空闲时才能切，否则两个对话的回答会串。
func (e *Engine) Switch(ctx context.Context, sessionPath string) error {
	if e.Busy() {
		return pkg.New(pkg.CodeEngineBusy, "当前对话还没结束，稍后再切换", "")
	}
	var out struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := e.Call(ctx, "switch_session", map[string]any{"sessionPath": sessionPath}, &out, 60*time.Second); err != nil {
		return err
	}
	if out.Cancelled {
		return pkg.New(pkg.CodeEngineBusy, "引擎拒绝了这次切换（有扩展拦下了）", "")
	}
	e.afterSessionBound()
	// 切完之后立刻回填会话文件：不回填的话下一次 CurrentSessionFile 拿到空串，
	// 于是每一次发消息都要重新问一次引擎，并且可能多发一次 switch_session。
	e.setSessionFile(sessionPath)
	return nil
}

// NewSession 在当前目录开一个新会话。
func (e *Engine) NewSession(ctx context.Context) error {
	var out struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := e.Call(ctx, "new_session", nil, &out, 60*time.Second); err != nil {
		return err
	}
	e.afterSessionBound()
	// 新会话的文件名由引擎决定，只能问一次。
	if st, err := e.State(ctx); err == nil {
		e.setSessionFile(st.SessionFile)
	}
	return nil
}

// afterSessionBound 在会话换了之后清掉随会话存活的投影。
// 引擎内部在 new/switch/fork 成功后会 clear() 子代理 roster，并把旧 id 放进墓碑表、丢弃其后的帧；
// 我们不清就会显示「已经消失的子代理还挂着」。
func (e *Engine) afterSessionBound() {
	e.tr.Reset()
	if e.subs != nil {
		e.subs.Clear()
		e.send("", "agents:cleared", map[string]any{"kind": e.Kind})
	}
	e.mu.Lock()
	e.session = ""
	e.mu.Unlock()
	// 会话换了，本地记住的文件路径也要清掉：Switch / NewSession 会在之后回填正确的那个，
	// 而这里先清空是为了不让一个已不属于这个进程的旧路径被当成「当前会话」。
	e.setSessionFile("")
}

// State 拉一份会话状态快照。
func (e *Engine) State(ctx context.Context) (*SessionState, error) {
	var st SessionState
	if err := e.Call(ctx, "get_state", nil, &st, 30*time.Second); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.session = st.SessionID
	e.mu.Unlock()
	// 顺手记住会话文件：State 是唯一稳定拿到它的地方，
	// 记下来之后热路径（发消息 / 删会话 / 改名）就不必再问一次。
	e.setSessionFile(st.SessionFile)
	if st.ContextUsage == nil {
		// pi 的 get_state 不含用量，只有 get_session_stats 里有；补一次让界面统一。
		var stats SessionStats
		if err := e.Call(ctx, "get_session_stats", nil, &stats, 20*time.Second); err == nil {
			st.ContextUsage = stats.ContextUsage
		}
	}
	return &st, nil
}

// Stats 拉会话累计用量。
func (e *Engine) Stats(ctx context.Context) (*SessionStats, error) {
	var st SessionStats
	if err := e.Call(ctx, "get_session_stats", nil, &st, 30*time.Second); err != nil {
		return nil, err
	}
	return &st, nil
}

// Messages 拉完整消息列表（界面刷新时用）。
func (e *Engine) Messages(ctx context.Context) ([]json.RawMessage, error) {
	var out struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := e.Call(ctx, "get_messages", nil, &out, 60*time.Second); err != nil {
		return nil, err
	}
	if out.Messages == nil {
		out.Messages = []json.RawMessage{}
	}
	return out.Messages, nil
}

// CachedSessionFile 只读本地缓存的会话文件，不发 RPC、不拉起进程。
// 与 CurrentSessionFile 的分工：需要绝对正确写路径的（发消息、删会话）用它；
// 只判断「要不要切一下」的用这个——那里为空串最多多切一次，远比拉起一个 pi 进程（3 秒）便宜。
func (e *Engine) CachedSessionFile() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionFile
}

// CurrentSessionFile 返回当前会话文件路径。
// 优先读本地缓存（State() 与每次会话绑定后都会更新它，调用方全在热路径上，多一次 get_state 往返是纯浪费）。
// 缓存为空时退回一次 RPC：正确性优先于省那一次往返，宁可慢一次也不能把消息发到错的会话。
func (e *Engine) CurrentSessionFile() string {
	e.mu.Lock()
	cached := e.sessionFile
	e.mu.Unlock()
	if cached != "" {
		return cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := e.State(ctx)
	if err != nil {
		return ""
	}
	return st.SessionFile
}

// setSessionFile 记住当前会话文件。
func (e *Engine) setSessionFile(path string) {
	e.mu.Lock()
	e.sessionFile = path
	e.mu.Unlock()
}

// SetSessionID 记录当前会话 id（会话内切换时由引擎通知）。
func (e *Engine) SetSessionID(id string) {
	e.mu.Lock()
	e.session = id
	e.mu.Unlock()
}

// SetSessionName 改会话标题（走 RPC：引擎同时会更新文件头的标题槽）。
func (e *Engine) SetSessionName(ctx context.Context, name string) error {
	return e.Call(ctx, "set_session_name", map[string]any{"name": name}, nil, 30*time.Second)
}

// Prompt 发一轮提问。返回 runID，界面据此把事件归到这一轮。
// payload 里的图片直接进引擎的 images 字段：这是唯一能传二进制的通道，模型不支持时由 service 提前拦住，不靠引擎报错。
func (e *Engine) Prompt(ctx context.Context, payload PromptPayload) (string, error) {
	p, err := e.Client(ctx)
	if err != nil {
		return "", err
	}
	runID, dir, sessionID, kind := e.beginRun()

	// 先拿到当前模型名，让界面在首个 token 到达前就能显示
	model := ""
	if st, err := e.State(ctx); err == nil && st.Model != nil {
		model = st.Model.Provider + "/" + st.Model.ID
	}
	turn := e.tr.BeginTurn(runID, payload.Message, model)

	e.send("", "chat:start", map[string]any{
		"run_id": runID, "kind": kind, "work_dir": dir, "session_id": sessionID,
		"user_message": payload.Message, "image_count": len(payload.Images),
	})
	e.send("", "chat:turn", turn)

	if err := p.Call(ctx, "prompt", payload, nil, 120*time.Second); err != nil {
		e.mu.Lock()
		e.runID = ""
		e.mu.Unlock()
		e.send("", "chat:error", map[string]any{"run_id": runID, "message": errText(err)})
		return runID, err
	}
	return runID, nil
}

// beginRun 领一个 runID 并交出这一轮的上下文（目录 / 会话 / 引擎种类）。
func (e *Engine) beginRun() (runID, dir, sessionID string, kind Kind) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runSeq++
	// runID 是「这一轮」的身份：前端按它认领事件，别的一轮的事件直接丢。
	// 用单调 ULID 而不是时间戳+序号：同一毫秒内连开两轮时序号也会撞，
	// 而撞了的结果是两轮的流式内容混在一起。
	runID = pkg.NewULIDMonotonic(pkg.PrefixRun)
	e.runID = runID
	return runID, e.WorkDir, e.session, e.Kind
}

// Abort 中止当前轮。排队消息要跟着停，否则用户排的那句话凭空消失。
func (e *Engine) Abort(ctx context.Context) error {
	return e.Call(ctx, "abort", nil, nil, 30*time.Second)
}

// Steer 插话（当前轮正在跑时补一句）。
func (e *Engine) Steer(ctx context.Context, payload PromptPayload) error {
	return e.Call(ctx, "steer", payload, nil, 30*time.Second)
}

// FollowUp 排队（这一轮结束后再说）。
func (e *Engine) FollowUp(ctx context.Context, payload PromptPayload) error {
	return e.Call(ctx, "follow_up", payload, nil, 30*time.Second)
}

// ClearQueue 一次性清空排队消息。
// 只有 pi 有 clear_queue；omp 的命令表里没有这条（它只提供逐条移除），
// 所以能力由 Caps.QueueClear 表达，界面按 caps 隐藏而不是发一个必然失败的调用。
func (e *Engine) ClearQueue(ctx context.Context) error {
	if !e.Caps().QueueClear {
		return pkg.New(pkg.CodeEngineUnsupported,
			string(e.Kind)+" 不支持一次性清空排队，请逐条移除", "")
	}
	return e.Call(ctx, "clear_queue", nil, nil, 20*time.Second)
}

// RemoveQueued 移除一条排队消息（仅 omp）。queue 取 steering / followUp。
func (e *Engine) RemoveQueued(ctx context.Context, message, queue string) (bool, error) {
	if !e.Caps().QueueEdit {
		return false, pkg.New(pkg.CodeEngineUnsupported,
			string(e.Kind)+" 不支持移除单条排队消息", "")
	}
	if queue != "followUp" {
		queue = "steering"
	}
	var out struct {
		Removed bool `json:"removed"`
	}
	err := e.Call(ctx, "remove_queued_message",
		map[string]any{"message": message, "queue": queue}, &out, 20*time.Second)
	return out.Removed, err
}

// Compact 手动压缩上下文。
func (e *Engine) Compact(ctx context.Context, instructions string) error {
	params := map[string]any{}
	if strings.TrimSpace(instructions) != "" {
		params["customInstructions"] = instructions
	}
	return e.Call(ctx, "compact", params, nil, 180*time.Second)
}

// SetAutoCompaction 开关自动压缩。
func (e *Engine) SetAutoCompaction(ctx context.Context, enabled bool) error {
	return e.Call(ctx, "set_auto_compaction", map[string]any{"enabled": enabled}, nil, 20*time.Second)
}

// SetModel 换模型。
func (e *Engine) SetModel(ctx context.Context, provider, modelID string) error {
	if provider == "" || modelID == "" {
		return pkg.New(pkg.CodeModelInvalid, "请给出服务商与模型 id", "")
	}
	return e.Call(ctx, "set_model", map[string]any{"provider": provider, "modelId": modelID}, nil, 90*time.Second)
}

// SetThinking 换思考档。
func (e *Engine) SetThinking(ctx context.Context, level string) error {
	return e.Call(ctx, "set_thinking_level", map[string]any{"level": level}, nil, 30*time.Second)
}

// ThinkingLevels 拉可用思考档。
func (e *Engine) ThinkingLevels(ctx context.Context) []string {
	var out struct {
		Levels []string `json:"levels"`
	}
	if err := e.Call(ctx, "get_available_thinking_levels", nil, &out, 15*time.Second); err != nil {
		return []string{}
	}
	if out.Levels == nil {
		return []string{}
	}
	return out.Levels
}

// AvailableModels 拉当前可用模型。
func (e *Engine) AvailableModels(ctx context.Context) ([]Model, error) {
	var out struct {
		Models []Model `json:"models"`
	}
	if err := e.Call(ctx, "get_available_models", nil, &out, 30*time.Second); err != nil {
		return nil, err
	}
	if out.Models == nil {
		out.Models = []Model{}
	}
	return out.Models, nil
}

// Commands 拉可用斜杠命令。omp 会主动推，pi 只能拉。
func (e *Engine) Commands(ctx context.Context) []SlashCommand {
	var out struct {
		Commands []SlashCommand `json:"commands"`
	}
	if err := e.Call(ctx, "get_commands", nil, &out, 20*time.Second); err != nil {
		// omp 的取命令接口叫 get_available_commands
		if err2 := e.Call(ctx, "get_available_commands", nil, &out, 20*time.Second); err2 != nil {
			return []SlashCommand{}
		}
	}
	if out.Commands == nil {
		return []SlashCommand{}
	}
	return out.Commands
}
