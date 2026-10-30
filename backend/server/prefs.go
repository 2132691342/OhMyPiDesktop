// 设置页的接口：应用偏好（开机自启 / 恢复会话）、引擎配置原文、数据目录。与 routes.go 里的 /settings/* 分开：
// 那几个是「设置 KV 本身」，这里的是「设置背后真正做事」的那一类。
package server

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"

	"github.com/gin-gonic/gin"
)

// prefsRoutes 挂应用偏好与配置原文读取。
func (s *Server) prefsRoutes(api *gin.RouterGroup) {
	api.GET("/prefs", func(c *gin.Context) {
		s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.Prefs(), nil })
	})
	api.POST("/prefs/autostart", s.setAutoStart)
	api.POST("/prefs/restore", s.setRestoreSession)
	api.POST("/prefs/session", s.rememberSession)
	api.POST("/prefs/close-action", s.setCloseAction)
	api.POST("/prefs/tray-hint-shown", func(c *gin.Context) {
		s.run(c, func(*gin.Context) (any, error) {
			if err := s.svc.MarkTrayHintShown(); err != nil {
				return nil, err
			}
			return s.svc.Prefs(), nil
		})
	})
	api.POST("/prefs/data-dir", func(c *gin.Context) {
		s.run(c, func(*gin.Context) (any, error) { return nil, s.svc.OpenDataDir() })
	})
	// 引擎配置：先列清单，再按路径读原文。
	api.GET("/config/files", s.configFiles)
	api.GET("/config/text", s.configText)
}

func (s *Server) setAutoStart(c *gin.Context) {
	var body domain.AutoStartREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SetAutoStart(body.Enabled); err != nil {
			return nil, err
		}
		return s.svc.Prefs(), nil
	})
}

func (s *Server) setRestoreSession(c *gin.Context) {
	var body domain.RestoreSessionREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SetRestoreSession(body.Enabled); err != nil {
			return nil, err
		}
		return s.svc.Prefs(), nil
	})
}

// setCloseAction 改关窗行为：收托盘还是直接退出。
func (s *Server) setCloseAction(c *gin.Context) {
	var body domain.CloseActionREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.SetCloseAction(body.Action); err != nil {
			return nil, err
		}
		return s.svc.Prefs(), nil
	})
}

// rememberSession 记住当前会话。前端在会话切换后调它一次。
func (s *Server) rememberSession(c *gin.Context) {
	var body domain.LastSessionREQ
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		s.svc.RememberSession(engine.Kind(body.EngineKind), body.SessionFile)
		return s.svc.Prefs(), nil
	})
}

// configText 按引擎返回它的配置文件原文。file 为空时给「这个引擎最主要的那份」（omp: models.yml，pi: models.json）。
// 只读不写：写引擎配置有专门的节点级接口，原文编辑会打乱它们的假设。
func (s *Server) configText(c *gin.Context) {
	kind := s2kind(query(c, "engine"))
	file := query(c, "file")
	if !kind.Valid() {
		kind = s.svc.ActiveKind()
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		return s.svc.ConfigText(kind, file)
	})
}

// configFiles 列出该引擎的配置文件清单（路径 / 是否已生成 / 大小）。
func (s *Server) configFiles(c *gin.Context) {
	kind := s2kind(query(c, "engine"))
	if !kind.Valid() {
		kind = s.svc.ActiveKind()
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		return s.svc.ConfigFiles(kind)
	})
}
