package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/service"
)

// 这一层是安全边界：令牌、来源、方法三件事任何一件漏了，
// 本机上任何一个网页都能拿到引擎的控制权。所以只测这三件。

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := repo.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc, err := service.New(service.Options{
		Store:   store,
		Hub:     hub.New(16),
		DataDir: dir,
		Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(svc, svc.Hub, "secret-token")
	return srv, srv.Handler()
}

// req 造的请求默认来自 192.0.2.1，会被来源闸门挡下；测试要显式站在本机上。
func loopback(req *http.Request) *http.Request {
	req.RemoteAddr = "127.0.0.1:51234"
	return req
}

// 缺令牌必须拒绝：端口就在本机上，猜不到端口≠猜不到令牌，两道都要在。
func TestRequiresToken(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无令牌的请求状态 = %d，期望 403", rec.Code)
	}
}

func TestAcceptsHeaderToken(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d，期望 200；body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是统一外壳: %v", err)
	}
	if envelope.Code != "0" {
		t.Fatalf("code = %q，期望 0", envelope.Code)
	}
}

// SSE 走查询参数带令牌：EventSource 不能设自定义头。
func TestAcceptsQueryToken(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings?token=secret-token", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d，期望 200", rec.Code)
	}
}

// 只允许 GET / POST：没有 DELETE 的路由，就不存在「顺手写一个删除接口」的入口。
func TestRejectsOtherMethods(t *testing.T) {
	_, h := testServer(t)
	for _, method := range []string{http.MethodDelete, http.MethodPut, http.MethodPatch} {
		req := loopback(httptest.NewRequest(method, "/api/v1/settings", nil))
		req.Header.Set("X-Desktop-Token", "secret-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s 的状态 = %d，期望 405", method, rec.Code)
		}
	}
}

// 健康探测不需要令牌（只用来看服务在不在），但它不该泄漏任何业务数据。
func TestHealthNeedsNoToken(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// 错误也必须是统一外壳（HTTP 200 + code），否则前端拦截器分不出业务失败与网络故障。
func TestErrorUsesEnvelope(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodPost, "/api/v1/settings/workspace", strings.NewReader(`{"path":"Z:\\definitely\\missing\\dir"}`)))
	req.Header.Set("X-Desktop-Token", "secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d，期望 200（错误在 code 里）", rec.Code)
	}
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code == "0" {
		t.Fatalf("不存在的目录不该成功: %s", rec.Body.String())
	}
	if envelope.Message == "" {
		t.Error("错误必须给人的那句话")
	}
	if !strings.Contains(envelope.Message, "Z:") {
		t.Errorf("错误里该带上路径，便于用户核对: %q", envelope.Message)
	}
}

// 来源闸门：别的机器 / 别的网卡地址打进来一律拒绝，哪怕令牌对。
func TestRejectsNonLoopback(t *testing.T) {
	_, h := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.RemoteAddr = "192.0.2.10:5000"
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("外部来源状态 = %d，期望 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "8002") {
		t.Errorf("应回报来源被拒的错误码: %s", rec.Body.String())
	}
}
