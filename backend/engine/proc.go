package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// ErrClosed 表示进程已经结束；之后的调用都返回它。
var ErrClosed = errors.New("engine: 进程已结束")

// CallError 是引擎回了 success:false 的命令失败。
//
// pi 的回包只有 error 字符串没有 code，所以 Code 可空；
// 界面只用 Message，Code 留给日志。
type CallError struct {
	Command string
	Message string
	Code    string
}

func (e *CallError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s 失败（%s）：%s", e.Command, e.Code, e.Message)
	}
	return fmt.Sprintf("%s 失败：%s", e.Command, e.Message)
}

// Response 是命令响应信封。两个引擎同构：{id, type:"response", command, success, error, data}。
type Response struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Code    string          `json:"code"`
	Data    json.RawMessage `json:"data"`
}

// Options 是拉起一个引擎进程的参数。
type Options struct {
	Launch   Launch
	Dialect  Dialect
	OnFrame  func(Frame) // 读协程逐帧回调，禁止阻塞
	OnExit   func(error) // 进程结束时回调一次
	StartMax time.Duration
}

// Proc 是一个引擎子进程：进程 + 传输 + 请求关联。
//
// 并发模型：一个读协程独占 stdout 并按 id 唤醒等待者；写路径用互斥锁串行化，
// 因为协议是行分隔 JSON，半行写入会让引擎吞掉后续命令。
type Proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	opts Options

	wmu     sync.Mutex
	seq     int
	pending map[string]chan Response
	pmu     sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
	cerrMu    sync.Mutex

	readyOnce sync.Once
	readyCh   chan struct{}
	protocol  int
	maxFrame  int
	supported []int

	// 协议 v2 分片重组：同一时刻只允许一个 chunk 序列在途。
	chunkID  string
	chunkBuf []byte
	chunkGot int
	chunkCnt int
	chunkLen int
}

// Start 拉起进程并走完该引擎的握手。
func Start(ctx context.Context, opts Options) (*Proc, error) {
	if opts.Launch.Exe == "" {
		return nil, pkg.New(pkg.CodeEngineDown, "没有找到 "+string(opts.Dialect.Kind())+" 可执行文件", "")
	}
	if opts.StartMax <= 0 {
		opts.StartMax = 45 * time.Second
	}
	launch := opts.Launch
	args := opts.Dialect.Args(launch.WorkDir, launch.ExtraArgs)
	cmd := exec.Command(launch.Exe, args...)
	cmd.Dir = launch.WorkDir
	cmd.Env = append(environ(), launch.Env...)
	hideWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "无法接管引擎标准输入", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "无法接管引擎标准输出", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "无法接管引擎错误输出", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, pkg.Wrap(pkg.CodeEngineDown, "启动 "+string(opts.Dialect.Kind())+" 失败", err)
	}

	p := &Proc{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		opts:    opts,
		pending: make(map[string]chan Response),
		closed:  make(chan struct{}),
		readyCh: make(chan struct{}),
	}
	go p.drainStderr(stderr)
	go p.readLoop()

	if err := opts.Dialect.Handshake(ctx, p); err != nil {
		p.Kill()
		return nil, err
	}
	pkg.Infof("%s: 进程就绪 pid=%d 目录=%s 协议=v%d",
		opts.Dialect.Kind(), cmd.Process.Pid, launch.WorkDir, p.Protocol())
	return p, nil
}

// Kind 返回引擎种类。
func (p *Proc) Kind() Kind { return p.opts.Dialect.Kind() }

// Caps 返回能力表。
func (p *Proc) Caps() Caps { return p.opts.Dialect.Caps() }

// Protocol 返回当前生效的协议版本。
func (p *Proc) Protocol() int {
	p.cerrMu.Lock()
	defer p.cerrMu.Unlock()
	return p.protocol
}

// MaxFrame 返回引擎声明的单帧上限（未知为 0）。
func (p *Proc) MaxFrame() int {
	p.cerrMu.Lock()
	defer p.cerrMu.Unlock()
	return p.maxFrame
}

