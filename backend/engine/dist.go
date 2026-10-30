package engine

// 引擎的分发事实：它叫哪个 npm 包、官方安装器是哪一行、自己怎么升级。
//
// 这一层只回答「装它要跑什么命令」，不执行任何东西。执行在 service/apps.go。
// 只放**官方文档里原样写着**的命令：跟着上游改动改这一处，比在任何地方手写一条
// 「看起来能跑」的命令都安全——命令写错了，用户看到的只是安装失败，
// 却不知道失败的是我们的命令还是引擎的安装器。

// Channel 是一条安装渠道。它决定「卸载用哪条命令」——
// 装与升级都交给引擎自己的安装器，只有卸载需要知道当初是谁装的。
type Channel string

const (
	// ChannelNPM 是 npm 全局安装（`npm i -g <包>`）。
	ChannelNPM Channel = "npm"
	// ChannelPNPM 是 pnpm 全局安装。
	ChannelPNPM Channel = "pnpm"
	// ChannelBun 是 bun 全局安装。
	ChannelBun Channel = "bun"
	// ChannelOMPInstaller 是 omp 官方 PowerShell 安装器（装进 %LOCALAPPDATA%\omp）。
	ChannelOMPInstaller Channel = "omp-installer"
	// ChannelPiInstaller 是 pi 官方安装器（装进 <agent>/install 的托管布局）。
	ChannelPiInstaller Channel = "pi-installer"
	// ChannelUnknown 表示「看得见这个文件，但不知道谁装的」。
	ChannelUnknown Channel = "unknown"
)

// PackageName 返回引擎在 npm 上的包名；没有 npm 分发时返回空串。
//
// 它同时用于「装要跑什么」与「这个 shim 旁边有没有同名包目录」，
// 所以只写一处——写两处就会有一处写错（@oh-my-pi 与 @ohmy-pi 差一个连字符，
// 而那种错只会在用户点安装时暴露）。
func PackageName(kind Kind) string {
	if kind == KindOMP {
		return "@oh-my-pi/pi-coding-agent"
	}
	if kind == KindPi {
		return "@earendil-works/pi-coding-agent"
	}
	return ""
}

// NpmInstallArgs 是官方文档里的全局安装命令参数（不含 `npm` 本身）。
//
// pi 要 `--ignore-scripts`：官方文档就是这么写的，去掉会让装出来的东西
// 与官方分发的形态不一致。omp 返回空——它的 npm 包要求 bun 运行时
// （package.json 的 engines 只有 bun），机器上没有 bun 时装不上，
// 所以 omp 只走官方安装器。
func NpmInstallArgs(kind Kind) []string {
	if kind == KindPi {
		return []string{"i", "-g", "--ignore-scripts", PackageName(kind)}
	}
	return nil
}

// InstallerScript 返回 Windows 官方安装器的一行脚本原文；没有安装器时返回空串。
//
// 这一行会被原样交给 powershell -Command。它必须是常量：
// 拼接出来的命令等于我们开始自己实现安装流程。
func InstallerScript(kind Kind) string {
	if kind == KindOMP {
		return "irm https://omp.sh/install.ps1 | iex"
	}
	return ""
}

// UpdateArgs 是引擎自己的升级子命令。
//
// 用引擎自己的 updater 而不是重跑安装器：它认得出 brew / mise / nix / bun / npm /
// 预编译二进制各自的升级方式，而重跑安装器只认得自己那一种。
// 返回空表示这个引擎没有自升级能力（那就只给安装命令原文）。
func UpdateArgs(kind Kind) []string {
	if kind == KindOMP {
		return []string{"update"}
	}
	if kind == KindPi {
		return []string{"update", "self"}
	}
	return nil
}

// UninstallCommand 返回某渠道下的卸载命令；第一个元素是可执行文件，其余是参数。
//
// 只有 npm / pnpm / bun 三种渠道知道怎么卸载。官方安装器装进来的那份
// 没有非交互式卸载路径（omp 的安装器没有 uninstall，pi 的安装器要按 `u` 键），
// 所以返回空——界面据此不给按钮，只给手动路径。
func UninstallCommand(kind Kind, channel Channel) []string {
	pkg := PackageName(kind)
	if pkg == "" {
		return nil
	}
	switch channel {
	case ChannelNPM:
		return []string{"npm", "uninstall", "-g", pkg}
	case ChannelPNPM:
		return []string{"pnpm", "remove", "-g", pkg}
	case ChannelBun:
		return []string{"bun", "remove", "-g", pkg}
	default:
		return nil
	}
}

// InstallHint 返回该引擎的安装命令原文（给人复制，也给人看懂我们要跑什么）。
func InstallHint(kind Kind) string {
	if script := InstallerScript(kind); script != "" {
		return script
	}
	if args := NpmInstallArgs(kind); len(args) > 0 {
		return "npm " + joinArgs(args)
	}
	return ""
}

// DocsURL 是引擎自己的文档站。装不上时给用户一个能继续查的地方。
func DocsURL(kind Kind) string {
	if kind == KindOMP {
		return "https://omp.sh"
	}
	return "https://pi.dev"
}

// joinArgs 拼一条命令行原文（只用于展示）。
func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
