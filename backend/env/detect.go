package env

import (
	"context"
	"strings"

	"OhMyPiDesktop/backend/engine"
)

// 探测与安装提示：应用不安装引擎，只回答「装在哪、什么版本、缺什么」。
//
// 三个候选来源，优先级固定：设置里的显式路径 → 自管目录 → PATH。
// 复用机器上已有的那份是默认策略：用户可能已经在终端里用它，
// 我们不该悄悄换成另一个版本。

// Detect 探测某引擎。找不到时返回 Exe 为空的 Identity（不是错误）。
//
// customExe 来自设置（用户显式指定的路径）；空表示没指定。
func Detect(customExe string, kind engine.Kind) (engine.Identity, error) {
	candidates := engine.Candidates(
		customExe,
		managedCandidate(kind),
		string(kind),
	)
	id := engine.Detect(context.Background(), kind, candidates)
	if id.Exe != "" {
		return id, nil
	}
	// 显式路径失效时给个可诊断的提示，但仍然是「没找到」而不是失败：
	// 界面据此显示「路径无效」+ 重新选择，而不是弹错误。
	return engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}, nil
}

// managedCandidate 返回自管安装目录里的可执行文件（可能不存在）。
func managedCandidate(kind engine.Kind) string {
	return engine.ManagedDir(kind) + pathSep() + engine.ExeName(kind)
}

func pathSep() string {
	if strings.Contains(engine.ManagedDir(engine.KindOMP), "/") {
		return "/"
	}
	return `\`
}

// MatchKind 判断一个可执行文件是不是目标引擎（给「手动指定路径」用）。
func MatchKind(kind engine.Kind, exe string) bool {
	id := engine.Detect(context.Background(), kind, []engine.Candidate{{Exe: exe, Source: "custom"}})
	return id.Exe != ""
}
