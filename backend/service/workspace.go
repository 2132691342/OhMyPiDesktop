// 工作区：文件浏览与 git 读写。回答的是「助手到底改了什么，能不能收成一次提交」：
// 读全开放（status / diff / 历史 / 文件），写限白名单动作且每个都由用户点下去。
// 只在这里调 `git`：git 不存在或目录不是仓库都返回 Available=false 的正常结果，不抛错误。
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

// 搜索的限界：工作区可能有几十万个文件，一次请求必须封顶，
// 否则一次手滑的查询就是一次全盘遍历。
const (
	// maxSearchHits 返回的命中数上限
	maxSearchHits = 50
	// maxSearchScan 最多看多少个条目就停，命中不满也停
	maxSearchScan = 20000
	// maxSearchDepth 目录深度上限，再深的不看
	maxSearchDepth = 10
)

// searchSkipDirs 这些目录不看：依赖、构建产物与缓存，命中它们只会干扰。
var searchSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"__pycache__":  true,
	"vendor":       true,
}

// SearchFiles 在整个工作区里按文件名找文件（大小写不敏感子串）。
// 只回文件不回目录：@ 引用要的是能发进对话的东西。
func (a *App) SearchFiles(query string, limit int) (*domain.FileSearchVO, error) {
	root := a.Workspace()
	if strings.TrimSpace(root) == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "还没有设置工作目录，先选一个文件夹", "")
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "先输入要找的文件名", "")
	}
	if limit <= 0 || limit > maxSearchHits {
		limit = maxSearchHits
	}
	hits := make([]domain.FileHitVO, 0, 16)
	scanned := 0
	truncated := false
	// walk 返回 false 表示「已经够了，整棵树都不用再看」。
	var walk func(full, rel string, depth int) bool
	walk = func(full, rel string, depth int) bool {
		if len(hits) >= limit || scanned >= maxSearchScan || depth > maxSearchDepth {
			truncated = truncated || len(hits) >= limit || scanned >= maxSearchScan
			return false
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			return true // 读不了的目录跳过，不让一个坏目录废掉整次搜索
		}
		for _, e := range entries {
			scanned++
			if scanned > maxSearchScan {
				truncated = true
				return false
			}
			name := e.Name()
			if e.IsDir() {
				if strings.HasPrefix(name, ".") || searchSkipDirs[strings.ToLower(name)] {
					continue
				}
				childRel := name
				if rel != "" {
					childRel = rel + "/" + name
				}
				if !walk(filepath.Join(full, name), childRel, depth+1) {
					return false
				}
				continue
			}
			// 隐藏文件与系统噪音跟 Files 的口径一致：给人看的搜索不列它们。
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
				continue
			}
			if !strings.Contains(strings.ToLower(name), needle) {
				continue
			}
			hitRel := name
			if rel != "" {
				hitRel = rel + "/" + name
			}
			hits = append(hits, domain.FileHitVO{FilePath: hitRel, EntryName: name})
			if len(hits) >= limit {
				truncated = true
				return false
			}
		}
		return true
	}
	walk(root, "", 1)
	// 路径浅的在前：越靠近根的文件越可能是用户要的那一个。
	sort.SliceStable(hits, func(i, j int) bool {
		di := strings.Count(hits[i].FilePath, "/")
		dj := strings.Count(hits[j].FilePath, "/")
		if di != dj {
			return di < dj
		}
		return strings.ToLower(hits[i].FilePath) < strings.ToLower(hits[j].FilePath)
	})
	return &domain.FileSearchVO{Query: query, Hits: hits, Truncated: truncated}, nil
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

// GitFileHistory 列出某个文件的提交历史。
func (a *App) GitFileHistory(rel string, limit int) ([]domain.GitCommitVO, error) {
	return git.FileHistory(a.Workspace(), rel, limit)
}

// GitFileDiffInCommit 取一次提交里某个文件的逐行差异。
func (a *App) GitFileDiffInCommit(hash, rel string) (*domain.GitDiffVO, error) {
	return git.FileDiffInCommit(a.Workspace(), hash, rel)
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
