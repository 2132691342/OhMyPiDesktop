//go:build windows

// 屏幕度量：缩放比与可用区，用原生 API 读。
// 不用 application.Screen：它在建窗之前还没初始化（GetPrimary 返回 nil），
// 而窗口尺寸必须在建窗那一刻就定下来。
package desktop

import "syscall"

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	shcore = syscall.NewLazyDLL("shcore.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC           = user32.NewProc("GetDC")
	procReleaseDC       = user32.NewProc("ReleaseDC")
	procGetDpiForSystem = user32.NewProc("GetDpiForSystem")
	procSetDPIContext   = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDpi   = shcore.NewProc("SetProcessDpiAwareness")
	procGetDeviceCaps   = gdi32.NewProc("GetDeviceCaps")
)

// 屏幕度量的常量。写在调用点旁边会淹没逻辑，集中在这里。
const (
	logPixelsX    = 88          // GetDeviceCaps: 水平 DPI
	smCxScreen    = 0           // GetSystemMetrics: 屏幕宽（物理）
	smCyScreen    = 1           // GetSystemMetrics: 屏幕高（物理）
	defaultDPI    = 96          // 100% 缩放
	dpiAwareV2    = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	perMonitorDpi = 2           // PROCESS_PER_MONITOR_DPI_AWARE
)

// ensureDPIAware 声明进程为 per-monitor DPI 感知，必须在任何取 DPI 的调用之前执行。
// manifest 里的声明对某些启动路径不生效（实测经 Start-Process 拉起时 GetDpiForSystem
// 仍报 96），显式声明之后才稳定返回真实值。失败不影响运行：最坏情况是按 100% 布局。
func ensureDPIAware() {
	if procSetDPIContext.Find() == nil {
		if r, _, _ := procSetDPIContext.Call(dpiAwareV2); r != 0 {
			return
		}
	}
	if procSetProcessDpi.Find() == nil {
		procSetProcessDpi.Call(perMonitorDpi)
	}
}

// displayScaleOS 返回主屏缩放比（1 = 100%，2 = 200%）。
// 优先 GetDpiForSystem（Win10 1607+）；老系统退回 GetDeviceCaps(LOGPIXELSX)，
// 两者都失败返回 1。
func displayScaleOS() int {
	ensureDPIAware()

	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			if scale := dpiToScale(int(dpi)); scale > 0 {
				return scale
			}
		}
	}

	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return 1
	}
	defer procReleaseDC.Call(0, hdc)
	dpi, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX)
	if scale := dpiToScale(int(dpi)); scale > 0 {
		return scale
	}
	return 1
}

// dpiToScale 把 DPI 折成整数缩放比：四舍五入到最近的整档（96→1、144→2，150% 向上取，
// 宁可窗口略大也不要把内容挤坏），1.25 档（120 DPI）也归到 1，避免窗口无谓变大。
func dpiToScale(dpi int) int {
	if dpi <= 0 {
		return 0
	}
	scale := (dpi + 48) / defaultDPI
	if scale < 1 {
		return 1
	}
	return scale
}
