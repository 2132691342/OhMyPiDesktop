package env

import (
	"context"
	"os"

	"OhMyPiDesktop/backend/engine"
)

// 探测：回答「装在哪、什么版本、哪个渠道装的」。
//
// 两个候选来源，优先级固定：设置里的显式路径 → PATH。
// 复用机器上已有的那份是默认策略：用户可能已经在终端里用它，
// 我们不该悄悄换成另一个版本。
//
// 渠道（Channel）只用于决定「卸载按钮存不存在」，探测本身不依赖它。

// Detect 探测某引擎。找不到时返回 Exe 为空的 Identity（不是错误）。
//
// customExe 来自设置（用户显式指定的路径）；空表示没指定。
func Detect(customExe string, kind engine.Kind) (engine.Identity, error) {
	candidates := engine.Candidates(customExe, string(kind))
	id := engine.Detect(context.Background(), kind, candidates)
	if id.Exe != "" {
		id.Channel = engine.DetectChannel(kind, id.Exe, homeDir(), localAppData())
		return id, nil
	}
	// 显式路径失效时给个可诊断的提示，但仍然是「没找到」而不是失败：
	// 界面据此显示「路径无效」+ 重新选择，而不是弹错误。
	return engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}, nil
}

// MatchKind 判断一个可执行文件是不是目标引擎（给「手动指定路径」用）。
func MatchKind(kind engine.Kind, exe string) bool {
	id := engine.Detect(context.Background(), kind, []engine.Candidate{{Exe: exe, Source: "custom"}})
	return id.Exe != ""
}

// homeDir 与 localAppData 由本包读，而不是让 engine 读环境变量：
// engine 只该回答「这个路径是哪条渠道」，不该知道 Windows 的变量名。
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func localAppData() string { return os.Getenv("LOCALAPPDATA") }
