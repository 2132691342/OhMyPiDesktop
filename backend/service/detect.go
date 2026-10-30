package service

// 引擎探测的生命周期：跑一轮、去重、缓存、广播、等待点。
//
// 探测是全应用最慢的一个动作（omp 的 --version 几十毫秒；
// pi 的 --help 要起一次 Node，实测 2.7~3.6 秒）。所以这里的每一处
// 设计都在回答同一个问题：「用户在哪一刻必须等，哪一刻不必等」。
//
// 本文件不做什么：不做渠道判定、不读写引擎配置、不装任何东西。
// 探测只回答「装在哪、什么版本、为什么没探到」，其余交给上层。

import (
	"context"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
)

// detectFunc 是探测一个引擎的实际动作。
//
// 抽成变量是为了让测试不必依赖「这台机器上装了什么」：
// 探测要起子进程（pi 的 --help 要起一次 Node，几秒），而单元测试
// 断言的是「响应形态对不对」，不是「机器上有没有装 pi」。
//
// 返回值第二项是「为什么没探到」（见 env.DetectReason）：空串表示结论可信。
var detectFunc = env.DetectReason

// detectResult 是一颗探测的产出：结论 + 「没探到」的原因。
type detectResult struct {
	id     engine.Identity
	reason string
}

// detectAll 探测所有引擎并把结果记下来。fore 为真时忽略缓存。
//
// 两个引擎并发探测：它们互不依赖，串行只是让首屏多等一个 pi 的 Node 启动。
//
// 并发去重：首屏会同时打 Bootstrap / Health / EngineStatus，三个请求都
// 发现缓存过期就各自起一轮探测——机器上就多出六个子进程，而它们查的是同一件事。
// 用一个「探测中就等它」的锁把它们合成一次。
//
// 阻塞策略（实测数据：pi 的 --help 起一次 Node 要 3.6 秒，而 Health 本身只要 0.5 毫秒）：
//
//	tab: 有落盘结论 → 立即返回 + 后台补验。首屏永远不吃这 3.6 秒。
//	tab: 啥都没有（首发）→ 不等。立即交空值 + 后台探，界面显示「正在检测」
//	tab: 而不是白屏 3.6 秒。
//	tab: fore（用户点「重新检测」）→ 这次真的等，因为用户就是来看结果的。
func (a *App) detectAll(ctx context.Context, fore bool) {
	if a.detectFresh(fore) {
		return
	}
	if !fore {
		// 首屏路径：永不阻塞。有旧值用旧值，没旧值就空着，两者都后台补验。
		a.detectInBackground()
		return
	}
	a.detectMu.Lock()
	if a.detectFresh(fore) {
		a.detectMu.Unlock()
		return
	}
	a.openDetectWait()
	defer close(a.detectWait)
	a.detectOnce(ctx)
	a.detectMu.Unlock()
}

// openDetectWait 为本轮探测换一个新的等待点。
//
// 每轮都新建一个 channel 而不是复用并清空：在关掉与重开之间
// 阻塞着的调用方要保证能确切地收到「这一轮结束了」，复用会让他们
// 要么错过唤醒，要么被下一轮提前唤醒。
func (a *App) openDetectWait() {
	a.mu.Lock()
	a.detectWait = make(chan struct{})
	a.mu.Unlock()
}

// DetectWait 返回本轮探测的等待点（可能已经关掉 = 探测已完成）。
func (a *App) DetectWait() <-chan struct{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.detectWait == nil {
		// 从未探测过：给一个已关闭的，等它等于不等。
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return a.detectWait
}

// detectInBackground 在后台补一次真探测，不阻塞调用方。
//
// 三个限制：只一颗在跑（bgDetecting）、不重复排队、失败不重试——
// 失败时界面用已有的落盘结果继续工作，下次请求自然会再试。
func (a *App) detectInBackground() {
	if !a.bgDetecting.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.bgDetecting.Store(false)
		a.detectMu.Lock()
		defer a.detectMu.Unlock()
		a.openDetectWait()
		defer close(a.detectWait)
		// 后台补验用不带超时的 context：请求自己的 ctx 会随响应结束而取消，
		// 而探测结果是要落盘的，不该被一个客户端断开打断。
		//
		// 但探测命令本身仍有上限（engine.ProbeTimeout）：那是给「候选卡死」
		// 兜底的，与「客户端断开」不是一回事。
		a.detectOnce(context.Background())
	}()
}

// WaitBackground 等后台探测跑完。
//
// 为什么要有它：探测是故意异步的（首屏不该等子进程），而它会写设置缓存。
// 进程退出时不等它，就会在临时目录已经被删的情况下继续写——
// 测试里表现为 t.TempDir() 的 RemoveAll 报「directory is not empty」，
// 而那个报错与测试想断言的东西毫无关系。
func (a *App) WaitBackground() {
	a.detectMu.Lock()
	a.detectMu.Unlock()
}

