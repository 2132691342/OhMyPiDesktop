//go:build windows

// 原生错误框：UI 还没起来时唯一能通知用户的通道。
//
// 发布版用 -H windowsgui 构建，没有控制台——stderr 上的原因没人看得见。
// 数据目录都解析不出来时，窗口也不存在，这时只剩一个系统消息框能把话说出去。
package desktop

import (
	"syscall"
	"unsafe"

	"OhMyPiDesktop/backend/pkg"
)

// mbIconError 是消息框的错误图标。
const mbIconError = 0x00000010

var procMessageBox = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

// fatalDialog 弹出原生错误框并写日志，然后返回（由调用方决定退出）。
func fatalDialog(title, message, detail string) {
	pkg.Errorf("%s: %s %s", title, message, detail)
	text := message
	if detail != "" {
		text += "\n\n" + detail
	}
	titlePtr, err := syscall.UTF16PtrFromString("OhMyPiDesktop — " + title)
	if err != nil {
		return
	}
	textPtr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	// 显示一个模态框：用户看到原因才能去修，不看到只会以为双击没反应。
	_, _, _ = procMessageBox.Call(0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbIconError)
}
