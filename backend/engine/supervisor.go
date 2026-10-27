package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// FrameNames 是同一语义在两个引擎里的帧名差异。
//
// 只列「名字不同」的那些；名字相同的帧不在这里出现，避免维护两份清单。
type FrameNames struct {
	Settled         string // 整段静默：omp `session_settled` / pi `agent_settled`
	CompactionStart string // omp `auto_compaction_start` / pi `compaction_start`
	CompactionEnd   string // omp `auto_compaction_end` / pi `compaction_end`
}

// Engine 是一个 (Kind, WorkDir) 上的引擎实例：进程 + 协议客户端 + 当前轮投影。
type Engine struct {
	Kind    Kind
	WorkDir string

	mu      sync.Mutex
	proc    *Proc
	tr      *Translator
	session string
	runSeq  int
	runID   string
	booting bool
	subs    SubagentStore

	sup     *Supervisor
	dialect Dialect
}

// RunID 返回当前轮 id（空闲为空）。
func (e *Engine) RunID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runID
}

// Busy 表示正在流式回答。
func (e *Engine) Busy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proc != nil && e.runID != ""
}

// SessionID 返回当前会话 id。
func (e *Engine) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.session
}

// Caps 返回能力表。
func (e *Engine) Caps() Caps { return e.dialect.Caps() }

// Subagents 返回子代理观测台状态；引擎不支持时返回 nil。
func (e *Engine) Subagents() SubagentStore {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.subs
}

// send 走监督者的事件出口：出口可能在装配完成后才注入，不能缓存副本。
func (e *Engine) send(session, event string, data any) {
	if fn := e.sup.emitter(); fn != nil {
		fn(session, event, data)
	}
}

// Emit 暴露给 Dialect：私有帧要能复用同一条出口。
func (e *Engine) Emit(session, event string, data any) { e.send(session, event, data) }

// Emitter 取出当前事件出口。
func (s *Supervisor) emitter() Emitter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emit
}

// Supervisor 按 (Kind, WorkDir) 管理引擎实例：一个键一个进程。
//
// 为什么一个目录一个进程：cwd 是进程级属性，共进程会让工具写到错的目录。
// 为什么键里带 Kind：同一目录下 omp 与 pi 是两个进程，互不干扰；
// 用户在输入框里切引擎，切的是「跟哪个进程说话」，不是「换一个目录」。
type Supervisor struct {
	mu      sync.Mutex
	engines map[string]*Engine
	emit    Emitter
	dialect map[Kind]Dialect
	resolve func(kind Kind) (string, []string, []string)
	newSubs func(kind Kind) SubagentStore
}

// NewSupervisor 构造监督者。resolve 在每次拉起进程时调用，让「换引擎 / 换路径」立刻生效。
func NewSupervisor(emit Emitter, dialects map[Kind]Dialect,
	resolve func(kind Kind) (string, []string, []string),
	newSubs func(kind Kind) SubagentStore) *Supervisor {
	return &Supervisor{
		engines: map[string]*Engine{},
		emit:    emit,
		dialect: dialects,
		resolve: resolve,
		newSubs: newSubs,
	}
}

// SetEmitter 在服务装配完成后注入事件出口。
func (s *Supervisor) SetEmitter(emit Emitter) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}

func key(kind Kind, workDir string) string {
	return string(kind) + "\x00" + normalizeDir(workDir)
}

// Engine 取出（必要时创建）某键的引擎句柄，不拉进程。
func (s *Supervisor) Engine(kind Kind, workDir string) *Engine {
	if !kind.Valid() {
		kind = KindOMP
	}
	k := key(kind, workDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.engines[k]; ok {
		return e
	}
	d, ok := s.dialect[kind]
	if !ok {
		d = s.dialect[KindOMP]
	}
	e := &Engine{
		Kind:    kind,
		WorkDir: normalizeDir(workDir),
		tr:      NewTranslator(),
		sup:     s,
		dialect: d,
	}
	if s.newSubs != nil {
		e.subs = s.newSubs(kind)
	}
	s.engines[k] = e
	return e
}

// Running 返回当前在跑的引擎快照。
func (s *Supervisor) Running() []RunRow {
	s.mu.Lock()
	engines := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		engines = append(engines, e)
	}
	s.mu.Unlock()
	out := make([]RunRow, 0, len(engines))
	for _, e := range engines {
		e.mu.Lock()
		pid, session := 0, e.session
		if e.proc != nil {
			pid = e.proc.Pid()
		}
		out = append(out, RunRow{
			Kind:      e.Kind,
			WorkDir:   e.WorkDir,
			PID:       pid,
			SessionID: session,
			Running:   pid != 0,
		})
		e.mu.Unlock()
	}
	return out
}

// RunRow 是一个在跑的引擎进程。
type RunRow struct {
	Kind      Kind   `json:"kind"`
	WorkDir   string `json:"work_dir"`
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
	Running   bool   `json:"running"`
}

// StopAll 关闭所有引擎（退出应用 / 重启引擎前）。
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	engines := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		engines = append(engines, e)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range engines {
		wg.Add(1)
		go func(e *Engine) {
			defer wg.Done()
			e.Stop()
		}(e)
	}
	wg.Wait()
}

