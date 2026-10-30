package server

// 技能、插件与系统能力的 handler。技能与插件的写操作都落在引擎自己的目录或 CLI 上：
// 不另建一份清单，也不自己解 node_modules。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"github.com/gin-gonic/gin"
	"os"
	"path/filepath"
)

// ---------------- 技能 ----------------

func (s *Server) skills(c *gin.Context) {
	s.run(c, func(*gin.Context) (any, error) {
		list, err := s.svc.SkillList(kind(c))
		if err != nil {
			return nil, err
		}
		return gin.H{"skills": list}, nil
	})
}

func (s *Server) skillContent(c *gin.Context) {
	path := query(c, "path")
	s.run(c, func(*gin.Context) (any, error) {
		body, err := s.svc.SkillContent(path)
		if err != nil {
			return nil, err
		}
		return gin.H{"content": body}, nil
	})
}

// saveSkillContent 覆写技能正文。写后把最新原文回给前端，
// 避免界面自己再拉一次（两次请求之间用户可能已经手改过了）。
func (s *Server) saveSkillContent(c *gin.Context) {
	var body domain.SkillSaveREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		text, err := s.svc.SaveSkillContent(engine.Kind(body.EngineKind), body.FilePath, body.Text)
		if err != nil {
			return nil, err
		}
		return gin.H{"content": text}, nil
	})
}

func (s *Server) toggleSkill(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Path    string `json:"path"`
		Enabled bool   `json:"enabled"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.ToggleSkill(s2kind(body.Engine), body.Path, body.Enabled); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) createSkill(c *gin.Context) {
	var body struct {
		Engine      string `json:"engine"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		path, err := s.svc.CreateSkill(s2kind(body.Engine), body.Name, body.Description, body.Body)
		if err != nil {
			return nil, err
		}
		return gin.H{"path": path}, nil
	})
}

func (s *Server) deleteSkill(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Path   string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.DeleteSkill(s2kind(body.Engine), body.Path); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

// ---------------- 系统能力 ----------------

func (s *Server) revealPath(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if body.Path == "" {
			return nil, pkg.New(pkg.CodeBadRequest, "缺少路径", "")
		}
		if _, err := os.Stat(body.Path); err != nil {
			return nil, pkg.Wrap(pkg.CodeNotFound, "这个路径不存在", err)
		}
		// 在资源管理器里定位：只打开父目录并选中，不执行文件。
		cmd := command("explorer", "/select,"+filepath.Clean(body.Path))
		_ = cmd.Start()
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) openPath(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if body.Path == "" {
			return nil, pkg.New(pkg.CodeBadRequest, "缺少路径", "")
		}
		if err := openWith(body.Path); err != nil {
			return nil, pkg.Wrap(pkg.CodeNotFound, "打不开这个路径", err)
		}
		return gin.H{"ok": true}, nil
	})
}
