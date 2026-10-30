package service

// 对话：开一轮、切会话、排队、插话、停止、换模型与思考档。
// 每个动作是界面点击的完整链路：「先查会话属于哪个引擎、再决定跟哪个进程说话」不在 handler 分两步——
// 拆开要么多一次请求，要么让前端知道进程键。

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
	// BindErr 是「没能把引擎切到请求的会话」的原因原话；空表示切好了或不需要切。
	// 界面据此提一句「历史可看，但接下来的话会写到原会话」，而不是静默撒谎。
	BindErr string `json:"bind_err,omitempty"`
}

// ChatState 拉对话页快照。会话文件为空时用当前工作目录的引擎状态。它是「点了某条历史会话」的唯一入口：
// 必须真的把引擎切到那个会话（只把路径写进返回值等于撒谎，下一句话就写错文件）；
// 引擎没跑时不为读历史拉起它：读历史走会话文件（引擎自己写的账本），比拉进程问它快三个数量级（pi 起 Node 实测 3 秒）。
func (a *App) ChatState(ctx context.Context, kind engine.Kind, sessionFile string) (*ChatState, error) {
	// 会话文件决定它属于哪个引擎，请求里给的 engine 只是个默认值。这条不能省：
	// 用户可能点开另一个引擎的会话，照旧用请求的 kind 会让 pi 去绑 omp 的会话文件——
	// 引擎拒绝，界面弹「切不到这个会话」，而用户什么也没做错。
	kind = a.kindOfSession(kind, sessionFile)
	dir := a.Workspace()
	detail, detailErr := a.sessionDetailFor(kind, sessionFile)
	if detailErr == nil && detail.CWD != "" {
		dir = detail.CWD
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

	// 会话对齐：要看的会话与引擎当前的不是同一条时，真的切过去。
	// 切不动（引擎正忙 / 有扩展拦下）就不切，但也不假装切了：
	// SessionFile 保持请求值（界面会去看历史），BindErr 原话说清为什么没切。
	bound := true
	if sessionFile != "" && !samePath(e.CachedSessionFile(), sessionFile) {
		if err := e.Switch(ctx, sessionFile); err != nil {
			bound = false
			state.BindErr = pkg.As(err).Message
			if e.CachedSessionFile() != "" {
				// 引擎仍停在别处：把真实会话说出来，界面别把它当成用户点的那条。
				state.SessionFile = e.CachedSessionFile()
			}
		}
	}

	// 引擎活着才问它要状态与消息。没跑就从文件投影——两条路径的投影规则同一份。
	if e.Live() {
		if st, err := e.State(ctx); err == nil {
			state.EngineRunning = true
			state.Session = st.SessionID
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
			if bound {
				if st.SessionFile != "" {
					state.SessionFile = st.SessionFile
				}
			}
			if msgs, err := e.Messages(ctx); err == nil && len(msgs) > 0 {
				state.Messages = msgs
			}
			if caps.SlashCommands {
				if cmds := e.Commands(ctx); len(cmds) > 0 {
					state.Commands = cmds
				}
			} else {
				state.Commands = e.Commands(ctx)
			}
		}
	}

	if len(state.Messages) == 0 && detailErr == nil {
		// 引擎没跑、或刚切过去还没装载消息：用文件投影。
		// omp 的会话文件没有 usage 条目时也能读出正文，这是唯一覆盖
		// 「在终端里聊过、这里没进程」那种会话的来源。
		state.Messages = itemsToRaw(detail.Items)
	}
	if state.SessionFile == "" && sessionFile != "" {
		state.SessionFile = sessionFile
	}
	// 历史投影按会话的隐藏账本过滤。它必须在 SessionFile 定稿之后：
	// 账是按会话文件路径记的，拿错路径就过滤不到该过滤的轮次。
	state.History = ProjectHistory(state.Messages, a.HiddenSetFor(state.SessionFile))
	// 记住当前会话，供下次启动恢复。ChatState 是「会话确实打开了」的唯一定点——
	// 在 Send 里记得太晚（用户可能看完就关），在前端记得不可靠（刷窗口时可能丢）。
	if state.SessionFile != "" {
		a.RememberSession(kind, state.SessionFile)
	}
	return state, nil
}

// sessionDetailFor 读一条会话的文件投影；路径为空或读不到时返回空详情。
func (a *App) sessionDetailFor(kind engine.Kind, sessionFile string) (*session.Detail, error) {
	if sessionFile == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "没有指定会话", "")
	}
	return a.Index(kind).Detail(sessionFile)
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

// Send 发一轮提问。kind 为空时用会话所属引擎。附件在 buildPrompt 里装配：工作区文件只拼路径清单，图片进 images 字段。
// 图片还要过一道模型能力判断——模型不收图时提前拒绝，别让引擎静默丢掉。
func (a *App) Send(ctx context.Context, kind engine.Kind, sessionFile string, req domain.ChatSendREQ) (string, error) {
	payload, err := buildPrompt(req.Message, req.Attachments)
	if err != nil {
		return "", err
	}
	// 会话文件属于哪个引擎就发给哪个：界面上的选择可能与会话文件不一致。
	kind = a.kindOfSession(kind, sessionFile)
	// 把选择落回设置：下一句话仍然发给同一个引擎。
	_ = a.SetActive(kind)
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(kind, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	e := a.EngineFor(kind, dir)
	if sessionFile != "" && !samePath(e.CurrentSessionFile(), sessionFile) {
		// 打开的是历史会话：先切过去再发，否则消息会落到另一个会话里。
		// 切换失败（含「正在回答、切不了」）必须报错：忽略它意味着
		// 「用户以为在 A 里聊，实际发到了 B」——内容写对了，位置错了，事后很难发现。
		if e.Busy() {
			return "", pkg.New(pkg.CodeEngineBusy, "这个会话还没切过去：引擎正在回答，先停下来再发", "")
		}
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
	// 会话文件比请求里的 engine 更权威：界面上的「当前引擎」是用户上一次的选择，
	// 而用户可能从会话列表里点开一条属于另一个引擎的会话。
	k := a.kindOfSession(kind, sessionFile)
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(k).Detail(sessionFile); err == nil && d.CWD != "" {
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
