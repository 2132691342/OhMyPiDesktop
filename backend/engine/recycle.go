package engine

// 空闲实例回收：切目录会新建实例，没人回收就会堆到退出才释放。
//
// 两条规则（TTL 与硬上限）都是「只碰能安全停掉的」——正在流式回答的
// 实例永不回收，否则用户正在看的那一轮会被打断。

import (
	"sort"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// sweepLoop 周期回收空闲引擎。
func (s *Supervisor) sweepLoop() {
	defer close(s.done)
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.Sweep()
		}
	}
}

// Stats 返回实例数与上限，供「引擎健康」那一屏显示。
//
// 把进程数暴露出来的理由：它是「有没有泄漏」的唯一可见信号。
// 只有日志的话，用户看到的是「机器越来越卡」而不知道为什么。
func (s *Supervisor) Stats() (live int, max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.engines), maxEngines
}

// Sweep 回收空闲引擎与超出上限的引擎，返回被回收的数量。
//
// 两条规则，都只碰「能安全停掉的」：
//  1. 空闲超过 idleTTL 且没有进程在跑、也不在忙；
//  2. 总数超过 maxEngines 时，按最久未用淘汰到上限为止。
//
// 正在流式回答（Busy）的引擎永不回收——回收它等于把用户正在看的那轮打断。
func (s *Supervisor) Sweep() int {
	stale := s.pickStale()
	if len(stale) == 0 {
		return 0
	}
	// 停进程要在锁外做：Stop 会关 stdin、等退出，可能耗时数秒，
	// 持锁期间整个监督者（包括拉起新引擎）都会卡住。
	for _, e := range stale {
		e.Stop()
	}
	s.mu.Lock()
	for _, e := range stale {
		delete(s.engines, key(e.Kind, e.WorkDir))
	}
	s.mu.Unlock()
	if len(stale) > 0 {
		pkg.Infof("engine: 回收 %d 个空闲引擎", len(stale))
	}
	return len(stale)
}

// pickStale 挑出该回收的引擎。持锁执行，只做判断不做 I/O。
func (s *Supervisor) pickStale() []*Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.engines) == 0 {
		return nil
	}
	// 先按「最久未用」排序，两条规则都从这个顺序上取。
	all := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].lastUseBefore(all[j]) })

	var out []*Engine
	cutoff := time.Now().Add(-idleTTL)
	for _, e := range all {
		if e.lastUse().After(cutoff) {
			break // 后面的都更常用
		}
		if e.recyclable() {
			out = append(out, e)
		}
	}
	// 上限兜底：还在 TTL 内也淘汰，直到数量回到 maxEngines
	if len(s.engines)-len(out) > maxEngines {
		for _, e := range all {
			if len(s.engines)-len(out) <= maxEngines {
				break
			}
			if contains(out, e) {
				continue
			}
			if e.recyclable() {
				out = append(out, e)
			}
		}
	}
	return out
}

func contains(list []*Engine, want *Engine) bool {
	for _, e := range list {
		if e == want {
			return true
		}
	}
	return false
}

// RunRow 是一个在跑的引擎进程。
type RunRow struct {
	Kind      Kind   `json:"kind"`
	WorkDir   string `json:"work_dir"`
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
	Running   bool   `json:"running"`
	// Protocol 生效的协议版本（0 表示还没握手完）
	Protocol int `json:"protocol"`
	// StartedAt 进程拉起的时刻（Unix 毫秒）；没在跑时为 0
	StartedAt int64 `json:"started_at"`
	// CallCount 这个进程处理过的命令数
	CallCount int64 `json:"call_count"`
	// LastError 最近一次命令失败的原文；没失败过为空
	LastError string `json:"last_error,omitempty"`
	// Busy 正在流式回答
	Busy bool `json:"busy"`
}

// Running 返回当前在跑的引擎快照。
func (s *Supervisor) Running() []RunRow {
	s.mu.Lock()
	engines := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		engines = append(engines, e)
	}
	s.mu.Unlock()
	out := make([]RunRow, 0, len(engines))
	for _, e := range engines {
		e.mu.Lock()
		run := RunRow{Kind: e.Kind, WorkDir: e.WorkDir, SessionID: e.session, Busy: e.runID != ""}
		if e.proc != nil {
			run.PID = e.proc.Pid()
			run.Protocol = e.proc.Protocol()
			run.CallCount = e.proc.Calls()
			run.LastError = e.proc.LastError()
			if at := e.proc.StartedAt(); !at.IsZero() {
				run.StartedAt = at.UnixMilli()
			}
			run.Running = run.PID != 0
		}
		e.mu.Unlock()
		out = append(out, run)
	}
	return out
}

// running 快照所有实例。Stop* 与 AnyRunning 都要同一份拷贝。
func (s *Supervisor) running() []*Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Engine, 0, len(s.engines))
	for _, e := range s.engines {
		out = append(out, e)
	}
	return out
}
