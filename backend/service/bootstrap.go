package service

// 启动引导：首屏一次拉齐的全部内容。首屏要同时知道引擎、设置、布局、会话与技能计数，
// 分五六个请求会让「界面半亮」的中间态反复出现；探测带一分钟缓存，第二次之后不再起子进程。

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/session"
)

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
		// 技能数走同一份计数快照，不再扫一次目录：直接调 a.Skills 会遍历插件的 node_modules，
		// 十几个插件的机器实测 4.6 秒，且与 Health 的缓存计数对不上，首屏还白等四秒。
		b.Skills[string(kind)] = a.countsOf(kind).skills
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
