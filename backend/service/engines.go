package service

// 引擎状态：探测结果、落盘布局、重启、健康快照。
//
// 探测不安装。缺引擎时这里只回答「缺什么」与「装它的命令原文」，
// 界面的其余部分照常可用。
//
// 本文件不做什么：不扫目录计四个数（见 counts.go）、不发外网请求（见 engine/latest.go）。

import (
	"context"

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
	// DetectedAt 上次探测完成的时刻（Unix 毫秒）；从没探过时为 0。
	//
	// 它是「要不要再拉一次」的判据。engine:detected 那一帧可能丢——
	// Bootstrap 触发的那轮探测可能跑完在 SSE 连接建立之前，
	// 帧发在没有订阅者的总线上就被静默丢弃。时间戳是存在后端的事实，
	// 不依赖任何一帧有没有送到。
	DetectedAt int64 `json:"detected_at"`
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
//
// 它会顺手催一次探测（缓存不新时后台补），因为这是设置页与缺引擎横幅
// 拿状态的入口——那些场景下用户期望的就是「给我最新的」。
func (a *App) EngineStatus(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, false)
	return a.engineSnapshot(), nil
}

// EngineSnapshot 只读当前已知的引擎状态，**不触发任何探测**。
//
// 为什么单独一个：SSE 连接建立时要补一帧状态，而那件事
// 不能顺手催一次探测——探测会起子进程、会写缓存，
// 而客户端随时可能在下一毫秒断开。更实际的是：连接建立时
// 连「探测该不该跑」都还没确定（可能刚跑完）。
//
// 它只回答「后端现在知道什么」，不知道的部分交给之后那一帧
// engine:detected——那才是探测真出结果的地方。
func (a *App) EngineSnapshot() *EngineStatus {
	return a.engineSnapshot()
}

// engineSnapshot 是 EngineStatus 与 EngineSnapshot 共用的组装体。
func (a *App) engineSnapshot() *EngineStatus {
	out := &EngineStatus{
		Engines:    []EngineInfo{},
		Running:    a.Engine.Running(),
		Install:    map[string]string{},
		Detecting:  a.bgDetecting.Load() || a.DetectedAt() == 0,
		DetectedAt: a.DetectedAt(),
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
	return out
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
// 它是概览页首屏，硬约束是不阻塞在子进程与外网上：探测、落盘计数、上游最新版
// 都改成「先交旧值 / 空值 + 后台补」。refreshing 告诉界面「有后台补验在跑」，
// 据此显示不阻断操作的刷新指示，而不是骨架屏。
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
		// 四个合计各自「有没有哪一项是没读到的」。
		//
		// 未知的那一格贡献了 0，于是合计是一个偏小的真话而不是假话——
		// 但界面得知道它是偏小的，否则「12 个技能」少了 pi 那一份也没人说。
		if !row.SessionCountKnown || !row.SkillCountKnown ||
			!row.PluginCountKnown || !row.McpCountKnown {
			out.TotalsPartial = true
		}
	}
	out.LiveEngines, out.MaxEngines = a.Engine.Stats()
	// Ready 的判据只有两条：探过了，且没有后台补验在跑。
	//
	// 不看 counts 的 age：计数有 30 秒缓存，但它是一份「已完成的观测」，
	// 而不是「还在算」。把它算进来会让 Ready 在没任何变化的情况下自己变假。
	out.Ready = !out.Detecting && !out.Refreshing
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

// healthOf 组装单个引擎的健康行。
//
// 这里的每一条都是在回答「界面上这一句话说错了会被当成什么」：
// 「已安装 · 0 个会话」在磁盘上躺着九十个会话时会让人不再相信任何一个数字；
// 「未安装」在一台其实装好的机器上会让人去装第二遍。
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
		row.ProbeUnknownReason = a.ProbeReason(kind)
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

	// 布局解不出来时不能提前返回：否则四个计数也被一起留在零值，
	// 界面会显示「已安装 · 0 个会话」而磁盘上其实有九十个会话。
	// 布局与计数是两条独立证据链，各解各的。
	if layout, err := a.Layout(kind); err == nil {
		row.LayoutResolved = true
		row.RootDir = layout.Root
		row.AgentDir = layout.Agent
		row.DirExists = layout.Exists()
		row.ConfigFiles = a.configFileRows(kind)
	} else {
		row.ConfigFiles = []domain.ConfigFileVO{}
	}

	// 四个计数走缓存：它们要扫目录（会话文件、技能、插件、MCP），
	// 而概览页是首屏，不该为此等一秒。
	c := a.countsOf(kind)
	row.SessionCount, row.SessionCountKnown = c.sessions, c.known.sessions
	row.SkillCount, row.SkillCountKnown = c.skills, c.known.skills
	row.PluginCount, row.PluginCountKnown = c.plugins, c.known.plugins
	row.McpCount, row.McpCountKnown = c.mcp, c.known.mcp
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
