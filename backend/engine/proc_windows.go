package engine

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// Windows 下的进程控制。两条纪律：
// 子进程一律 CREATE_NO_WINDOW（引擎拉起时若带控制台窗口，桌面应用会闪黑框）；
// 杀进程走 taskkill /T（引擎自己会拉起子代理与 bash 工具进程，只杀父进程会把它们留在后台）。

const createNoWindow = 0x08000000

func environ() []string { return os.Environ() }

func hideWindowImpl(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := kill.Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
