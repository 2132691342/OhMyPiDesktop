//go:build windows

// 开机自启：常驻型助手必须能在登录后自己回来，否则「关到托盘」等于用不了。
//
// 写 HKCU\...\Run 而不是 HKLM：不需要管理员权限，也不会在多用户机器上互相干扰。
// 值里带 --hidden：登录后只拉起托盘，不往屏幕上弹一个空窗口。

package platform

import (
	"os"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/pkg"

	"golang.org/x/sys/windows/registry"
)

const (
	autoStartKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartValue = "OhMyPiDesktop"
)

// AutoStartEnabled 报告当前是否已设置开机自启。
func AutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	value, _, err := k.GetStringValue(autoStartValue)
	if err != nil {
		return false
	}
	// 值存在但指向的 exe 已经不在了（重装到别处）：按「没开」处理，
	// 否则用户看到开关是开的，而登录后什么都不会发生。
	exe, exeErr := os.Executable()
	if exeErr != nil {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(filepath.Clean(exe)))
}

// SetAutoStart 开关开机自启。开启时写入当前 exe 的绝对路径。
func SetAutoStart(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, autoStartKey, registry.SET_VALUE)
		if err != nil {
			return pkg.Wrap(pkg.CodeSettingsWrite, "打不开启动项注册表", err)
		}
		defer k.Close()
		if err := k.DeleteValue(autoStartValue); err != nil && err != registry.ErrNotExist {
			return pkg.Wrap(pkg.CodeSettingsWrite, "取消开机自启失败", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return pkg.Wrap(pkg.CodeSettingsWrite, "取不到程序自己的路径", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartKey, registry.SET_VALUE)
	if err != nil {
		return pkg.Wrap(pkg.CodeSettingsWrite, "打不开启动项注册表", err)
	}
	defer k.Close()
	cmd := `"` + filepath.Clean(exe) + `" --hidden`
	if err := k.SetStringValue(autoStartValue, cmd); err != nil {
		return pkg.Wrap(pkg.CodeSettingsWrite, "写不开机自启", err)
	}
	return nil
}
