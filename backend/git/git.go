// Package git 读写工作区的版本状态：让界面能回答「它到底改了什么」并能把改动收成一次提交。
// 读全开放（status / diff / 历史），写限白名单动作，且每种都由用户点下去。
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
)

// 命令超时：仓库再大也只给这么久，界面不能为一个状态卡住。
const (
	statusTimeout = 10 * time.Second
	diffTimeout   = 15 * time.Second
)

// maxLines 差异行数上限：一个几万行的生成文件会把前端渲染拖死，行数先兜住。
const maxLines = 3000

// maxLineBytes 单行上限：minified 的一行可能上兆，Scanner 不设上限就是整行进内存。
const maxLineBytes = 256 * 1024

// maxUntrackedBytes 未跟踪文件的读取上限：Git 给不出它的差异，只能读正文，
// 超了就得说清楚而不是把整文件闷进内存。
const maxUntrackedBytes = 8 << 20

// gitExeMu / gitExe 缓存 git 可执行文件的解析结果：一次进程只解析一次，
// 状态页反复打开不再反复探盘。装配期注入的那份优先（与助手用的是同一份）。
var (
	gitExeMu sync.Mutex
	gitExe   string
	gitDone  bool
)

// SetExe 注入当前生效的 git：助手用它跑命令，界面也用它读状态，两处不能各挑一份。
func SetExe(exe string) {
	if exe == "" {
		return
	}
	gitExeMu.Lock()
	gitExe, gitDone = exe, true
	gitExeMu.Unlock()
}

// resolveGit 解析 git 可执行文件：注入的那份优先，其次 PATH 与常见安装位置。
func resolveGit() string {
	gitExeMu.Lock()
	defer gitExeMu.Unlock()
	if gitDone {
		return gitExe
	}
	if p, err := exec.LookPath("git"); err == nil {
		gitExe = p
		gitDone = true
		return gitExe
	}
	for _, cand := range gitCandidatePaths() {
		if pkg.FileExists(cand) {
			gitExe = cand
			break
		}
	}
	gitDone = true
	return gitExe
}

// gitCandidatePaths 常见安装位置（按环境变量拼，不写死盘符）。
func gitCandidatePaths() []string {
	out := make([]string, 0, 4)
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if root := os.Getenv(env); root != "" {
			out = append(out, filepath.Join(root, "Git", "cmd", "git.exe"))
		}
	}
	if la := os.Getenv("LocalAppData"); la != "" {
		out = append(out, filepath.Join(la, "Microsoft", "WinGet", "Links", "git.exe"))
	}
	return out
}

// Available 报告系统里有没有可执行的 git。
func Available() bool { return resolveGit() != "" }

// insideWorkTree 探测 dir 是否位于 Git 工作树内；探测失败按「不是仓库」处理（两种情况界面动作相同）。
func insideWorkTree(dir string) (bool, error) {
	res, err := run(dir, statusTimeout, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if isTimeout(err) {
			return false, err
		}
		return false, nil
	}
	return strings.TrimSpace(res.stdout) == "true", nil
}

// Version 返回 git 版本串；不可用返回空串。
func Version() string {
	if !Available() {
		return ""
	}
	res, err := run("", statusTimeout, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.stdout)
}

// Status 读取 dir 的仓库状态；不是仓库或没装 git 时返回 Available/IsRepo 标记而不是错误。
func Status(dir string) (*domain.GitStatusVO, error) {
	vo := &domain.GitStatusVO{
		Workspace:    dir,
		RepoState:    domain.GitStateNone,
		Remotes:      []domain.GitRemoteVO{},
		ChangedFiles: []domain.GitFileVO{},
	}
	if !Available() {
		vo.UnavailableReason = "这台电脑上还没有 Git，装好后这里就能看到改动"
		return vo, nil
	}
	vo.Available = true
	vo.GitVersion = Version()

	scope, err := openScope(dir)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		vo.UnavailableReason = "这个工作目录还不是 Git 仓库，初始化后这里就能看到改动"
		return vo, nil
	}
	vo.IsRepo = true
	vo.RepoRoot = scope.root
	vo.BranchName = scope.branch()
	vo.UpstreamRef = scope.upstream()
	vo.Remotes = scope.remotes()
	st := scope.state()
	vo.RepoState = st.StateCode
	vo.RepoStateDetail = st.Detail
	vo.Ahead, vo.Behind = scope.aheadBehind()
	files, err := scope.changedFiles()
	if err != nil {
		return nil, err
	}
	vo.ChangedFiles = files
	for _, f := range files {
		if f.Conflict {
			vo.Conflict = true
			break
		}
	}
	return vo, nil
}

// Diff 读取 dir 工作的 rel 文件差异；rel 为空表示整个工作区。路径必须落在 dir 之内。
func Diff(dir, rel string) (*domain.GitDiffVO, error) {
	if !Available() {
		return nil, pkg.New("1015", "这台电脑上还没有 Git，装好后就能看改动", "")
	}
	scope, err := openScope(dir)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return nil, pkg.New("1015", "这个工作目录还不是 Git 仓库，初始化后才能看改动", "")
	}

	rel = strings.TrimSpace(rel)
	if rel == "" {
		if !pkg.DirExists(dir) {
			return nil, pkg.New("1003", "工作目录不存在", dir)
		}
		return scope.diffAll()
	}

	full, err := pkg.SafeJoin(dir, rel)
	if err != nil {
		return nil, err
	}
	inside, err := relTo(dir, full)
	if err != nil {
		return nil, pkg.New("1004", "路径越出工作目录", rel)
	}
	// git 的路径参数是仓库根相对，界面给的是工作区相对：两者之差就是工作区前缀。
	fromRoot := scope.relFromRoot(inside)
	if !pkg.FileExists(full) && !scope.isTracked(fromRoot) {
		return nil, pkg.New("1005", "文件不存在", rel)
	}
	return scope.diffOne(full, rel, fromRoot)
}

// scope 是一次 git 读写的定位结果：仓库根 + 工作区前缀。所有命令都在仓库根执行、
