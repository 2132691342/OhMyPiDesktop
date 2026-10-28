// Package desktop 是 Wails 外壳：窗口、托盘、单实例、绑定给前端的系统能力。
//
// 边界：本包之外（backend/ 全部）不知道 Wails 存在。业务只通过回环 HTTP 与
// 前端说话，这里只提供「界面外面的那一层」——端口、目录选择框、窗控、托盘。
// 这样换桌面框架版本时，业务层一行都不用改，也仍然能在没有 WebView 的机器上跑测试。
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

	// 建窗尺寸要按**屏幕缩放比**放大后再声明。
	//
	// Wails v3.0.0 把 Width/Height 当物理像素用（实测 200% 缩放下声明 800
	// 得到 400 的 CSS 空间，界面被放大一倍）。想要 800 逻辑像素就得声明
	// 800×缩放比。同时不能超过屏幕可用区，否则高度会被系统裁掉。
	winWidth, winHeight := windowPhysicalSize()

	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "OhMyPiDesktop",
		Width:  winWidth,
		Height: winHeight,
		// 无边框：标题栏由前端自绘，窗口位置与尺寸才与界面一致。
		Frameless: true,
		// 不设 MinWidth / MinHeight。
		//
		// WM_GETMINMAXINFO 里的最小尺寸按窗口 DPI 换算，与上面声明的物理尺寸
		// 是两个基准；两者不一致时窗口会被系统强行撑大。布局尺寸由前端负责：
		// .shell-main 是容器查询锚点，窄窗口下侧栏与三栏都会收窄。
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

	// 关窗不退出：收托盘。真正退出走托盘菜单的「退出」。
	// e.Cancel() 是 v3 的拦截方式——返回 false 不是拦截（v2 才是）。
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.quitting.Load() {
			return
		}
		e.Cancel()
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
//
// 逻辑尺寸 800×600 是设计目标（CSS 像素）；Wails v3.0.0 把它们当物理像素用，
// 所以要乘缩放比。同时不能超出屏幕可用区——超出部分会被系统裁掉，
// 表现出来是「窗口比预期矮一截」。
func windowPhysicalSize() (int, int) {
	// 宽高都要乘缩放比：Wails v3.0.0 把 Width/Height 当物理像素直接用，
	// 不声明缩放就会在 200% 的屏幕上得到一半的 CSS 空间。
	//
	// 高度乘完可能超出工作区（实测 1200 被系统裁到 915），那是建窗时的临时限制；
	// Ready() 里会再按目标尺寸设一次，那条路径不受这个限制。
	scale := displayScale()
	width := windowLogicalWidth * scale
	height := windowLogicalHeight * scale
	println("PROBE width=", width, "height=", height)
	return width, height
}

// App 是 Wails 绑定面：只暴露系统能力，业务全走本地 HTTP。
//
// 为什么不把 gin 的 handler 嵌进来：嵌入会把它们的导出方法一起绑成
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
		return
	}
	a.startOK.Store(true)
	pkg.Infof("app: 装配完成 workspace=%s", a.svc.Workspace())
}

// Ready 由前端在首屏渲染完后调用，用来把隐藏的窗口显示出来。
//
// 为什么显示窗口的时机在这里而不是装配完成时：v3 把建窗推迟到主循环的
// pendingRun，ServiceStartup 执行时窗口还不存在。此刻调 Show() 只会走
// 「先建窗、再 return」那条分支——窗口建好了却仍然隐藏；紧随其后的 Focus()
// 还会踩到尚未初始化的 WebView2 controller，直接 panic。
// 等前端把首屏画完再显示：两个问题一起消失，顺带没有白屏。
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
//
// 三步缺一不可：窗口可能在隐藏、最小化、最大化三种状态中的任意一种，
// 只做一半唤不回来。
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

func (a *App) shutdown() {
	if a.svc != nil {
		// 先停回收协程再停引擎：否则它会与 StopAll 抢同一把锁，
		// 并试图停已经被停掉的实例。
		a.svc.Engine.Shutdown()
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
