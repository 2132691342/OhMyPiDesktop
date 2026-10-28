package service

// 对话：开一轮、切会话、排队、插话、停止、换模型与思考档。
//
// 每个动作都是一次界面点击的完整链路：「先查会话属于哪个引擎、再决定跟哪个进程说话」
// 不该在 handler 里分两步——拆开就要么多一次请求，要么让前端知道进程键。

import (
	"context"
	"encoding/json"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/session"
)

// ---------------- 对话 ----------------

// ChatState 是对话页的一次快照。
type ChatState struct {
	Kind            engine.Kind            `json:"kind"`
	Session         string                 `json:"session"`
	SessionFile     string                 `json:"session_file"`
	WorkDir         string                 `json:"work_dir"`
	EngineRunning   bool                   `json:"engine_running"`
	Streaming       bool                   `json:"streaming"`
	Model           *engine.Model          `json:"model"`
	ThinkingLevel   string                 `json:"thinking_level"`
	Levels          []string               `json:"levels"`
	Context         *engine.ContextUsage   `json:"context"`
	TokensPerSecond *float64               `json:"tokens_per_second"`
	AutoCompaction  bool                   `json:"auto_compaction"`
	Queued          *engine.QueuedMessages `json:"queued"`
	Stats           *engine.SessionStats   `json:"stats"`
	Messages        []json.RawMessage      `json:"messages"`
	History         []HistoryTurn          `json:"history"`
	Commands        []engine.SlashCommand  `json:"commands"`
	Profile         string                 `json:"profile"`
	Caps            engine.Caps            `json:"caps"`
}

// ChatState 拉对话页快照。会话文件为空时用当前工作目录的引擎状态。
func (a *App) ChatState(ctx context.Context, kind engine.Kind, sessionFile string) (*ChatState, error) {
	if !kind.Valid() {
		kind = a.kindOfSession(kind, sessionFile)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(kind, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	caps := a.Identity(kind).Caps
	state := &ChatState{
		Kind:        kind,
		SessionFile: sessionFile,
		WorkDir:     dir,
		Profile:     a.Profile(),
		Caps:        caps,
		Levels:      []string{},
		Messages:    []json.RawMessage{},
		History:     []HistoryTurn{},
		Commands:    []engine.SlashCommand{},
	}
	e := a.EngineFor(kind, dir)
	st, err := e.State(ctx)
	if err != nil {
		// 引擎没起来不算错误：界面要显示「输入框禁用 + 连不上」。
		return state, nil
	}
	state.Session = st.SessionID
	if st.SessionFile != "" {
		state.SessionFile = st.SessionFile
	}
	state.EngineRunning = true
	state.Streaming = st.IsStreaming
	state.Model = st.Model
	state.ThinkingLevel = st.ThinkingLevel
	state.Context = st.ContextUsage
	state.TokensPerSecond = st.TokensPerSecond
	state.AutoCompaction = st.AutoCompaction
	state.Queued = st.Queued
	state.Levels = e.ThinkingLevels(ctx)
	if stats, err := e.Stats(ctx); err == nil {
		state.Stats = stats
		if state.Context == nil {
			state.Context = stats.ContextUsage
		}
	}
	if msgs, err := e.Messages(ctx); err == nil && len(msgs) > 0 {
		state.Messages = msgs
	} else if state.SessionFile != "" {
		// 引擎在跑但拿不到消息（换过会话 / 刚启动）时退回文件投影。
		if d, err := a.Index(kind).Detail(state.SessionFile); err == nil {
			state.Messages = itemsToRaw(d.Items)
		}
	}
	state.History = ProjectHistory(state.Messages)
	if caps.SlashCommands {
		if cmds := e.Commands(ctx); len(cmds) > 0 {
			state.Commands = cmds
		}
	} else {
		state.Commands = e.Commands(ctx)
	}
	if state.SessionFile == "" && sessionFile != "" {
		state.SessionFile = sessionFile
	}
	// 记住当前会话，供下次启动恢复。ChatState 是「会话确实打开了」的唯一定点——
	// 在 Send 里记得太晚（用户可能看完就关），在前端记得不可靠（刷窗口时可能丢）。
	if state.SessionFile != "" {
		a.RememberSession(kind, state.SessionFile)
	}
	return state, nil
}

// itemsToRaw 把文件投影的条目转回 message 形态，让投影只有一条路径。
func itemsToRaw(items []session.Item) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		msg := map[string]any{"role": it.Role}
		if len(it.Blocks) > 0 {
			msg["content"] = it.Blocks
		} else {
			msg["content"] = []map[string]any{{"type": "text", "text": it.Text}}
		}
		if it.ToolCall != "" {
			msg["toolCallId"] = it.ToolCall
		}
		if it.ToolName != "" {
			msg["toolName"] = it.ToolName
		}
		if it.IsError {
			msg["isError"] = true
		}
		raw, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		out = append(out, raw)
	}
	return out
}

