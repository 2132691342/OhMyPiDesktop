// Package upstream 向上游服务商问「有哪些模型」。
//
// 模型清单在引擎里是一份运行时事实，而配一个服务商时用户手上只有地址与密钥——
// 「有哪些模型」这个问题只有上游知道。
//
// 只做一件事：按协议向 /models 问一次，把响应归一成模型 id 列表。
// 不写配置、不管凭据管理、不猜模型能力。
package upstream

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// fetchTimeout 是问上游的时限。上游慢时一分钟太长，界面会像卡住了。
const fetchTimeout = 20 * time.Second

// maxBodyBytes 是响应体上限：上游理论上不会返回这么大的东西，
// 而不设上限的话，一个坏地址能让我们吃下几百 MB。
const maxBodyBytes = 8 << 20

// Request 是一次「问上游有哪些模型」的入参。
type Request struct {
	// BaseURL 服务商地址
	BaseURL string `json:"base_url"`
	// API 协议（openai-completions / anthropic-messages / …）
	API string `json:"api"`
	// APIKey 密钥；空表示这个服务商靠环境变量或不需要密钥
	APIKey string `json:"api_key"`
	// Auth 认证方式：api-key（默认）/ oauth / none
	Auth string `json:"auth"`
}

// ModelEntry 是问回来的一个模型：只有 id 与展示名。
//
// 上游给的字段五花八门（owned_by、created、permission…），
// 而界面的「拉取模型」只关心「有哪些 id、叫什么」——
// 别的字段（上下文窗口、是否推理）引擎自己知道。
type ModelEntry struct {
	// ModelID 模型标识，写进配置就是它
	ModelID string `json:"model_id"`
	// ModelName 展示名；上游没给就是空
	ModelName string `json:"model_name,omitempty"`
}

// FetchModels 向上游问一次模型清单。
func FetchModels(req Request) ([]ModelEntry, error) {
	base := strings.TrimSpace(req.BaseURL)
	if base == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "还没填接口地址，先填地址再拉模型", "")
	}
	endpoint, err := modelsEndpoint(base)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: fetchTimeout}
	httpReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNetwork, "拼不出请求地址", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	applyAuth(httpReq, req)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeNetwork, "连不上这个服务商", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode != http.StatusOK {
		return nil, pkg.New(pkg.CodeNetwork, statusMessage(resp.StatusCode, string(body)), "")
	}
	return parseModels(string(body))
}

// applyAuth 按认证方式加请求头。
//
// oauth 与 api-key 两种都发 Authorization：多数兼容服务商会忽略用不上的那个头，
// 而少发一个的后果是直接 401。
func applyAuth(req *http.Request, r Request) {
	key := strings.TrimSpace(r.APIKey)
	if key == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(r.Auth)) {
	case "none":
		return
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// statusMessage 把上游的状态码翻成一句能照着做的提示。
func statusMessage(code int, body string) string {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "服务商拒绝了这次请求：密钥不对或没有权限"
	case http.StatusNotFound:
		return "这个地址没有 /models 接口：检查接口地址是否写全（可能要带 /v1）"
	case http.StatusTooManyRequests:
		return "服务商限流了：等一会儿再试"
	case http.StatusBadRequest:
		return "服务商说请求不合法：检查接口地址与协议"
	}
	msg := strings.TrimSpace(body)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if msg == "" {
		return "服务商返回了异常状态"
	}
	return "服务商返回了异常状态：" + msg
}

// modelsEndpoint 拼出「问模型清单」的地址。
//
// 规则只有两条，但两条都必要：
//   - 地址以 / 结尾：直接接 models（用户已经写全了路径）。
//   - 地址里有 /v1：接 /models（OpenAI 兼容服务商的模型清单就在 /v1/models）。
//   - 其余：接 /v1/models（大多数兼容服务商的默认形状）。
//
// 也支持 base 里已经带着 /models 的写法——那就不该再拼一次。
func modelsEndpoint(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", pkg.Wrap(pkg.CodeBadRequest, "接口地址不合法", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", pkg.New(pkg.CodeBadRequest, "接口地址不完整：要写成 https://host/v1 这种样子", base)
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/models") {
		return u.String(), nil
	}
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	u.Path = path + "/models"
	return u.String(), nil
}

// parseModels 从响应里挑出模型 id。
//
// 兼容三种形状：
//
//	{"data":[{"id":"..."}]}          OpenAI / Anthropic
//	{"data":[{"model":"..."}]}        一些兼容服务商的变体
//	{"models":[{"id":"..."}]}         部分网关
func parseModels(body string) ([]ModelEntry, error) {
	var doc struct {
		Data []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"data"`
		Models []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, pkg.Wrap(pkg.CodeNetwork, "读不懂服务商的响应", err)
	}
	items := doc.Data
	if len(items) == 0 {
		items = doc.Models
	}
	out := make([]ModelEntry, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = strings.TrimSpace(it.Model)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ModelEntry{ModelID: id, ModelName: strings.TrimSpace(it.Name)})
	}
	if len(out) == 0 {
		return nil, pkg.New(pkg.CodeNetwork, "这个地址没返回模型清单：检查地址与协议是否匹配", "")
	}
	return out, nil
}
