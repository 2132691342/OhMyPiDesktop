//go:build !windows

// 非 Windows 上的原生错误框：本应用只面向 Windows，这里只为保持可编译。
package desktop

import "OhMyPiDesktop/backend/pkg"

// fatalDialog 只写日志（没有消息框）。
func fatalDialog(title, message, detail string) {
	pkg.Errorf("%s: %s %s", title, message, detail)
}
