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
	// SkipWarm 不在装配时预热「上游最新版」。
	//
	// 预热会发外网请求并把结果写回设置文件，且没有任何同步等待点——
	// 测试里那会在 t.TempDir() 已经被回收之后才落盘，
	// 报一个与用例本身毫无关系的「directory is not empty」。
	// 生产路径永远不带这个，首屏的「先有个旧值」靠的就是它。
	SkipWarm bool
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
	if !opts.SkipWarm {
		engine.WarmLatest()
	}
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
