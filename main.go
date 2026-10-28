// 应用入口：装配 Wails 外壳并运行。业务一律在 backend/，桌面层在 desktop/。
//
// 这里只做三件事：内嵌前端产物、读出构建期注入的版本号、把两者交给 desktop.Run。
// 任何「启动逻辑」都不该长在这个文件里——它在根目录，最容易被当成放什么都行的抽屉。
package main

import (
	"embed"
	"os"

	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

// version 由构建时注入（-ldflags "-X main.version=…"），跟着 git tag 走。
// 默认值是 dev：本地 `go run` 出来的就是开发版，不会误当成正式版。
var version = "dev"

func main() {
	if err := desktop.Run(desktop.Options{Version: version, Assets: assets}); err != nil {
		pkg.Errorf("app: 窗口启动失败: %v", err)
		os.Exit(1)
	}
}
