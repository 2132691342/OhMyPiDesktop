// Package engine 管理引擎进程与协议翻译：一个 (引擎种类, 工作目录) 一个进程。
//
// 本包不做的事：不下载 / 安装 / 升级引擎，不改 PATH，不写注册表。
// 缺引擎时只回答「缺什么」与「装它的命令原文」。
//
// 文件分工：
//   - supervisor.go 类型定义与监督者（按键管理实例、拉起、停止）
//   - process.go    单个引擎进程（启动、重启、发命令）
//   - session.go    会话相关命令（切换、新建、提问、排队、改设置）
//   - frames.go     读协程的帧回调与事件投递
package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

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

// Emitter 取出当前事件出口。
func (s *Supervisor) emitter() Emitter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emit
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

// RunRow 是一个在跑的引擎进程。
type RunRow struct {
	Kind      Kind   `json:"kind"`
	WorkDir   string `json:"work_dir"`
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
	Running   bool   `json:"running"`
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

// running 快照所有实例。Stop* 与 AnyRunning 都要同一份拷贝。
func (s *Supervisor) running() []*Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		out = append(out, e)
	}
	return out
}

// StopAll 关闭所有引擎（退出应用 / 重启引擎前）。
func (s *Supervisor) StopAll() {
	s.stopAll(s.running())
}

// StopKind 只停某一种引擎（换版本 / 换路径后）。
func (s *Supervisor) StopKind(kind Kind) {
	all := s.running()
	hit := make([]*Engine, 0, 2)
	for _, e := range all {
		if e.Kind == kind {
			hit = append(hit, e)
		}
	}
	s.stopAll(hit)
}

// stopAll 并发停一批实例：一个引擎的退出不该拖住另一个。
func (s *Supervisor) stopAll(list []*Engine) {
	var wg sync.WaitGroup
	for _, e := range list {
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
	for _, e := range s.running() {
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

// normalizeDir 归一目录：进程键与界面路径要能对齐，尾部分隔符不算差异。
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

// errText 取给用户看的错误原文：CallError 与 AppError 各自藏着引擎的原话。
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

// firstNonEmpty 取第一个非空串：两个引擎给同一语义填的字段名不一样。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// deref 取字符串指针的值，nil 当空串。
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
