// Package service 是编排层：把引擎、会话索引、配置读写、设置 KV 组装成
// 界面直接可用的用例。它不含 HTTP，也不碰 Wails。
package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
	"OhMyPiDesktop/backend/skill"
)

// App 是用例入口。
type App struct {
	Store  *repo.Store
	Hub    *hub.Hub
	Engine *engine.Supervisor

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

// ---------------- 启动引导 ----------------

// Bootstrap 是首屏一次拉齐的全部内容。
type Bootstrap struct {
	Version    string                 `json:"version"`
	Engines    []engine.Identity      `json:"engines"`
	Caps       map[string]engine.Caps `json:"caps"`
	Active     engine.Kind            `json:"active"`
	Workspace  string                 `json:"workspace"`
	Profile    string                 `json:"profile"`
	Settings   map[string]string      `json:"settings"`
	Layouts    map[string]env.Layout  `json:"layouts"`
	Workspaces []domain.WorkspaceRow  `json:"workspaces"`
	Skills     map[string]int         `json:"skills"`
	Sessions   map[string]int         `json:"sessions"`
	Install    map[string]string      `json:"install"`
}

// Bootstrap 组装首屏数据。
func (a *App) Bootstrap(ctx context.Context) (*Bootstrap, error) {
	settings, err := a.Store.All()
	if err != nil {
		return nil, err
	}
	a.detectAll(ctx, false)
	b := &Bootstrap{
		Version:    a.Version,
		Active:     a.ActiveKind(),
		Workspace:  a.Workspace(),
		Profile:    a.Profile(),
		Settings:   settings,
		Engines:    []engine.Identity{},
		Caps:       map[string]engine.Caps{},
		Layouts:    map[string]env.Layout{},
		Skills:     map[string]int{},
		Sessions:   map[string]int{},
		Install:    map[string]string{},
		Workspaces: []domain.WorkspaceRow{},
	}
	if b.Settings == nil {
		b.Settings = map[string]string{}
	}
	allSessions := make([]session.Session, 0, 32)
	for _, kind := range engine.AllKinds() {
		id := a.Identity(kind)
		b.Engines = append(b.Engines, id)
		b.Caps[string(kind)] = id.Caps
		b.Install[string(kind)] = engine.InstallHint(kind)
		if layout, err := a.Layout(kind); err == nil {
			b.Layouts[string(kind)] = layout
		}
		if list, err := a.Index(kind).List(""); err == nil {
			b.Sessions[string(kind)] = len(list)
			allSessions = append(allSessions, list...)
		}
		if skills, err := a.Skills(kind); err == nil {
			b.Skills[string(kind)] = len(skills)
		}
	}
	b.Workspaces, _ = a.RecentWorkspaces(allSessions)
	return b, nil
}

// workspacesFrom 从两侧会话列表推出最近用过的工作目录。
func (a *App) workspacesFrom(sessions []session.Session) []domain.WorkspaceRow {
	seen := map[string]domain.WorkspaceRow{}
	for _, s := range sessions {
		dir := s.CWD
		if dir == "" {
			continue
		}
		key := strings.ToLower(dir)
		if existing, ok := seen[key]; ok {
			if s.UpdatedAt.Format(time.RFC3339) > existing.UpdatedAt {
				existing.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
				seen[key] = existing
			}
			continue
		}
		_, statErr := os.Stat(dir)
		row := domain.WorkspaceRow{WorkspacePath: dir, Exists: statErr == nil}
		if !s.UpdatedAt.IsZero() {
			row.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
		}
		seen[key] = row
	}
	rows := make([]domain.WorkspaceRow, 0, len(seen))
	for _, r := range seen {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt > rows[j].UpdatedAt })
	if len(rows) > 12 {
		rows = rows[:12]
	}
	return rows
}

// ---------------- 设置 ----------------

// Settings 返回全部设置。
func (a *App) Settings() (map[string]string, error) { return a.Store.All() }

// SaveSettings 批量保存设置，并在必要时重建索引。
func (a *App) SaveSettings(kv map[string]string) error {
	if err := a.Store.SetMany(kv); err != nil {
		return err
	}
	a.mu.Lock()
	profileChanged := false
	if v, ok := kv[domain.KeyProfile]; ok && v != a.profile {
		a.profile = v
		profileChanged = true
	}
	if v, ok := kv[domain.KeyWorkspace]; ok && v != "" {
		a.workspace = v
	}
	if v, ok := kv[domain.KeyActiveEngine]; ok && engine.Kind(v).Valid() {
		a.active = engine.Kind(v)
	}
	a.mu.Unlock()

	if profileChanged {
		a.refreshIndexes()
	}
	// 引擎路径可能变了：重新探测，让界面与下次拉进程都用新路径。
	for _, kind := range engine.AllKinds() {
		if _, ok := kv[domain.KeyEngineExePrefix+string(kind)]; ok {
			a.detectAll(context.Background(), true)
			break
		}
	}
	return nil
}

// SetWorkspace 切工作目录。
func (a *App) SetWorkspace(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return pkg.New(pkg.CodeBadRequest, "工作目录不能为空", "")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return pkg.Wrap(pkg.CodeBadRequest, "工作目录不合法", err)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return pkg.New(pkg.CodeNotFound, "这个目录不存在："+abs, "")
	}
	if err := a.Store.Set(domain.KeyWorkspace, abs); err != nil {
		return err
	}
	a.mu.Lock()
	a.workspace = abs
	a.mu.Unlock()
	// 切过的目录要记住：下次打开时能一键回去，不必重新找路径。
	// 记录失败不拦切换——「没记住」比「切不过去」轻得多。
	if err := a.rememberWorkspace(abs); err != nil {
		pkg.Warnf("app: 记不住这个工作目录：%v", err)
	}
	return nil
}

