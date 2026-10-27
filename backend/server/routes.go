package server

import (
	"os"
	"path/filepath"
	"strconv"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"

	"github.com/gin-gonic/gin"
)

// routes 是路径表。约定：/api/v1/{resource}/{action}，动作段在最后。
//
// 引擎维度统一用 query/body 的 `engine` 字段（omp / pi），省略即「当前选中的那个」。
// 不按引擎拆路径：界面只有一份代码，拆路径会让每个调用点都多一个 if。
func (s *Server) routes(api *gin.RouterGroup) {
	// 引导与设置
	api.GET("/bootstrap", func(c *gin.Context) { s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.Bootstrap(ctx) }) })
	api.GET("/settings", func(c *gin.Context) { s.run(c, func(*gin.Context) (any, error) { return s.svc.Settings() }) })
	api.POST("/settings/save", s.saveSettings)
	api.POST("/settings/workspace", s.setWorkspace)
	api.POST("/settings/engine", s.setEngine)
	api.GET("/logs", func(c *gin.Context) { ok(c, gin.H{"lines": pkg.Ring()}) })

	// 引擎（探测，不安装）
	api.GET("/engines", s.engines)
	api.POST("/engines/detect", s.detectEngines)
	api.POST("/engines/restart", s.restartEngines)
	api.GET("/engine/logs", func(c *gin.Context) { ok(c, gin.H{"lines": pkg.Ring()}) })

	// 会话
	api.GET("/sessions", s.sessions)
	api.GET("/sessions/detail", s.sessionDetail)
	api.POST("/sessions/delete", s.deleteSession)
	api.POST("/sessions/rename", s.renameSession)

	// 对话
	api.GET("/chat/state", s.chatState)
	api.POST("/chat/send", s.send)
	api.POST("/chat/stop", s.stop)
	api.POST("/chat/steer", s.steer)
	api.POST("/chat/followup", s.followUp)
	api.POST("/chat/clear-queue", s.clearQueue)
	api.POST("/chat/compact", s.compact)
	api.POST("/chat/answer", s.answer)
	api.POST("/chat/new", s.newChat)
	api.POST("/chat/model", s.setModel)
	api.POST("/chat/thinking", s.setThinking)
	api.POST("/chat/autocompact", s.setAutoCompaction)

	// 子代理（仅 omp）
	api.GET("/agents", s.agents)
	api.GET("/agents/messages", s.agentMessages)
	api.POST("/agents/steer", s.steerAgent)
	api.POST("/agents/cancel", s.cancelAgent)
	api.POST("/agents/subscribe", s.subscribeAgents)

	// 模型与服务商
	api.GET("/models", s.models)
	api.GET("/providers", s.providers)
	api.POST("/providers/upsert", s.upsertProvider)
	api.POST("/providers/delete", s.deleteProvider)
	api.GET("/login/providers", s.loginProviders)
	api.POST("/login/start", s.startLogin)

	// 技能
	api.GET("/skills", s.skills)
	api.GET("/skills/content", s.skillContent)
	api.POST("/skills/toggle", s.toggleSkill)
	api.POST("/skills/create", s.createSkill)
	api.POST("/skills/delete", s.deleteSkill)

	// 插件 / 扩展
	api.GET("/plugins", s.plugins)
	api.POST("/plugins/install", s.installPlugin)
	api.POST("/plugins/uninstall", s.uninstallPlugin)

	// 工作区：文件树与 git
	api.GET("/files", s.files)
	api.GET("/files/read", s.fileRead)
	api.GET("/git/status", s.gitStatus)
	api.GET("/git/diff", s.gitDiff)
	api.GET("/git/history", s.gitHistory)
	api.GET("/git/commit", s.gitCommitDetail)
	api.GET("/git/branches", s.gitBranches)
	api.POST("/git/stage", s.gitStage)
	api.POST("/git/unstage", s.gitUnstage)
	api.POST("/git/discard", s.gitDiscard)
	api.POST("/git/resolve", s.gitResolve)
	api.POST("/git/commit", s.gitCommit)
	api.POST("/git/branch", s.gitCreateBranch)
	api.POST("/git/switch", s.gitSwitchBranch)
	api.POST("/git/branch/delete", s.gitDeleteBranch)
	api.POST("/git/merge", s.gitMerge)
	api.POST("/git/state", s.gitStateAction)
	api.POST("/git/init", s.gitInit)
	api.POST("/git/remote", s.gitAddRemote)
	api.POST("/git/push", s.gitPush)
	api.POST("/git/pull", s.gitPull)
	api.POST("/git/fetch", s.gitFetch)

	// 系统能力
	api.POST("/system/reveal", s.revealPath)
	api.POST("/system/open", s.openPath)
}

