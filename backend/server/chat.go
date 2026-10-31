package server

// 会话与对话的 handler：列会话、发一轮、插话、排队、停止、换模型与思考档。每个 handler 只做三件事：
// 解参数、调用例、包响应；「这个会话属于哪个引擎」由 service 解析，handler 不重复判断。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"

	"github.com/gin-gonic/gin"
)

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
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.DeleteSession(ctx, s2kind(body.Engine), body.Path); err != nil {
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

// chatBody 是发送类接口共用的入参：引擎 / 会话 / 正文 / 附件。三个接口（send / steer / followup）
// 形状一致共用一份，分开写三份的后果是某天只在一个加了附件字段，用户看不出为什么「插话能带图、发送不能」。
type chatBody struct {
	Engine      string                     `json:"engine"`
	Session     string                     `json:"session"`
	Message     string                     `json:"message"`
	Attachments []domain.ChatAttachmentREQ `json:"attachments,omitempty"`
}

// toREQ 转成用例层的入参。
func (b chatBody) toREQ() domain.ChatSendREQ {
	return domain.ChatSendREQ{Message: b.Message, Attachments: b.Attachments}
}

func (s *Server) send(c *gin.Context) {
	var body chatBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		runID, err := s.svc.Send(ctx, s2kind(body.Engine), body.Session, body.toREQ())
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
	var body chatBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.Steer(ctx, s2kind(body.Engine), body.Session, body.toREQ()); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) followUp(c *gin.Context) {
	var body chatBody
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		if err := s.svc.FollowUp(ctx, s2kind(body.Engine), body.Session, body.toREQ()); err != nil {
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

func (s *Server) removeQueued(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		Message string `json:"message"`
		Queue   string `json:"queue"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		removed, err := s.svc.RemoveQueued(ctx, s2kind(body.Engine), body.Session, body.Message, body.Queue)
		if err != nil {
			return nil, err
		}
		return gin.H{"removed": removed}, nil
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
		Engine  string            `json:"engine"`
		Session string            `json:"session"`
		Request engine.UIResponse `json:"request"`
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

// cloneChat 复制当前会话开新分支（仅 Caps.SessionClone 的引擎提供）。
func (s *Server) cloneChat(c *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
	}
	_ = c.ShouldBindJSON(&body)
	s.run(c, func(ctx *gin.Context) (any, error) {
		file, err := s.svc.CloneChat(ctx, s2kind(body.Engine))
		if err != nil {
			return nil, err
		}
		return gin.H{"session_file": file}, nil
	})
}

// exportSessionHTML 导出当前会话为单文件 HTML（仅 Caps.ExportHTML 的引擎提供）。
func (s *Server) exportSessionHTML(c *gin.Context) {
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(ctx *gin.Context) (any, error) {
		return s.svc.ExportSessionHTML(ctx, s2kind(body.Engine), body.Session)
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

// ---------------- 消息隐藏 ----------------

// hideTurn / unhideTurn：界面上「删除一轮消息」的两个方向。语义是隐藏：hid 是历史投影带下去的
// 轮次标识，service 按会话记账，引擎的会话文件与上下文不动；账本只认会话路径，引擎参数不参与。

func (s *Server) hideTurn(c *gin.Context) {
	var body struct {
		Session string `json:"session"`
		Hid     string `json:"hid"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.HideTurn(body.Session, body.Hid); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

func (s *Server) unhideTurn(c *gin.Context) {
	var body struct {
		Session string `json:"session"`
		Hid     string `json:"hid"`
	}
	if !bind(c, &body) {
		return
	}
	s.run(c, func(*gin.Context) (any, error) {
		if err := s.svc.UnhideTurn(body.Session, body.Hid); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}
