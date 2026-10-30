package git

// 分支与中间态：新建、切换、删除、合并，以及冲突时的继续与中止。
// 与「暂存 / 提交」分开的原因：这些动作会改动 HEAD 或 refs，失败之后
// 仓库可能停在一个中间态（合并 / 变基中），界面需要给出继续与中止两条路。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
	"strings"
)

func CreateBranch(dir, name string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	if !validBranchName(name) {
		return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法",
			"只允许字母、数字、点、下划线、斜杠与连字符，且不能以连字符开头")
	}
	if _, err := writeRun(sc.root, writeTimeout, "checkout", "-b", strings.TrimSpace(name)); err != nil {
		return nil, err
	}
	return Status(dir)
}

// SwitchBranch 切到已有分支；不带 --force，避免用索引覆盖工作区改动。
func SwitchBranch(dir, name string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	if !validBranchName(name) {
		return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", name)
	}
	if _, err := writeRun(sc.root, writeTimeout, "checkout", strings.TrimSpace(name)); err != nil {
		return nil, checkoutFailure(err)
	}
	return Status(dir)
}

// DeleteBranch 删除已合并的本地分支；未合并的分支 git 会拒绝（这里不提供 -D）。
func DeleteBranch(dir, name string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	if !validBranchName(name) {
		return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", name)
	}
	if _, err := writeRun(sc.root, writeTimeout, "branch", "-d", strings.TrimSpace(name)); err != nil {
		return nil, pkg.New(pkg.CodeGitRejected, "这个分支删不掉", errText(err)+"\n未合并的分支要先用自己的 Git 工具处理")
	}
	return Status(dir)
}

// Merge 把 name 合并进当前分支；rebase 为真时用变基。
// 冲突不算失败：git 留下未完成的合并，界面要接着引导解决。
func Merge(dir, name string, rebase bool) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	if !validBranchName(name) {
		return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", name)
	}
	return mergeLike(dir, sc, rebase, "merge", "rebase", strings.TrimSpace(name))
}

// mergeLike 执行 merge 或 rebase 并按结果决定是否把「有冲突」当成功返回。
func mergeLike(dir string, sc *scope, rebase bool, mergeCmd, rebaseCmd, target string) (*domain.GitStatusVO, error) {
	args := []string{mergeCmd, "--no-edit", target}
	if rebase {
		args = []string{rebaseCmd, target}
	}
	if _, err := writeRun(sc.root, writeTimeout, args...); err != nil {
		st, serr := Status(dir)
		// 冲突时 git 退出码是 1，但仓库里已经留下未完成的合并：这是可继续的状态，不是失败。
		if serr == nil && st.Conflict {
			return st, nil
		}
		return nil, mergeFailure(err)
	}
	return Status(dir)
}

// mergeFailure 把合并失败翻成可执行的下一步。
func mergeFailure(err error) error {
	text := strings.ToLower(errText(err))
	switch {
	case strings.Contains(text, "local changes"), strings.Contains(text, "would be overwritten"):
		return pkg.New(pkg.CodeGitRejected, "工作区有未提交的改动，先提交或丢弃后再合并", errText(err))
	case strings.Contains(text, "not something we can merge"), strings.Contains(text, "not a valid"):
		return pkg.New(pkg.CodeGitBadInput, "找不到这个分支", errText(err))
	}
	return err
}

// checkoutFailure 把切分支失败翻成可执行的下一步。
func checkoutFailure(err error) error {
	text := strings.ToLower(errText(err))
	if strings.Contains(text, "local changes") || strings.Contains(text, "would be overwritten") {
		return pkg.New(pkg.CodeGitRejected, "工作区有未提交的改动，先提交或丢弃后再切换分支", errText(err))
	}
	return err
}

// UpdateState 继续或中止一个未完成的合并 / 变基。
func UpdateState(dir, action string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	st := sc.state()
	if st.StateCode == domain.GitStateNone {
		return nil, pkg.New(pkg.CodeGitBadInput, "当前没有进行中的合并或变基", "")
	}
	verb := strings.TrimSpace(action)
	if verb != domain.GitStateContinue && verb != domain.GitStateAbort {
		return nil, pkg.New(pkg.CodeGitBadInput, "只支持继续或中止", verb)
	}
	cmd := stateCommand(st.StateCode, verb)
	if len(cmd) == 0 {
		return nil, pkg.New(pkg.CodeGitBadInput, "这个中间状态不支持该操作", st.StateCode)
	}
	if _, err := writeRun(sc.root, writeTimeout, cmd...); err != nil {
		text := strings.ToLower(errText(err))
		if verb == domain.GitStateContinue && strings.Contains(text, "unmerged") {
			return nil, pkg.New(pkg.CodeConflictUnresolved, "还有文件没解决冲突，先在改动列表里逐个标记已解决", "")
		}
		return nil, err
	}
	return Status(dir)
}

// stateCommand 按中间状态与动作给出具体的 git 子命令。
func stateCommand(kind, action string) []string {
	switch kind {
	case domain.GitStateMerge, domain.GitStateCherryPick, domain.GitStateRevert:
		if action == domain.GitStateContinue {
			return []string{"commit", "--no-edit"}
		}
		// 三种状态各有一个 --abort，拣选与回退是 cherry-pick --abort / revert --abort
		return []string{kind, "--abort"}
	case domain.GitStateRebase:
		if action == domain.GitStateContinue {
			// 不设 GIT_EDITOR=: 时变基会拉起编辑器，界面会永远等下去。
			return []string{"rebase", "--continue"}
		}
		return []string{"rebase", "--abort"}
	}
	return nil
}
