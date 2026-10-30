package service

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// 跑引擎自己的 CLI 取数据（插件列表、扩展列表）。走 CLI 而不是读包目录：
// 包的落盘布局是引擎私有契约（今天在 node_modules，明天可能进内容寻址存储），CLI 的输出是它承诺的接口。

// runJSON 跑一条返回 JSON 的 CLI 并解到 out。
func runJSON(ctx context.Context, exe string, out any, args ...string) error {
	raw, err := runText(ctx, exe, 60*time.Second, args...)
	if err != nil {
		return pkg.Wrap(pkg.CodePluginFailed, exe+" "+strings.Join(args, " ")+" 执行失败", err)
	}
	body := extractJSON(raw)
	if body == "" {
		return pkg.New(pkg.CodeProtocol, "命令没有返回 JSON", truncate(raw, 400))
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return pkg.Wrap(pkg.CodeProtocol, "返回的 JSON 无法解析", err)
	}
	return nil
}

// runText 跑一条 CLI 并返回合并输出。
func runText(ctx context.Context, exe string, timeout time.Duration, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// extractJSON 剥掉 CLI 打在 JSON 前后的进度行。
func extractJSON(raw string) string {
	start := strings.IndexAny(raw, "{[")
	if start < 0 {
		return ""
	}
	end := strings.LastIndexAny(raw, "}]")
	if end <= start {
		return ""
	}
	return raw[start : end+1]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
