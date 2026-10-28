package engine

// 最新版查询：只回答「上游现在是多少」，不参与装与升级。
//
// 两条来源，都是引擎自己发布的地方：
//   - pi 在 npm 上，问 registry 的 dist-tags.latest；
//   - omp 的预编译产物由它自己的构建服务发，问 /api/products/omp/latest/<target>。
//
// 查不到不是错误：离线、代理、镜像都属正常情况。界面那时只显示本地版本，
// 不显示「未知」以外的任何判断——把网络问题说成「引擎有问题」是最糟的一种误报。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LatestTimeout 是单次查询的上限。首屏会并发问两处，卡住就等于首屏卡住。
const LatestTimeout = 4 * time.Second

// npmRegistry 与 ompBuildURL 是唯一的分发地址。改这里等于改镜像，别处不用动。
const (
	npmRegistry = "https://registry.npmjs.org"
	ompBuildURL = "https://build.stencil.so"
)

// latestTTL 是「最新版」的保鲜期。它比探测结果长得多：上游发版以天计，
// 而用户在一个会话里会反复进出应用页。
const latestTTL = 30 * time.Minute

// Latest 是一处上游的最新版。
type Latest struct {
	Version   string `json:"version"`
	CheckedAt int64  `json:"checked_at"` // Unix 毫秒
	Note      string `json:"note,omitempty"`
}

// latestCache 按引擎缓存查询结果，避免每进一次应用页就发两次外网请求。
type latestCache struct {
	mu   sync.Mutex
	rows map[Kind]Latest
}

var latestStore = &latestCache{rows: map[Kind]Latest{}}

// QueryLatest 并发查两个引擎的最新版。fore 为真时忽略缓存。
//
// 返回的 map 一定包含两个键：查不到的引擎对应空版本 + 一句原因，
// 界面据此区分「已是最新」与「没法判断」。
func QueryLatest(ctx context.Context, fore bool) map[Kind]Latest {
	out := map[Kind]Latest{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, kind := range AllKinds() {
		wg.Add(1)
		go func(kind Kind) {
			defer wg.Done()
			row := latestOf(ctx, kind, fore)
			mu.Lock()
			out[kind] = row
			mu.Unlock()
		}(kind)
	}
	wg.Wait()
	return out
}

// latestOf 查一个引擎，优先走缓存。
func latestOf(ctx context.Context, kind Kind, fore bool) Latest {
	latestStore.mu.Lock()
	cached, ok := latestStore.rows[kind]
	latestStore.mu.Unlock()
	if ok && !fore && time.Since(time.UnixMilli(cached.CheckedAt)) < latestTTL {
		return cached
	}

	version, note := fetchLatest(ctx, kind)
	row := Latest{Version: version, Note: note, CheckedAt: time.Now().UnixMilli()}
	latestStore.mu.Lock()
	latestStore.rows[kind] = row
	latestStore.mu.Unlock()
	return row
}

// fetchLatest 分派到两条来源。失败只记一句原因，不返回错误。
func fetchLatest(ctx context.Context, kind Kind) (string, string) {
	if kind == KindOMP {
		return fetchOMPLatest(ctx)
	}
	return fetchNPMLatest(ctx, "@earendil-works/pi-coding-agent")
}

// fetchNPMLatest 读 registry 的 dist-tags.latest。
//
// 只读 dist-tags 而不读版本列表：版本列表可能几百 KB，而界面只需要一个数。
func fetchNPMLatest(ctx context.Context, pkg string) (string, string) {
	body, err := httpGet(ctx, npmRegistry+"/"+escapePackage(pkg)+"/latest")
	if err != nil {
		return "", "查不到 npm 上的最新版：" + err.Error()
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "npm 返回的不是预期格式"
	}
	if payload.Version == "" {
		return "", "npm 上没有 latest 标签"
	}
	return payload.Version, ""
}

// fetchOMPLatest 问 omp 自己的构建服务。它也回答「这个平台有没有产物」。
func fetchOMPLatest(ctx context.Context) (string, string) {
	target := ompTarget()
	if target == "" {
		return "", "omp 没有为这个平台发布预编译产物"
	}
	url := ompBuildURL + "/api/products/omp/latest/" + target + "?channel=stable"
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", "查不到 omp 的最新版：" + err.Error()
	}
	var payload struct {
		Build struct {
			Version string `json:"version"`
		} `json:"build"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "构建服务返回的不是预期格式"
	}
	if payload.Build.Version == "" {
		return "", "构建服务没有给出稳定版"
	}
	return payload.Build.Version, ""
}

// ompTarget 是构建服务的平台标识（与官方安装器的取法一致）。
func ompTarget() string {
	switch runtime.GOARCH {
	case "amd64":
		return "windows-x86_64"
	case "arm64":
		return "windows-arm64"
	default:
		return ""
	}
}

// escapePackage 把 @scope/name 编成 registry 路径里的 %2f 形式。
func escapePackage(pkg string) string {
	return strings.Replace(pkg, "/", "%2f", 1)
}

// httpGet 是一次带超时的 GET，响应体上限 64 KB。
//
// 上限不是为了省内存：上游某个时刻改成返回整份版本清单时，
// 我们不该把几百 KB 读进内存再丢掉。
func httpGet(ctx context.Context, url string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, LatestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OhMyPiDesktop")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{Code: resp.StatusCode}
	}
	return readLimited(resp.Body, 64<<10)
}

// statusError 让「HTTP 404」这类原因在界面上读起来像一句话。
type statusError struct{ Code int }

func (e *statusError) Error() string {
	if e.Code == http.StatusNotFound {
		return "上游没有这个包（HTTP 404）"
	}
	return "上游返回 HTTP " + strconv.Itoa(e.Code)
}

// readLimited 读 resp.Body，超过上限就截断并返回错误。
//
// 截断而不静默丢弃：JSON 被截一半后解析失败，错误会落在「格式不对」上，
// 而真因是响应太大——两者给用户的下一步完全不同。
func readLimited(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil {
		return nil, err
	}
	return raw, nil
}