// HasCachedIdentity 报告是否已有可展示的探测结论。
//
// 它跟 refreshing 一起回答「界面上这个版本号是不是刚验过的」。
// 首发（还没探过）时返回 false，界面据此说「正在检测」而不是「未安装」。
func (a *App) HasCachedIdentity() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, kind := range engine.AllKinds() {
		if id, ok := a.ids[kind]; ok && id.Exe != "" {
			return true
		}
	}
	return false
}

// detectFresh 判断现有结果是否还新。
//
// 判据不能用「ids 里有几个引擎」：没装的引擎不会有可执行文件，
// 用它做判据会让「装了一个、缺一个」的机器永远被判成不新鲜——
// 于是每次 Health 都重新探测（本机实测 1.6 秒），而缓存其实好好的。
//
// 正确判据：上次探测发生过（detectedAt 非零）且未过期。
func (a *App) detectFresh(fore bool) bool {
	if fore {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.detectedAt.IsZero() && time.Since(a.detectedAt) < detectTTL
}

// detectOnce 实际探测一轮并写入结果。调用方持有 detectMu。
func (a *App) detectOnce(ctx context.Context) {
	type result struct {
		kind   engine.Kind
		id     engine.Identity
		reason string
	}
	kinds := engine.AllKinds()
	ch := make(chan result, len(kinds))
	for _, kind := range kinds {
		go func(kind engine.Kind) {
			id, reason := detectFunc(ctx, a.Store.Get(domain.KeyEngineExePrefix+string(kind)), kind)
			if id.Exe == "" {
				// 没装不是「不知道」：记下空 Exe 的条目。
				// 不记的话 detectFresh 永远认为「有引擎还没探过」，
				// 于是每次请求都重探一遍。
				id = engine.Identity{Kind: kind, Caps: engine.CapsFor(kind)}
			}
			ch <- result{kind: kind, id: id, reason: reason}
		}(kind)
	}
	// 先把两个结果收齐，再持锁写入。
	//
	// 这一顺序是硬要求：之前是「持写锁等 channel」，于是探测期间（pi 的
	// --help 要起 Node，实测 3.6 秒）所有读 ids / workspace / index 的调用
	// 全部堵在 RLock 上——界面上表现就是「打开应用卡四秒」，
	// 而日志里探测明明已经改成后台了。
	collected := make(map[engine.Kind]engine.Identity, len(kinds))
	reasons := make(map[engine.Kind]string, len(kinds))
	for range kinds {
		r := <-ch
		collected[r.kind] = r.id
		reasons[r.kind] = r.reason
	}

	a.mu.Lock()
	for k, v := range collected {
		a.ids[k] = v
	}
	a.probeReason = reasons
	a.detectedAt = time.Now()
	snapshot := make(map[engine.Kind]engine.Identity, len(a.ids))
	for k, v := range a.ids {
		snapshot[k] = v
	}
	a.mu.Unlock()
	// 探测要起子进程（pi 的 --help 要起一次 Node，约 2.7 秒），
	// 而结果只会在「用户换了引擎路径或版本」时变。落盘之后，
	// 下次启动可以直接用上次的结论，首屏不必再等这三秒。
	a.persistIdentities(snapshot)
	// 探完了就说一声。界面据此把「正在检测」换成真结果——
	// 之前只有界面自己去轮询，于是概览页显示的是探测前的旧值，
	// 而用户看到的是「版本号是旧的」，却没有任何提示说它正在更新。
	a.emitDetected()
}

// emitDetected 广播探测完成。
//
// 事件不带结论本身：结论由界面自己去拉一次 /engines。
// 理由是探测结果会落盘并被各处缓存，多一份广播就要多一处可能漂移的真相。
func (a *App) emitDetected() {
	if a.Hub == nil {
		return
	}
	a.Hub.Emit("", "engine:detected", map[string]any{"at": time.Now().UnixMilli()})
}

// DetectedAt 返回上次探测完成的时刻；从未探过时返回 0。
//
// 它回答「屏幕上这个版本号是什么时候验的」，所以必须是真实完成时刻，
// 不能是「本次响应的时刻」——后者会让界面永远显示「刚刚检测过」。
func (a *App) DetectedAt() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.detectedAt.IsZero() {
		return 0
	}
	return a.detectedAt.UnixMilli()
}

// SetDetector 换掉探测引擎的动作，返回恢复用的函数。
//
// 只有测试用。生产代码走默认的 env.DetectReason。
//
// 签名带 ctx 与 reason：探测的真实成本（起子进程）与「为什么没探到」
// 都是被测行为的一部分，去掉任一个测试就只能测出一个假的形态。
func SetDetector(fn func(context.Context, string, engine.Kind) (engine.Identity, string)) func() {
	prev := detectFunc
	detectFunc = fn
	return func() { detectFunc = prev }
}
