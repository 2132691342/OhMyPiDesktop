package git

// 受控写入：白名单动作全部显式列参数；写入不可逆，
// 因此不解析不猜测——参数逐个校验，命中禁止清单一律拒绝。
//
// 这一份管「暂存、丢弃、提交」——它们只动工作区与索引，失败之后仓库状态完好。
// 改动 HEAD 或 refs 的动作在 branch.go，走网络的在 remote.go。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
	"os"
	"strings"
	"time"
)

const writeTimeout = 60 * time.Second

// writeRun 执行一条 git 写命令；失败折成 1019，归因合并 stdout 与 stderr（git 的失败原因
// 大多写在 stdout，只看 stderr 只会报 exit status 1），git 原文进 details。
func writeRun(dir string, timeout time.Duration, args ...string) (result, error) {
	return writeRunEnv(dir, timeout, nil, args...)
}

// writeRunEnv 同 writeRun，但可以追加环境变量。
func writeRunEnv(dir string, timeout time.Duration, env []string, args ...string) (result, error) {
	res, err := runEnv(dir, timeout, env, args...)
	if err == nil {
		return res, nil
	}
	if !res.failed() {
		// runEnv 在拿到退出码前就失败了（超时 / 起不了进程）：原样上交，别改语义。
		return res, err
	}
	return res, pkg.New("1019", "这个 Git 操作没有完成", res.output())
}

// requireScope 统一的前置校验：装了 git、dir 在仓库里，并交出定位结果。
func requireScope(dir string) (*scope, error) {
	if !Available() {
		return nil, pkg.New("1015", "这台电脑上还没有 Git，装好后就能看历史", "")
	}
	sc, err := openScope(dir)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, pkg.New("1015", "这个工作目录还不是 Git 仓库，初始化后才能写入", "")
	}
	return sc, nil
}

// Stage 把 paths 放进待提交区；paths 为空表示全部改动。
func Stage(dir string, paths []string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	args, err := sc.pathsArg(paths)
	if err != nil {
		return nil, err
	}
	if _, err := writeRun(sc.root, writeTimeout, append([]string{"add", "--"}, args...)...); err != nil {
		return nil, err
	}
	return Status(dir)
}

// Unstage 把 paths 从待提交区取出；只动索引，不动工作区文件。
func Unstage(dir string, paths []string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	args, err := sc.pathsArg(paths)
	if err != nil {
		return nil, err
	}
	// 首次提交前还没有 HEAD，reset HEAD 会失败；那时用不带 HEAD 的 --cached 形式清索引。
	prefix := []string{"reset", "HEAD"}
	if !sc.hasHead() {
		prefix = []string{"reset"}
	}
	cmd := append(prefix, "--")
	cmd = append(cmd, args...)
	if _, err := writeRun(sc.root, writeTimeout, cmd...); err != nil {
		return nil, err
	}
	return Status(dir)
}

// ResolveConflict 把冲突文件标记为已解决（git add 之后冲突状态消失，但还没提交）。
// 未跟踪的新文件可能不在「冲突清单」里，因此不按冲突名单限制，只按路径是否落进工作区。
func ResolveConflict(dir string, paths []string) (*domain.GitStatusVO, error) {
	return Stage(dir, paths)
}

// Discard 丢弃 paths 在工作区的改动（tracked 回到 HEAD，未跟踪直接删）。
// 这是可逆性最低的读侧动作，因此必须有显式路径：空 paths 不允许。
func Discard(dir string, paths []string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, pkg.New("1018", "丢弃改动必须显式指定文件", "不提供「全部丢弃」：那等于一次不可逆的清空")
	}
	args, err := sc.pathsArg(paths)
	if err != nil {
		return nil, err
	}
	st, err := Status(dir)
	if err != nil {
		return nil, err
	}
	// 分成两边：已跟踪的走 checkout 回 HEAD，未跟踪的只能删文件。
	var tracked, untracked []string
	want := map[string]bool{}
	for _, rel := range args {
		want[rel] = true
	}
	for _, f := range st.ChangedFiles {
		rootRel := sc.relFromRoot(f.FilePath)
		if !want[rootRel] {
			continue
		}
		if f.ChangeCode == domain.GitChangeUntracked {
			untracked = append(untracked, rootRel)
		} else {
			tracked = append(tracked, rootRel)
		}
	}
	if len(tracked) > 0 {
		// unstaged 走 checkout --；已入栈的还要先 reset，否则工作区回不去。
		if _, err := writeRun(sc.root, writeTimeout, append([]string{"checkout", "--"}, tracked...)...); err != nil {
			return nil, err
		}
	}
	for _, rel := range untracked {
		full, jerr := pkg.SafeJoin(sc.root, rel)
		if jerr != nil {
			return nil, jerr
		}
		if err := os.RemoveAll(full); err != nil {
			return nil, pkg.Wrap("1007", "删除未跟踪文件失败", err)
		}
	}
	return Status(dir)
}

// Commit 用用户填的信息提交；信息非空且不超上限，提交后返回重读的状态与新 hash。
func Commit(dir, message string, amend bool) (*domain.GitCommitRESP, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return nil, pkg.New("1016", "提交信息还没填，写一句这次改了什么", "")
	}
	if len([]rune(msg)) > domain.GitCommitMsgLimit {
		return nil, pkg.New("1016", "提交信息太长了，缩短到一句摘要", "")
	}
	args := []string{"commit", "-m", msg}
	if amend {
		// --amend 改写已有提交：只允许配 -m，不允许 --no-verify 跳过钩子。
		args = append(args, "--amend")
	}
	// 不带 --no-verify：跳过钩子不该由界面替用户决定。
	if _, err := writeRun(sc.root, writeTimeout, args...); err != nil {
		return nil, commitFailure(sc, err)
	}
	st, err := Status(dir)
	if err != nil {
		return nil, err
	}
	return &domain.GitCommitRESP{Status: st, CommitHash: sc.headHash()}, nil
}

// commitFailure 把 git 的多种拒绝形态翻成用户能照着做的下一步。
// 判定靠输出里的稳定关键词（这些串不受界面语言影响），不靠退出码——它们全是 1。
func commitFailure(sc *scope, err error) error {
	text := strings.ToLower(errText(err))
	switch {
	case strings.Contains(text, "nothing to commit"), strings.Contains(text, "no changes added to commit"),
		strings.Contains(text, "nothing added to commit"):
		return pkg.New("1017", "没有可提交的改动，先勾选文件入栈", "")
	case strings.Contains(text, "author identity unknown"), strings.Contains(text, "unable to auto-detect email"):
		return pkg.New("1020", "这台电脑上还没配置 Git 用户名与邮箱，配好后才能提交",
			"在设置页的 Git 一节填入用户名与邮箱，或执行 git config --global user.name/user.email")
	case strings.Contains(text, "unmerged"), strings.Contains(text, "conflict"):
		return pkg.New("1021", "还有文件没解决冲突，先在改动列表里逐个标记已解决", "")
	case strings.Contains(text, "hook"), strings.Contains(text, "pre-commit"):
		return pkg.New("1019", "提交钩子拒绝了这次提交", errText(err))
	}
	return err
}