// Send 发一轮提问。kind 为空时用会话所属引擎。
//
// 附件在 buildPrompt 里装配：工作区文件只拼路径清单，图片进 images 字段。
// 图片还要过一道模型能力判断——模型不收图时提前拒绝，别让引擎静默丢掉。
func (a *App) Send(ctx context.Context, kind engine.Kind, sessionFile string, req domain.ChatSendREQ) (string, error) {
	payload, err := buildPrompt(req.Message, req.Attachments)
	if err != nil {
		return "", err
	}
	if !kind.Valid() {
		kind = a.kindOfSession(kind, sessionFile)
		// 用户没指定引擎时，把选择落回设置：下一句话仍然发给同一个引擎。
		_ = a.SetActive(kind)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(kind, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	e := a.EngineFor(kind, dir)
	if sessionFile != "" && !samePath(e.CurrentSessionFile(), sessionFile) && !e.Busy() {
		// 打开的是历史会话：先切过去再发，否则消息会落到另一个会话里。
		// 切换失败必须报错：忽略它意味着「用户以为在 A 里聊，实际发到了 B」——
		// 这是最坏的一类错：内容写对了，位置错了，事后很难发现。
		if err := e.Switch(ctx, sessionFile); err != nil {
			return "", pkg.Wrap(pkg.CodeEngineBusy, "切不到这个会话，消息没有发出去", err)
		}
	}
	// 模型信息从这一轮的状态里拿：State 本来就要跑，不额外加一次 RPC。
	if len(payload.Images) > 0 {
		var model *engine.Model
		if st, stateErr := e.State(ctx); stateErr == nil {
			model = st.Model
		}
		if err := ensureImagesAllowed(model, len(payload.Images)); err != nil {
			return "", err
		}
	}
	return e.Prompt(ctx, payload)
}

// engineForSession 按会话解析出引擎（省略引擎参数时用）。
func (a *App) engineForSession(kind engine.Kind, sessionFile string) (*engine.Engine, error) {
	k := kind
	if !k.Valid() {
		k = a.kindOfSession(k, sessionFile)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(k, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	return a.EngineFor(k, dir), nil
}

// Stop 停止当前轮。
func (a *App) Stop(ctx context.Context, kind engine.Kind, sessionFile string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Abort(ctx)
}

// Steer 插话（当前轮正在跑时补一句）。
func (a *App) Steer(ctx context.Context, kind engine.Kind, sessionFile string, req domain.ChatSendREQ) error {
	payload, err := buildPrompt(req.Message, req.Attachments)
	if err != nil {
		return err
	}
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	if len(payload.Images) > 0 {
		var model *engine.Model
		if st, stateErr := e.State(ctx); stateErr == nil {
			model = st.Model
		}
		if err := ensureImagesAllowed(model, len(payload.Images)); err != nil {
			return err
		}
	}
	return e.Steer(ctx, payload)
}

// FollowUp 排队（这一轮结束后再说）。
func (a *App) FollowUp(ctx context.Context, kind engine.Kind, sessionFile string, req domain.ChatSendREQ) error {
	payload, err := buildPrompt(req.Message, req.Attachments)
	if err != nil {
		return err
	}
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	if len(payload.Images) > 0 {
		var model *engine.Model
		if st, stateErr := e.State(ctx); stateErr == nil {
			model = st.Model
		}
		if err := ensureImagesAllowed(model, len(payload.Images)); err != nil {
			return err
		}
	}
	return e.FollowUp(ctx, payload)
}

// ClearQueue 一次性清空排队（仅 Caps.QueueClear 的引擎提供）。
func (a *App) ClearQueue(ctx context.Context, kind engine.Kind, sessionFile string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.ClearQueue(ctx)
}

// RemoveQueued 移除一条排队消息（仅 Caps.QueueEdit 的引擎提供）。
func (a *App) RemoveQueued(ctx context.Context, kind engine.Kind, sessionFile, message, queue string) (bool, error) {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return false, err
	}
	return e.RemoveQueued(ctx, message, queue)
}

// Compact 手动压缩上下文。
func (a *App) Compact(ctx context.Context, kind engine.Kind, sessionFile, instructions string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Compact(ctx, instructions)
}

// Answer 回答引擎的提问。
func (a *App) Answer(ctx context.Context, kind engine.Kind, sessionFile string, resp engine.UIResponse) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Answer(ctx, resp)
}

// SetModel 换模型。
func (a *App) SetModel(ctx context.Context, kind engine.Kind, sessionFile, provider, modelID string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetModel(ctx, provider, modelID)
}

// SetThinking 换思考档。
func (a *App) SetThinking(ctx context.Context, kind engine.Kind, sessionFile, level string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetThinking(ctx, level)
}

// SetAutoCompaction 开关自动压缩。
func (a *App) SetAutoCompaction(ctx context.Context, kind engine.Kind, sessionFile string, enabled bool) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetAutoCompaction(ctx, enabled)
}

// NewChat 在某引擎的当前工作目录开一个新会话。
func (a *App) NewChat(ctx context.Context, kind engine.Kind) (string, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	e := a.EngineFor(kind, a.Workspace())
	if err := e.NewSession(ctx); err != nil {
		return "", err
	}
	st, err := e.State(ctx)
	if err != nil {
		return "", err
	}
	return st.SessionFile, nil
}
