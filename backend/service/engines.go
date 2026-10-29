package service

// 引擎状态：探测结果、落盘布局、重启。
//
// 探测不安装。缺引擎时这里只回答「缺什么」与「装它的命令原文」，
// 界面的其余部分照常可用。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
)

// ---------------- 引擎状态 ----------------

// EngineStatus 是「设置 · 引擎」页需要的全部事实。
type EngineStatus struct {
	Engines []EngineInfo      `json:"engines"`
	Running []engine.RunRow   `json:"running"`
	Install map[string]string `json:"install"`
	// Detecting 探测正在进行（含「从没探过」）
	//
	// 界面据此说「检测中」而不是「未安装」——两者都是「不知道」，
	// 但只有前者是对的：装没装要等探测说了算。
	Detecting bool `json:"detecting"`
}

// EngineInfo 是单个引擎的探测结果与落盘位置。
type EngineInfo struct {
	engine.Identity
	Installed bool        `json:"installed"`
	Layout    envLayoutVO `json:"layout"`
}

type envLayoutVO struct {
	Root     string `json:"root"`
	Agent    string `json:"agent"`
	Sessions string `json:"sessions"`
	Skills   string `json:"skills"`
	Plugins  string `json:"plugins"`
	Models   string `json:"models"`
	Settings string `json:"settings"`
	Exists   bool   `json:"exists"`
}

// EngineStatus 组装引擎状态。
func (a *App) EngineStatus(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, false)
	out := &EngineStatus{
		Engines:   []EngineInfo{},
		Running:   a.Engine.Running(),
		Install:   map[string]string{},
		Detecting: a.bgDetecting.Load() || a.DetectedAt() == 0,
	}
	if out.Running == nil {
		out.Running = []engine.RunRow{}
	}
	for _, kind := range engine.AllKinds() {
		id := a.Identity(kind)
		info := EngineInfo{Identity: id, Installed: id.Exe != ""}
		if layout, err := a.Layout(kind); err == nil {
			info.Layout = envLayoutVO{
				Root: layout.Root, Agent: layout.Agent, Sessions: layout.Sessions,
				Skills: layout.Skills, Plugins: layout.Plugins, Models: layout.Models,
				Settings: layout.Settings, Exists: layout.Exists(),
			}
		}
		out.Engines = append(out.Engines, info)
		out.Install[string(kind)] = engine.InstallHint(kind)
	}
	return out, nil
}

// DetectEngines 强制重新探测（设置页的「重新检测」）。
func (a *App) DetectEngines(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, true)
	return a.EngineStatus(ctx)
}