// SetReady 由握手阶段写入协议版本与帧上限，并放行等待者。
func (p *Proc) SetReady(protocol, maxFrame int) {
	p.cerrMu.Lock()
	p.protocol = protocol
	p.maxFrame = maxFrame
	p.cerrMu.Unlock()
	p.readyOnce.Do(func() { close(p.readyCh) })
}

// SupportsV2 判断引擎是否广播了协议 v2（omp 独有）。
func (p *Proc) SupportsV2() bool {
	p.cerrMu.Lock()
	defer p.cerrMu.Unlock()
	for _, v := range p.supported {
		if v >= 2 {
			return true
		}
	}
	return false
}

// SetSupported 记录握手帧里声明的协议版本表。
func (p *Proc) SetSupported(versions []int, maxFrame int) {
	p.cerrMu.Lock()
	p.supported = versions
	p.maxFrame = maxFrame
	p.cerrMu.Unlock()
}

// Ready 返回「握手已完成的信号通道」。
//
// 握手方在收到 ready 帧时调用 SetReady；需要等它就绪的调用方读这个通道。
func (p *Proc) Ready() <-chan struct{} { return p.readyCh }

// Err 返回进程结束原因。
func (p *Proc) Err() error {
	p.cerrMu.Lock()
	defer p.cerrMu.Unlock()
	return p.closeErr
}

func (p *Proc) setErr(err error) {
	p.cerrMu.Lock()
	if p.closeErr == nil {
		p.closeErr = err
	}
	p.cerrMu.Unlock()
}

// Closed 返回进程结束的信号通道。
func (p *Proc) Closed() <-chan struct{} { return p.closed }

// Call 发一条命令并等它的响应，把 data 解到 out。
//
// timeout <= 0 时取 60 秒。
func (p *Proc) Call(ctx context.Context, command string, params any, out any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}
	id := p.nextID()
	ch := make(chan Response, 1)
	p.pmu.Lock()
	p.pending[id] = ch
	p.pmu.Unlock()
	defer func() {
		p.pmu.Lock()
		delete(p.pending, id)
		p.pmu.Unlock()
	}()

	body := map[string]any{"id": id, "type": command}
	for k, v := range asMap(params) {
		body[k] = v
	}
	line, err := json.Marshal(body)
	if err != nil {
		return pkg.Wrap(pkg.CodeProtocol, "命令编码失败", err)
	}
	if err := p.writeLine(line); err != nil {
		return err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp, ok := <-ch:
		if !ok {
			return ErrClosed
		}
		if !resp.Success {
			return &CallError{Command: command, Message: resp.Error, Code: resp.Code}
		}
		if out != nil && len(resp.Data) > 0 {
			if err := json.Unmarshal(resp.Data, out); err != nil {
				return pkg.Wrap(pkg.CodeProtocol, command+" 的返回无法解析", err)
			}
		}
		return nil
	case <-timer.C:
		return pkg.New(pkg.CodeEngineTimeout, command+" 超时", fmt.Sprintf("%s 内没有响应", timeout))
	case <-p.closed:
		return ErrClosed
	case <-ctx.Done():
		return pkg.Wrap(pkg.CodeEngineDown, command+" 被取消", ctx.Err())
	}
}

// Send 发一条无需响应的帧（界面应答）。
func (p *Proc) Send(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return pkg.Wrap(pkg.CodeProtocol, "帧编码失败", err)
	}
	return p.writeLine(line)
}

func (p *Proc) writeLine(line []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if _, err := p.stdin.Write(append(line, '\n')); err != nil {
		return pkg.Wrap(pkg.CodeEngineDown, "写入引擎失败", err)
	}
	return nil
}

func (p *Proc) nextID() string {
	p.wmu.Lock()
	p.seq++
	n := p.seq
	p.wmu.Unlock()
	return "h" + strconv.Itoa(n)
}

func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
