// 屏幕度量：窗口尺寸要用它换算，见 app.go 的 windowPhysicalSize。
package desktop

// displayScale 返回主屏缩放比（1 = 100%，2 = 200%）。
// 本应用只面向 Windows，具体实现在 scale_windows.go；这里留一个平台无关的
// 入口，好让 app.go 不必带 build tag。
func displayScale() int { return displayScaleOS() }
