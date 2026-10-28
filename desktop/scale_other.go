//go:build !windows

// 非 Windows 上的屏幕度量：本应用只面向 Windows，这里只为保持可编译。
package desktop

// displayScaleOS 恒返回 1（不缩放）。
func displayScaleOS() int { return 1 }
