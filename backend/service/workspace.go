// 工作区：文件浏览与 git 读写。
//
// 这一域回答一个真问题：「助手到底改了什么，我能不能收成一次提交」。
// 读全开放（status / diff / 历史 / 文件），写限白名单动作，且每个动作都由用户点下去。
//
// 边界：只在这里调 `git` 可执行文件。git 不存在或目录不是仓库，都返回带
// Available=false 的正常结果，不抛错误——那两种状态界面都要能解释清楚。
package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/git"
	"OhMyPiDesktop/backend/pkg"
)

// maxFileEntries 单次列举上限：防手滑把整个磁盘根甩给前端。
const maxFileEntries = 500

// maxPreviewBytes 单文件预览上限。超过就截断并告诉界面，不假装读全了。
const maxPreviewBytes = 512 * 1024

// ---------------- 文件 ----------------

// Files 列出工作区内 rel 目录的条目；目录在前，名称排序。
func (a *App) Files(rel string) (*domain.FileListVO, error) {
	root := a.Workspace()
	if strings.TrimSpace(root) == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "还没有设置工作目录，先选一个文件夹", "")
	}
	full, err := pkg.SafeJoin(root, rel)
	if err != nil {
		return nil, err
	}
	if !pkg.DirExists(full) {
		return nil, pkg.New(pkg.CodeNotFound, "目录不存在", rel)
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNotFound, "读取目录失败", err)
	}
	out := make([]domain.FileEntryVO, 0, len(entries))
	for _, e := range entries {
		// 隐藏条目与系统噪音文件不进列表：这是一个给人看的浏览器。
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "$") {
			continue
		}
		info, _ := e.Info()
		item := domain.FileEntryVO{EntryName: e.Name(), IsDir: e.IsDir()}
		if info != nil && !e.IsDir() {
			item.SizeBytes = info.Size()
		}
		out = append(out, item)
		if len(out) >= maxFileEntries {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].EntryName) < strings.ToLower(out[j].EntryName)
	})
	return &domain.FileListVO{
		DirPath:   pkg.RelPath(root, full),
		Entries:   out,
		Truncated: len(out) >= maxFileEntries,
	}, nil
}

// ReadFile 读一个文件的预览。二进制文件不返回内容，只给标记。
func (a *App) ReadFile(rel string) (*domain.FileReadVO, error) {
	root := a.Workspace()
	full, err := pkg.SafeJoin(root, rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		return nil, pkg.New(pkg.CodeNotFound, "文件不存在", rel)
	}
	head := make([]byte, 8000)
	f, err := os.Open(full)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNotFound, "打不开文件", err)
	}
	n, _ := f.Read(head)
	_ = f.Close()
	if isBinary(head[:n]) {
		return &domain.FileReadVO{FilePath: rel, SizeBytes: st.Size(), IsBinary: true}, nil
	}
	limit := int64(maxPreviewBytes)
	readLen := st.Size()
	if readLen > limit {
		readLen = limit
	}
	buf := make([]byte, readLen)
	f2, err := os.Open(full)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNotFound, "打不开文件", err)
	}
	n2, _ := f2.Read(buf)
	_ = f2.Close()
	return &domain.FileReadVO{
		FilePath:  rel,
		Content:   string(buf[:n2]),
		SizeBytes: st.Size(),
		Truncated: st.Size() > limit,
	}, nil
}

// isBinary 用「头部有没有 NUL」判断：文本文件极少含 NUL，二进制几乎必含。
func isBinary(head []byte) bool {
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}

// ---------------- git ----------------

// GitStatus 读版本状态。不是仓库或没装 git 时返回标记而不是错误。
func (a *App) GitStatus(rel string) (*domain.GitStatusVO, error) {
	root, err := a.gitRoot(rel)
	if err != nil {
		return nil, err
	}
	return git.Status(root)
}

// GitDiff 取 rel 的差异；rel 为空表示整个工作区。
func (a *App) GitDiff(rel string) (*domain.GitDiffVO, error) {
	root := a.Workspace()
	return git.Diff(root, filepath.ToSlash(rel))
}

