package engine

import (
	"context"
	"os"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// Client 取出已就绪的进程，必要时拉起。
//
// 并发拉起的语义：第二个调用方等第一个的结论，而不是自己也去拉一个进程。
func (e *Engine) Client(ctx context.Context) (*Proc, error) {
	e.touch()
	e.mu.Lock()
	if e.proc != nil {
		select {
		case <-e.proc.Closed():
			e.proc = nil
		default:
			p := e.proc
			e.mu.Unlock()
			return p, nil
		}
	}
	if e.booting {
		e.mu.Unlock()
		return e.waitBoot(ctx)
	}
	e.booting = true
	e.mu.Unlock()

	proc, err := e.boot(ctx)

	e.mu.Lock()
	e.booting = false
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	e.proc = proc
	kind, dir := e.Kind, e.WorkDir
	e.mu.Unlock()

	e.send("", "engine:ready", map[string]any{
		"kind": kind, "work_dir": dir, "pid": proc.Pid(), "protocol": proc.Protocol(),
	})
	return proc, nil
}

// waitBoot 等别人拉起的进程：自己再拉一个会留下两个进程抢同一个 cwd。
func (e *Engine) waitBoot(ctx context.Context) (*Proc, error) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, pkg.Wrap(pkg.CodeEngineDown, "等待引擎启动被取消", ctx.Err())
		case <-time.After(80 * time.Millisecond):
		}
		e.mu.Lock()
		if e.proc != nil {
			p := e.proc
			e.mu.Unlock()
			return p, nil
		}
		booting := e.booting
		e.mu.Unlock()
		if !booting {
			break
		}
	}
	return nil, pkg.New(pkg.CodeEngineDown, "引擎正在启动，请稍后再试", "")
}

// boot 拉起进程。工作目录先建好：cwd 不存在时进程会立刻退出。
func (e *Engine) boot(ctx context.Context) (*Proc, error) {
	exe, extra, env := e.sup.resolve(e.Kind)
	if exe == "" {
		return nil, pkg.New(pkg.CodeEngineUnsupported, "没有找到 "+string(e.Kind)+" 引擎", "")
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return nil, pkg.New(pkg.CodeEngineDown, "引擎文件不存在："+exe, "")
	}
	if err := os.MkdirAll(e.WorkDir, 0o755); err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "工作目录不可用", err)
	}
	kind, dir := e.Kind, e.WorkDir
	proc, err := Start(ctx, Options{
		Launch:  Launch{Exe: exe, WorkDir: e.WorkDir, ExtraArgs: extra, Env: env},
		Dialect: e.dialect,
		OnFrame: e.onFrame,
		OnExit: func(err error) {
			e.mu.Lock()
			e.proc = nil
			e.runID = ""
			e.mu.Unlock()
			e.send("", "engine:exit", map[string]any{
				"kind": kind, "work_dir": dir, "reason": errText(err),
			})
		},
	})
	if err != nil {
		return nil, err
	}
	return proc, nil
}

// Stop 关闭引擎进程。
func (e *Engine) Stop() {
	e.mu.Lock()
	// 停掉之后也算「刚用过」：StopKind 之后的实例已被摘除，
	// 而手动停止的场景（重启引擎）紧接着就会重新拉起，不该被回收抢先。
	e.lastUsed = time.Now()
	p := e.proc
	e.proc = nil
	e.runID = ""
	e.mu.Unlock()
	if p != nil {
		_ = p.Close(5 * time.Second)
	}
}

// Restart 重启引擎（换模型 / 装完插件 / 换引擎版本后）。
func (e *Engine) Restart(ctx context.Context) error {
	e.Stop()
	_, err := e.Client(ctx)
	return err
}

// Call 在已就绪的进程上发一条命令。
func (e *Engine) Call(ctx context.Context, command string, params any, out any, timeout time.Duration) error {
	e.touch()
	p, err := e.Client(ctx)
	if err != nil {
		return err
	}
	return p.Call(ctx, command, params, out, timeout)
}
