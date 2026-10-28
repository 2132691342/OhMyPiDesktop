//go:build !windows

package platform

import "OhMyPiDesktop/backend/pkg"

// RevealInExplorer 非 Windows 平台不支持。
func RevealInExplorer(string) error {
	return pkg.New(pkg.CodeEngineUnsupported, "当前系统不支持在资源管理器里定位", "")
}
