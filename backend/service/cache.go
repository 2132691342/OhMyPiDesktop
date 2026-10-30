package service

// 两种「下次就不必再慢一次」的落盘缓存：探测结果与上游最新版。它们都不是真相，只是加速器：
// 真相由 detectAll 那一轮重新判定并覆盖这里读回的值，所以写不进去不报错、读到过期的只是多花几秒，正确性从不依赖它们。
// 「为什么没探到」的原因不落盘：那是一轮的即时结论，落盘会留下已不成立的原因。

import (
	"encoding/json"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- 探测结果 ----------------

// identityCacheKey 是探测结果的落盘键。
func identityCacheKey(kind engine.Kind) string {
	return "cache.identity." + string(kind)
}

// loadCachedIdentities 在启动时用上次的探测结果填满 ids，只是「首屏先有个显示」，
// 真正的判定仍是第一次 detectAll 并被它覆盖。没装（可执行文件不在）的引擎也要留空 Exe 条目：
// detectFresh 靠「ids 非空」判断「上次探过」，缺条目会被当成没探过而重探。
func (a *App) loadCachedIdentities() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, kind := range engine.AllKinds() {
		raw := a.Store.Get(identityCacheKey(kind))
		if raw == "" {
			continue
		}
		var id engine.Identity
		if err := json.Unmarshal([]byte(raw), &id); err != nil || id.Exe == "" {
			continue
		}
		// 可执行文件已经不在了就不算数：那是卸载或换机器之后的残留。
		if !pkg.FileExists(id.Exe) {
			continue
		}
		id.Caps = engine.CapsFor(kind)
		a.ids[kind] = id
	}
}

// persistIdentities 把探测结果写进设置文件。
//
// 写不进去不影响使用：它只是为了让下次启动快一点。
func (a *App) persistIdentities(ids map[engine.Kind]engine.Identity) {
	for kind, id := range ids {
		if id.Exe == "" {
			_ = a.Store.Set(identityCacheKey(kind), "")
			continue
		}
		raw, err := json.Marshal(id)
		if err != nil {
			continue
		}
		if err := a.Store.Set(identityCacheKey(kind), string(raw)); err != nil {
			pkg.Warnf("app: 探测结果落盘失败: %v", err)
		}
	}
}

// ---------------- 上游最新版 ----------------

// latestCacheKey 是「上游最新版」的落盘键。
func latestCacheKey(kind engine.Kind) string {
	return "cache.latest." + string(kind)
}

// wireLatestPersistence 把「最新版」的落盘接到 engine 层。
//
// engine 层不 import repo（那会让它反向依赖），所以只给一个回调。
func (a *App) wireLatestPersistence() {
	engine.SetLatestPersister(func(kind engine.Kind, row engine.Latest) {
		if row.Version == "" {
			return // 「查不到」不值得落盘，下次还得再查
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return
		}
		_ = a.Store.Set(latestCacheKey(kind), string(raw))
	})
}

// loadCachedLatest 用上次落盘的最新版填缓存，让首屏不必等外网。
func (a *App) loadCachedLatest() {
	for _, kind := range engine.AllKinds() {
		raw := a.Store.Get(latestCacheKey(kind))
		if raw == "" {
			continue
		}
		var row engine.Latest
		if err := json.Unmarshal([]byte(raw), &row); err != nil || row.Version == "" {
			continue
		}
		engine.SeedLatest(kind, row)
	}
}
