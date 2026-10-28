// Package runtime 解析本应用自己的落盘位置，并与 omp 的路径对接。
package runtime

import (
	"os"
	"path/filepath"

	"OhMyPiDesktop/backend/pkg"
)

// Paths 是本应用的落盘位置。
type Paths struct {
	DataDir      string // %LOCALAPPDATA%\OhMyPiDesktop
	SettingsFile string // 界面设置（一个 JSON 文件）
	LogDir       string // 按天切的日志
	WebViewDir   string // WebView2 缓存，放在数据目录里卸载才能一次清干净
}

// Resolve 解析应用数据目录。纯 Windows 目标，LOCALAPPDATA 一定存在；
// 缺失时退到用户目录，不让应用因为环境变量被清掉而起不来。
func Resolve() (Paths, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, pkg.Wrap(pkg.CodeStartup, "找不到可写的应用目录", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dataDir := filepath.Join(base, "OhMyPiDesktop")
	p := Paths{
		DataDir:      dataDir,
		SettingsFile: filepath.Join(dataDir, "settings.json"),
		LogDir:       filepath.Join(dataDir, "logs"),
		WebViewDir:   filepath.Join(dataDir, "webview"),
	}
	// WebView2 的缓存目录也在这里建好：Wails 拿到不存在的路径会弹错误框然后退出，
	// 而「先由安装器建目录」是靠不住的前提。
	for _, dir := range []string{p.DataDir, p.LogDir, p.WebViewDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Paths{}, pkg.Wrap(pkg.CodeStartup, "应用目录不可写："+dir, err)
		}
	}
	return p, nil
}
