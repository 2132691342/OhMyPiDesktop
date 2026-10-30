package env

import (
	"context"
	"os"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// 探测：回答「装在哪、什么版本、哪个渠道装的」。候选来源优先级固定：设置里的显式路径 → PATH；
// 复用机器上已有的那份是默认策略，用户可能已经在终端里用它，不该悄悄换成另一个版本。
// 本包不做什么：不下载、不安装、不改 PATH，缺引擎时只回答「缺什么」。

// Detect 探测某引擎。找不到时返回 Exe 为空的 Identity（不是错误）。
// customExe 来自设置（用户显式指定的路径），空表示没指定；ctx 必须带得上取消：
// 探测要起子进程（pi 的 --help 要起一次 Node），调用方是 HTTP 请求——客户端断开后这次探测就该跟着放弃。
func Detect(ctx context.Context, customExe string, kind engine.Kind) (engine.Identity, error) {
	// 整条探测链再套一道总超时：单条命令已有 ProbeTimeout，但候选有四个、
	// 每个可能要跑两次（--version 与 --help），最坏情况叠起来是分钟级。
	// 而探测在首屏路径上——卡住的表现就是「概览页一直说检测中」。
	ctx, cancel := context.WithTimeout(ctx, detectBudget)
	defer cancel()

	candidates := engine.Candidates(customExe, string(kind), InstallCandidates(kind)...)
	id := engine.Detect(ctx, kind, candidates)
	if id.Exe != "" {
		id.Channel = engine.DetectChannel(kind, id.Exe, homeDir(), localAppData())
		return id, nil
	}
	// 显式路径失效时给个可诊断的提示，但仍然是「没找到」而不是失败：
	// 界面据此显示「路径无效」+ 重新选择，而不是弹错误。
	return engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}, nil
}

// detectBudget 是一条探测链的总预算。
// 三个候选取值（显式路径 / PATH / 安装落点）各跑一次 --version、疑似 pi 再补一次 --help，
// 6 秒足够跑完；而“卡死”要以「探测超时」而不是「无限挂起」的形式告诉用户。
const detectBudget = 6 * time.Second

// DetectReason 顺便回答「为什么没探到」；探测成功时 reason 为空串，那时结论可信，不需要解释。
// 三种原因给用户的下一步完全不同，混成一句「未安装」会让用户去装一个其实已经装好的引擎：
// missing=PATH 与显式路径都无候选（真没装，去装）；bad-path=显式路径配了但文件不存在或不是目标引擎（去改设置）；timeout=候选存在但探测命令超时没给出版本（装了却跑不起来）。
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
	if len(engine.Candidates(customExe, string(kind), InstallCandidates(kind)...)) == 0 {
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