// RememberWorkspace 把一个目录提到「最近」的最前面。
func (a *App) rememberWorkspace(abs string) error {
	rows, err := a.recentWorkspaces()
	if err != nil {
		rows = nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	out := make([]domain.WorkspaceRow, 0, len(rows)+1)
	out = append(out, domain.WorkspaceRow{WorkspacePath: abs, Exists: true, UpdatedAt: now})
	for _, r := range rows {
		if strings.EqualFold(r.WorkspacePath, abs) {
			continue
		}
		out = append(out, r)
	}
	return a.saveRecentWorkspaces(out)
}

// recentWorkspaces 读最近列表，丢掉已经不存在的目录。
//
// 不存在的目录直接丢而不是标灰：拔掉的 U 盘、删掉的项目不该永久占着列表，
// 而用户想回去时会发现它「还在但点不动」——那更糟。
func (a *App) recentWorkspaces() ([]domain.WorkspaceRow, error) {
	raw := a.Store.Get(domain.KeyRecentWorkspaces)
	if raw == "" {
		return nil, nil
	}
	var rows []domain.WorkspaceRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		// 这个键坏了不影响别的功能：当作没有历史，让写的时候覆盖掉。
		pkg.Warnf("app: 最近工作目录无法解析，按空处理：%v", err)
		return nil, nil
	}
	out := make([]domain.WorkspaceRow, 0, len(rows))
	for _, r := range rows {
		if r.WorkspacePath == "" {
			continue
		}
		if st, err := os.Stat(r.WorkspacePath); err != nil || !st.IsDir() {
			continue
		}
		r.Exists = true
		out = append(out, r)
	}
	return out, nil
}

func (a *App) saveRecentWorkspaces(rows []domain.WorkspaceRow) error {
	if len(rows) > domain.RecentWorkspaceMax {
		rows = rows[:domain.RecentWorkspaceMax]
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "最近工作目录无法序列化", err)
	}
	if err := a.Store.Set(domain.KeyRecentWorkspaces, string(raw)); err != nil {
		return err
	}
	return nil
}

// ForgetWorkspace 从「最近」里去掉一个目录。
func (a *App) ForgetWorkspace(dir string) ([]domain.WorkspaceRow, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "工作目录不能为空", "")
	}
	rows, _ := a.recentWorkspaces()
	out := make([]domain.WorkspaceRow, 0, len(rows))
	for _, r := range rows {
		if strings.EqualFold(r.WorkspacePath, dir) {
			continue
		}
		out = append(out, r)
	}
	if err := a.saveRecentWorkspaces(out); err != nil {
		return nil, err
	}
	return out, nil
}

// RecentWorkspaces 是「最近 + 当前会话推出来的目录」的并集。
//
// 两份来源都要：记住的列表能立刻显示（不用扫盘），
// 而从会话推出来的能捞到「在别的机器上聊过、这里还没切过」的目录。
func (a *App) RecentWorkspaces(allSessions []session.Session) ([]domain.WorkspaceRow, error) {
	rows, _ := a.recentWorkspaces()
	seen := map[string]bool{}
	for _, r := range rows {
		seen[strings.ToLower(r.WorkspacePath)] = true
	}
	for _, r := range a.workspacesFrom(allSessions) {
		if seen[strings.ToLower(r.WorkspacePath)] {
			continue
		}
		rows = append(rows, r)
	}
	if len(rows) > domain.RecentWorkspaceMax {
		rows = rows[:domain.RecentWorkspaceMax]
	}
	if rows == nil {
		rows = []domain.WorkspaceRow{}
	}
	return rows, nil
}

// ---------------- 技能 ----------------

// Skills 列出某引擎会加载的技能。
func (a *App) Skills(kind engine.Kind) ([]skill.Skill, error) {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	roots := skill.Roots{
		Kind:        kind,
		UserDir:     layout.Skills,
		ManagedDir:  layout.Managed,
		ProjectDir:  layout.ProjectDir,
		ProjectDirs: a.projectSkillDirs(layout.ProjectDir),
		PluginDirs:  a.pluginSkillDirs(layout),
	}
	return skill.Scan(roots)
}

// projectSkillDirs 找出工作目录到仓库边界上每一级的项目级技能目录。
func (a *App) projectSkillDirs(projectDir string) []string {
	dir := a.Workspace()
	var out []string
	for i := 0; i < 8 && dir != ""; i++ {
		candidate := filepath.Join(dir, projectDir, "skills")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			out = append(out, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// pluginSkillDirs 找出已装包自带的 skills 目录。
//
// 两个引擎的包根不同（omp: ~/.omp/plugins/node_modules；pi: ~/.pi/agent/npm），
// 但都是 node_modules 布局，所以扫描逻辑共用。
func (a *App) pluginSkillDirs(layout env.Layout) []string {
	roots := []string{
		filepath.Join(layout.Plugins, "node_modules"),
		layout.Plugins,
	}
	var out []string
	seen := map[string]bool{}
	for _, nodeModules := range roots {
		entries, err := os.ReadDir(nodeModules)
		if err != nil {
			continue
		}
		walk := func(dir string) {
			sub, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range sub {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				cand := filepath.Join(dir, e.Name(), "skills")
				if st, err := os.Stat(cand); err == nil && st.IsDir() && !seen[cand] {
					seen[cand] = true
					out = append(out, cand)
				}
			}
		}
		walk(nodeModules)
		// scoped 包：node_modules/@scope/pkg/skills
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "@") {
				walk(filepath.Join(nodeModules, e.Name()))
			}
		}
	}
	return out
}
