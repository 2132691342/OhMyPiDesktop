package service

// 界面设置与工作目录记忆。设置全在 Store 里（一个 JSON 文件），这里只负责「改设置之后该跟着动什么」：
// 换 profile 重建会话索引、换引擎路径重新探测、换目录记进「最近」——都是一次设置动作的一部分，
// 不该由前端按顺序调三个接口。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/session"
)

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

// SetWorkspace 切工作目录。切目录不切引擎、不停旧进程：别的目录可能还有正在跑的那一轮，
// 旧目录的引擎由回收逻辑按空闲时间清掉（见 engine.Supervisor.Sweep）。
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
	// 切目录会新建一个 (引擎, 目录) 的进程键，这是进程数增长的唯一入口；旧目录的引擎从这一刻起空闲。
	// 立刻回收一次，比等下一轮定时扫描更贴合「刚切走」的时机。
	// 计数也跟着变（项目级技能 / MCP 换了目录就不一样），一并失效。
	a.InvalidateCounts()
	a.Engine.Sweep()
	if a.OnWorkspaceChange != nil {
		a.OnWorkspaceChange()
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

// recentWorkspaces 读最近列表，丢掉已经不存在的目录。直接丢而不是标灰：拔掉的 U 盘、删掉的项目
// 不该永久占着列表，用户想回去时发现它「还在但点不动」更糟。
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

// RecentWorkspaces 是「最近 + 当前会话推出来的目录」的并集。两份来源都要：记住的列表能立刻显示
// （不用扫盘），从会话推出来的能捞到「在别的机器上聊过、这里还没切过」的目录。
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