// GitHistory 提交列表。
func (a *App) GitHistory(limit int) ([]domain.GitCommitVO, error) {
	return git.History(a.Workspace(), limit)
}

// GitCommitDetail 单次提交的完整内容。
func (a *App) GitCommitDetail(hash string) (*domain.GitCommitDetailVO, error) {
	return git.CommitDetail(a.Workspace(), hash)
}

// GitBranches 分支清单。
func (a *App) GitBranches() ([]domain.GitBranchVO, error) {
	return git.Branches(a.Workspace())
}

// GitStage 入栈；写后返回重读的状态。
func (a *App) GitStage(paths []string) (*domain.GitStatusVO, error) {
	return git.Stage(a.Workspace(), paths)
}

// GitUnstage 出栈。
func (a *App) GitUnstage(paths []string) (*domain.GitStatusVO, error) {
	return git.Unstage(a.Workspace(), paths)
}

// GitDiscard 丢弃工作区改动（危险动作，界面必须二次确认）。
func (a *App) GitDiscard(paths []string) (*domain.GitStatusVO, error) {
	return git.Discard(a.Workspace(), paths)
}

// GitResolve 把冲突文件标记为已解决。
func (a *App) GitResolve(paths []string) (*domain.GitStatusVO, error) {
	return git.ResolveConflict(a.Workspace(), paths)
}

// GitCommit 提交。
func (a *App) GitCommit(message string, amend bool) (*domain.GitCommitRESP, error) {
	if strings.TrimSpace(message) == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "写一句提交信息", "")
	}
	return git.Commit(a.Workspace(), message, amend)
}

// GitCreateBranch 新建分支。
func (a *App) GitCreateBranch(name string) (*domain.GitStatusVO, error) {
	return git.CreateBranch(a.Workspace(), name)
}

// GitSwitchBranch 切换分支。
func (a *App) GitSwitchBranch(name string) (*domain.GitStatusVO, error) {
	return git.SwitchBranch(a.Workspace(), name)
}

// GitDeleteBranch 删除分支。
func (a *App) GitDeleteBranch(name string) (*domain.GitStatusVO, error) {
	return git.DeleteBranch(a.Workspace(), name)
}

// GitMerge 合并 / 变基到当前分支。
func (a *App) GitMerge(name string, rebase bool) (*domain.GitStatusVO, error) {
	return git.Merge(a.Workspace(), name, rebase)
}

// GitStateAction 处理中间态（continue / abort）。
func (a *App) GitStateAction(action string) (*domain.GitStatusVO, error) {
	return git.UpdateState(a.Workspace(), action)
}

// GitInit 初始化仓库。
func (a *App) GitInit(branch string) (*domain.GitStatusVO, error) {
	return git.Init(a.Workspace(), branch)
}

// GitAddRemote 加一个远端。
func (a *App) GitAddRemote(name, url string) (*domain.GitStatusVO, error) {
	return git.AddRemote(a.Workspace(), name, url)
}

// GitPull 拉取。
func (a *App) GitPull(remote, branch string, rebase bool) (*domain.GitStatusVO, error) {
	return git.Pull(a.Workspace(), remote, branch, rebase)
}

// GitPush 推送（分级：none → lease → force，界面逐级确认）。
func (a *App) GitPush(req domain.GitPushREQ) (*domain.GitStatusVO, error) {
	return git.Push(a.Workspace(), req)
}

// GitFetch 只取不算。
func (a *App) GitFetch(remote string) (*domain.GitStatusVO, error) {
	return git.Fetch(a.Workspace(), remote)
}

// gitRoot 允许面板指向工作区里的子目录：rel 为空就是工作区本身。
func (a *App) gitRoot(rel string) (string, error) {
	root := a.Workspace()
	if strings.TrimSpace(rel) == "" {
		return root, nil
	}
	full, err := pkg.SafeJoin(root, rel)
	if err != nil {
		return "", err
	}
	if !pkg.DirExists(full) {
		return root, nil
	}
	return full, nil
}