// StopKind 只停某一种引擎（换版本 / 换路径后）。
func (s *Supervisor) StopKind(kind Kind) {
	s.mu.Lock()
	engines := make([]*Engine, 0, 2)
	for _, e := range s.engines {
		if e.Kind == kind {
			engines = append(engines, e)
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range engines {
		wg.Add(1)
		go func(e *Engine) {
			defer wg.Done()
			e.Stop()
		}(e)
	}
	wg.Wait()
}

// AnyRunning 判断是否有引擎正在跑一轮对话。
func (s *Supervisor) AnyRunning() bool {
	s.mu.Lock()
	engines := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		engines = append(engines, e)
	}
	s.mu.Unlock()
	for _, e := range engines {
		if e.Busy() {
			return true
		}
	}
	return false
}

// Ensure 主动拉起某引擎并等到就绪。
func (s *Supervisor) Ensure(ctx context.Context, kind Kind, workDir string) (*Engine, error) {
	e := s.Engine(kind, workDir)
	if _, err := e.Client(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// ------------- Engine 进程 -------------

// Client 取出已就绪的进程，必要时拉起。
//
// 并发拉起的语义：第二个调用方等第一个的结论，而不是自己也去拉一个进程。
func (e *Engine) Client(ctx context.Context) (*Proc, error) {
	e.mu.Lock()
	if e.proc != nil {
		select {
		case <-e.proc.Closed():
			e.proc = nil
		default:
			p := e.proc
			e.mu.Unlock()
			return p, nil
		}
	}
	if e.booting {
		e.mu.Unlock()
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return nil, pkg.Wrap(pkg.CodeEngineDown, "等待引擎启动被取消", ctx.Err())
			case <-time.After(80 * time.Millisecond):
			}
			e.mu.Lock()
			if e.proc != nil {
				p := e.proc
				e.mu.Unlock()
				return p, nil
			}
			if !e.booting {
				e.mu.Unlock()
				break
			}
			e.mu.Unlock()
		}
		return nil, pkg.New(pkg.CodeEngineDown, "引擎正在启动，请稍后再试", "")
	}
	e.booting = true
	e.mu.Unlock()

	proc, err := e.boot(ctx)

	e.mu.Lock()
	e.booting = false
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	e.proc = proc
	kind, dir := e.Kind, e.WorkDir
	e.mu.Unlock()

	e.send("", "engine:ready", map[string]any{
		"kind": kind, "work_dir": dir, "pid": proc.Pid(), "protocol": proc.Protocol(),
	})
	return proc, nil
}

func (e *Engine) boot(ctx context.Context) (*Proc, error) {
	exe, extra, env := e.sup.resolve(e.Kind)
	if exe == "" {
		return nil, pkg.New(pkg.CodeEngineUnsupported,
			"没有找到 "+string(e.Kind)+" 引擎", "")
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return nil, pkg.New(pkg.CodeEngineDown, "引擎文件不存在："+exe, "")
	}
	if err := os.MkdirAll(e.WorkDir, 0o755); err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "工作目录不可用", err)
	}
	kind, dir := e.Kind, e.WorkDir
	proc, err := Start(ctx, Options{
		Launch:  Launch{Exe: exe, WorkDir: e.WorkDir, ExtraArgs: extra, Env: env},
		Dialect: e.dialect,
		OnFrame: e.onFrame,
		OnExit: func(err error) {
			e.mu.Lock()
			e.proc = nil
			e.runID = ""
			e.mu.Unlock()
			e.send("", "engine:exit", map[string]any{
				"kind": kind, "work_dir": dir, "reason": errText(err),
			})
		},
	})
	if err != nil {
		return nil, err
	}
	return proc, nil
}

// Stop 关闭引擎进程。
func (e *Engine) Stop() {
	e.mu.Lock()
	p := e.proc
	e.proc = nil
	e.runID = ""
	e.mu.Unlock()
	if p != nil {
		_ = p.Close(5 * time.Second)
	}
}

// Restart 重启引擎（换模型 / 装完插件 / 换引擎版本后）。
func (e *Engine) Restart(ctx context.Context) error {
	e.Stop()
	_, err := e.Client(ctx)
	return err
}

// Call 在已就绪的进程上发一条命令。
func (e *Engine) Call(ctx context.Context, command string, params any, out any, timeout time.Duration) error {
	p, err := e.Client(ctx)
	if err != nil {
		return err
	}
	return p.Call(ctx, command, params, out, timeout)
}

// ------------- 会话 -------------

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
	return nil
}

// afterSessionBound 在会话换了之后清掉随会话存活的投影。
//
// 引擎内部在 new/switch/fork 成功后会 clear() 子代理 roster，并把旧 id
// 放进墓碑表、丢弃其后的帧。我们不清就会显示「已经消失的子代理还挂着」。
func (e *Engine) afterSessionBound() {
	e.tr.Reset()
	if e.subs != nil {
		e.subs.Clear()
		e.send("", "agents:cleared", map[string]any{"kind": e.Kind})
	}
	e.mu.Lock()
	e.session = ""
	e.mu.Unlock()
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

// CurrentSessionFile 返回当前会话文件路径（界面用来判断「是不是同一个会话」）。
func (e *Engine) CurrentSessionFile() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := e.State(ctx)
	if err != nil {
		return ""
	}
	return st.SessionFile
}