// RestartEngines 重启引擎（有进程在跑时停掉后重新拉起）。
func (a *App) RestartEngines(ctx context.Context, kind engine.Kind) error {
	if kind.Valid() {
		a.Engine.StopKind(kind)
		_, err := a.Engine.Ensure(ctx, kind, a.Workspace())
		return err
	}
	a.Engine.StopAll()
	for _, k := range engine.AllKinds() {
		if a.Identity(k).Exe == "" {
			continue
		}
		if _, err := a.Engine.Ensure(ctx, k, a.Workspace()); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- 引擎健康度 ----------------

// Health 组装两个引擎的健康快照。
//
// 它是概览页首屏，所以有一条硬约束：**不阻塞在子进程与外网上**。
// 三个可能慢的动作全部改成「先交旧值 / 交空值 + 后台补」：
//
//	tab: 探测（pi 要跑 --help，起 Node 实测 2.7 秒）
//	tab: 落盘计数（四类目录扫描，冷盘 1.6 秒）
//	tab: 上游最新版（两次外网请求，超时上限 4 秒）
//
// 三者叠加就是之前实测的 5.9 秒首屏。语义上它们都是「可以稍后知道」的事实，
// 而用户打开概览页的第一需求是「能不能开始干活」。
//
// refreshing 告诉界面「有后台补验在跑」——它据此显示一个不阻断操作的刷新指示，
// 而不是把整屏变成骨架屏。
func (a *App) Health(ctx context.Context) (*domain.HealthVO, error) {
	a.detectAll(ctx, false)
	latest := engine.QueryLatestFast(ctx)

	byKind := a.runningByKind()
	out := &domain.HealthVO{
		Engines:    []domain.EngineHealthVO{},
		ActiveKind: string(a.ActiveKind()),
		Workspace:  a.Workspace(),
		Refreshing: a.bgDetecting.Load(),
		// Detecting 覆盖两种「还不知道」：正在探，或从没探过。
		// 只报「正在探」的话，首发时（探测还没跑完）界面会显示成「没装」。
		Detecting:  a.bgDetecting.Load() || a.DetectedAt() == 0,
		DetectedAt: a.DetectedAt(),
	}
	kinds := engine.AllKinds()
	rows := make([]domain.EngineHealthVO, len(kinds))
	done := make(chan int, len(kinds))
	for i, k := range kinds {
		go func(i int, k engine.Kind) {
			rows[i] = a.healthOf(k, byKind[k], latest[k])
			done <- i
		}(i, k)
	}
	for range kinds {
		<-done
	}
	for _, row := range rows {
		out.Engines = append(out.Engines, row)
		out.TotalSessions += row.SessionCount
		out.TotalSkills += row.SkillCount
		out.TotalPlugins += row.PluginCount
		out.TotalMcp += row.McpCount
	}
	out.LiveEngines, out.MaxEngines = a.Engine.Stats()
	return out, nil
}

// runningByKind 把「在跑的实例」按引擎归并成一个代表行。
//
// 同一引擎可能在多个目录各有一个进程；健康卡只报「在跑」这一个结论，
// 所以取命令数最多的那个（它多半就是用户正在用的那个）。
func (a *App) runningByKind() map[engine.Kind]engine.RunRow {
	byKind := map[engine.Kind]engine.RunRow{}
	for _, row := range a.Engine.Running() {
		if prev, ok := byKind[row.Kind]; !ok || row.CallCount > prev.CallCount {
			byKind[row.Kind] = row
		}
	}
	return byKind
}

// healthCounts 是一份「落盘计数」快照。
//
// 为什么单独缓存：这四个数要扫目录（会话头、技能文件、插件清单、MCP 配置），
// 而它们的变化频率远低于查看频率——用户不会为了看到一个数字变化而反复刷新。
// 不缓存的话，每次 Health / Bootstrap 都重新扫一遍，
// 而概览页是首屏——用户看到的就是「打开应用卡一秒」。
type healthCounts struct {
	sessions int
	skills   int
	plugins  int
	mcp      int
	at       time.Time
}

// countsTTL 是计数快照的保鲜期。
//
// 短到足够跟上「刚装完插件 / 刚建了会话」，长到能把首屏的多个请求合成一次扫描。
const countsTTL = 30 * time.Second

// countSessions 数会话文件数量。
//
// 不调 Index.List：那会逐个读文件头解析标题与用量（本机 90 个会话冷启动要 1.5 秒，
// 而这里只需要一个数字）。概览页的「会话数」是一个指标，不是列表——
// 列表由会话页自己去取。
//
// 目录结构是 <会话目录>/<目录 slug>/<文件>.jsonl，只需两层 ReadDir，不读文件内容。
func (a *App) countSessions(kind engine.Kind) int {
	layout, err := a.Layout(kind)
	if err != nil {
		return 0
	}
	dirs, err := os.ReadDir(layout.Sessions)
	if err != nil {
		return 0
	}
	total := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(layout.Sessions, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl") {
				total++
			}
		}
	}
	return total
}

// countsOf 取一份计数快照。
//
// 三个纪律：
//  1. 四个数并发取（各扫不同目录，串行会把四段磁盘延迟相加）；
//  2. 结果通过 channel 回传，**不并发写同一个 struct**——
//     四个 goroutine 写四个字段在 Go 里是数据竞争，race detector 会直接报；
//  3. 不动盘时立即返回旧值 + 后台补，首屏不为一次目录扫描停顿。
func (a *App) countsOf(kind engine.Kind) healthCounts {
	a.countsMu.Lock()
	cached, ok := a.counts[kind]
	a.countsMu.Unlock()
	if ok && time.Since(cached.at) < countsTTL {
		return cached
	}

	// 过期或没有：交旧值（可能是零值），后台刷新。
	// 实测冷扫一次 pi 要 40 毫秒、omp 1 毫秒——但那是这台机器、这点数据量；
	// 用户有几千个会话时这一步就是秒级，而它只影响四个数字。
	if ok {
		a.refreshCountsAsync(kind)
		return cached
	}
	c := a.scanCounts(kind)
	a.storeCounts(kind, c)
	return c
}

// refreshCountsAsync 后台重扫一次计数（同一引擎同时只一颗）。
func (a *App) refreshCountsAsync(kind engine.Kind) {
	a.countsBusyMu.Lock()
	if a.countsBusy == nil {
		a.countsBusy = map[engine.Kind]bool{}
	}
	if a.countsBusy[kind] {
		a.countsBusyMu.Unlock()
		return
	}
	a.countsBusy[kind] = true
	a.countsBusyMu.Unlock()

	go func() {
		defer func() {
			a.countsBusyMu.Lock()
			delete(a.countsBusy, kind)
			a.countsBusyMu.Unlock()
		}()
		a.storeCounts(kind, a.scanCounts(kind))
	}()
}

