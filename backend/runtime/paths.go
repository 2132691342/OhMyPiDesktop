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
	ConfigDir    string // viper 配置
	LogDir       string
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
		ConfigDir:    dataDir,
		LogDir:       filepath.Join(dataDir, "logs"),
	}
	for _, dir := range []string{p.DataDir, p.LogDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Paths{}, pkg.Wrap(pkg.CodeStartup, "应用目录不可写："+dir, err)
		}
	}
	return p, nil
}
