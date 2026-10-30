package engine

// 最新版查询：只回答「上游现在是多少」，不参与装与升级。两条来源都是引擎自己发布的地方：
// pi 问 npm registry 的 dist-tags.latest，omp 的预编译产物问构建服务 /api/products/omp/latest/<target>。
// 查不到不是错误（离线、代理、镜像都属正常情况），界面那时只显示本地版本——把网络问题说成「引擎有问题」是最糟的误报。

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
	// disk 是落盘回调：把结果存到设置里，下次启动直接用。
	// 它存在的理由：这个进程内缓存每次启动都是空的，而首屏的 Health
	// 会同步等它——用户看到的「打开应用卡两秒」大多是在等外网。
	disk func(kind Kind, row Latest)
}

var latestStore = &latestCache{rows: map[Kind]Latest{}}

// SetLatestPersister 注入落盘回调（由 service 装配时提供）。
// 本包不 import repo：它属于 engine 层，而设置存储属于 repo，只留一个回调让上层决定存哪里。
func SetLatestPersister(fn func(kind Kind, row Latest)) {
	latestStore.mu.Lock()
	latestStore.disk = fn
	latestStore.mu.Unlock()
}

// SeedLatest 用上次落盘的结论填缓存，只填一条（版本 + 检查时刻）。
// 它代表「上次我们看的时候是多少」而不是「现在是多少」，所以 TTL 仍然照算——过期的会重新查，
// 但首屏拿到的是立即值而不是四秒等待。
func SeedLatest(kind Kind, row Latest) {
	latestStore.mu.Lock()
	defer latestStore.mu.Unlock()
	if _, ok := latestStore.rows[kind]; ok {
		return
	}
	latestStore.rows[kind] = row
}

// fetchLatestFunc 是查上游最新版的实际动作。
// 抽成变量是为了让测试不必联网：真去问 npm registry 与构建服务会让单元测试
// 变成「测网络通不通」——网络一慢测试就变慢、一断测试就红。
var fetchLatestFunc = fetchLatest

// SetLatestFetcher 换掉查最新版的动作，返回恢复用的函数。
//
// 只有测试用。生产代码走默认的 fetchLatest。
func SetLatestFetcher(fn func(context.Context, Kind) (string, string)) func() {
	prev := fetchLatestFunc
	fetchLatestFunc = fn
	return func() { fetchLatestFunc = prev }
}

// QueryLatest 并发查两个引擎的最新版。fore 为真时忽略缓存。
// 返回的 map 一定包含两个键：查不到的引擎对应空版本 + 一句原因，界面据此区分「已是最新」与「没法判断」。
// 首屏模式（缓存里已有值就直接返回它，同时后台补查）：首屏不该为「上游发版了没」等四秒。
func QueryLatest(ctx context.Context, fore bool) map[Kind]Latest {
	return queryLatest(ctx, fore, false)
}

// QueryLatestFast 是首屏用的版本：有缓存就用缓存，后台补查。
func QueryLatestFast(ctx context.Context) map[Kind]Latest {
	return queryLatest(ctx, false, true)
}

func queryLatest(ctx context.Context, fore, fast bool) map[Kind]Latest {
	out := map[Kind]Latest{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, kind := range AllKinds() {
		wg.Add(1)
		go func(kind Kind) {
			defer wg.Done()
			row := latestOf(ctx, kind, fore, fast)
			mu.Lock()
			out[kind] = row
			mu.Unlock()
		}(kind)
	}
	wg.Wait()
	return out
}

// latestOf 查一个引擎，优先走缓存。
// fast 模式（首屏）的语义是绝不同步等网络：有旧值就用旧值 + 后台补查，没旧值就返回空 + 后台查。
// 界面那时显示的是「还没查到」而不是「卡住」——后者会让用户以为应用挂了。
func latestOf(ctx context.Context, kind Kind, fore, fast bool) Latest {
	latestStore.mu.Lock()
	cached, ok := latestStore.rows[kind]
	latestStore.mu.Unlock()
	if ok && !fore && time.Since(time.UnixMilli(cached.CheckedAt)) < latestTTL {
		return cached
	}
	if fast && !fore {
		// 首屏：无论有没有旧值都不等。有旧值就先交旧的。
		go refreshLatest(kind)
		return cached
	}

	version, note := fetchLatestFunc(ctx, kind)
	row := Latest{Version: version, Note: note, CheckedAt: time.Now().UnixMilli()}
	latestStore.mu.Lock()
	latestStore.rows[kind] = row
	disk := latestStore.disk
	latestStore.mu.Unlock()
	if disk != nil {
		disk(kind, row)
	}
	return row
}

// WarmLatest 在后台预热两个引擎的最新版，装配阶段调用，不等结果：
// 首次启动时磁盘上没有任何结论，而首屏的快路径不会同步等网络——预热能让用户进概览页时已经有值。
func WarmLatest() {
	go func() { QueryLatest(context.Background(), false) }()
}

// refreshing 标记「这个引擎正在后台查」：没有它，首屏那一批请求（Health + Apps + Bootstrap）
// 会给每个引擎各发一个外网请求——而它们查的是同一个东西。
var refreshing sync.Map

// refreshLatest 后台补查一个引擎的最新版。
// 用独立的 context：调用方那个只在一次请求内有效，而这里的结果要落盘。
// 同一个引擎同时只允许一个后台查询在跑。
func refreshLatest(kind Kind) {
	if _, loaded := refreshing.LoadOrStore(kind, struct{}{}); loaded {
		return
	}
	defer refreshing.Delete(kind)

	ctx, cancel := context.WithTimeout(context.Background(), LatestTimeout*2)
	defer cancel()
	version, note := fetchLatestFunc(ctx, kind)
	if version == "" && note != "" {
		return // 查不到就保留旧值，不要把「已是最新」变成「未知」
	}
	row := Latest{Version: version, Note: note, CheckedAt: time.Now().UnixMilli()}
	latestStore.mu.Lock()
	latestStore.rows[kind] = row
	disk := latestStore.disk
	latestStore.mu.Unlock()
	if disk != nil {
		disk(kind, row)
	}
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

// httpGet 是一次带超时的 GET，响应体上限 64 KB。上限不是为了省内存：
// 上游某个时刻改成返回整份版本清单时，我们不该把几百 KB 读进内存再丢掉。
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

// readLimited 读 resp.Body，超过上限就截断并返回错误。截断而不静默丢弃：
// JSON 被截一半后解析失败，错误会落在「格式不对」上，而真因是响应太大——两者给用户的下一步完全不同。
func readLimited(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil {
		return nil, err
	}
	return raw, nil
}
