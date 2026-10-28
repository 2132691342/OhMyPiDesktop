//go:build !windows

// 非 Windows 平台给稳定的空实现：本项目只面向 Windows，
// 但测试与静态检查不该因为「本来就不可能发生的平台」而编译不过。

package platform

import "OhMyPiDesktop/backend/pkg"

// AutoStartEnabled 恒为 false。
func AutoStartEnabled() bool { return false }

// SetAutoStart 恒返回不支持。
func SetAutoStart(bool) error {
	return pkg.New(pkg.CodeEngineUnsupported, "当前系统不支持开机自启", "")
}
