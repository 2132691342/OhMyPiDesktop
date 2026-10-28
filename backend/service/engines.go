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
	"sync"
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
		Engines: []EngineInfo{},
		Running: a.Engine.Running(),
		Install: map[string]string{},
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
// 每个引擎的四组事实（探测 / 进程 / 落盘计数 / 配置现状）并发取，
// 再合并：这一屏要读好几个目录，串行会让首屏明显卡一下。
//
// 最新版走快路径：有上次的结论就先用它、后台补查。
// 首屏不该为「上游发版了没」等四秒——那是应用页的主动行为。
func (a *App) Health(ctx context.Context) (*domain.HealthVO, error) {
	a.detectAll(ctx, false)
	running := a.Engine.Running()
	latest := engine.QueryLatestFast(ctx)

	byKind := map[engine.Kind]engine.RunRow{}
	for _, row := range running {
		// 同一引擎可能在多个目录各有一个进程；健康卡只报「在跑」这一个结论，
		// 所以取命令数最多的那个（它多半就是用户正在用的那个）。
		if prev, ok := byKind[row.Kind]; !ok || row.CallCount > prev.CallCount {
			byKind[row.Kind] = row
		}
	}

	out := &domain.HealthVO{
		Engines:    []domain.EngineHealthVO{},
		ActiveKind: string(a.ActiveKind()),
		Workspace:  a.Workspace(),
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
	out.DetectedAt = time.Now().UnixMilli()
	return out, nil
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

// countsOf 取一份计数快照，命中缓存则直接返回。
//
// 四个计数互相独立，并发取：它们各自扫不同目录，串行会把四段磁盘延迟相加。
// 冷文件系统上这一步是首屏最重的一段（本机实测：90 个会话文件 + 12 个插件
// 目录 + 9 个技能 = 1.6 秒），并行之后取最慢的那一项而不是四项相加。
func (a *App) countsOf(kind engine.Kind) healthCounts {
	a.countsMu.Lock()
	if c, ok := a.counts[kind]; ok && time.Since(c.at) < countsTTL {
		a.countsMu.Unlock()
		return c
	}
	a.countsMu.Unlock()

	c := healthCounts{at: time.Now()}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		c.sessions = a.countSessions(kind)
	}()
	go func() {
		defer wg.Done()
		if skills, err := a.Skills(kind); err == nil {
			c.skills = len(skills)
		}
	}()
	go func() {
		defer wg.Done()
		if plugins, err := a.pluginsOf(kind); err == nil && plugins != nil {
			c.plugins = len(plugins.Plugins)
		}
	}()
	go func() {
		defer wg.Done()
		if mcp, err := a.McpList(context.Background(), kind, false); err == nil && mcp != nil {
			c.mcp = len(mcp.Servers)
		}
	}()
	wg.Wait()

	a.countsMu.Lock()
	a.counts[kind] = c
	a.countsMu.Unlock()
	return c
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
