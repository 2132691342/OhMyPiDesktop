// 托盘：关窗之后应用还活着，所以托盘菜单必须能回答「我现在要做什么」。
// 菜单项由后端定，文案不进前端——语言切换时后端菜单不会跟着变，前后端各准备一份
// 文案只会让它们不同步，这里是唯一一份。
package desktop

import (
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
)

// trayRecentMax 是托盘菜单里「最近工作目录」最多铺几条。
//
// 托盘菜单是一个弹出层，长到要滚动就算不上快捷方式了。
const trayRecentMax = 5

// quitGrace 是退出时给引擎进程的收尾时间。
// 退出不该等引擎“优雅完成”：用户点退出时想要的是一秒内关掉；超过这个时间就直接
// 终止整棵进程树（保留 1.5 秒让正常情况下的 stdin 关闭有反应，又不至于让人以为卡死）。
const quitGrace = 1500 * time.Millisecond

// buildTrayMenu 组装托盘菜单。
// 菜单是树，但「最近工作目录」这一支会随设置变化，所以整棵树可重建：
// 直接换掉菜单比就地改某几项更省心（v3 的菜单项没有稳定的可变句柄）。
func (a *App) buildTrayMenu(app *application.App, win *application.WebviewWindow) {
	menu := app.NewMenu()
	menu.Add("显示窗口").OnClick(func(*application.Context) { a.showWindow() })

	// 最近工作目录：点一条就切过去并把窗口带出来。
	recent := menu.AddSubmenu("最近工作目录")
	a.fillRecentWorkspaces(recent, win)

	menu.Add("打开数据目录").OnClick(func(*application.Context) {
		if err := a.svc.OpenDataDir(); err != nil {
			pkg.Warnf("app: 打不开数据目录：%v", err)
		}
	})

	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { a.requestQuit() })
	a.tray.SetMenu(menu)
	a.tray.SetTooltip("OhMyPiDesktop")
	// 单击图标 = 把窗口带出来；菜单用右键。
	a.tray.OnClick(func() { a.showWindow() })
}

// requestQuit 退出应用。
// 不能在这里直接调 app.Quit()：菜单回调在托盘窗口的 WM_COMMAND 里同步执行，那一刻
// TrackPopupMenuEx 还没返回，在那个栈上销毁托盘与菜单会让消息循环在已释放的菜单上继续处理命令——所以先置标志，再异步做。
func (a *App) requestQuit() {
	if a.quitting.Swap(true) {
		return // 已经在退出：重复点击不该再走一遍
	}
	go func() {
		// 离开菜单回调的栈再销毁。
		time.Sleep(50 * time.Millisecond)
		if a.app != nil {
			a.app.Quit()
		}
	}()
}

// fillRecentWorkspaces 往子菜单里铺最近用过的目录。
// 空列表时留一条禁用的说明项而不是空菜单：
// 一个点开什么都没有的菜单会让人以为功能坏了。
func (a *App) fillRecentWorkspaces(sub *application.Menu, win *application.WebviewWindow) {
	rows := a.recentWorkspaces()
	if len(rows) == 0 {
		item := sub.Add("还没有用过工作目录")
		item.SetEnabled(false)
		return
	}
	for i, row := range rows {
		if i >= trayRecentMax {
			break
		}
		dir := row.WorkspacePath
		// 只显示最后两级：完整路径在托盘菜单里长到看不清哪个是哪个。
		item := sub.Add(shortPath(dir))
		item.SetTooltip(dir)
		item.OnClick(func(*application.Context) {
			// 切目录是 HTTP 层的动作，这里让它照同一条路走：
			// 界面与托盘对「当前目录」的理解必须一致。
			if err := a.svc.SetWorkspace(dir); err != nil {
				pkg.Warnf("app: 托盘切工作目录失败：%v", err)
				return
			}
			a.showWindow()
			// 目录变了要重铺菜单：下一次右键看到的第一条就是刚切过去的那个。
			a.refreshTrayMenu()
		})
	}
}

// refreshTrayMenu 重建托盘菜单。工作目录或最近列表变化后调用。
func (a *App) refreshTrayMenu() {
	if a.tray == nil || a.app == nil || a.win == nil {
		return
	}
	a.buildTrayMenu(a.app, a.win)
}

// recentWorkspaces 取最近工作目录（只含还存在的那些）。
func (a *App) recentWorkspaces() []domain.WorkspaceRow {
	if a.svc == nil {
		return nil
	}
	return a.svc.WorkspaceRows()
}

// shortPath 取最后两级路径，够看清是哪个项目。
func shortPath(dir string) string {
	tail := filepath.Base(dir)
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "" || parent == "." || parent == dir {
		return tail
	}
	return parent + `\` + tail
}
