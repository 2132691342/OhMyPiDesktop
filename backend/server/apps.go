package server

// 「应用」页的 handler：列引擎的安装事实、装 / 升级 / 卸载。
//
// 三条路径对应三个动作，而不是一条 `/apps/action`：动作名进 URL 之后
// 前端不必先把字符串拼出来再发，也让后端的方法白名单一眼能看完。

import (
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/service"
	"github.com/gin-gonic/gin"
)

// ---------------- 应用 ----------------

// apps 列出两个引擎的安装事实与可做的动作。
//
// `refresh=1` 忽略缓存（最新版查询走外网，默认 30 分钟）。
func (s *Server) apps(c *gin.Context) {
	fore := query(c, "refresh") == "1"
	s.run(c, func(ctx *gin.Context) (any, error) {
		rows, err := s.svc.Apps(ctx.Request.Context(), fore)
		if err != nil {
			return nil, err
		}
		return gin.H{"apps": rows}, nil
	})
}

func (s *Server) installApp(c *gin.Context) { s.runAppAction(c, service.ActionInstall) }
func (s *Server) updateApp(c *gin.Context)  { s.runAppAction(c, service.ActionUpdate) }
func (s *Server) uninstallApp(c *gin.Context) {
	s.runAppAction(c, service.ActionUninstall)
}

// runAppAction 收口三个动作的公共部分：解参数、跑用例、回最新清单。
//
// 返回值是整个清单而不是被改的那一行：装完两个引擎的相对状态可能都变了
// （比如 omp 装上了，界面里「缺引擎」的横幅就该消失），前端只认一份真相。
func (s *Server) runAppAction(c *gin.Context, action service.AppAction) {
	var body struct {
		Engine string `json:"engine"`
	}
	if !bind(c, &body) {
		return
	}
	kind := engine.Kind(body.Engine)
	if !kind.Valid() {
		fail(c, pkg.New(pkg.CodeBadRequest, "要指定装哪个引擎：omp 或 pi", ""))
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if _, err := s.svc.RunAppAction(ctx.Request.Context(), kind, action); err != nil {
			return nil, err
		}
		rows, err := s.svc.Apps(ctx.Request.Context(), true)
		if err != nil {
			return nil, err
		}
		return gin.H{"apps": rows}, nil
	})
}
