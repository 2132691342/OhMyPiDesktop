package server

// 路径表：/api/v1/{resource}/{action}。动作段在最后，写操作一律 POST。
//
// 引擎维度统一用 query/body 的 `engine` 字段（omp / pi），省略即「当前选中的那个」。
// 不按引擎拆路径：界面只有一份代码，拆路径会让每个调用点都多一个 if。
//
// 这张表是全站唯一的路由声明；handler 按域分在同目录的 settings.go /
// chat.go / models.go / skills.go 里，一处只回答一个问题。

import (
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
	api.POST("/settings/workspace/forget", s.forgetWorkspace)
	api.POST("/settings/engine", s.setEngine)
	api.GET("/logs", func(c *gin.Context) { ok(c, gin.H{"lines": pkg.Ring()}) })

	// 应用偏好：开机自启、恢复上次会话、数据目录
	s.prefsRoutes(api)

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
	api.POST("/chat/remove-queued", s.removeQueued)
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
	api.POST("/skills/save", s.saveSkillContent)

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
	api.GET("/git/file-history", s.gitFileHistory)
	api.GET("/git/file-diff", s.gitFileDiff)
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
