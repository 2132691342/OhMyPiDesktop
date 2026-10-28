package engine

// 读循环与分片重组：引擎的 stdout 只有这一个读者。
//
// 为什么独占：stdout 是字节流，两个读者会互相抢走对方要的行。
// 为什么按 \n 切：JSON 字符串里可以有 U+2028 / U+2029，按 Unicode 行分隔切会截断记录。
// 为什么按 id 关联响应：prompt / bash / predict_word 会乱序返回，按到达顺序配会配错。

import (
	"OhMyPiDesktop/backend/pkg"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"
)

func (p *Proc) readLoop() {
	// 1MB 起步：整段工具输出与长会话历史都可能单行很大。
	r := bufio.NewReaderSize(p.stdout, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			p.handleLine(line)
		}
		if err != nil {
			p.shutdown(err)
			return
		}
	}
}

func (p *Proc) drainStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		pkg.Infof("%s[stderr]: %s", p.opts.Dialect.Kind(), sc.Text())
	}
}

func (p *Proc) handleLine(line []byte) {
	line = trimSpace(line)
	if len(line) == 0 {
		return
	}
	// stdout 只该有 JSON；扩展偶尔会直接往 stdout 写字，
	// 静默丢弃而不是报错——把一行日志当协议解析会让后面的帧全部错位。
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		pkg.Infof("%s[stdout]: %s", p.opts.Dialect.Kind(), truncate(string(line), 200))
		return
	}
	if head.Type == "ready" {
		// 只有 omp 会发这一帧；pi 没有握手，等首帧超时。
		var ready Ready
		if json.Unmarshal(line, &ready) == nil {
			p.SetSupported(ready.SupportedProtocolVersions, ready.MaxFrameBytes)
			p.SetReady(ready.ProtocolVersion, ready.MaxFrameBytes)
		}
		p.dispatch(line)
		return
	}
	if head.Type == "rpc_chunk" && p.opts.Dialect.Caps().Fragments {
		if assembled, ok := p.assemble(line); ok {
			p.dispatch(assembled)
		}
		return
	}
	p.dispatch(line)
}

func (p *Proc) dispatch(line []byte) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return
	}
	if head.Type == "response" {
		var resp Response
		if err := json.Unmarshal(line, &resp); err == nil && resp.ID != "" {
			p.pmu.Lock()
			ch, ok := p.pending[resp.ID]
			delete(p.pending, resp.ID)
			p.pmu.Unlock()
			if ok {
				ch <- resp
				return
			}
		}
	}
	// 握手帧由 Dialect 在 Handshake 里消费；这里只做广播。
	// 帧副本必须给出去：读协程的缓冲区会被下一次读取覆盖。
	if p.opts.OnFrame != nil {
		p.opts.OnFrame(Frame{Type: head.Type, Raw: json.RawMessage(append([]byte(nil), line...))})
	}
}

// assemble 处理协议 v2 的超大帧：按 index 拼接，校验 chunkId/count/byteLength。
func (p *Proc) assemble(line []byte) ([]byte, bool) {
	var chunk struct {
		ChunkID    string `json:"chunkId"`
		Index      int    `json:"index"`
		Count      int    `json:"count"`
		ByteLength int    `json:"byteLength"`
		Data       string `json:"data"`
	}
	if err := json.Unmarshal(line, &chunk); err != nil {
		return nil, false
	}
	if chunk.Index == 0 {
		p.chunkID = chunk.ChunkID
		p.chunkBuf = p.chunkBuf[:0]
		p.chunkGot = 0
		p.chunkCnt = chunk.Count
		p.chunkLen = chunk.ByteLength
	}
	if chunk.ChunkID != p.chunkID || chunk.Index != p.chunkGot || chunk.Count != p.chunkCnt {
		pkg.Warnf("%s: 收到交错的分片序列，丢弃 chunkId=%s", p.opts.Dialect.Kind(), chunk.ChunkID)
		p.chunkID = ""
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		p.chunkID = ""
		return nil, false
	}
	p.chunkBuf = append(p.chunkBuf, data...)
	p.chunkGot++
	if p.chunkGot < p.chunkCnt {
		return nil, false
	}
	if len(p.chunkBuf) != p.chunkLen {
		pkg.Warnf("%s: 分片总长不符 expected=%d got=%d", p.opts.Dialect.Kind(), p.chunkLen, len(p.chunkBuf))
		p.chunkID = ""
		return nil, false
	}
	out := append([]byte(nil), p.chunkBuf...)
	p.chunkID = ""
	p.chunkBuf = p.chunkBuf[:0]
	return out, true
}

func (p *Proc) shutdown(cause error) {
	p.closeOnce.Do(func() {
		reason := cause
		if reason == io.EOF || reason == nil {
			reason = ErrClosed
		}
		p.setErr(reason)
		close(p.closed)
		p.pmu.Lock()
		for id, ch := range p.pending {
			close(ch)
			delete(p.pending, id)
		}
		p.pmu.Unlock()
		if p.opts.OnExit != nil {
			p.opts.OnExit(reason)
		}
		pkg.Infof("%s: 进程结束 %v", p.opts.Dialect.Kind(), reason)
	})
}

// Close 优雅关闭：先关 stdin 让引擎自己收尾，超时再杀。
func (p *Proc) Close(grace time.Duration) error {
	select {
	case <-p.closed:
		return nil
	default:
	}
	_ = p.stdin.Close()
	select {
	case <-p.closed:
	case <-time.After(grace):
		p.Kill()
	}
	return nil
}

// Kill 强杀进程树（引擎会拉起子代理与 bash 工具进程）。
func (p *Proc) Kill() {
	if p.cmd.Process != nil {
		killTree(p.cmd)
	}
	_ = p.stdin.Close()
	_ = p.stdout.Close()
}

// Pid 返回进程号。
func (p *Proc) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
