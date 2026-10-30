package service

// 长任务的输出流：把一条命令的 stdout/stderr 逐行推给界面（不用 CombinedOutput：
// 装引擎要几十秒到几分钟，攒到最后一起显示等于把可能失败的长任务变成没有反馈的等待）。
// 两处必须做对：行缓冲（npm 进度用 \r，按 \n 切会把整批进度挤成一行）；有界（单行超长、总行数超多时截断，否则无换行的输出会吃掉内存）。

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
)

// 单行上限与总行数上限。安装日志通常几百行，两个上限都只是兜底。
const (
	maxLineBytes = 8 << 10
	maxOutLines  = 4000
)

// runStreaming 跑一条命令并把每一行推到 app:progress。
// 失败时返回带退出码的 AppError：退出码是判断「装没装上」的唯一依据，只看输出里的 error 字样会误报（npm 的 warning 里也有 error）。
func runStreaming(ctx context.Context, h *hub.Hub, kind engine.Kind, action AppAction, argv []string) error {
	if len(argv) == 0 {
		return pkg.New(pkg.CodeInternal, "没有要跑的命令", "")
	}
	runCtx, cancel := context.WithTimeout(ctx, appActionTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return pkg.Wrap(pkg.CodeInstallFailed, "启动安装进程失败", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return pkg.Wrap(pkg.CodeInstallFailed, "启动安装进程失败", err)
	}
	if err := cmd.Start(); err != nil {
		return pkg.Wrap(pkg.CodeInstallFailed,
			"跑不起来这条命令："+strings.Join(argv, " "), err)
	}

	emit := func(phase, line string) {
		if h == nil {
			return
		}
		h.Emit("", "app:progress", AppProgress{
			Kind: kind, Action: action, Phase: phase, Line: line,
		})
	}

	// 两路各开一个读协程：同一个管道上串行读会让 stderr 把 stdout 堵住。
	var count atomic.Int32
	done := make(chan struct{}, 2)
	go pump(stdout, emit, &count, done)
	go pump(stderr, emit, &count, done)
	<-done
	<-done

	waitErr := cmd.Wait()

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		emit("fail", "超时")
		return pkg.New(pkg.CodeEngineTimeout,
			"这条命令超时了（20 分钟）。它可能还在后台跑，稍后点「重新检测」看看结果", "")
	}
	if waitErr != nil {
		emit("fail", waitErr.Error())
		return pkg.Wrap(pkg.CodeInstallFailed, failureMessage(action, kind, waitErr), waitErr)
	}
	emit("done", "")
	return nil
}

// pump 读一路输出，按行推出去。
// \r 也当行边界：npm / bun 的进度条只回 \r 不换行，不切的话用户看到的是「一整行被反复覆盖」而不是「进度在推进」。
func pump(r io.Reader, emit func(phase, line string), count *atomic.Int32, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)
	scanner.Split(splitLines)
	for scanner.Scan() {
		if count.Load() >= maxOutLines {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		count.Add(1)
		emit("line", line)
	}
}

// splitLines 在 \n 或 \r 处切行，且不产生空行。
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// failureMessage 把退出码翻成一句能照着做的话。
func failureMessage(action AppAction, kind engine.Kind, err error) string {
	verb := "安装"
	switch action {
	case ActionUpdate:
		verb = "升级"
	case ActionUninstall:
		verb = "卸载"
	}
	msg := verb + " " + kind.Label() + " 失败"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		msg += "（退出码 " + strconv.Itoa(exitErr.ExitCode()) + "）"
	}
	return msg + "。上面是命令原文的输出，照着最后一行排查；也可以自己跑一次：" +
		engine.InstallHint(kind)
}
