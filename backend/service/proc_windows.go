package service

import (
	"os/exec"
	"syscall"
)

// CLI 子进程一律隐藏窗口：桌面应用里每跑一次命令闪一个黑框是不可接受的。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