// run 收口「取 ctx → 调用例 → 统一响应」这三步，避免每个 handler 抄一遍。
func (s *Server) run(c *gin.Context, fn func(ctx *gin.Context) (any, error)) {
	data, err := fn(c)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, data)
}

// kind 读请求里的引擎名（空 = 当前选中的）。
func kind(c *gin.Context) engine.Kind {
	return s2kind(query(c, "engine"))
}

func s2kind(name string) engine.Kind {
	if engine.Kind(name).Valid() {
		return engine.Kind(name)
	}
	return ""
}

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

// ---------------- 会话 ----------------

func (s *Server) sessions(c *gin.Context) {
	ws := query(c, "workspace")
	s.run(c, func(*gin.Context) (any, error) {
		list, err := s.svc.Sessions(kind(c), ws)
		if err != nil {
			return nil, err
		}
		return gin.H{"sessions": list}, nil
	})
}

func (s *Server) sessionDetail(c *gin.Context) {
	path := query(c, "path")
	s.run(c, func(*gin.Context) (any, error) { return s.svc.SessionDetail(kind(c), path) })
}

func (s *Server) deleteSession(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Path   string `json:"path"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.DeleteSession(s2kind(body.Engine), body.Path); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) renameSession(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.RenameSession(ctx, s2kind(body.Engine), body.Path, body.Name); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

// ---------------- 对话 ----------------

func (s *Server) chatState(c *gin.Context) {
	file := query(c, "session")
	s.run(c, func(ctx *gin.Context) (any, error) { return s.svc.ChatState(ctx, kind(c), file) })
}

func (s *Server) send(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Message string `json:"message"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		runID, err := s.svc.Send(ctx, s2kind(body.Engine), body.Session, body.Message)
		if err != nil {
			return nil, err
		}
		return gin.H{"run_id": runID}, nil
	})
}

func (s *Server) stop(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Stop(ctx, s2kind(body.Engine), body.Session); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) steer(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Message string `json:"message"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Steer(ctx, s2kind(body.Engine), body.Session, body.Message); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) followUp(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Message string `json:"message"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.FollowUp(ctx, s2kind(body.Engine), body.Session, body.Message); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) clearQueue(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.ClearQueue(ctx, s2kind(body.Engine), body.Session); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) compact(c *gin.Context) {
	var body struct {
		Engine       string `json:"engine"`
		Session      string `json:"session"`
		Instructions string `json:"instructions"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Compact(ctx, s2kind(body.Engine), body.Session, body.Instructions); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) answer(c *gin.Context) {
	var body struct {
		Engine  string             `json:"engine"`
		Session string             `json:"session"`
		Request engine.UIResponse  `json:"request"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Answer(ctx, s2kind(body.Engine), body.Session, body.Request); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) newChat(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
	}
	_ = c.ShouldBindJSON(&body)
	s.run(c, func(ctx *gin.Context) (any, error) {
		file, err := s.svc.NewChat(ctx, s2kind(body.Engine))
		if err != nil {
			return nil, err
		}
		return gin.H{"session_file": file}, nil
	})
}

func (s *Server) setModel(c *gin.Context) {
	var body struct {
		Engine   string `json:"engine"`
		Session  string `json:"session"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.SetModel(ctx, s2kind(body.Engine), body.Session, body.Provider, body.Model); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) setThinking(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Level   string `json:"level"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.SetThinking(ctx, s2kind(body.Engine), body.Session, body.Level); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) setAutoCompaction(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Enabled bool   `json:"enabled"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.SetAutoCompaction(ctx, s2kind(body.Engine), body.Session, body.Enabled); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

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

// ---------------- 插件 ----------------

func (s *Server) plugins(c *gin.Context) {
	s.run(c, func(ctx *gin.Context) (any, error) {
		rows, err := s.svc.Plugins(ctx, kind(c))
		if err != nil {
			return nil, err
		}
		return gin.H{"plugins": rows}, nil
	})
}

func (s *Server) installPlugin(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Source string `json:"source"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		out, err := s.svc.InstallPlugin(ctx, s2kind(body.Engine), body.Source)
		if err != nil {
			return nil, err
		}
		return gin.H{"output": out}, nil
	})
}

func (s *Server) uninstallPlugin(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Source string `json:"source"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		out, err := s.svc.UninstallPlugin(ctx, s2kind(body.Engine), body.Source)
		if err != nil {
			return nil, err
		}
		return gin.H{"output": out}, nil
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
