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
	store, err := repo.Open(dir + "/settings.json")
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

// 令牌闸门有四个状态，一个都不能错：
//
//	无令牌 → 403（端口在本机上，猜到端口不等于猜到令牌）
//	头令牌 → 200（前端正常请求）
//	查询令牌 → 200（SSE 走 EventSource，它设不了自定义头）
//	/health → 200 且不需要令牌（只用来看服务在不在）
func TestTokenGate(t *testing.T) {
	_, h := testServer(t)

	// 无令牌
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("无令牌的请求状态 = %d，期望 403", rec.Code)
	}

	// 头令牌
	req = loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("头令牌状态 = %d，期望 200；body=%s", rec.Code, rec.Body.String())
	}
	// 200 的响应也必须是统一外壳
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是统一外壳: %v", err)
	}
	if envelope.Code != "0" {
		t.Errorf("code = %q，期望 0", envelope.Code)
	}

	// 查询令牌
	req = loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings?token=secret-token", nil))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("查询令牌状态 = %d，期望 200", rec.Code)
	}

	// 健康探测
	req = loopback(httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("health 状态 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("health body = %s", rec.Body.String())
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

// /apps 的契约：两个引擎各一行、字段齐全、动作开关与「命令存在」一致。
//
// 测的是形态而不是这台机器装了什么：CI 上可能一个引擎都没有，
// 那时 Installed 为假正好是另一种要覆盖的状态。
func TestAppsContract(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil))
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d；body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Code string `json:"code"`
		Data struct {
			Apps []struct {
				Kind             string `json:"kind"`
				AppName          string `json:"app_name"`
				Channel          string `json:"channel"`
				ChannelLabel     string `json:"channel_label"`
				InstallCommand   string `json:"install_command"`
				DocsURL          string `json:"docs_url"`
				UninstallCommand string `json:"uninstall_command"`
				CanUninstall     bool   `json:"can_uninstall"`
				CanInstall       bool   `json:"can_install"`
			} `json:"apps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "0" {
		t.Fatalf("code = %q；body=%s", envelope.Code, rec.Body.String())
	}
	if len(envelope.Data.Apps) != 2 {
		t.Fatalf("应该有 2 行（omp 与 pi），得到 %d", len(envelope.Data.Apps))
	}
	for _, row := range envelope.Data.Apps {
		if row.InstallCommand == "" {
			t.Errorf("%s 没有安装命令：界面会给一个点不动的按钮", row.Kind)
		}
		if row.DocsURL == "" {
			t.Errorf("%s 没有文档地址", row.Kind)
		}
		if row.ChannelLabel == "" {
			t.Errorf("%s 的渠道没有说明文案", row.Kind)
		}
		// 「有卸载命令」与「能不能卸」必须一致，否则会出现点了报错的按钮。
		if (row.UninstallCommand != "") != row.CanUninstall {
			t.Errorf("%s：uninstall_command=%q 但 can_uninstall=%v", row.Kind, row.UninstallCommand, row.CanUninstall)
		}
	}
}

// 动作入口只接受 GET / POST，且写操作必须带引擎名。
func TestAppsActionRequiresEngine(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodPost, "/api/v1/apps/install", strings.NewReader(`{}`)))
	req.Header.Set("X-Desktop-Token", "secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "7001" {
		t.Fatalf("缺引擎名应回 7001，得到 %q；body=%s", envelope.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "omp") || !strings.Contains(envelope.Message, "pi") {
		t.Errorf("提示要告诉用户填什么: %q", envelope.Message)
	}
}

// DELETE 一律拒绝：没有 PUT/DELETE 就没有「顺手加一个删除接口」的入口。
func TestAppsRejectsDelete(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodDelete, "/api/v1/apps/install", nil))
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE 状态 = %d，期望 405", rec.Code)
	}
}
