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
//
// 本包不做什么：不下载、不安装、不改 PATH。缺引擎时只回答「缺什么」。

// Detect 探测某引擎。找不到时返回 Exe 为空的 Identity（不是错误）。
//
// customExe 来自设置（用户显式指定的路径）；空表示没指定。
//
// ctx 必须带得上取消：探测要起子进程（pi 的 --help 要起一次 Node），
// 而调用方是 HTTP 请求——客户端断开后这次探测就该跟着放弃，
// 否则一个卡死的候选能把一颗 goroutine 与一个子进程挂到地老天荒。
func Detect(ctx context.Context, customExe string, kind engine.Kind) (engine.Identity, error) {
	candidates := engine.Candidates(customExe, string(kind))
	id := engine.Detect(ctx, kind, candidates)
	if id.Exe != "" {
		id.Channel = engine.DetectChannel(kind, id.Exe, homeDir(), localAppData())
		return id, nil
	}
	// 显式路径失效时给个可诊断的提示，但仍然是「没找到」而不是失败：
	// 界面据此显示「路径无效」+ 重新选择，而不是弹错误。
	return engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}, nil
}

// DetectReason 顺便回答「为什么没探到」。
//
// 三种原因给用户的下一步完全不同，混成一句「未安装」会让用户去
// 装一个其实已经装好的引擎：
//
//	missing  PATH 与显式路径上都没有候选 —— 真的没装，去装
//	bad-path 显式路径配了，但那个文件不存在或不是目标引擎 —— 去改设置里的路径
//	timeout  候选存在但探测命令超时没能给出版本 —— 装了却跑不起来
//
// 探测成功时 reason 为空串：那时结论可信，不需要解释。
func DetectReason(ctx context.Context, customExe string, kind engine.Kind) (engine.Identity, string) {
	id, err := Detect(ctx, customExe, kind)
	if err != nil || id.Exe != "" {
		return id, ""
	}
	custom := candidatesOf(customExe)
	if len(custom) > 0 {
		for _, c := range custom {
			if !engine.FileUsable(c.Exe) {
				return id, "bad-path"
			}
		}
		// 显式路径存在、但不是这个引擎（版本对不上 / 种类对不上）
		return id, "bad-path"
	}
	if len(engine.Candidates(customExe, string(kind))) == 0 {
		return id, "missing"
	}
	// 有候选却没探出版本：命令跑不通或超时，不是没装。
	return id, "timeout"
}

// candidatesOf 取出显式路径那部分候选（不含 PATH 上的）。
func candidatesOf(customExe string) []engine.Candidate {
	var out []engine.Candidate
	for _, c := range engine.Candidates(customExe, "") {
		if c.Source == "custom" {
			out = append(out, c)
		}
	}
	return out
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
