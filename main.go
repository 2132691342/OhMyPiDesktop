// 应用入口：Wails v3 壳 + 本地 HTTP 服务 + 单实例 + 托盘。
//
// 业务一律走内嵌的 gin（回环 + 随机端口 + 启动令牌），这里只暴露系统能力：
// 端口、目录选择框、窗口控制、托盘。改这里时记住 backend/ 不 import wails，
// 所以业务层不受桌面框架版本影响。
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/runtime"
	"OhMyPiDesktop/backend/server"
	"OhMyPiDesktop/backend/service"
)

//go:embed all:frontend/dist
var assets embed.FS

// 用户手册随二进制一起发行，断网也能看。声明在 assets.go。

// version 由构建时注入；默认 dev 便于本地区分。
var version = "0.1.0"

// singleInstanceID 判「两次启动是不是同一个应用」的唯一标识。
const singleInstanceID = "com.ohmypi.desktop"

func main() {
	app := &App{version: version, startDone: make(chan struct{}), startHidden: hasArg("--hidden")}

	a := application.New(application.Options{
		Name:        "OhMyPiDesktop",
		Description: "omp 与 pi 的桌面工作台",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// 第二次启动在 New 内部就完成「通知首个实例 → 退出」，走不到下面建窗。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				app.onSecondInstance(data)
			},
		},
		OnShutdown: func() {
			app.shutdown()
		},
	})

	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "OhMyPiDesktop",
		Width:  1360,
		Height: 900,
		// 无边框：标题栏由前端自绘，窗口位置与尺寸才与界面一致。
		Frameless:        true,
		MinWidth:         1040,
		MinHeight:        680,
		BackgroundColour: application.NewRGB(248, 250, 252),
		URL:              "/",
		// 装配完成前不显示窗口，避免用户看见一帧白屏。
		// 开机自启（--hidden）时连装配完成后也不显示：只上托盘。
		Hidden: true,
		Windows: application.WindowsWindow{
			NonClientRegionSupport: true,
		},
	})
	app.win = win
	app.app = a

	// 关窗不退出：收托盘。真正退出走托盘菜单的「退出」。
	// e.Cancel() 是 v3 的拦截方式——返回 false 不是拦截（v2 才是）。
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.quitting.Load() {
			return
		}
		e.Cancel()
		app.toTray()
	})

	// 托盘：菜单项由后端定，文案不进前端，避免语言切换时后端菜单不同步。
	// 托盘在装配前就建好：用户可能一启动就把窗口关到托盘。
	app.tray = a.SystemTray.New()
	app.buildTrayMenu(a, win)

	// 业务装配在 ServiceStartup 里做，失败也一定关闭 startDone：
	// 前端拿端口时有一个确定的等待点，不会把「还没装好」当成永久失败。
	if err := a.Run(); err != nil {
		pkg.Errorf("app: 窗口启动失败: %v", err)
		os.Exit(1)
	}
}

// App 是 Wails 绑定面：只暴露系统能力，业务全走本地 HTTP。
//
// 为什么不把 gin 的 handler 嵌进来：嵌入会把它们的导出方法一起绑成
// 前端永远调不通的 JS 方法（签名带 *gin.Context）。
type App struct {
	version string

	svc   *service.App
	srv   *server.Server
	store *repo.Store
	hub   *hub.Hub
	token string

	win  *application.WebviewWindow
	tray *application.SystemTray
	app  *application.App

	startDone chan struct{}
	startOK   atomic.Bool
	startErr  string
	errMu     sync.Mutex

	// startHidden 来自命令行 --hidden：开机自启时只拉起托盘，不弹窗。
	startHidden bool

	quitting atomic.Bool
}

// ServiceStartup 是 v3 的服务生命周期钩子，等价于 v2 的 OnStartup。
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}

