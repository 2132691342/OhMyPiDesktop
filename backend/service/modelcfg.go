package service

// 模型清单的缓存：让「打开模型下拉」与「切引擎」都不必等引擎冷启动。
//
// 引擎的 get_available_models 是运行时状态，拿到它必须先拉起一个引擎进程
// （omp 冷启动实测 11 秒，pi 起一次 Node 3 秒）——而「看一眼模型下拉」
// 这种零风险操作不该付这个代价。
//
// 缓存的是「上一次问到的清单」，不是「当前能用的清单」：换了服务商配置或
// 登录了新账号缓存就过期。所以它有 TTL，且那三件事会主动失效它。

import (
	"context"
	"sync"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/ompcfg"
	"OhMyPiDesktop/backend/env/picfg"
)

// modelCacheTTL 是模型清单的保鲜期。
//
// 短到能跟上「刚换完服务商就打开下拉」，长到能把首屏的多次请求合成一次。
// 与 countsTTL 同量级：它们都是「变化比查看慢」的事实。
const modelCacheTTL = 30 * time.Second

// modelCache 是按引擎分桶的模型清单缓存。
//
// 按引擎分桶而不是一张表：两个引擎的清单完全不同，而一次查询只问一个引擎。
type modelCache struct {
	mu   sync.Mutex
	rows map[engine.Kind]modelCacheEntry
}

// modelCacheEntry 是缓存里的一条：清单与它被问到的时刻。
type modelCacheEntry struct {
	rows []ModelRow
	at   time.Time
}

// hit 判断这条缓存是不是还能用。
func (e modelCacheEntry) hit() bool {
	return !e.at.IsZero() && time.Since(e.at) < modelCacheTTL
}

// cachedModels 返回命中缓存的清单；没命中返回 nil。
//
// 返回 nil 与返回空切片的区别很重要：空切片是「问了，确实没有模型」，
// nil 是「没问过」——后者不能当答案交出去。
func (m *modelCache) cachedModels(kind engine.Kind) ([]ModelRow, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.rows[kind]
	if !ok || !entry.hit() {
		return nil, false
	}
	return entry.rows, true
}

// storeModels 写下一份清单。
func (m *modelCache) storeModels(kind engine.Kind, rows []ModelRow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[engine.Kind]modelCacheEntry{}
	}
	m.rows[kind] = modelCacheEntry{rows: rows, at: time.Now()}
}

// invalidate 丢掉某引擎的缓存（配置变了）。
func (m *modelCache) invalidate(kind engine.Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, kind)
}

// invalidateAll 丢掉全部缓存。
func (m *modelCache) invalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = map[engine.Kind]modelCacheEntry{}
}

// ModelsFromConfig 从配置文件里读出模型清单。
//
// kind 为空时两个引擎都读。读不出来（文件不存在 / 格式有误）就少一条，
// 不返回错误：模型清单拉不到时界面仍应可用，只是下拉里少了几个选项。
func (a *App) ModelsFromConfig(kind engine.Kind) []ModelRow {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := make([]ModelRow, 0, 16)
	for _, k := range kinds {
		out = append(out, a.modelsFromConfigOf(k)...)
	}
	return out
}

// modelsFromConfigOf 读单个引擎的配置模型清单。
func (a *App) modelsFromConfigOf(kind engine.Kind) []ModelRow {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil
		}
		out := make([]ModelRow, 0, 16)
		for name, p := range f.Providers {
			for _, m := range p.Models {
				out = append(out, ModelRow{
					Kind: kind, Provider: name, ID: m.ID, Name: m.Name, API: m.API,
					ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
					Reasoning: m.Reasoning, Input: m.Input,
				})
			}
		}
		return out
	}
	f, err := ompcfg.ReadModels(layout.Models)
	if err != nil {
		return nil
	}
	out := make([]ModelRow, 0, 16)
	for name, p := range f.Providers {
		for _, m := range p.Models {
			out = append(out, ModelRow{
				Kind: kind, Provider: name, ID: m.ID, Name: m.Name, API: m.API,
				ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
				Reasoning: m.Reasoning, Input: m.Input,
			})
		}
	}
	return out
}

// AvailableModels 是「模型清单」的统一出口。
//
// 三档来源，从快到准：
//  1. 缓存命中：直接交上次问到的清单（零等待）。
//  2. 引擎在跑：问引擎（它知道自己有哪些凭据、哪些模型真的能调）。
//  3. 引擎没跑：读配置文件（不为了看个下拉去起一个进程）。
//
// 第 1 档是「上次问到的」，不是「此刻真的能调」——所以它有 TTL，
// 而且配置一变就主动失效。宁可让用户看到一份略旧的清单，
// 也不要为了一个下拉等十秒。
func (a *App) AvailableModels(ctx context.Context, kind engine.Kind) ([]ModelRow, error) {
	if rows, ok := a.modelCache.cachedModels(kind); ok {
		return rows, nil
	}
	e := a.EngineFor(kind, a.Workspace())
	if e.Live() {
		list, err := e.AvailableModels(ctx)
		if err == nil {
			out := make([]ModelRow, 0, len(list))
			for _, m := range list {
				out = append(out, ModelRow{
					Kind: kind, Provider: m.Provider, ID: m.ID, Name: m.Name, API: m.API,
					ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
					Reasoning: m.Reasoning, Input: m.Input,
				})
			}
			a.modelCache.storeModels(kind, out)
			return out, nil
		}
	}
	// 引擎没跑或问不出来：退到配置文件。
	rows := a.ModelsFromConfig(kind)
	a.modelCache.storeModels(kind, rows)
	return rows, nil
}

// InvalidateModels 丢掉模型清单缓存。
//
// 在「配置变了」之后调用：保存 / 删除服务商、登录成功、重新探测引擎。
// 不失效的话，用户会看到一份改了之前的清单，而且要等 30 秒才更新。
func (a *App) InvalidateModels(kind engine.Kind) {
	if kind.Valid() {
		a.modelCache.invalidate(kind)
		return
	}
	a.modelCache.invalidateAll()
}
