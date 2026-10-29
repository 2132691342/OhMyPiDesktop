package engine

// 退出路径上的快速停止：与 StopAll 的差别只有超时策略。
//
// 为什么单独一个文件：退出与回收是两套完全不同的取舍
// （前者要“一秒内关掉”，后者要“别打断用户正在看的那一轮”），
// 放在同一个文件里迟早会有人顺手把退出那套逻辑用到回收上。

import (
	"sync"
	"time"
)

// StopAllQuick 快速停止全部实例，给退出用。
//
// 与 StopAll 的差别只有超时策略：并发停，共用一个预算，
// 不单等每个进程的 5 秒优雅期。退出路径上“快”比“优雅”重要——
// 用户已经表达“我不想再用了”，而引擎没有未落盘的状态需要保护。
func (s *Supervisor) StopAllQuick(budget time.Duration) {
	all := s.running()
	if len(all) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, e := range all {
		wg.Add(1)
		go func(e *Engine) {
			defer wg.Done()
			e.stopQuick(budget)
		}(e)
	}
	wg.Wait()
	s.mu.Lock()
	for _, e := range all {
		delete(s.engines, key(e.Kind, e.WorkDir))
	}
	s.mu.Unlock()
}

// stopQuick 停止一个实例，不等它慢慢收尾。
//
// 先关 stdin（正常情况引擎会立即退出），超过一半预算就直接终止进程树。
func (e *Engine) stopQuick(budget time.Duration) {
	e.AbortRun("引擎已停止")
	e.mu.Lock()
	p := e.proc
	e.proc = nil
	e.runID = ""
	e.mu.Unlock()
	if p == nil {
		return
	}
	grace := budget / 2
	if grace <= 0 {
		grace = time.Second
	}
	_ = p.Close(grace)
}
