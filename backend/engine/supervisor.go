// 监督者：按 (引擎种类, 工作目录) 管理引擎实例，一个键一个进程。
// cwd 是进程级属性，共进程会让工具写到错的目录；键里带 Kind 是因为同一目录下
// omp 与 pi 是两个进程，用户在输入框里切引擎切的是「跟哪个进程说话」。
package engine

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
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
	// sessionFile 是当前会话文件的本地副本，由 State() 与会话绑定维护。
	// 它存在的唯一目的是让热路径不必为「现在哪个会话」发一次 RPC。
	sessionFile string
	runSeq      int
	runID       string
	booting     bool
	subs        SubagentStore

	// lastUsed 是最后一次有人跟这个引擎说话的时刻。
	// 它是回收的唯一依据：进程在不在跑不等于「有没有人在用」——
	// 一个刚发完消息、正在等下一句的引擎，进程是活的但下一秒就可能被用到。
	lastUsed time.Time

	sup     *Supervisor
	dialect Dialect
}

// touch 记一次使用。每个对外操作都要调，否则回收会把正在用的引擎停掉。
func (e *Engine) touch() {
	e.mu.Lock()
	e.lastUsed = time.Now()
	e.mu.Unlock()
}

// lastUse 返回最后使用时刻。
func (e *Engine) lastUse() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastUsed
}

// lastUseBefore 判断本引擎是否比 other 更久没被用过（排序用）。
func (e *Engine) lastUseBefore(other *Engine) bool {
	return e.lastUse().Before(other.lastUse())
}

// recyclable 判断这个引擎现在能不能安全回收。
// 两个否决条件：正在流式回答（会打断用户正在看的那一轮）、正在拉起进程（停掉会留下半启动状态的进程）。
func (e *Engine) recyclable() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runID == "" && !e.booting
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

// Live 表示进程当前活着，且不会为这个问题去拉起一个：它回答「现在跟它说话要不要先等几秒握手」。
// 读会话、列历史这类只读动作靠它决定「顺手问一次引擎」还是「退回文件投影」——
// 用 Client() 判断会顺手拉起进程，「点开历史会话」会变成「等 pi 起 Node」。
func (e *Engine) Live() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.proc == nil {
		return false
	}
	select {
	case <-e.proc.Closed():
		return false
	default:
		return true
	}
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
// 每一帧都带上 engine_kind 与 work_dir（两个引擎与多个工作目录共用同一条 SSE）：
// 不带的话旧实例仍在推的帧会被界面当成当前那个的——「切走之后旧会话的正文还在往新会话里跳」，前端按这两个字段丢弃不属于自己的帧。
func (e *Engine) send(session, event string, data any) {
	fn := e.sup.emitter()
	if fn == nil {
		return
	}
	stamp := e.origin()
	if stamp != nil {
		data = mergeOrigin(data, stamp)
	}
	fn(session, event, data)
}

// origin 返回本帧的身份标记（nil 表示不附）。
func (e *Engine) origin() map[string]any {
	out := map[string]any{"engine_kind": string(e.Kind)}
	if e.WorkDir != "" {
		out["work_dir"] = e.WorkDir
	}
	return out
}

// mergeOrigin 把身份标记并进载荷。
// 载荷绝大多数是 map[string]any（临时构造的），直接写；遇到其它类型（少数地方传结构体）就跳过——
// 宁可少两个字段，也不能因为「附不上标记」把整帧弄丢。
func mergeOrigin(data any, stamp map[string]any) any {
	m, ok := data.(map[string]any)
	if !ok || m == nil {
		return data
	}
	for k, v := range stamp {
		m[k] = v
	}
	return m
}

// Emit 暴露给 Dialect：私有帧要能复用同一条出口。
func (e *Engine) Emit(session, event string, data any) { e.send(session, event, data) }

// Supervisor 按 (Kind, WorkDir) 管理引擎实例：一个键一个进程。cwd 是进程级属性，共进程会让工具写到错的目录；
// 键里带 Kind 是因为同一目录下 omp 与 pi 是两个进程，用户在输入框里切引擎切的是「跟哪个进程说话」。
// idleTTL 是一个引擎进程空闲多久之后可以被回收：换一个目录就新建一个进程，没有回收会常驻一堆 node；取 10 分钟——切走再切回是常态，太激进会每次切回都重新握手。
const idleTTL = 10 * time.Minute