// Prompt 发一轮提问。返回 runID，界面据此把事件归到这一轮。
func (e *Engine) Prompt(ctx context.Context, message string) (string, error) {
	p, err := e.Client(ctx)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	e.runSeq++
	// runID 是「这一轮」的身份：前端按它认领事件，别的一轮的事件直接丢。
	// 用单调 ULID 而不是时间戳+序号：同一毫秒内连开两轮时序号也会撞，
	// 而撞了的结果是两轮的流式内容混在一起。
	runID := pkg.NewULIDMonotonic(pkg.PrefixRun)
	e.runID = runID
	dir := e.WorkDir
	sessionID := e.session
	kind := e.Kind
	e.mu.Unlock()

	// 先拿到当前模型名，让界面在首个 token 到达前就能显示
	model := ""
	if st, err := e.State(ctx); err == nil && st.Model != nil {
		model = st.Model.Provider + "/" + st.Model.ID
	}
	turn := e.tr.BeginTurn(runID, message, model)

	e.send("", "chat:start", map[string]any{
		"run_id": runID, "kind": kind, "work_dir": dir, "session_id": sessionID, "user_message": message,
	})
	e.send("", "chat:turn", turn)

	if err := p.Call(ctx, "prompt", map[string]any{"message": message}, nil, 120*time.Second); err != nil {
		e.mu.Lock()
		e.runID = ""
		e.mu.Unlock()
		e.send("", "chat:error", map[string]any{"run_id": runID, "message": errText(err)})
		return runID, err
	}
	return runID, nil
}

// Abort 中止当前轮。排队消息要跟着停，否则用户排的那句话凭空消失。
func (e *Engine) Abort(ctx context.Context) error {
	return e.Call(ctx, "abort", nil, nil, 30*time.Second)
}

// Steer 插话（当前轮正在跑时补一句）。
func (e *Engine) Steer(ctx context.Context, message string) error {
	return e.Call(ctx, "steer", map[string]any{"message": message}, nil, 30*time.Second)
}

// FollowUp 排队（这一轮结束后再说）。
func (e *Engine) FollowUp(ctx context.Context, message string) error {
	return e.Call(ctx, "follow_up", map[string]any{"message": message}, nil, 30*time.Second)
}

// ClearQueue 一次性清空排队消息。
//
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

// SetAutoCompaction 开关自动压缩。
func (e *Engine) SetAutoCompaction(ctx context.Context, enabled bool) error {
	return e.Call(ctx, "set_auto_compaction", map[string]any{"enabled": enabled}, nil, 20*time.Second)
}

// SetSessionName 改会话标题（走 RPC：引擎同时会更新文件头的标题槽）。
func (e *Engine) SetSessionName(ctx context.Context, name string) error {
	return e.Call(ctx, "set_session_name", map[string]any{"name": name}, nil, 30*time.Second)
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

// ------------- 读协程 -------------

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
		var head struct {
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		_ = json.Unmarshal(f.Raw, &head)
		if head.Message.Role == "user" {
			return // 用户消息由前端本地回显，不再回灌
		}
		e.pushUpdate(e.tr.Apply(f.Type, f.Raw))

	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		e.pushUpdate(e.tr.Apply(f.Type, f.Raw))

	case "turn_end":
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

	case "queue_update":
		e.SetQueue(f.Raw)

	case "auto_retry_start":
		var r struct {
			Attempt     int `json:"attempt"`
			MaxAttempts int `json:"maxAttempts"`
			DelayMS     int `json:"delayMs"`
		}
		_ = json.Unmarshal(f.Raw, &r)
		e.send("", "chat:retry", map[string]any{"attempt": r.Attempt, "max": r.MaxAttempts, "delay_ms": r.DelayMS})

	case "auto_retry_end":
		var r struct {
			Success bool `json:"success"`
		}
		_ = json.Unmarshal(f.Raw, &r)
		e.send("", "chat:retry_end", map[string]any{"success": r.Success})

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

// Translator 返回本实例的投影器（Dialect 用它处理私有帧的共用形态）。
func (e *Engine) Translator() *Translator { return e.tr }

// SetSessionID 记录当前会话 id（会话内切换时由引擎通知）。
func (e *Engine) SetSessionID(id string) {
	e.mu.Lock()
	e.session = id
	e.mu.Unlock()
}

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

// ------------- 小工具 -------------

func normalizeDir(dir string) string {
	if dir == "" {
		return dir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return strings.TrimRight(abs, `\/`)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	var ce *CallError
	if errors.As(err, &ce) {
		return ce.Message
	}
	var ae *pkg.AppError
	if errors.As(err, &ae) {
		if ae.Detail != "" {
			return ae.Message + "：" + ae.Detail
		}
		return ae.Message
	}
	return err.Error()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
