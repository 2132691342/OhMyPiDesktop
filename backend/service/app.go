// Package service 是编排层：把引擎、会话索引、配置读写、设置 KV 组装成
// 界面直接可用的用例。它不含 HTTP，也不碰 Wails。
package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/engine/omp"
	"OhMyPiDesktop/backend/engine/pi"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/session"
)

// App 是用例入口。
type App struct {
	Store  *repo.Store
	Hub    *hub.Hub
	Engine *engine.Supervisor

	// OnWorkspaceChange 在当前工作目录切换后被调用。
	//
	// 托盘菜单里有一支「最近工作目录」，它必须跟着变；但 service 不认识窗口，
	// 所以只留一个回调——不回调就等于菜单与设置页各说一套话。
	OnWorkspaceChange func()

	mu         sync.RWMutex
	profile    string
	workspace  string
	active     engine.Kind
	indexes    map[engine.Kind]*session.Index
	ids        map[engine.Kind]engine.Identity
	detectedAt time.Time
	appDir     string

	// detectMu 是探测的单飞锁：首屏三个请求同时到达时只探测一轮。
	// 它与 a.mu 分开：探测要起子进程（几秒），持 a.mu 会把读缓存的人也堵住。
	detectMu sync.Mutex
	// detectWait 在每轮探测开始时换上新的、结束时关掉。
	//
	// 它给「愿意等」的调用方一个正确的等待点。没有它，调用方只能靠
	// SSE 上那帧 engine:detected 得知探测完了——而那一帧可能发在还没有
	// 订阅者的时刻（Bootstrap 的探测早于 SSE 连接），于是被静默丢弃，
	// 前端再也不会刷新，概览页就停在「未安装」不动。
	detectWait chan struct{}
	// bgDetecting 保证同一时刻只有一颗后台补验在跑。
	bgDetecting atomic.Bool

	// countsMu / counts 是「落盘计数」的短 TTL 缓存（概览页首屏用）。
	countsMu sync.Mutex
	counts   map[engine.Kind]healthCounts
	// probeReason 是「为什么没探到」的逐引擎答案（探到了就是空串）。
	//
	// 它与 ids 一起写、一起读：只留结论不留原因，界面就只能把「超时没探出来」
	// 说成「没装」，而这两件事的下一步完全相反。
	probeReason map[engine.Kind]string

	// countsBusy 保证同一引擎同时只有一颗后台重扫在跑。
	countsBusyMu sync.Mutex
	countsBusy   map[engine.Kind]bool

	Version string
}

// Options 是构造参数。
type Options struct {
	Store   *repo.Store
	Hub     *hub.Hub
	DataDir string
	Version string
}

// New 装配用例层并恢复上次的设置。
func New(opts Options) (*App, error) {
	app := &App{
		Store:   opts.Store,
		Hub:     opts.Hub,
		Version: opts.Version,
		appDir:  opts.DataDir,
		indexes: map[engine.Kind]*session.Index{},
		ids:     map[engine.Kind]engine.Identity{},
		counts:  map[engine.Kind]healthCounts{},
	}
	app.profile = app.Store.Get(domain.KeyProfile)
	app.workspace = app.Store.Get(domain.KeyWorkspace)
	if app.workspace == "" {
		if home, err := os.UserHomeDir(); err == nil {
			app.workspace = home
		}
	}
	app.active = engine.Kind(app.Store.Get(domain.KeyActiveEngine))
	if !app.active.Valid() {
		app.active = engine.KindOMP
	}
	app.Engine = engine.NewSupervisor(
		app.Hub.Emit,
		map[engine.Kind]engine.Dialect{
			engine.KindOMP: omp.Dialect{},
			engine.KindPi:  pi.Dialect{},
		},
		app.resolveEngine,
		app.newSubagentStore,
	)
	app.refreshIndexes()
	// 先用上次的探测结果填一下：首屏不必为空等一次 Node 启动，
	// 而真正的判定由第一次 detectAll 覆盖（它的结果比缓存权威）。
	app.loadCachedIdentities()
	// 最新版查询同理：它要发外网请求，首屏不该等它。
	// 先接上落盘回调，再把上次的结论读回来。
	app.wireLatestPersistence()
	app.loadCachedLatest()
	// 首次启动时磁盘上没有任何结论：后台预热一次，
	// 用户进概览页时通常已经有值了（而这条路径本身不等网络）。
	engine.WarmLatest()
	// 探测推迟到第一次 Bootstrap：装配阶段不该等两个子进程，
	// 而「引擎装在哪」只在界面要显示时才需要。
	return app, nil
}

