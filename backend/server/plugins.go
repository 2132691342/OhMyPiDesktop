package server

// 插件与 MCP 的 handler。两者的读都来自引擎自己的配置文件，写分两路：
// 插件的装 / 卸代跑引擎 CLI（只有引擎知道怎么维护它的私有目录与锁文件），
// MCP 直接写 mcp.json（只改我们掌管的键，未知字段原样保留）。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"github.com/gin-gonic/gin"
)

// ---------------- 插件 ----------------

func (s *Server) plugins(c *gin.Context) {
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.PluginList(kind(c))
	})
}

func (s *Server) installPlugin(c *gin.Context) {
	var body domain.PluginActionREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		out, err := s.svc.InstallPlugin(ctx, s2kind(body.EngineKind), body.PluginSource)
		if err != nil {
			return nil, err
		}
		return gin.H{"output": out}, nil
	})
}

func (s *Server) uninstallPlugin(c *gin.Context) {
	var body domain.PluginActionREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		out, err := s.svc.UninstallPlugin(ctx, s2kind(body.EngineKind), body.PluginSource)
		if err != nil {
			return nil, err
		}
		return gin.H{"output": out}, nil
	})
}

// ---------------- MCP ----------------

// mcpList 列出 MCP 服务器。`live=1` 时额外问引擎一次连接状态。
func (s *Server) mcpList(c *gin.Context) {
	live := query(c, "live") == "1" || query(c, "live") == "true"
	s.run(c, func(ctx *gin.Context) (any, error) {
		return s.svc.McpList(ctx, kind(c), live)
	})
}

func (s *Server) mcpSave(c *gin.Context) {
	var body domain.McpSaveREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.McpSave(body)
	})
}

func (s *Server) mcpToggle(c *gin.Context) {
	var body domain.McpToggleREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.McpToggle(engine.Kind(body.EngineKind), body.ServerName, body.Enabled)
	})
}

func (s *Server) mcpRemove(c *gin.Context) {
	var body domain.McpRemoveREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		return s.svc.McpRemove(engine.Kind(body.EngineKind), body.ServerName)
	})
}
