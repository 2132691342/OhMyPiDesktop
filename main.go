// 应用入口：Wails 壳 + 本地 HTTP 服务 + 单实例 + 托盘。
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

	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/runtime"
	"OhMyPiDesktop/backend/server"
	"OhMyPiDesktop/backend/service"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// version 由构建时注入；默认 dev 便于本地区分。
var version = "0.1.0"

func main() {
	app := &App{version: version, startDone: make(chan struct{})}

	err := wails.Run(&options.App{
		Title:            "OhMyPiDesktop",
		Width:            1360,
		Height:           900,
		MinWidth:         1040,
		MinHeight:        680,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			WebviewUserDataPath:  "",
		},
	})
	if err != nil {
		pkg.Errorf("app: 窗口启动失败: %v", err)
		os.Exit(1)
	}
}

// App 是 Wails 绑定面：只暴露系统能力（端口、标题栏、对话框、退出），业务全走本地 HTTP。
//
// 为什么不把 handler 嵌进来：嵌入会把它的导出方法一起绑成前端永远调不通的 JS 方法。
type App struct {
	version string

	svc   *service.App
	srv   *server.Server
	store *repo.Store
	hub   *hub.Hub
	token string

	startDone chan struct{}
	startOK   atomic.Bool
	startErr  string
	errMu     sync.Mutex

	quitting  atomic.Bool
	startWait time.Duration
}

// startup 装配业务。结束前一定关闭 startDone，让 domReady 有确定的等待点。
func (a *App) startup(ctx context.Context) {
	defer close(a.startDone)
	appCtx.Store(ctx)

	if err := a.assemble(); err != nil {
		ae := pkg.As(err)
		a.setStartErr(ae.Message)
		pkg.Errorf("app: 业务装配失败 [%s] %s %s", ae.Code, ae.Message, ae.Detail)
		return
	}
	a.startOK.Store(true)
	pkg.Infof("app: 装配完成 path=%s", a.svc.Workspace())
}

func (a *App) assemble() error {
	paths, err := runtime.Resolve()
	if err != nil {
		return err
	}
	pkg.SetLogDir(paths.LogDir)
	pkg.Infof("app: 数据目录=%s", paths.DataDir)

	store, err := repo.Open(paths.DBFile)
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
	return nil
}

// domReady 起内嵌 HTTP 并把端口交给前端。
func (a *App) domReady(ctx context.Context) {
	select {
	case <-a.startDone:
	case <-time.After(30 * time.Second):
		a.setStartErr("后端装配超时")
	}
	if !a.startOK.Load() {
		pkg.Errorf("app: 装配未完成，界面将显示启动失败")
		wruntime.EventsEmit(ctx, "app:startup-error", map[string]string{"message": a.startMessage()})
		wruntime.WindowShow(ctx)
		return
	}
	port, err := a.srv.Start()
	if err != nil {
		a.setStartErr(pkg.As(err).Message)
		wruntime.EventsEmit(ctx, "app:startup-error", map[string]string{"message": a.startMessage()})
		wruntime.WindowShow(ctx)
		return
	}
	wruntime.EventsEmit(ctx, "app:ready", map[string]any{
		"port":     port,
		"token":    a.token,
		"version":  a.version,
		"contract": server.ContractVersion,
	})
	wruntime.WindowShow(ctx)
}

// beforeClose 关窗时先停下引擎：不给子进程留孤儿。
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false
	}
	if a.svc != nil && a.svc.Engine.AnyRunning() {
		a.quitting.Store(true)
		go func() {
			time.Sleep(50 * time.Millisecond)
			wruntime.Quit(ctx)
		}()
		return true
	}
	a.quitting.Store(true)
	return false
}

// shutdown 释放资源。
func (a *App) shutdown(ctx context.Context) {
	if a.svc != nil {
		a.svc.Engine.StopAll()
	}
	if a.srv != nil {
		a.srv.Stop(ctx)
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	pkg.Infof("app: 已退出")
}

func (a *App) setStartErr(msg string) {
	a.errMu.Lock()
	a.startErr = msg
	a.errMu.Unlock()
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
// 而服务器是在 OnDomReady 里异步起的——不在这里等，前端必然拿到一个
// 「端口还是 0」的瞬间，然后把「后端没有启动起来」当成永久失败。
// 阻塞在这里，前端就只需要处理一种结果。
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
	// 装配完成到开始监听之间还有一个很短的窗口（起 listener）。
	deadline := time.Now().Add(20 * time.Second)
	for a.srv == nil || a.srv.Port() == 0 {
		if time.Now().After(deadline) {
			return Endpoint{}, fmt.Errorf("本地服务没有起来，请看设置页的日志")
		}
		time.Sleep(20 * time.Millisecond)
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
func (a *App) PickFolder(defaultDir string) (string, error) {
	ctx := a.ctx()
	if ctx == nil {
		return "", fmt.Errorf("窗口还没就绪")
	}
	return wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{
		Title:                "选择工作目录",
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
}

// RevealInExplorer 在资源管理器里定位一个文件。
func (a *App) RevealInExplorer(path string) error {
	ctx := a.ctx()
	if ctx == nil {
		return fmt.Errorf("窗口还没就绪")
	}
	wruntime.BrowserOpenURL(ctx, "file:///"+path)
	return nil
}

// OpenExternal 用系统默认浏览器打开链接。
func (a *App) OpenExternal(url string) error {
	ctx := a.ctx()
	if ctx == nil {
		return fmt.Errorf("窗口还没就绪")
	}
	wruntime.BrowserOpenURL(ctx, url)
	return nil
}

// WindowMinimize 最小化。
func (a *App) WindowMinimize() {
	if ctx := a.ctx(); ctx != nil {
		wruntime.WindowMinimise(ctx)
	}
}

// WindowToggleMaximize 最大化 / 还原。
func (a *App) WindowToggleMaximize() {
	ctx := a.ctx()
	if ctx == nil {
		return
	}
	if wruntime.WindowIsMaximised(ctx) {
		wruntime.WindowUnmaximise(ctx)
		return
	}
	wruntime.WindowMaximise(ctx)
}

// WindowClose 关窗。
func (a *App) WindowClose() {
	if ctx := a.ctx(); ctx != nil {
		wruntime.Quit(ctx)
	}
}

// Quit 退出应用。
func (a *App) Quit() {
	if ctx := a.ctx(); ctx != nil {
		a.quitting.Store(true)
		wruntime.Quit(ctx)
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

// appCtx 在 startup 里写入：Wails 的 Runtime API 全都需要它。
var appCtx atomic.Value

func (a *App) ctx() context.Context {
	if v := appCtx.Load(); v != nil {
		if c, ok := v.(context.Context); ok {
			return c
		}
	}
	return nil
}
