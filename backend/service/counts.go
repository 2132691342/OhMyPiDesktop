package service

// 概览页要的「落盘计数」：会话 / 技能 / 插件 / MCP 四个数。
//
// 为什么单独一个文件：这四个数要扫目录，而它们是首屏的数字。整段设计的重心
// 不是「怎么数」，而是「数不到的时候怎么说话」——零值不等于「有 0 个」。
//
// 本文件不做什么：不算用量、不解析会话正文、不判断引擎装没装。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// healthCounts 是一份「落盘计数」快照。
//
// 为什么单独缓存：这四个数要扫目录（会话头、技能文件、插件清单、MCP 配置），
// 而它们的变化频率远低于查看频率——用户不会为了看到一个数字变化而反复刷新。
// 不缓存的话，每次 Health / Bootstrap 都重新扫一遍，
// 而概览页是首屏——用户看到的就是「打开应用卡一秒」。
type healthCounts struct {
	sessions int
	skills   int
	plugins  int
	mcp      int
	at       time.Time
	// known 逐位标记上面四个数是不是真扫出来的。
	//
	// 为什么不能用一个整包的 ok bool：四类目录各自的失败原因不同
	// （会话目录读不动、技能判定要遍历插件的 node_modules 被跳过、
	// 落盘布局本身解不出来）。整包判据会让「技能没扫到」把「会话明明读到了」
	// 也一起抹成未知——而用户要的恰恰是「这个数是可信的」。
	known knownFlags
}

// knownFlags 是四个计数的「是否可信」标记。
type knownFlags struct {
	sessions bool
	skills   bool
	plugins  bool
	mcp      bool
}

// countsTTL 是计数快照的保鲜期。
//
// 短到足够跟上「刚装完插件 / 刚建了会话」，长到能把首屏的多个请求合成一次扫描。
const countsTTL = 30 * time.Second

// countSessions 数会话文件数量。不调 Index.List（那会逐个读文件头解析标题与用量，
// 而这里只需要一个数字）。目录结构是 <会话目录>/<目录 slug>/<文件>.jsonl，只需两层
// ReadDir，不读文件内容。
func (a *App) countSessions(kind engine.Kind) (int, bool) {
	layout, err := a.Layout(kind)
	if err != nil {
		return 0, false
	}
	dirs, err := os.ReadDir(layout.Sessions)
	if err != nil {
		return 0, false
	}
	total := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(layout.Sessions, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl") {
				total++
			}
		}
	}
	return total, true
}

// countsOf 取一份计数快照：命中缓存直接返回；过期则交旧值并后台补扫。
// 四个数并发取（各扫不同目录），结果经 channel 回传，不并发写同一 struct。
func (a *App) countsOf(kind engine.Kind) healthCounts {
	a.countsMu.Lock()
	cached, ok := a.counts[kind]
	a.countsMu.Unlock()
	if ok && time.Since(cached.at) < countsTTL {
		return cached
	}

	// 过期：交旧值并后台刷新；首屏不为一次目录扫描停顿。
	if ok {
		a.refreshCountsAsync(kind)
		return cached
	}
	c := a.scanCounts(kind)
	a.storeCounts(kind, c)
	return c
}

// refreshCountsAsync 后台重扫一次计数（同一引擎同时只一颗）。
func (a *App) refreshCountsAsync(kind engine.Kind) {
	a.countsBusyMu.Lock()
	if a.countsBusy == nil {
		a.countsBusy = map[engine.Kind]bool{}
	}
	if a.countsBusy[kind] {
		a.countsBusyMu.Unlock()
		return
	}
	a.countsBusy[kind] = true
	a.countsBusyMu.Unlock()

	go func() {
		defer func() {
			a.countsBusyMu.Lock()
			delete(a.countsBusy, kind)
			a.countsBusyMu.Unlock()
		}()
		a.storeCounts(kind, a.scanCounts(kind))
	}()
}

// scanCounts 真正扫一遍四类目录。四个 goroutine 各自通过 channel 交结果。
//
// 每个槽除了数字还交回一个 ok：调用方只读规定 RMS 的目录（它可能在网络盘上、
// 可能被权限挡住），失败就是「没读到」，不是一个 0。
func (a *App) scanCounts(kind engine.Kind) healthCounts {
	type slot struct {
		which int
		n     int
		ok    bool
	}
	out := make(chan slot, 4)

	go func() {
		n, ok := a.countSessions(kind)
		out <- slot{0, n, ok}
	}()
	go func() {
		if skills, err := a.Skills(kind); err == nil {
			out <- slot{1, len(skills), true}
			return
		}
		out <- slot{1, 0, false}
	}()
	go func() {
		if plugins, err := a.pluginsOf(kind); err == nil && plugins != nil {
			out <- slot{2, len(plugins.Plugins), true}
			return
		}
		out <- slot{2, 0, false}
	}()
	go func() {
		if mcp, err := a.McpList(context.Background(), kind, false); err == nil && mcp != nil {
			out <- slot{3, len(mcp.Servers), true}
			return
		}
		out <- slot{3, 0, false}
	}()

	c := healthCounts{at: time.Now()}
	for i := 0; i < 4; i++ {
		s := <-out
		switch s.which {
		case 0:
			c.sessions, c.known.sessions = s.n, s.ok
		case 1:
			c.skills, c.known.skills = s.n, s.ok
		case 2:
			c.plugins, c.known.plugins = s.n, s.ok
		case 3:
			c.mcp, c.known.mcp = s.n, s.ok
		}
	}
	return c
}

// storeCounts 写入一份快照。
func (a *App) storeCounts(kind engine.Kind, c healthCounts) {
	a.countsMu.Lock()
	a.counts[kind] = c
	a.countsMu.Unlock()
}

// InvalidateCounts 丢掉计数缓存（装完插件 / 删了会话 / 换了工作目录之后）。
func (a *App) InvalidateCounts() {
	a.countsMu.Lock()
	a.counts = map[engine.Kind]healthCounts{}
	a.countsMu.Unlock()
}