// resolveEngine 是监督者每次拉进程时问的「用哪份引擎、带什么参数」。
//
// 每次现问而不是缓存：换路径、换 profile 之后立刻生效，不必重启应用。
func (a *App) resolveEngine(kind engine.Kind) (string, []string, []string) {
	a.mu.RLock()
	envForKind := a.envForLocked(kind)
	a.mu.RUnlock()
	id, err := env.Detect(context.Background(), a.Store.Get(domain.KeyEngineExePrefix+string(kind)), kind)
	if err != nil || id.Exe == "" {
		return "", nil, envForKind
	}
	return id.Exe, nil, envForKind
}

// envForLocked 返回该引擎需要的环境变量。profile 只有 omp 认。
func (a *App) envForLocked(kind engine.Kind) []string {
	if kind != engine.KindOMP {
		return nil
	}
	if a.profile == "" || a.profile == "default" {
		return nil
	}
	return []string{"OMP_PROFILE=" + a.profile}
}

// newSubagentStore 给 omp 造观测台状态；pi 没有子代理，返回 nil。
func (a *App) newSubagentStore(kind engine.Kind) engine.SubagentStore {
	if kind != engine.KindOMP {
		return nil
	}
	return omp.NewRoster()
}

// detectTTL 是探测结果的保鲜期。
//
// 探测要起子进程：omp 的 --version 几十毫秒，pi 的 --help 要起一次 Node（约 3 秒）。
// 每次 Bootstrap 都真探一遍会让首屏慢三秒，而「引擎装在 PATH 上」这件事
// 一分钟内不会变。所以默认走缓存，只有显式「重新检测」才强制。
const detectTTL = time.Minute

// detectFunc 是探测一个引擎的实际动作。
//
// 抽成变量是为了让测试不必依赖「这台机器上装了什么」：
// 探测要起子进程（pi 的 --help 要起一次 Node，几秒），而单元测试
// 断言的是「响应形态对不对」，不是「机器上有没有装 pi」。
//
// 返回值第二项是「为什么没探到」（见 env.DetectReason）：空串表示结论可信。
var detectFunc = env.DetectReason

// detectResult 是一颗探测的产出：结论 + 「没探到」的原因。
type detectResult struct {
	id     engine.Identity
	reason string
}

// detectAll 探测所有引擎并把结果记下来。fore 为真时忽略缓存。
//
// 两个引擎并发探测：它们互不依赖，串行只是让首屏多等一个 pi 的 Node 启动。
//
// 并发去重：首屏会同时打 Bootstrap / Health / EngineStatus，三个请求都
// 发现缓存过期就各自起一轮探测——机器上就多出六个子进程，而它们查的是同一件事。
// 用一个「探测中就等它」的锁把它们合成一次。
//
// 阻塞策略（实测数据：pi 的 --help 起一次 Node 要 3.6 秒，而 Health 本身只要 0.5 毫秒）：
//
//	tab: 有落盘结论 → 立即返回 + 后台补验。首屏永远不吃这 3.6 秒。
//	tab: 啥都没有（首发）→ 不等。立即交空值 + 后台探，界面显示「正在检测」
//	tab: 而不是白屏 3.6 秒。
//	tab: fore（用户点「重新检测」）→ 这次真的等，因为用户就是来看结果的。
func (a *App) detectAll(ctx context.Context, fore bool) {
	if a.detectFresh(fore) {
		return
	}
	if !fore {
		// 首屏路径：永不阻塞。有旧值用旧值，没旧值就空着，两者都后台补验。
		a.detectInBackground()
		return
	}
	a.detectMu.Lock()
	if a.detectFresh(fore) {
		a.detectMu.Unlock()
		return
	}
	a.openDetectWait()
	defer close(a.detectWait)
	a.detectOnce(ctx)
	a.detectMu.Unlock()
}

// openDetectWait 为本轮探测换一个新的等待点。
//
// 每轮都新建一个 channel 而不是复用并清空：在关掉与重开之间
// 阻塞着的调用方要保证能确切地收到「这一轮结束了」，复用会让他们
// 要么错过唤醒，要么被下一轮提前唤醒。
func (a *App) openDetectWait() {
	a.mu.Lock()
	a.detectWait = make(chan struct{})
	a.mu.Unlock()
}

