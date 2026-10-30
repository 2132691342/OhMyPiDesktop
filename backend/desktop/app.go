// Package desktop 是 Wails 外壳：窗口、托盘、单实例、绑定给前端的系统能力。
// 边界：本包之外（backend/ 全部）不知道 Wails 存在，业务只通过回环 HTTP 与前端说话，
// 这里只提供「界面外面的那一层」——端口、目录选择框、窗控、托盘。
package desktop

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

// Options 是外壳启动参数。main 把版本号与内嵌资源交进来，本包不自己读构建信息。
type Options struct {
	// Version 由构建时注入（-ldflags "-X main.version=…"）。
	Version string
	// Assets 是打进二进制的前端产物。
	Assets embed.FS
}

// singleInstanceID 判「两次启动是不是同一个应用」的唯一标识。
const singleInstanceID = "com.ohmypi.desktop"

// 窗口的逻辑尺寸（CSS 像素）。与前端断点配套：
// 800 宽时对话侧栏收窄、工作区三栏压缩，见 views 里的容器查询。
const (
	windowLogicalWidth  = 800
	windowLogicalHeight = 600
)

// Run 装配并运行桌面应用，阻塞到退出。
func Run(opts Options) error {
	// 数据目录必须在 application.New 之前解析：WebView2 的缓存路径要交给它，
	// 而那一步一旦拿到不可写的路径就会弹框退出。
	paths, err := runtime.Resolve()
	if err != nil {
		// 此刻什么都没有：没有窗口、没有日志目录、发布版还没有控制台。
		// 只剩一个系统消息框能把话说出去。
		ae := pkg.As(err)
		fatalDialog("无法启动", ae.Message, ae.Detail)
		return err
	}

	app := &App{
		version:     opts.Version,
		paths:       paths,
		startDone:   make(chan struct{}),
		startHidden: hasArg("--hidden"),
	}

	a := application.New(application.Options{
		Name:        "OhMyPiDesktop",
		Description: "omp 与 pi 的桌面工作台",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
		// 第二次启动在 New 内部就完成「通知首个实例 → 退出」，走不到下面建窗。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				app.onSecondInstance(data)
			},
		},
		OnShutdown: func() { app.shutdown() },
		Windows: application.WindowsOptions{
			// WebView2 的缓存放进我们自己的数据目录（默认落在 %APPDATA%\<exe 名>）。
			// 否则卸载时那个几十 MB 的目录没人清理——安装器不认识它。
			WebviewUserDataPath: paths.WebViewDir,
		},
	})

	// 建窗尺寸要按**屏幕缩放比**放大后再声明：Wails v3.0.0 把 Width/Height 当
	// 物理像素用（200% 缩放下声明 800 得到 400 的 CSS 空间），要 800 逻辑像素
	// 就得声明 800×缩放比，且不能超过屏幕可用区，否则高度会被系统裁掉。
	winWidth, winHeight := windowPhysicalSize()

	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "OhMyPiDesktop",
		Width:  winWidth,
		Height: winHeight,
		// 无边框：标题栏由前端自绘，窗口位置与尺寸才与界面一致。
		Frameless: true,
		// 初始位置在屏幕正中：不设时 Windows 走级联默认位置，首次启动时窗口
		// 靠屏幕右上角，用户每次启动都要拖一下才能用。
		InitialPosition: application.WindowCentered,
		// 不设 MinWidth / MinHeight：WM_GETMINMAXINFO 的最小尺寸按窗口 DPI 换算，
		// 与上面声明的物理尺寸基准不一致时窗口会被系统强行撑大。布局收窄由前端负责，
		// .shell-main 是容器查询锚点。
		BackgroundColour: application.NewRGB(248, 250, 252),
		URL:              "/",
		// 建窗时不显示：装配阶段显形会让用户看到一帧白屏，而且那时窗口还没初始化完。
		// 前端画完首屏会调 Ready()，由那里显示；开机自启（--hidden）则一直不显示。
		Hidden: true,
		Windows: application.WindowsWindow{
			NonClientRegionSupport: true,
		},
	})
	app.win = win
	app.app = a

	// 关窗行为可配：收托盘（默认）或直接退出。收托盘是默认（正在跑的对话不该
	// 被一键弄死），但用户必须能关掉它，第一次收托盘要告诉他“我还在”。
	// e.Cancel() 是 v3 的拦截方式——返回 false 不是拦截（v2 才是）。
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.quitting.Load() {
			return
		}
		if app.closeAction() == service.CloseActionQuit {
			// 关窗即退出。不能只置标志就放行：窗口关闭后托盘还挂着，
			// 只剩托盘时消息循环不会自己结束，进程就“关不掉”；
			// 所以拦截这次关闭，改走主动退出（requestQuit 会走完整 cleanup）。
			e.Cancel()
			app.requestQuit()
			return
		}
		e.Cancel()
		// 第一次收托盘先问一句：静默躲起来在用户眼里就是「关不掉」。
		if app.svc != nil && app.svc.TrayHintPending() {
			app.askCloseDecision()
			return
		}
		app.toTray()
	})

	// 托盘在装配前就建好：用户可能一启动就把窗口关到托盘。
	app.tray = a.SystemTray.New()
	app.buildTrayMenu(a, win)

	if err := a.Run(); err != nil {
		return err
	}
	return nil
}

