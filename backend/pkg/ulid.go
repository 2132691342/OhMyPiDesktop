package pkg

// ULID 是业务标识符的生成与解析。
//
// 两种 ID 的取舍：UUID 无序（做主键会让索引碎片化），自增要中心发号。
// ULID 前 48 位是毫秒时间、后 80 位是随机，字典序即时间序，且不需要协调。
//
// 带前缀是给「这一个 ID 属于什么」一个可回答的形式：日志里看到
// RUN_01J... 就知道它是一轮对话，不必回去翻生成它的那行代码。

import (
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID 前缀。同一类实体共用一个前缀，新增一类就在这里加一行。
const (
	PrefixRun     = "RUN"     // 一轮对话
	PrefixSession = "SESSION" // 会话（界面侧引用引擎会话时）
)

// ulidMu 串行化同一毫秒内的生成：ulid 的单调性靠上一次的值，
// 并发调用会各自拿到独立的随机段，顺序就丢了。
var ulidMu sync.Mutex

// 单调源：同一毫秒内递增而不是重新随机，保证同一进程内严格有序。
var ulidEntropy = ulid.Monotonic(rand.Reader, 0)

// NewULID 生成一个带业务前缀的标识符，形如 RUN_01J...。
//
// prefix 为空时返回裸 ULID——只在「这类实体没有前缀」这种不该发生的情况下出现，
// 留着而不是 panic：ID 生成不该成为让应用起不来的理由。
func NewULID(prefix string) string {
	ulidMu.Lock()
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	ulidMu.Unlock()
	if err != nil {
		// rand.Reader 失败意味着系统熵源不可用，退回时间的十六进制表示。
		return joinPrefix(prefix, time.Now().Format("20060102150405.000"))
	}
	if prefix == "" {
		return id.String()
	}
	return prefix + "_" + id.String()
}

// NewULIDMonotonic 生成带前缀且**同一进程内严格递增**的标识符。
//
// 同一毫秒内连续生成时用这个：NewULID 在同一毫秒里是随机段，
// 而「一轮对话的 ID」这类要按生成顺序排队的场景需要单调。
func NewULIDMonotonic(prefix string) string {
	ulidMu.Lock()
	id, err := ulid.New(ulid.Now(), ulidEntropy)
	ulidMu.Unlock()
	if err != nil {
		return joinPrefix(prefix, time.Now().Format("20060102150405.000"))
	}
	return joinPrefix(prefix, id.String())
}

func joinPrefix(prefix, body string) string {
	if prefix == "" {
		return body
	}
	return prefix + "_" + body
}

// ULIDPrefix 取一个标识符的前缀；没有前缀时返回空串。
func ULIDPrefix(id string) string {
	if i := strings.IndexByte(id, '_'); i > 0 {
		return id[:i]
	}
	return ""
}