// maxEngines 是同时存活的引擎实例上限。
// TTL 是「温和的」回收（只清空闲的），上限是「硬的」兜底——快速切过八个目录时 TTL 还没到但进程已经堆起来，
// 这时按最久未用淘汰，保证进程数有界。
const maxEngines = 8

// sweepInterval 是回收扫描的间隔。
const sweepInterval = time.Minute

type Supervisor struct {
	mu      sync.Mutex
	engines map[string]*Engine
	emit    Emitter
	dialect map[Kind]Dialect
	resolve func(kind Kind) (string, []string, []string)
	newSubs func(kind Kind) SubagentStore

	// stop 停掉回收协程；done 等它退出。
	// 退出应用时要先停回收，否则它会与 StopAll 抢同一把锁、并试图停已经停掉的引擎。
	stop chan struct{}
	done chan struct{}
}

// NewSupervisor 构造监督者。resolve 在每次拉起进程时调用，让「换引擎 / 换路径」立刻生效。
func NewSupervisor(emit Emitter, dialects map[Kind]Dialect,
	resolve func(kind Kind) (string, []string, []string),
	newSubs func(kind Kind) SubagentStore) *Supervisor {
	s := &Supervisor{
		engines: map[string]*Engine{},
		emit:    emit,
		dialect: dialects,
		resolve: resolve,
		newSubs: newSubs,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go s.sweepLoop()
	return s
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
	if e, ok := s.engines[k]; ok {
		s.mu.Unlock()
		e.touch()
		return e
	}
	defer s.mu.Unlock()
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
		// 新建即视为「正在被用」：否则 Sweep 拿到零值时间会立刻把它回收掉，
		// 而调用方正拿着这个句柄要去拉进程。
		lastUsed: time.Now(),
	}
	if s.newSubs != nil {
		e.subs = s.newSubs(kind)
	}
	s.engines[k] = e
	// 建完就地做一次上限淘汰。不能只依赖外部调 Sweep：
	// 取引擎句柄本身就会新建实例（EngineFor 每切一次目录就新增一个键），
	// 而并发取几十次就能把实例数推得远超上限——压测里实测到 32 个。
	s.evictLocked()
	return e
}

// evictLocked 在持锁状态下把实例数压到上限以内。
// 只摘除「能安全停掉的」：正在回答（runID 非空）与正在启动（booting）的永不摘；能回收的不够就让它超过上限——不能打断用户正在看的那轮。
// 不在这里同步调 e.Stop()（关 stdin、等退出是秒级）：持着 s.mu 会堵住所有取引擎的调用，所以只从表里摘除，停进程异步做。
func (s *Supervisor) evictLocked() {
	if len(s.engines) <= maxEngines {
		return
	}
	candidates := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		if e.recyclable() {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		return
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastUse().Before(candidates[j].lastUse())
	})
	need := len(s.engines) - maxEngines
	if need > len(candidates) {
		need = len(candidates)
	}
	for _, e := range candidates[:need] {
		delete(s.engines, key(e.Kind, e.WorkDir))
		victim := e
		go victim.Stop()
	}
}

// Shutdown 停掉回收协程。退出应用前必须调，否则回收协程会与 StopAll 抢锁，
// 并试图停已经被停掉的引擎。
func (s *Supervisor) Shutdown() {
	select {
	case <-s.stop:
		return // 已经停过
	default:
	}
	close(s.stop)
	<-s.done
}

// StopAll 关闭所有引擎并清空实例表（退出应用 / 重启引擎前）。
// 清空实例表是关键：留着的话，这些句柄仍然持有已死进程的引用，map 本身也永远不会被回收。
func (s *Supervisor) StopAll() {
	all := s.running()
	s.stopAll(all)
	s.mu.Lock()
	for _, e := range all {
		delete(s.engines, key(e.Kind, e.WorkDir))
	}
	s.mu.Unlock()
}

// StopKind 只停某一种引擎（换版本 / 换路径后）。
// 停完要把它们从实例表里摘掉：这些句柄记录的是旧路径的进程，留着就是僵尸——下次按同一个键取出来的还是它。
func (s *Supervisor) StopKind(kind Kind) {
	all := s.running()
	hit := make([]*Engine, 0, 2)
	for _, e := range all {
		if e.Kind == kind {
			hit = append(hit, e)
		}
	}
	s.stopAll(hit)
	s.mu.Lock()
	for _, e := range hit {
		delete(s.engines, key(e.Kind, e.WorkDir))
	}
	s.mu.Unlock()
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