// windowPhysicalSize 算出建窗时要声明的物理像素尺寸。
// 逻辑尺寸 800×600 是设计目标（CSS 像素）；Wails v3.0.0 把它们当物理像素用，
// 所以要乘缩放比，且不能超出屏幕可用区——超出部分会被系统裁掉。
func windowPhysicalSize() (int, int) {
	// 宽高都要乘缩放比：Wails v3.0.0 把 Width/Height 当物理像素直接用，
	// 不声明缩放就会在 200% 的屏幕上得到一半的 CSS 空间。高度乘完可能超出
	// 工作区（实测 1200 被裁到 915），Ready() 里会再按目标尺寸设一次。
	scale := displayScale()
	width := windowLogicalWidth * scale
	height := windowLogicalHeight * scale
	return width, height
}

// App 是 Wails 绑定面：只暴露系统能力，业务全走本地 HTTP。
// 不把 gin 的 handler 嵌进来：嵌入会把它们的导出方法一起绑成
// 前端永远调不通的 JS 方法（签名带 *gin.Context）。
type App struct {
	version string

	// paths 在 application.New 之前就解析好：WebView2 的缓存目录要交给它。
	paths runtime.Paths

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

	// revealed 记录窗口是否已显示过。前端每次热重载都会再调 Ready()，
	// 重复 Show 会让窗口在用户眼前闪一下。
	revealed atomic.Bool

	quitting atomic.Bool
}

// ServiceStartup 是 Wails 的服务生命周期钩子：装配业务、启动后端。
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
		return
	}
	a.startOK.Store(true)
	pkg.Infof("app: 装配完成 workspace=%s", a.svc.Workspace())
}

// centerOnPrimaryScreen 把窗口放到主屏工作区正中。
// 不用 Wails 的 Center()：它用 GetClientRect（客户区）取尺寸、GetMonitorInfoForWindow 取监视器，
// 对 Hidden 建窗后又被 SetSize 改过的无边框窗口两基准不一致会偏；屏幕信息建窗前不可用（GetPrimary 返回 nil）。
func (a *App) centerOnPrimaryScreen() {
	if a.win == nil {
		return
	}
	screen := a.app.Screen.GetPrimary()
	if screen == nil || screen.WorkArea.Width <= 0 || screen.WorkArea.Height <= 0 {
		a.win.Center()
		return
	}
	// SetPosition 收的是 DIP，与 SetSize 同一套基准。
	width, height := a.win.Size()
	area := screen.WorkArea
	a.win.SetPosition(
		area.X+(area.Width-width)/2,
		area.Y+(area.Height-height)/2,
	)
}

