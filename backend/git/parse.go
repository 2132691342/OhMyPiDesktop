package git

// 解析与执行：porcelain 输出、diff 正文、以及跑 git 的那一层。
//
// 解析放在一处的原因：status 与 diff 的路径、编码、非 ASCII 转义规则相同；
// 两处各写一份的结果是「改了 status 的路径处理，diff 没跟上」。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func parseDiff(text string, fileHeader func(string) string) ([]domain.GitDiffLineVO, int) {
	out := make([]domain.GitDiffLineVO, 0, 128)
	oldNo, newNo, dropped := 0, 0, 0
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header := diffPath(line)
			if fileHeader != nil {
				header = fileHeader(header)
			}
			out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineFile, LineText: header})
			continue
		case strings.HasPrefix(line, "@@"):
			oldNo, newNo = hunkPos(line)
			out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineHunk, LineText: line})
			continue
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file ") ||
			strings.HasPrefix(line, "deleted file ") || strings.HasPrefix(line, "similarity ") ||
			strings.HasPrefix(line, "rename ") || strings.HasPrefix(line, "old mode ") ||
			strings.HasPrefix(line, "new mode ") || strings.HasPrefix(line, "Binary files "):
			continue
		}
		if len(out) >= maxLines {
			dropped++
			continue
		}
		// @@ 头给出的就是首行行号，先落号再推进，否则整块行号偏移一行。
		switch {
		case strings.HasPrefix(line, "+"):
			out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineAdd, NewNo: newNo, LineText: line[1:]})
			newNo++
		case strings.HasPrefix(line, "-"):
			out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineDel, OldNo: oldNo, LineText: line[1:]})
			oldNo++
		case strings.HasPrefix(line, "\\"):
			// `\ No newline at end of file` 是注解，不是内容行
			continue
		default:
			body := line
			if len(line) > 0 {
				body = line[1:]
			}
			out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineCtx, OldNo: oldNo, NewNo: newNo, LineText: body})
			oldNo++
			newNo++
		}
	}
	// 扫描提前中止（超长行）必须算进丢弃数：静默丢掉的差异比截断更糟。
	if err := sc.Err(); err != nil {
		dropped++
	}
	return out, dropped
}

// hunkPos 解析 @@ -a,b +c,d @@ 拿到两侧起始行号。
func hunkPos(line string) (int, int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0
	}
	return atoiPrefix(strings.TrimPrefix(fields[1], "-")), atoiPrefix(strings.TrimPrefix(fields[2], "+"))
}

// atoiPrefix 取 "12,3" 里的起始行号。
func atoiPrefix(s string) int {
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// diffPath 从 `diff --git a/x b/x` 里取改动后的路径。
func diffPath(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return strings.TrimSpace(line)
	}
	return strings.TrimPrefix(fields[len(fields)-1], "b/")
}

// splitLines 按 LF 切行并去掉末尾空行与 CR。
func splitLines(s string) []string {
	rows := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r = strings.TrimSuffix(r, "\r"); r != "" || len(out) > 0 {
			out = append(out, r)
		}
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// result 是一次 git 调用的输出；code 是退出码，committed 只对写命令有意义。
type result struct {
	stdout string
	stderr string
	code   int
}

// output 合并 stdout 与 stderr 供错误归因：git 把大部分失败原因写 stdout
// （`nothing to commit`、`no upstream branch` 都在 stdout），只看 stderr 会退化成 `exit status 1`。
func (r result) output() string {
	parts := make([]string, 0, 2)
	if s := strings.TrimSpace(r.stderr); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(r.stdout); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// failed 报告 git 到底有没有跑起来并返回了非零退出码。
// 退出码 0 而 err 非空（超时 / 起不了进程）不是「git 拒绝了这个操作」，不能当 1019 归因。
func (r result) failed() bool { return r.code != 0 }

// run 执行一条 git 命令。「是不是仓库」由调用方先探（openScope）；
// 探过之后仍出现的 128 按仓库损坏报，原文进 details。
func run(dir string, timeout time.Duration, args ...string) (result, error) {
	return runEnv(dir, timeout, nil, args...)
}

// runEnv 同 run，但可以追加环境变量（提交时补身份、推送时禁止交互式提问）。
func runEnv(dir string, timeout time.Duration, env []string, args ...string) (result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// 解析出的绝对路径优先；没解析到（没装）时退回 "git"，让标准错误形态不变。
	name := "git"
	if exe := resolveGit(); exe != "" {
		name = exe
	}
	cmd := exec.CommandContext(ctx, name, args...)
	hideConsole(cmd)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := result{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return res, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		res.code = ee.ExitCode()
	}
	if ctx.Err() != nil {
		return res, pkg.Wrap("1015", "读取版本信息超时，仓库可能太大", err)
	}
	msg := res.output()
	if msg == "" {
		msg = err.Error()
	}
	return res, pkg.Wrap("1015", "读取版本信息失败："+firstLine(msg), err)
}

// isTimeout 判断错误是不是超时（超时已经是 1015，不该被当成「不是仓库」吞掉）。
func isTimeout(err error) bool {
	return strings.Contains(err.Error(), "超时")
}

// firstLine 取一段输出的首行：完整输出可能有几十行，toast 里放不下。
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