// scanCounts 真正扫一遍四类目录。四个 goroutine 各自通过 channel 交结果。
func (a *App) scanCounts(kind engine.Kind) healthCounts {
	type slot struct {
		which int
		n     int
	}
	out := make(chan slot, 4)

	go func() { out <- slot{0, a.countSessions(kind)} }()
	go func() {
		if skills, err := a.Skills(kind); err == nil {
			out <- slot{1, len(skills)}
			return
		}
		out <- slot{1, 0}
	}()
	go func() {
		if plugins, err := a.pluginsOf(kind); err == nil && plugins != nil {
			out <- slot{2, len(plugins.Plugins)}
			return
		}
		out <- slot{2, 0}
	}()
	go func() {
		if mcp, err := a.McpList(context.Background(), kind, false); err == nil && mcp != nil {
			out <- slot{3, len(mcp.Servers)}
			return
		}
		out <- slot{3, 0}
	}()

	c := healthCounts{at: time.Now()}
	for i := 0; i < 4; i++ {
		s := <-out
		switch s.which {
		case 0:
			c.sessions = s.n
		case 1:
			c.skills = s.n
		case 2:
			c.plugins = s.n
		case 3:
			c.mcp = s.n
		}
	}
	return c
}

// storeCounts 写入一份快照。
func (a *App) storeCounts(kind engine.Kind, c healthCounts) {
	a.countsMu.Lock()
	a.counts[kind] = c
	a.countsMu.Unlock()
}

// invalidateCountsLater 在后台补验之后延迟丢掉计数缓存。
//
// 不立刻丢：刚探完的那一刻各引擎目录还没变，立即失效只会多扫一轮；
// 而计数缓存本身的 TTL 就是 countsTTL，等它自然过期即可。
// 这里只给「探测刚证明引擎换了版本」这一种情况留一个后手。
func (a *App) invalidateCountsLater() {
	go func() {
		time.Sleep(countsTTL / 2)
		a.InvalidateCounts()
	}()
}

// InvalidateCounts 丢掉计数缓存（装完插件 / 删了会话 / 换了工作目录之后）。
func (a *App) InvalidateCounts() {
	a.countsMu.Lock()
	a.counts = map[engine.Kind]healthCounts{}
	a.countsMu.Unlock()
}

// countsFor 返回某引擎的计数（兼容旧调用点）。
func (a *App) countsFor(kind engine.Kind) (sessions, skills, plugins, mcp int) {
	c := a.countsOf(kind)
	return c.sessions, c.skills, c.plugins, c.mcp
}
func (a *App) healthOf(kind engine.Kind, run engine.RunRow, latest engine.Latest) domain.EngineHealthVO {
	id := a.Identity(kind)
	row := domain.EngineHealthVO{
		EngineKind:    string(kind),
		EngineLabel:   kind.Label(),
		Installed:     id.Exe != "",
		ExePath:       id.Exe,
		Version:       id.Version,
		ChannelKind:   string(id.Channel),
		Caps:          capsToMap(id.Caps),
		ConfigFiles:   []domain.ConfigFileVO{},
		LatestVersion: latest.Version,
	}
	if row.ChannelKind == "" {
		row.ChannelKind = string(engine.ChannelUnknown)
	}
	row.ChannelLabel = channelLabel(engine.Channel(row.ChannelKind))
	if !row.Installed {
		row.InstallHint = engine.InstallHint(kind)
	}
	row.Outdated = id.Version != "" && latest.Version != "" &&
		compareVersion(id.Version, latest.Version) < 0

	row.Running = run.Running
	row.Busy = run.Busy
	row.PID = run.PID
	row.Protocol = run.Protocol
	row.StartedAt = run.StartedAt
	row.CallCount = run.CallCount
	row.LastError = run.LastError

	layout, err := a.Layout(kind)
	if err != nil {
		return row
	}
	row.RootDir = layout.Root
	row.AgentDir = layout.Agent
	row.DirExists = layout.Exists()
	row.ConfigFiles = a.configFileRows(kind)

	// 四个计数走缓存：它们要扫目录（会话头、技能、插件、MCP），
	// 而概览页是首屏，不该为此等一秒。
	sessions, skills, plugins, mcp := a.countsFor(kind)
	row.SessionCount = sessions
	row.SkillCount = skills
	row.PluginCount = plugins
	row.McpCount = mcp
	return row
}

// configFileRows 列出该引擎的配置文件现状。
func (a *App) configFileRows(kind engine.Kind) []domain.ConfigFileVO {
	vo, err := a.ConfigFiles(kind)
	if err != nil || vo == nil {
		return []domain.ConfigFileVO{}
	}
	return vo.Entries
}

// capsToMap 把能力表摊成 map：界面要按名字查，不想到处写结构体字段。
func capsToMap(c engine.Caps) map[string]bool {
	return map[string]bool{
		"subagents": c.Subagents, "goals": c.Goals, "todos": c.Todos,
		"login": c.Login, "fragments": c.Fragments, "queue_edit": c.QueueEdit,
		"queue_clear": c.QueueClear, "live_steer_count": c.LiveSteerCount,
		"skill_toggle": c.SkillToggle, "extensions": c.Extensions,
		"export_html": c.ExportHTML, "slash_commands": c.SlashCommands,
	}
}
