package server

// 设置与引擎的 handler：设置 KV、工作目录、当前引擎、探测与重启。
//
// 这些动作都改「应用自己的状态」，不碰引擎的会话与配置——
// 后者在 models.go / skills.go 里，因为它们写的是引擎的文件。

import (
	"OhMyPiDesktop/backend/engine"
	"github.com/gin-gonic/gin"
)

// ---------------- 设置 ----------------

func (s *Server) saveSettings(c *gin.Context) {
	var body map[string]string
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SaveSettings(body); err != nil {
			return nil, err
		}
		return s.svc.Settings()
	})
}

func (s *Server) setWorkspace(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SetWorkspace(body.Path); err != nil {
			return nil, err
		}
		return gin.H{"workspace": s.svc.Workspace()}, nil
	})
}

func (s *Server) forgetWorkspace(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		rows, err := s.svc.ForgetWorkspace(body.Path)
		if err != nil {
			return nil, err
		}
		return gin.H{"workspaces": rows}, nil
	})
}

func (s *Server) setEngine(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SetActive(engine.Kind(body.Engine)); err != nil {
			return nil, err
		}
		return gin.H{"engine": s.svc.ActiveKind()}, nil
	})
}

// ---------------- 引擎 ----------------

func (s *Server) engines(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.EngineStatus(ctx) })
}

func (s *Server) detectEngines(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.DetectEngines(ctx) })
}

func (s *Server) restartEngines(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
	}
	_ = c.ShouldBindJSON(&body)
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.RestartEngines(ctx, s2kind(body.Engine)); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

// engineHealth 是概览页要用的引擎健康快照。
func (s *Server) engineHealth(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.Health(ctx) })
}
