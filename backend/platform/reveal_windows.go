//go:build windows

// 「在资源管理器里定位」的实现：走 explorer 而不是拼 file:/// 交给默认浏览器。
// 默认浏览器把 file:// 当成要显示的文件，遇到 .go 这类未知类型会弹下载框；
// explorer /select 是「打开所在文件夹并选中它」，这是用户真正想做的事。

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"OhMyPiDesktop/backend/pkg"
)

// RevealInExplorer 在资源管理器里选中某个文件或目录。
func RevealInExplorer(path string) error {
	target := filepath.Clean(path)
	if target == "" || target == "." {
		return pkg.New(pkg.CodeBadRequest, "没有要定位的路径", "")
	}
	if _, err := os.Stat(target); err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "这个路径现在不在了："+target, err)
	}
	// 目录直接打开；文件用 /select 让它在所在文件夹里被选中。
	var cmd *exec.Cmd
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		cmd = exec.Command("explorer", target)
	} else {
		cmd = exec.Command("explorer", "/select,", target)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	// explorer 的退出码不可靠（成功也常回非零），这里只关心「能不能拉起来」。
	if err := cmd.Start(); err != nil {
		return pkg.Wrap(pkg.CodeInstallFailed, "打不开资源管理器", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
