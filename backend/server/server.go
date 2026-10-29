// Package server 是界面唯一的后端出口：回环地址上的 HTTP + SSE。
//
// 为什么走 HTTP 而不是直接把 Go 方法绑给前端：
//   - 业务调用要能流式（SSE），绑定调用只有一问一答；
//   - 前端可以用最普通的 axios 写法，不依赖 wails 生成的绑定代码；
//   - 端口随机 + 回环 + 只允许 GET/POST，外部网页也碰不到。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/service"

	"github.com/gin-gonic/gin"
)

// ContractVersion 与前端 types/api.ts 同键；改响应字段形态必须 +1。
const ContractVersion = 10

// Server 是内嵌 HTTP 服务。
type Server struct {
	svc   *service.App
	hub   *hub.Hub
	token string

	mu   sync.Mutex
	srv  *http.Server
	port int
}

// New 构造服务。token 由启动方生成，前端从 Wails 绑定拿到后写进请求头。
func New(svc *service.App, h *hub.Hub, token string) *Server {
	return &Server{svc: svc, hub: h, token: token}
}

// Handler 装配路由。抽出来是为了能单测：httptest 直接打它。
func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// CORS 必须在 guard 之前：浏览器的自定义头请求会先发 OPTIONS 预检，
	// 而 guard 只放行 GET/POST——顺序反了会让每个带令牌的请求先失败在预检上。
	// 预检本身不带业务语义，也不需要令牌，所以它由 cors 直接结束。
	r.Use(s.cors())
	r.Use(s.guard())

	api := r.Group("/api/v1")
	api.GET("/events", s.events)
	api.GET("/health", func(c *gin.Context) {
		ok(c, gin.H{"status": "ok", "contract": ContractVersion})
	})
	s.routes(api)
	return r
}

// guard 只允许回环来源、只允许 GET/POST，并要求启动令牌。
//
// 只允许这两种方法：没有 PUT/DELETE 就没有「顺手写一个删除接口」的入口，
// 所有写操作都是 POST + 动作段，审计时一眼能看完。
func (s *Server) guard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			// 预检已由 cors 处理并结束；走到这里说明有人直连 OPTIONS，按无内容返回。
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodPost {
			c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"code": pkg.CodeBadRequest, "message": "只允许 GET 与 POST"})
			return
		}
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil || !isLoopback(host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "8002", "message": "只接受本机请求"})
			return
		}
		// 令牌两种带法：普通请求走请求头；SSE 走查询参数（EventSource 不能设自定义头）。
		if c.Request.URL.Path != "/api/v1/health" && !s.tokenOK(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "8003", "message": "令牌不匹配"})
			return
		}
		c.Next()
	}
}

// tokenOK 校验启动令牌。
func (s *Server) tokenOK(c *gin.Context) bool {
	if c.GetHeader("X-Desktop-Token") == s.token {
		return true
	}
	return c.Query("token") == s.token
}

func isLoopback(host string) bool {
	if host == "::1" || host == "127.0.0.1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// cors 允许 WebView 直连回环端口。来源是 wails:// 与 dev 服务器的 http://localhost:。
func (s *Server) cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Desktop-Token")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Start 在回环地址上起服务，返回端口。
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, pkg.Wrap(pkg.CodeStartup, "本地端口占用，无法启动服务", err)
	}
	s.mu.Lock()
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv := s.srv
	port := s.port
	s.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			pkg.Errorf("server: 服务异常退出: %v", err)
		}
	}()
	pkg.Infof("server: 监听 127.0.0.1:%d", port)
	return port, nil
}

// Port 返回监听端口。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Stop 优雅关闭。
func (s *Server) Stop(ctx context.Context) {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return
	}
	_ = srv.Shutdown(ctx)
}

// ---------------- 统一响应 ----------------

// resp 是统一外壳：code 为数字字符串，"0" 成功。HTTP 状态恒为 200（除 SSE）。
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": "0", "message": "", "data": data})
}

func fail(c *gin.Context, err error) {
	ae := pkg.As(err)
	pkg.Warnf("api: %s %s -> [%s] %s %s", c.Request.Method, c.Request.URL.Path, ae.Code, ae.Message, ae.Detail)
	c.JSON(http.StatusOK, gin.H{
		"code":    ae.Code,
		"message": ae.Message,
		"detail":  ae.Detail,
		"data":    nil,
	})
}

// bind 解请求体。空体视为没有参数。
func bind(c *gin.Context, out any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(c.Request.Body).Decode(out); err != nil {
		fail(c, pkg.Wrap(pkg.CodeBadRequest, "请求体不是合法 JSON", err))
		return false
	}
	return true
}

func query(c *gin.Context, key string) string { return strings.TrimSpace(c.Query(key)) }

// events 是 SSE 长连接。
//
// 帧格式：`id: <seq>\nevent: <name>\ndata: <json>\n\n`。
// 断线重连带 lastEventId（EventSource 自带）或 ?since=，后端先补发环形窗口里的帧，
// 窗口外的不补——那部分走一次快照对账，绝不假装补全。
func (s *Server) events(c *gin.Context) {
	since := int64(0)
	if v := c.GetHeader("Last-Event-ID"); v != "" {
		fmt.Sscanf(v, "%d", &since)
	}
	if v := c.Query("since"); v != "" {
		fmt.Sscanf(v, "%d", &since)
	}
	sub, replay, cancel := s.hub.Subscribe(since)
	defer cancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, canFlush := c.Writer.(http.Flusher)
	write := func(frame []byte) bool {
		if _, err := c.Writer.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := c.Writer.Write(frame); err != nil {
			return false
		}
		if _, err := c.Writer.Write([]byte("\n\n")); err != nil {
			return false
		}
		if canFlush {
			flusher.Flush()
		}
		return true
	}
	if len(replay) > 0 {
		for _, line := range strings.Split(strings.TrimRight(string(replay), "\n"), "\n") {
			if line == "" {
				continue
			}
			if !write([]byte(line)) {
				return
			}
		}
	}
	// 开场帧：让前端立刻知道连上了，且拿到当前序号
	hello, _ := json.Marshal(hub.Event{Event: "stream:open", Seq: s.hub.Seq(), SentAt: time.Now().UnixMilli(), Data: json.RawMessage(`{}`)})
	if !write(hello) {
		return
	}

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-sub.Done():
			// 订阅被摘除（服务关闭）。通道不会 close，靠这个信号退出。
			return
		case hf := <-sub.Stream():
			// 丢过帧就先补一条 chat:gap：前端看到跳号会去拉快照对账，
			// 而不是带着半截状态继续跑。
			if from, to, ok := sub.TakeGap(); ok {
				if !write(hub.GapFrame(hf.Seq(), from, to)) {
					return
				}
			}
			if !write(hf.Raw()) {
				return
			}
		case <-ping.C:
			if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}
