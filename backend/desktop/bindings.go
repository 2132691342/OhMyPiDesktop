// 绑定给前端的系统能力：本地服务地址、对话框、窗控、外部链接。
//
// 这些方法由 `wails3 generate bindings` 生成到 frontend/bindings/ 下，
// 前端通过它们拿端口与令牌；其余一切业务走 HTTP。保持这一层极薄，
// 少一个绑定方法就少一处需要重新生成的东西。
package desktop

import (
	"fmt"
	"time"

	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/server"
)

// Endpoint 返回本地服务地址与令牌。前端首屏只调这一个绑定方法。
type Endpoint struct {
	// Port 本地服务监听端口（随机回环端口）
	Port int `json:"port"`
	// Token 启动令牌，写进每个请求的 X-Desktop-Token
	Token string `json:"token"`
	// Version 应用版本
	Version string `json:"version"`
	// Contract 响应契约版本，与 frontend/src/types/api.ts 同键
	Contract int `json:"contract"`
	// BaseURL 拼好的基地址，前端直接给 axios
	BaseURL string `json:"base_url"`
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

// PickFiles 打开文件选择框，可多选。返回绝对路径列表。
//
// 对话框在应用层调（与 PickFolder 同理），因为只有这里能拿到系统原生对话框。
// 取消时返回空列表，不是错误。
func (a *App) PickFiles(title string) ([]string, error) {
	if a.app == nil {
		return nil, fmt.Errorf("窗口还没就绪")
	}
	if title == "" {
		title = "选择文件"
	}
	picked, err := a.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(false).
		CanChooseFiles(true).
		PromptForMultipleSelection()
	if err != nil {
		// 取消在部分平台会以错误形式返回；当成「没选」而不是失败。
		return nil, nil
	}
	return picked, nil
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
