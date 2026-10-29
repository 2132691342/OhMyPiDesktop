package server

import (
	"os/exec"
	"syscall"

	"OhMyPiDesktop/backend/domain"
	"github.com/gin-gonic/gin"
)

// 工作区的 HTTP 薄层：只解参数、调用例、包响应。
//
// 所有路径都是「相对当前工作目录」的，越界由 service 侧的 SafeJoin 拦住。

func (s *Server) files(c *gin.Context) {
	rel := c.Query("path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.Files(rel) })
}

func (s *Server) fileRead(c *gin.Context) {
	rel := c.Query("path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.ReadFile(rel) })
}

func (s *Server) gitStatus(c *gin.Context) {
	rel := c.Query("path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitStatus(rel) })
}

func (s *Server) gitDiff(c *gin.Context) {
	rel := c.Query("path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitDiff(rel) })
}

func (s *Server) gitHistory(c *gin.Context) {
	limit, _ := strconvAtoi(c.Query("limit"))
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitHistory(limit) })
}

func (s *Server) gitCommitDetail(c *gin.Context) {
	hash := c.Query("hash")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitCommitDetail(hash) })
}

// gitFileHistory 列出一个文件的提交历史（工作区页的「这个文件改过几次」）。
func (s *Server) gitFileHistory(c *gin.Context) {
	rel := c.Query("path")
	limit, _ := strconvAtoi(c.Query("limit"))
	s.run(c, func(*gin.Context) (any, error) {
		list, err := s.svc.GitFileHistory(rel, limit)
		if err != nil {
			return nil, err
		}
		return gin.H{"commits": list}, nil
	})
}

// gitFileDiff 取一次提交里某个文件的逐行差异。
func (s *Server) gitFileDiff(c *gin.Context) {
	hash := c.Query("hash")
	rel := c.Query("path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitFileDiffInCommit(hash, rel) })
}

func (s *Server) gitBranches(c *gin.Context) {
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitBranches() })
}

// pathsBody 是「一批文件路径」的公共入参。
type pathsBody struct {
	Paths []string `json:"paths"`
}

func (s *Server) gitStage(c *gin.Context) {
	var body pathsBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitStage(body.Paths) })
}

func (s *Server) gitUnstage(c *gin.Context) {
	var body pathsBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitUnstage(body.Paths) })
}

func (s *Server) gitDiscard(c *gin.Context) {
	var body pathsBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitDiscard(body.Paths) })
}

func (s *Server) gitResolve(c *gin.Context) {
	var body pathsBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitResolve(body.Paths) })
}

func (s *Server) gitCommit(c *gin.Context) {
	var body struct {
		Message string `json:"message"`
		Amend   bool   `json:"amend"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitCommit(body.Message, body.Amend) })
}

func (s *Server) gitCreateBranch(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitCreateBranch(body.Name) })
}

func (s *Server) gitSwitchBranch(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitSwitchBranch(body.Name) })
}

func (s *Server) gitDeleteBranch(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitDeleteBranch(body.Name) })
}

func (s *Server) gitMerge(c *gin.Context) {
	var body struct {
		Name   string `json:"name"`
		Rebase bool   `json:"rebase"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitMerge(body.Name, body.Rebase) })
}

func (s *Server) gitStateAction(c *gin.Context) {
	var body struct {
		Action string `json:"action"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitStateAction(body.Action) })
}

func (s *Server) gitInit(c *gin.Context) {
	var body struct {
		BranchName string `json:"branch_name"`
	}
	_ = c.ShouldBindJSON(&body)
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitInit(body.BranchName) })
}

func (s *Server) gitAddRemote(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitAddRemote(body.Name, body.URL) })
}

// pushBody 是推送入参（分级由 force 表达）。
type pushBody struct {
	RemoteName  string `json:"remote_name"`
	BranchName  string `json:"branch_name"`
	Force       string `json:"force"`
	SetUpstream bool   `json:"set_upstream"`
	// Confirm 强制推送时用户输入的分支名，必须与 BranchName 完全一致
	Confirm string `json:"confirm"`
}

func (s *Server) gitPush(c *gin.Context) {
	var body pushBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.GitPush(domain.GitPushREQ{
			RemoteName:  body.RemoteName,
			BranchName:  body.BranchName,
			Force:       body.Force,
			SetUpstream: body.SetUpstream,
			Confirm:     body.Confirm,
		})
	})
}

// command 造一个隐藏窗口的进程（打开资源管理器这类外壳动作）。
func command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

// openWith 用系统默认程序打开路径。
func openWith(path string) error {
	cmd := command("cmd", "/c", "start", "", path)
	return cmd.Start()
}

func strconvAtoi(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, nil
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func (s *Server) gitPull(c *gin.Context) {
	var body struct {
		RemoteName string `json:"remote_name"`
		BranchName string `json:"branch_name"`
		Rebase     bool   `json:"rebase"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitPull(body.RemoteName, body.BranchName, body.Rebase) })
}

func (s *Server) gitFetch(c *gin.Context) {
	var body struct {
		RemoteName string `json:"remote_name"`
	}
	_ = c.ShouldBindJSON(&body)
	s.run(c, func(*gin.Context) (any, error) { return s.svc.GitFetch(body.RemoteName) })
}