// DetectWait 返回本轮探测的等待点（可能已经关掉 = 探测已完成）。
func (a *App) DetectWait() <-chan struct{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.detectWait == nil {
		// 从未探测过：给一个已关闭的，等它等于不等。
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return a.detectWait
}

// detectInBackground 在后台补一次真探测，不阻塞调用方。
//
// 三个限制：只一颗在跑（bgDetecting）、不重复排队、失败不重试——
// 失败时界面用已有的落盘结果继续工作，下次请求自然会再试。
func (a *App) detectInBackground() {
	if !a.bgDetecting.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.bgDetecting.Store(false)
		a.detectMu.Lock()
		defer a.detectMu.Unlock()
		a.openDetectWait()
		defer close(a.detectWait)
		// 后台补验用不带超时的 context：请求自己的 ctx 会随响应结束而取消，
		// 而探测结果是要落盘的，不该被一个客户端断开打断。
		//
		// 但探测命令本身仍有上限（engine.ProbeTimeout）：那是给「候选卡死」
		// 兜底的，与「客户端断开」不是一回事。
		a.detectOnce(context.Background())
		a.invalidateCountsLater()
	}()
}

// HasCachedIdentity 报告是否已有可展示的探测结论。
//
// 它跟 refreshing 一起回答「界面上这个版本号是不是刚验过的」。
// 首发（还没探过）时返回 false，界面据此说「正在检测」而不是「未安装」。
func (a *App) HasCachedIdentity() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, kind := range engine.AllKinds() {
		if id, ok := a.ids[kind]; ok && id.Exe != "" {
			return true
		}
	}
	return false
}

// detectFresh 判断现有结果是否还新。
//
// 判据不能用「ids 里有几个引擎」：没装的引擎不会有可执行文件，
// 用它做判据会让「装了一个、缺一个」的机器永远被判成不新鲜——
// 于是每次 Health 都重新探测（本机实测 1.6 秒），而缓存其实好好的。
//
// 正确判据：上次探测发生过（detectedAt 非零）且未过期。
func (a *App) detectFresh(fore bool) bool {
	if fore {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.detectedAt.IsZero() && time.Since(a.detectedAt) < detectTTL
}

// detectOnce 实际探测一轮并写入结果。调用方持有 detectMu。
func (a *App) detectOnce(ctx context.Context) {
	type result struct {
		kind   engine.Kind
		id     engine.Identity
		reason string
	}
	kinds := engine.AllKinds()
	ch := make(chan result, len(kinds))
	for _, kind := range kinds {
		go func(kind engine.Kind) {
			id, reason := detectFunc(ctx, a.Store.Get(domain.KeyEngineExePrefix+string(kind)), kind)
			if id.Exe == "" {
				// 没装不是「不知道」：记下空 Exe 的条目。
				// 不记的话 detectFresh 永远认为「有引擎还没探过」，
				// 于是每次请求都重探一遍。
				id = engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}
			}
			ch <- result{kind: kind, id: id, reason: reason}
		}(kind)
	}
	// 先把两个结果收齐，再持锁写入。
	//
	// 这一顺序是硬要求：之前是「持写锁等 channel」，于是探测期间（pi 的
	// --help 要起 Node，实测 3.6 秒）所有读 ids / workspace / index 的调用
	// 全部堵在 RLock 上——界面上表现就是「打开应用卡四秒」，
	// 而日志里探测明明已经改成后台了。
	collected := make(map[engine.Kind]engine.Identity, len(kinds))
	reasons := make(map[engine.Kind]string, len(kinds))
	for range kinds {
		r := <-ch
		collected[r.kind] = r.id
		reasons[r.kind] = r.reason
	}

	a.mu.Lock()
	for k, v := range collected {
		a.ids[k] = v
	}
	a.probeReason = reasons
	a.detectedAt = time.Now()
	snapshot := make(map[engine.Kind]engine.Identity, len(a.ids))
	for k, v := range a.ids {
		snapshot[k] = v
	}
	a.mu.Unlock()
	// 探测要起子进程（pi 的 --help 要起一次 Node，约 2.7 秒），
	// 而结果只会在「用户换了引擎路径或版本」时变。落盘之后，
	// 下次启动可以直接用上次的结论，首屏不必再等这三秒。
	a.persistIdentities(snapshot)
	// 探完了就说一声。界面据此把「正在检测」换成真结果——
	// 之前只有界面自己去轮询，于是概览页显示的是探测前的旧值，
	// 而用户看到的是「版本号是旧的」，却没有任何提示说它正在更新。
	a.emitDetected()
}

