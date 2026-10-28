// Package service 是编排层：把引擎、会话索引、配置读写、设置 KV 组装成
// 界面直接可用的用例。它不含 HTTP，也不碰 Wails。
package service

import (
	"context"
	"os"
	"strings"
	"sync"
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
	id, err := env.Detect(a.Store.Get(domain.KeyEngineExePrefix+string(kind)), kind)
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

// detectAll 探测所有引擎并把结果记下来。fore 为真时忽略缓存。
//
// 两个引擎并发探测：它们互不依赖，串行只是让首屏多等一个 pi 的 Node 启动。
func (a *App) detectAll(ctx context.Context, fore bool) {
	a.mu.RLock()
	fresh := !fore && len(a.ids) == len(engine.AllKinds()) && time.Since(a.detectedAt) < detectTTL
	a.mu.RUnlock()
	if fresh {
		return
	}
	type result struct {
		kind engine.Kind
		id   engine.Identity
	}
	kinds := engine.AllKinds()
	ch := make(chan result, len(kinds))
	for _, kind := range kinds {
		go func(kind engine.Kind) {
			id, err := env.Detect(a.Store.Get(domain.KeyEngineExePrefix+string(kind)), kind)
			if err != nil || id.Exe == "" {
				id = engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}
			}
			ch <- result{kind: kind, id: id}
		}(kind)
	}
	a.mu.Lock()
	for range kinds {
		r := <-ch
		a.ids[r.kind] = r.id
	}
	a.detectedAt = time.Now()
	a.mu.Unlock()
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