// Ready 由前端在首屏渲染完后调用，用来把隐藏的窗口显示出来。
// 不能在装配完成时显示：v3 把建窗推迟到主循环 pendingRun，ServiceStartup 执行时
// 窗口还不存在，Show() 只会建窗仍隐藏，随后的 Focus() 会踩到未初始化的 WebView2 controller 而 panic。
func (a *App) Ready() {
	// 前端每次热重载都会再调一次。
	if a.revealed.Swap(true) {
		return
	}
	// 此刻工作目录与最近列表已经就绪，无论显不显窗口都要重铺一次托盘菜单
	// （建托盘时这些数据还没载入）。
	a.refreshTrayMenu()
	// 开机自启（--hidden）只上托盘，不显示窗口。
	if a.startHidden {
		return
	}
	// 建窗尺寸与 SetSize 的语义不同：前者是物理像素，后者是 DIP（会乘缩放比）。
	// 建窗时高度可能被裁（Hidden 窗口判定不准屏幕归属），所以这里用**逻辑值**
	// 再设一次——这条路径 Wails 会自己按 DPI 换算。
	a.win.SetSize(windowLogicalWidth, windowLogicalHeight)
	// 改完尺寸再居中：尺寸与位置必须基于同一个基准算，否则会偏。
	a.centerOnPrimaryScreen()
	a.showWindow()
	pkg.Infof("app: 首屏就绪，窗口已显示")
}

func (a *App) assemble() error {
	paths := a.paths
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
	a.showWindow()
}

// showWindow 把窗口带到前台：显示 + 还原 + 聚焦。
// 三步缺一不可：窗口可能在隐藏、最小化、最大化三种状态中的任意一种，只做一半唤不回来。
func (a *App) showWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Restore()
	a.win.Focus()
}

// toTray 收托盘：只隐藏窗口，进程与引擎继续活着。
func (a *App) toTray() {
	if a.win != nil {
		a.win.Hide()
	}
}

// closeAction 读设置里的关窗行为。装配未完成时按默认（收托盘）处理。
func (a *App) closeAction() string {
	if a.svc == nil {
		return service.CloseActionTray
	}
	return a.svc.CloseAction()
}

// askCloseDecision 请求界面就「关闭时做什么」做一次选择。
// 走事件而不是系统通知：Wails 没有通知 API，且窗口已隐藏、在隐藏窗口上画弹层
// 用户看不到；所以先不藏窗口，发一帧事件让界面弹选择框，选完再执行。
func (a *App) askCloseDecision() {
	if a.hub == nil {
		a.toTray()
		return
	}
	a.hub.Emit("", "app:close_requested", map[string]any{
		"close_action": a.closeAction(),
	})
}

// ApplyCloseDecision 执行界面对「关窗」的选择。
// 它是窗口层的动作（隐藏 / 退出都不是业务），所以住在这里而不是 service；
// remember 为真时把选择写进设置，之后直接按选择执行。
func (a *App) applyCloseDecision(action string, remember bool) error {
	if action != service.CloseActionQuit {
		action = service.CloseActionTray
	}
	if a.svc != nil {
		if remember {
			if err := a.svc.SetCloseAction(action); err != nil {
				return err
			}
		}
		if err := a.svc.MarkTrayHintShown(); err != nil {
			pkg.Warnf("app: 记不住托盘提示状态：%v", err)
		}
	}
	if action == service.CloseActionQuit {
		a.requestQuit()
		return nil
	}
	a.toTray()
	return nil
}

// shutdown 退出前的收尾，是 Wails 的 OnShutdown 回调，跑在**主线程**上（InvokeSync），
// 每多停一毫秒用户看到的就是「点了退出但窗口还在」。所以要先停回收协程
// （否则它会与 StopAll 抢锁、并试图停已停的引擎）；停引擎必须并发做并给统一短超时，否则「退不掉」。
func (a *App) shutdown() {
	if a.svc != nil {
		a.svc.Engine.Shutdown()
		done := make(chan struct{})
		go func() {
			defer close(done)
			a.svc.Engine.StopAllQuick(quitGrace)
		}()
		select {
		case <-done:
		case <-time.After(quitGrace + 500*time.Millisecond):
			pkg.Warnf("app: 引擎未在超时内停完，直接退出")
		}
	}
	if a.srv != nil {
		// SSE 长连接不会自己断：给一个短超时，别让它拖住退出。
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		a.srv.Stop(ctx)
		cancel()
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