// emitDetected 广播探测完成。
//
// 事件不带结论本身：结论由界面自己去拉一次 /engines。
// 理由是探测结果会落盘并被各处缓存，多一份广播就要多一处可能漂移的真相。
func (a *App) emitDetected() {
	if a.Hub == nil {
		return
	}
	a.Hub.Emit("", "engine:detected", map[string]any{"at": time.Now().UnixMilli()})
}

// DetectedAt 返回上次探测完成的时刻；从未探过时返回 0。
//
// 它回答「屏幕上这个版本号是什么时候验的」，所以必须是真实完成时刻，
// 不能是「本次响应的时刻」——后者会让界面永远显示「刚刚检测过」。
func (a *App) DetectedAt() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.detectedAt.IsZero() {
		return 0
	}
	return a.detectedAt.UnixMilli()
}

// SetDetector 换掉探测引擎的动作，返回恢复用的函数。
//
// 只有测试用。生产代码走默认的 env.DetectReason。
//
// 签名带 ctx 与 reason：探测的真实成本（起子进程）与「为什么没探到」
// 都是被测行为的一部分，去掉任一个测试就只能测出一个假的形态。
func SetDetector(fn func(context.Context, string, engine.Kind) (engine.Identity, string)) func() {
	prev := detectFunc
	detectFunc = fn
	return func() { detectFunc = prev }
}

// refreshIndexes 按每个引擎的会话目录重建索引。
func (a *App) refreshIndexes() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.indexes = map[engine.Kind]*session.Index{}
	for _, kind := range engine.AllKinds() {
		layout, err := env.ResolveLayout(kind, a.profile)
		if err != nil {
			continue
		}
		a.indexes[kind] = session.NewIndex(layout.Sessions, kind)
	}
}

// Profile 返回当前 profile 名。
func (a *App) Profile() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.profile
}

// Workspace 返回当前工作目录。
func (a *App) Workspace() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.workspace
}

// ActiveKind 返回当前选中的引擎。
func (a *App) ActiveKind() engine.Kind {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.active
}

// SetActive 切当前引擎。切引擎不切目录、不切会话——只换「跟哪个进程说话」。
func (a *App) SetActive(kind engine.Kind) error {
	if !kind.Valid() {
		return pkg.New(pkg.CodeBadRequest, "不认识的引擎："+string(kind), "")
	}
	if err := a.Store.Set(domain.KeyActiveEngine, string(kind)); err != nil {
		return err
	}
	a.mu.Lock()
	a.active = kind
	a.mu.Unlock()
	return nil
}

// KindOr 把可能为空的引擎名规范成有效值（空 = 当前选中的）。
func (a *App) KindOr(name string) engine.Kind {
	if name == "" {
		return a.ActiveKind()
	}
	k := engine.Kind(name)
	if !k.Valid() {
		return a.ActiveKind()
	}
	return k
}

// Index 返回某引擎的会话索引。
func (a *App) Index(kind engine.Kind) *session.Index {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if ix, ok := a.indexes[kind]; ok {
		return ix
	}
	layout, err := env.ResolveLayout(kind, a.profile)
	if err != nil {
		return session.NewIndex("", kind)
	}
	return session.NewIndex(layout.Sessions, kind)
}

// EngineFor 返回某引擎在某目录（默认当前）的引擎实例。
func (a *App) EngineFor(kind engine.Kind, dir string) *engine.Engine {
	if strings.TrimSpace(dir) == "" {
		dir = a.Workspace()
	}
	return a.Engine.Engine(kind, dir)
}

// Layout 返回某引擎的落盘布局。
func (a *App) Layout(kind engine.Kind) (env.Layout, error) {
	return env.ResolveLayout(kind, a.Profile())
}

// Identity 返回某引擎的探测结果。
func (a *App) Identity(kind engine.Kind) engine.Identity {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if id, ok := a.ids[kind]; ok {
		return id
	}
	return engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}
}

// ProbeReason 返回「为什么没探到这个引擎」的答案。
//
// 空串表示这一轮的结论可信（installed 说的是真的）。
// 非空只有三种：missing / bad-path / timeout，见 env.DetectReason。
func (a *App) ProbeReason(kind engine.Kind) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if id, ok := a.ids[kind]; ok && id.Exe != "" {
		return "" // 探到了，没什么要解释的
	}
	return a.probeReason[kind]
}
