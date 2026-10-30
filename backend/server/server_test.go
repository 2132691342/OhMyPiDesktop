package server

// 安全边界层：只测令牌闸门、来源拦截、方法限制，
// 外加应用页与附件的契约形态。
//
// 这一组测的是「谁能让这条服务干活」——闸门一旦失效，
// 任何本机进程都能读用户的会话、改他的设置。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/service"
)

// testServer 桩掉探测与查最新版（不依赖本机环境），造一个带令牌的服务。
func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	restoreDetect := service.SetDetector(func(_ context.Context, _ string, kind engine.Kind) (engine.Identity, string) {
		return engine.Identity{
			Kind:    kind,
			Exe:     `C:\fake\` + string(kind) + `.exe`,
			Version: "1.0.0",
			Source:  "custom",
			Channel: engine.ChannelNPM,
			Caps:    engine.CapsFor(kind),
		}, ""
	})
	t.Cleanup(restoreDetect)

	restoreLatest := engine.SetLatestFetcher(func(_ context.Context, _ engine.Kind) (string, string) {
		return "9.9.9", ""
	})
	t.Cleanup(restoreLatest)

	dir := t.TempDir()
	store, err := repo.Open(dir + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc, err := service.New(service.Options{
		Store:    store,
		Hub:      hub.New(16),
		DataDir:  dir,
		Version:  "test",
		SkipWarm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等后台探测退完再让 TempDir 被回收。
	//
	// 探测是故意异步的（首屏不该等子进程），而它会写设置缓存；
	// 不等它就跑完用例，t.TempDir() 的 RemoveAll 会撞上
	// 那个还在跑的写入，报「directory is not empty」——
	// 而那个报错与测试本身想断言的东西毫无关系。
	t.Cleanup(func() { svc.WaitBackground() })
	srv := New(svc, svc.Hub, "secret-token")
	return srv, srv.Handler()
}

// loopback 把请求来源标为本机，绕开来源闸门（默认来源会被挡）。
func loopback(req *http.Request) *http.Request {
	req.RemoteAddr = "127.0.0.1:51234"
	return req
}

// get 造一个带令牌的 GET。
func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := loopback(httptest.NewRequest(http.MethodGet, path, nil))
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// post 造一个带令牌的 POST。
func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := loopback(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	req.Header.Set("X-Desktop-Token", "secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 令牌闸门四个状态都不能错：无令牌 403、头令牌 200、查询令牌 200、health 免令牌。
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
	rec = get(t, h, "/api/v1/settings?token=secret-token")
	if rec.Code != http.StatusOK {
		t.Errorf("查询令牌状态 = %d，期望 200", rec.Code)
	}

	// 健康探测
	rec = get(t, h, "/api/v1/health")
	if rec.Code != http.StatusOK {
		t.Errorf("health 状态 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("health body = %s", rec.Body.String())
	}
}

// 只允许 GET / POST：不留 DELETE 路由就没有「顺手加删除接口」的入口。
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

// HTTP 恒回 200，错误在 body 的 code 里；message 要能直接显示给用户。
func TestErrorUsesEnvelope(t *testing.T) {
	_, h := testServer(t)
	rec := post(t, h, "/api/v1/settings/workspace", `{"path":"Z:\\definitely\\missing\\dir"}`)

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

// 来源闸门：非本机地址一律拒绝，哪怕令牌正确。
func TestRejectsNonLoopback(t *testing.T) {
	_, h := testServer(t)
	req := loopback(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	req.RemoteAddr = "192.0.2.10:5000"
	req.Header.Set("X-Desktop-Token", "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("外部来源状态 = %d，期望 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "7003") {
		t.Errorf("应回报来源被拒的错误码: %s", rec.Body.String())
	}
}

// 应用页契约：两引擎各一行、字段齐全、动作开关与「命令存在」一致。
func TestAppsContract(t *testing.T) {
	_, h := testServer(t)
	rec := get(t, h, "/api/v1/apps")
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

// 动作入口必须带引擎名，否则后端不知道该装谁。
func TestAppsActionRequiresEngine(t *testing.T) {
	_, h := testServer(t)
	rec := post(t, h, "/api/v1/apps/install", `{}`)

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

// 附件契约：坏输入必须在发给引擎前被拦下（图发过去模型却说没看到，无编译错误）。
func TestChatSendRejectsBadAttachments(t *testing.T) {
	_, h := testServer(t)

	cases := []struct {
		what string
		body string
	}{
		{
			what: "不认识的附件类型",
			body: `{"message":"hi","attachments":[{"kind":"video"}]}`,
		},
		{
			what: "图片内容不是 base64",
			body: `{"message":"hi","attachments":[{"kind":"image","image_base64":"@@@"}]}`,
		},
		{
			what: "图片内容是纯文本",
			body: `{"message":"hi","attachments":[{"kind":"image","image_base64":"aGVsbG8gd29ybGQ="}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			rec := post(t, h, "/api/v1/chat/send", tc.body)
			var out struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("响应不是 JSON：%v", err)
			}
			// 这些都必须在「发给引擎之前」被拦下：code 非 0 且话说得清楚。
			if out.Code == "0" {
				t.Fatalf("坏附件不该成功：%s", rec.Body.String())
			}
			if out.Message == "" {
				t.Fatalf("错误必须带一句人话：%s", rec.Body.String())
			}
		})
	}
}
