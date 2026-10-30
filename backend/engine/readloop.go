package engine

// 读循环与分片重组：引擎的 stdout 只有这一个读者（字节流，两个读者会互相抢走对方要的行）。
// 按 \n 切：JSON 字符串里可以有 U+2028 / U+2029，按 Unicode 行分隔切会截断记录。
// 按 id 关联响应：prompt / bash / predict_word 会乱序返回，按到达顺序配会配错。

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

// maxChunkBytes 是分片重组的总长上限（64 MB）。
// byteLength 是引擎声明的值，而分片数据是一条条拼起来的：跑飞或恶意的引擎声明 10GB，缓冲就会涨到 OOM。
// 正常的大帧是几 MB 的文件内容或工具输出，64 MB 已经很宽松。
const maxChunkBytes = 64 << 20

// assemble 处理协议 v2 的超大帧：按 index 拼接，校验 chunkId/count/byteLength。
// 只在读协程里调用（分片状态不额外加锁：这一层本来就只有单一消费者）；resetChunks 由 Kill 调用，那时读协程已经停了。
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
	// 声明的总长超限就整段丢弃：继续拼下去只会把内存吃掉，
	// 而且拼完也永远对不上校验。
	if chunk.ByteLength < 0 || chunk.ByteLength > maxChunkBytes {
		pkg.Warnf("%s: 拒绝一个声明长度为 %d 的分片序列", p.opts.Dialect.Kind(), chunk.ByteLength)
		p.resetChunks()
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
		p.resetChunks()
		return nil, false
	}
	// 累计长度也要实时卡：单帧合法但拼起来超限时，同样不能让它越长越大。
	// base64 是 4/3 膨胀，所以按解码前长度先估一道，避免无谓的分配。
	if len(p.chunkBuf)+len(chunk.Data)*3/4 > maxChunkBytes {
		pkg.Warnf("%s: 分片总量超过 %d 字节上限，丢弃", p.opts.Dialect.Kind(), maxChunkBytes)
		p.resetChunks()
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		p.resetChunks()
		return nil, false
	}
	p.chunkBuf = append(p.chunkBuf, data...)
	p.chunkGot++
	if p.chunkGot < p.chunkCnt {
		return nil, false
	}
	if len(p.chunkBuf) != p.chunkLen {
		pkg.Warnf("%s: 分片总长不符 expected=%d got=%d", p.opts.Dialect.Kind(), p.chunkLen, len(p.chunkBuf))
		p.resetChunks()
		return nil, false
	}
	out := append([]byte(nil), p.chunkBuf...)
	p.resetChunks()
	return out, true
}

// resetChunks 清掉分片状态并交还缓冲区，把 capacity 也一起放掉：
// 一个 64 MB 的缓冲留在 Proc 上等于永久占着内存，而下一个大帧可能几个小时后才来。
func (p *Proc) resetChunks() {
	p.chunkID = ""
	p.chunkGot = 0
	p.chunkCnt = 0
	p.chunkLen = 0
	p.chunkBuf = nil
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
