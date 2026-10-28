// Package platform 收口本应用对操作系统的直接操作：开机自启注册项，以及
// 「用资源管理器打开」这类系统唤起。
//
// 它不碰引擎、不碰业务：只有窗口与引导代码需要它，业务层不该 import 它。
package platform
