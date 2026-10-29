package service

import (
	"os"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/platform"
)

// 应用级偏好：开机自启、恢复上次会话、数据目录。
//
// 这三件事都不该由 HTTP handler 直接做——「开关一个设置」背后牵着注册表、
// 设置 KV 与引擎会话三处，只有编排层同时看得到它们。

// 关窗行为只有两种：收托盘（保持对话活着）与直接退出（进程结束）。
// 不需要第三种：最小化已经存在，而“关窗问一下”会让每次关闭多一次点击。
const (
	CloseActionTray = "tray"
	CloseActionQuit = "quit"
)

// AutoStart 报告开机自启是否打开。
//
// 以注册表为准而不是设置文件：用户可能在任务管理器里禁掉启动项，
// 此时开关该显示「关」——显示「开」而实际不生效是最容易让人不信任的一种界面。
func (a *App) AutoStart() bool { return platform.AutoStartEnabled() }

// SetAutoStart 开关开机自启，成功后才把镜像值写进设置。
func (a *App) SetAutoStart(enabled bool) error {
	if err := platform.SetAutoStart(enabled); err != nil {
		return err
	}
	return a.Store.Set(domain.KeyAutoStart, boolFlag(enabled))
}

// SyncAutoStart 把设置里的镜像值与注册表对齐。装配阶段调用一次。
func (a *App) SyncAutoStart() {
	value := boolFlag(platform.AutoStartEnabled())
	if a.Store.Get(domain.KeyAutoStart) == value {
		return
	}
	if err := a.Store.Set(domain.KeyAutoStart, value); err != nil {
		pkg.Warnf("app: 同步开机自启状态失败：%v", err)
	}
}

// RestoreSession 报告启动时是否回到上次那个会话。
func (a *App) RestoreSession() bool {
	return a.Store.Get(domain.KeyRestoreSession) != "0"
}

// SetRestoreSession 改「启动时回到上次会话」。
func (a *App) SetRestoreSession(enabled bool) error {
	return a.Store.Set(domain.KeyRestoreSession, boolFlag(enabled))
}

// CloseAction 返回关窗行为：tray / quit。
func (a *App) CloseAction() string {
	if a.Store.Get(domain.KeyCloseAction) == CloseActionQuit {
		return CloseActionQuit
	}
	return CloseActionTray
}

// SetCloseAction 改关窗行为。
func (a *App) SetCloseAction(action string) error {
	if action != CloseActionQuit {
		action = CloseActionTray
	}
	return a.Store.Set(domain.KeyCloseAction, action)
}

// TrayHintPending 报告是否需要给一次「收托盘」的首次提示。
//
// 只提示一次：每次关窗都弹一下是把用户当犯错的人对待。
// 看过之后就不再打扰，直到用户自己改回去。
func (a *App) TrayHintPending() bool {
	return a.Store.Get(domain.KeyTrayHintShown) != "1"
}

// MarkTrayHintShown 记下「收托盘」的首次提示已经给过。
func (a *App) MarkTrayHintShown() error {
	return a.Store.Set(domain.KeyTrayHintShown, "1")
}

// LastSession 返回上次打开的会话文件与它所属的引擎。
//
// 会话文件可能已经被删掉（在终端里删的）：那时返回空，界面就落到空会话，
// 而不是打开一个点了会报错的路径。
func (a *App) LastSession() (engine.Kind, string) {
	if !a.RestoreSession() {
		return a.ActiveKind(), ""
	}
	kind, path := splitSessionKey(a.Store.Get(domain.KeyLastSession))
	if path == "" {
		return a.ActiveKind(), ""
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return a.ActiveKind(), ""
	}
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	return kind, path
}

// RememberSession 记住当前会话，供下次启动恢复。
//
// 只记一个路径，不记「会话内容」——内容始终在引擎自己的会话文件里。
func (a *App) RememberSession(kind engine.Kind, sessionFile string) {
	value := ""
	if sessionFile != "" && kind.Valid() {
		value = string(kind) + "\x00" + sessionFile
	}
	if value == a.Store.Get(domain.KeyLastSession) {
		return
	}
	if err := a.Store.Set(domain.KeyLastSession, value); err != nil {
		// 记不住只影响「下次回来时的那一下」，不该拦任何操作。
		pkg.Warnf("app: 记不住当前会话：%v", err)
	}
}

// OpenDataDir 在资源管理器里打开数据目录。
//
// 用户报「设置不对」时，最快的排障路径是让他直接看到那个 JSON 文件。
func (a *App) OpenDataDir() error {
	if a.appDir == "" {
		return pkg.New(pkg.CodeDataDir, "数据目录还没解析出来", "")
	}
	return platform.RevealInExplorer(a.appDir)
}

// Prefs 组装设置页看到的全部开关状态。
func (a *App) Prefs() domain.AppPrefsVO {
	values, _ := a.Store.All()
	if values == nil {
		values = map[string]string{}
	}
	kind, session := a.LastSession()
	vo := domain.AppPrefsVO{
		Values:          values,
		AutoStart:       a.AutoStart(),
		RestoreSession:  a.RestoreSession(),
		LastSessionFile: session,
		LastEngineKind:  string(kind),
		DataDir:         a.appDir,
		LogDir:          filepath.Join(a.appDir, "logs"),
		CloseAction:     a.CloseAction(),
		TrayHintPending: a.TrayHintPending(),
	}
	if session != "" {
		if d, err := a.Index(kind).Detail(session); err == nil {
			vo.LastSessionTitle = d.Title
		}
	}
	return vo
}

// WorkspaceRows 返回最近用过的目录（只含还存在的那些）。
//
// 托盘菜单与设置页都用它；两处要看到同一份顺序，所以只有这一个入口。
func (a *App) WorkspaceRows() []domain.WorkspaceRow {
	rows, err := a.recentWorkspaces()
	if err != nil || rows == nil {
		return []domain.WorkspaceRow{}
	}
	return rows
}

// boolFlag 把布尔值写成设置里的 1 / 0。
//
// 设置文件里全是字符串：用 "1"/"0" 而不是 "true"/"false"，
// 是因为界面上还有别的枚举值（主题、语言），统一成短串更好读。
func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// splitSessionKey 拆 "kind\x00path"。
func splitSessionKey(raw string) (engine.Kind, string) {
	idx := strings.IndexByte(raw, 0)
	if idx < 0 {
		return "", raw
	}
	return engine.Kind(raw[:idx]), raw[idx+1:]
}