func (a *App) startup(ctx context.Context) {
	defer close(a.startDone)

	if err := a.assemble(); err != nil {
		ae := pkg.As(err)
		a.setStartErr(ae.Message)
		pkg.Errorf("app: 业务装配失败 [%s] %s %s", ae.Code, ae.Message, ae.Detail)
		// 装配失败必须把窗口调出来：启动失败的信息只能由界面说，
		// 若此时还藏在托盘后面，用户会以为双击没反应。
		a.revealWindow()
		return
	}
	a.startOK.Store(true)
	pkg.Infof("app: 装配完成 workspace=%s", a.svc.Workspace())
	// 装配成功、且不是开机自启，才把窗口显出来（窗口初始是 Hidden）。
	if !a.startHidden {
		a.revealWindow()
	}
}

func (a *App) assemble() error {
	paths, err := runtime.Resolve()
	if err != nil {
		return err
	}
	pkg.SetLogDir(paths.LogDir)
	pkg.Infof("app: 数据目录=%s", paths.DataDir)

	store, err := repo.Open(paths.SettingsFile)
	if err != nil {
		return err
	}
	a.store = store

	a.hub = hub.New(1024)
	a.token = newToken()
	pkg.Infof("app: 令牌来源=%s 长度=%d", tokenSource(), len(a.token))

	svc, err := service.New(service.Options{
		Store:   store,
		Hub:     a.hub,
		DataDir: paths.DataDir,
		Version: a.version,
	})
	if err != nil {
		return err
	}
	a.svc = svc
	a.srv = server.New(svc, a.hub, a.token)
	// 托盘菜单里的「最近工作目录」要跟着当前目录变，所以把重建回调交给 service。
	svc.OnWorkspaceChange = func() { a.refreshTrayMenu() }
	// 开机自启的真实状态以注册表为准，装配时对齐一次。
	svc.SyncAutoStart()
	return nil
}

// onSecondInstance 处理第二次启动：把已有窗口叫到前台。
func (a *App) onSecondInstance(data application.SecondInstanceData) {
	pkg.Infof("app: 检测到第二个实例 args=%d dir=%s", len(data.Args), data.WorkingDir)
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Restore()
	a.win.Focus()
}

// buildTrayMenu 组装托盘菜单。
//
// 关窗不退出，所以托盘菜单必须能回答「我现在要做什么」：显示窗口 / 回到最近干过活的目录 /
// 直接打开数据目录 / 退出。菜单项在 tray.go。

// toggleWindow 在显示与隐藏之间切换。
//
// 唤起必须同时处理「隐藏」与「最小化」两种状态：只做一半唤不回窗口。
func (a *App) toggleWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Restore()
	a.win.Focus()
}

// revealWindow 把隐藏的窗口显示出来并按需重建托盘菜单。
func (a *App) revealWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Restore()
	a.win.Focus()
	// 此时工作目录与最近列表已经就绪，重铺一次菜单。
	a.refreshTrayMenu()
}

// hasArg 判断命令行里有没有某个标志。
// 目前只用于 --hidden：开机自启时只拉起托盘，不往屏幕上弹一个空窗口。
func hasArg(flag string) bool {
	for _, arg := range os.Args[1:] {
		if arg == flag {
			return true
		}
	}
	return false
}

// toTray 收托盘前先停引擎，不给子进程留孤儿。
func (a *App) toTray() {
	if a.win != nil {
		a.win.Hide()
	}
}

