package engine

// 装的是哪条渠道：卸载按钮存在的前提。判据只有两类，都只读文件系统：
// 可执行文件落在谁的目录里、旁边有没有那个包的 node_modules；不跑命令（探测已在首屏，又慢又会被 npmrc 带偏）。
// 判不出来就是 `unknown`，界面只给手动路径——官方安装器的托管布局确实没有非交互式卸载。

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectChannel 按可执行文件路径判断安装渠道；home 与 localAppData 由调用方给（本包不读环境变量）。
// 判不出来返回 ChannelUnknown，不猜、不 panic。
func DetectChannel(kind Kind, exe, home, localAppData string) Channel {
	if strings.TrimSpace(exe) == "" {
		return ChannelUnknown
	}
	dir := filepath.Dir(exe)
	lower := strings.ToLower(dir)

	// ① 直接命中包目录（用 node 跑 cli.js 的那种安装）。
	if hasPathSegment(lower, "node_modules") {
		return channelFromNodeModules(lower)
	}

	// ② Windows 的全局安装是个 shim：<prefix>\pi.cmd，包在 <prefix>\node_modules。
	// 这是最常见的一种，也是只看目录名会漏掉的一种。
	if ch := channelFromSiblingModules(dir, kind); ch != ChannelUnknown {
		return ch
	}

	// ③ omp 官方安装器的落点固定是 %LOCALAPPDATA%\omp（可被 PI_INSTALL_DIR 改）。
	if kind == KindOMP && localAppData != "" && isUnder(dir, filepath.Join(localAppData, "omp")) {
		return ChannelOMPInstaller
	}

	// ④ pi 官方安装器的托管布局：往上找 managed-install.json。
	if kind == KindPi && piManagedRoot(dir) != "" {
		return ChannelPiInstaller
	}

	// ⑤ bun 的全局 bin 目录：~/.bun/bin。
	if home != "" && isUnder(dir, filepath.Join(home, ".bun", "bin")) {
		return ChannelBun
	}
	return ChannelUnknown
}

// CanUninstall 回答「这条渠道能不能用一条命令卸掉」。
//
// 与 UninstallCommand 保持一致：有命令才能有按钮。
func CanUninstall(channel Channel) bool {
	switch channel {
	case ChannelNPM, ChannelPNPM, ChannelBun:
		return true
	default:
		return false
	}
}

// channelFromNodeModules 按路径里的目录名判渠道。
func channelFromNodeModules(lower string) Channel {
	switch {
	case strings.Contains(lower, `\.bun\`) || strings.Contains(lower, `/.bun/`):
		return ChannelBun
	case strings.Contains(lower, `\pnpm\`) || strings.Contains(lower, `/.pnpm/`):
		return ChannelPNPM
	default:
		return ChannelNPM
	}
}

// channelFromSiblingModules 看 shim 旁边有没有那个包的 node_modules。
// 只认这个包自己存在：光有 node_modules 说明不了什么（项目目录里也有），
// `<dir>\node_modules\<包名>\package.json` 存在就基本能确定是全局安装。
func channelFromSiblingModules(dir string, kind Kind) Channel {
	modules := filepath.Join(dir, "node_modules")
	pkg := PackageName(kind)
	if pkg != "" && dirExists(filepath.Join(modules, filepath.FromSlash(pkg))) {
		// pnpm 的全局目录里一定会有一个 .pnpm 存储。
		if dirExists(filepath.Join(modules, ".pnpm")) {
			return ChannelPNPM
		}
		return ChannelNPM
	}
	// bun 的全局布局：~/.bun/install/global/node_modules/<包>，bin 在 ~/.bun/bin。
	if dirExists(filepath.Join(modules, ".bin")) && strings.Contains(strings.ToLower(dir), "bun") {
		return ChannelBun
	}
	return ChannelUnknown
}

// piManagedRoot 从目录往上找 managed-install.json，找到就返回它的父目录。
// 只往上找有限层：托管布局是 <root>/releases/<version>/...，再往上就出了 pi 自己的范围，
// 继续找只会撞到别的目录里恰好同名的文件。
func piManagedRoot(dir string) string {
	cur := dir
	for i := 0; i < 5; i++ {
		if fileExists(filepath.Join(cur, "managed-install.json")) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

// hasPathSegment 判断路径里有没有某个目录名（大小写不敏感，两种分隔符都认）。
func hasPathSegment(lower, segment string) bool {
	return strings.Contains(lower, segment) ||
		strings.Contains(lower, strings.ReplaceAll(segment, `\`, `/`))
}

// isUnder 判断 path 是否在 dir 之下（大小写不敏感）。
func isUnder(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(dir), strings.ToLower(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
