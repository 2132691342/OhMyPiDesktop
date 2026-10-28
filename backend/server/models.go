package server

// 子代理观测台与模型服务商的 handler。
//
// 子代理只有 omp 有：入口按 Caps 隐藏，真被调到就返回不支持，
// 而不是假装成功——静默成功会让界面显示一个不存在的面板。

import (
	"OhMyPiDesktop/backend/domain"
	"github.com/gin-gonic/gin"
	"strconv"
)

// ---------------- 子代理（仅 omp） ----------------

func (s *Server) agents(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) {
		list, err := s.svc.Agents(ctx, kind(c))
		if err != nil {
			return nil, err
		}
		return gin.H{"agents": list}, nil
	})
}

func (s *Server) agentMessages(c *gin.Context) {
	from, _ := strconv.ParseInt(c.Query("from"), 10, 64)
	s.run(c, func(ctx *gin.Context) (any, error) {
		return s.svc.AgentMessages(ctx, kind(c), c.Query("id"), from)
	})
}

func (s *Server) steerAgent(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.SteerAgent(ctx, s2kind(body.Engine), body.ID, body.Message); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) cancelAgent(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		ID     string `json:"id"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		done, err := s.svc.CancelAgent(ctx, s2kind(body.Engine), body.ID)
		if err != nil {
			return nil, err
		}
		// cancelled=false 是幂等答案（未知 / 已结束 / 已取消），不是错误。
		return gin.H{"cancelled": done}, nil
	})
}

func (s *Server) subscribeAgents(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Level  string `json:"level"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.SubscribeAgents(ctx, s2kind(body.Engine), body.Level); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

// ---------------- 模型与服务商 ----------------

func (s *Server) models(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) {
		rows, err := s.svc.Models(ctx, kind(c))
		if err != nil {
			return nil, err
		}
		return gin.H{"models": rows}, nil
	})
}

func (s *Server) providers(c *gin.Context) {
	s.run(c, func(*gin.Context) (any, error) { return s.svc.Providers(kind(c)) })
}

func (s *Server) upsertProvider(c *gin.Context) {
	var body struct {
		Engine   string              `json:"engine"`
		Name     string              `json:"name"`
		Provider domain.ProviderView `json:"provider"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.UpsertProvider(s2kind(body.Engine), body.Name, body.Provider)
	})
}

func (s *Server) deleteProvider(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Name   string `json:"name"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.DeleteProvider(s2kind(body.Engine), body.Name)
	})
}

func (s *Server) loginProviders(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) {
		list, err := s.svc.LoginProviders(ctx, kind(c))
		if err != nil {
			return nil, err
		}
		return gin.H{"providers": list}, nil
	})
}

func (s *Server) startLogin(c *gin.Context) {
	var body struct {
		Engine   string `json:"engine"`
		Provider string `json:"provider"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Login(ctx, s2kind(body.Engine), body.Provider); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}