func (a *App) shutdown() {
	if a.svc != nil {
		a.svc.Engine.StopAll()
	}
	if a.srv != nil {
		a.srv.Stop(context.Background())
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	pkg.Infof("app: 已退出")
}

func (a *App) setStartErr(msg string) {
	a.errMu.Lock()
	defer a.errMu.Unlock()
	a.startErr = msg
}

func (a *App) startMessage() string {
	a.errMu.Lock()
	defer a.errMu.Unlock()
	if a.startErr == "" {
		return "后端没有启动起来"
	}
	return a.startErr
}

// token 走随机值；OHMYPI_DEV_TOKEN 只在本地调试时覆盖它，
// 让「用浏览器打真后端」这类验证有路可走（正式运行时该变量不存在）。
func newToken() string {
	if dev := os.Getenv("OHMYPI_DEV_TOKEN"); dev != "" {
		return dev
	}
	return randomToken()
}

func tokenSource() string {
	if os.Getenv("OHMYPI_DEV_TOKEN") != "" {
		return "env"
	}
	return "random"
}

func randomToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ---------------- 绑定给前端的系统能力 ----------------

// Endpoint 返回本地服务地址与令牌。前端首屏只调这一个绑定方法。
type Endpoint struct {
	Port     int    `json:"port"`
	Token    string `json:"token"`
	Version  string `json:"version"`
	Contract int    `json:"contract"`
	BaseURL  string `json:"base_url"`
}

// Endpoint 返回本地服务地址。
//
// 它会**等到服务真的在监听**再返回。前端在 WebView 加载完就调这个方法，
// 而服务器是异步起的——不在这里等，前端必然拿到一个「端口还是 0」的瞬间，
// 然后把「后端没有启动起来」当成永久失败。
func (a *App) Endpoint() (Endpoint, error) {
	select {
	case <-a.startDone:
	case <-time.After(35 * time.Second):
		a.setStartErr("后端装配超时")
		return Endpoint{}, fmt.Errorf("后端装配超时，请看设置页的日志")
	}
	if !a.startOK.Load() {
		return Endpoint{}, fmt.Errorf("%s", a.startMessage())
	}
	// v3 没有 OnDomReady：装配成功就立刻起监听，前端拿端口时已经是确定值。
	// Start 返回 (int, error)，端口不会再变，不需要轮询等。
	if a.srv.Port() == 0 {
		if _, err := a.srv.Start(); err != nil {
			ae := pkg.As(err)
			a.setStartErr(ae.Message)
			return Endpoint{}, fmt.Errorf("%s", ae.Message)
		}
	}
	port := a.srv.Port()
	return Endpoint{
		Port:     port,
		Token:    a.token,
		Version:  a.version,
		Contract: server.ContractVersion,
		BaseURL:  fmt.Sprintf("http://127.0.0.1:%d/api/v1", port),
	}, nil
}

// PickFolder 打开系统目录选择框。
//
// v3 的对话框是「配置 → 弹一次」：结果从 PromptForSingleSelection 拿，
// 取消时返回空串而不是错误（空串就是用户没选，不是失败）。
func (a *App) PickFolder(defaultDir string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("窗口还没就绪")
	}
	return a.app.Dialog.OpenFile().
		SetTitle("选择工作目录").
		SetDirectory(defaultDir).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}

// RevealInExplorer 在资源管理器里定位一个文件。
func (a *App) RevealInExplorer(path string) error {
	if a.app == nil {
		return fmt.Errorf("窗口还没就绪")
	}
	a.app.Browser.OpenURL("file:///" + path)
	return nil
}

// OpenExternal 用系统默认浏览器打开链接。
func (a *App) OpenExternal(url string) error {
	if a.app == nil {
		return fmt.Errorf("窗口还没就绪")
	}
	a.app.Browser.OpenURL(url)
	return nil
}

// WindowMinimize 最小化。
func (a *App) WindowMinimize() {
	if a.win != nil {
		a.win.Minimise()
	}
}

// WindowToggleMaximize 最大化 / 还原。
func (a *App) WindowToggleMaximize() {
	if a.win == nil {
		return
	}
	if a.win.IsMaximised() {
		a.win.Restore()
		return
	}
	a.win.Maximise()
}

// WindowClose 关窗（收托盘，不退出）。
func (a *App) WindowClose() {
	a.toTray()
}

// Quit 退出应用。
func (a *App) Quit() {
	a.quitting.Store(true)
	if a.app != nil {
		a.app.Quit()
	}
}

// Settings 直接把设置 KV 交给前端首屏（避免前端连不上时白屏）。
func (a *App) Settings() map[string]string {
	if a.svc == nil {
		return map[string]string{}
	}
	out, err := a.svc.Settings()
	if err != nil {
		return map[string]string{}
	}
	return out
}
