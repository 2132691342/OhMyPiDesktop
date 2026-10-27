package engine

import (
	"context"
	"testing"
)

// 能力表与命令表必须一一对得上。
//
// 这类错误不会编译失败、不会报错：发出去的一条引擎不认识的命令，
// 引擎只回 success:false 或者干脆静默丢弃，界面表现为「点了没反应」。
// 所以这里把「哪条命令属于谁」钉死，改 Caps 或改命令名时必须一起改。

// 引擎命令表里的真实条目（逐字抄自各引擎的协议定义）。
// 只列我们真的会发的那些——没发的命令不需要在这里。
var engineCommands = map[Kind]map[string]bool{
	KindOMP: {
		"prompt": true, "steer": true, "follow_up": true, "abort": true,
		"new_session": true, "switch_session": true, "fork": true,
		"get_state": true, "get_session_stats": true, "get_messages": true,
		"set_model": true, "get_available_models": true,
		"set_thinking_level": true, "get_available_thinking_levels": true,
		"compact": true, "set_auto_compaction": true, "set_session_name": true,
		"negotiate_protocol": true,
		// 子代理
		"set_subagent_subscription": true, "get_subagents": true,
		"get_subagent_messages": true, "cancel_subagent": true, "steer_subagent": true,
		// 队列：只有逐条移除，没有 clear_queue
		"remove_queued_message": true,
		// 登录
		"get_login_providers": true, "login": true,
	},
	KindPi: {
		"prompt": true, "steer": true, "follow_up": true, "abort": true,
		"new_session": true, "switch_session": true, "fork": true,
		"get_state": true, "get_session_stats": true, "get_messages": true,
		"set_model": true, "get_available_models": true,
		"set_thinking_level": true, "get_available_thinking_levels": true,
		"compact": true, "set_auto_compaction": true, "set_session_name": true,
		"get_commands": true,
		// 队列：只有一次性清空，没有逐条移除
		"clear_queue": true,
	},
}

// 能力开关 ↔ 依赖的命令。双向断言：开关为真则命令必须存在，
// 开关为假则命令必须不存在（否则就有一处能力被漏声明，界面会永远藏着入口）。
var capsCommands = map[string]string{
	"QueueEdit":  "remove_queued_message",
	"QueueClear": "clear_queue",
}

// TestCapsMatchCommandTable 守住「能力声明 ↔ 引擎真实命令表」的一致性。
func TestCapsMatchCommandTable(t *testing.T) {
	for kind, cmds := range engineCommands {
		caps := CapsFor(kind)
		for field, cmd := range capsCommands {
			on := capOn(caps, field)
			_, exists := cmds[cmd]
			if on && !exists {
				t.Errorf("%s: Caps.%s 为真，但命令表里没有 %s", kind, field, cmd)
			}
			if !on && exists {
				t.Errorf("%s: Caps.%s 为假，但命令表里明明有 %s", kind, field, cmd)
			}
		}
	}
}

// TestQueueOpsRespectCaps 守住运行时门控：能力为假的引擎必须拒绝该操作，
// 而不是发出一条注定失败的命令。
func TestQueueOpsRespectCaps(t *testing.T) {
	omp := &Engine{Kind: KindOMP, dialect: fakeDialect{caps: CapsFor(KindOMP)}}
	pi := &Engine{Kind: KindPi, dialect: fakeDialect{caps: CapsFor(KindPi)}}

	if err := omp.ClearQueue(nil); err == nil {
		t.Error("omp 没有 clear_queue，ClearQueue 必须拒绝")
	}
	if _, err := pi.RemoveQueued(nil, "m", "steering"); err == nil {
		t.Error("pi 没有 remove_queued_message，RemoveQueued 必须拒绝")
	}
	// 放行一侧只能断言能力位为真：fakeDialect 不启动进程，发不出命令。
	if !pi.Caps().QueueClear {
		t.Error("pi 有 clear_queue，Caps.QueueClear 必须为真")
	}
	if !omp.Caps().QueueEdit {
		t.Error("omp 有 remove_queued_message，Caps.QueueEdit 必须为真")
	}
}

func capOn(c Caps, field string) bool {
	switch field {
	case "QueueEdit":
		return c.QueueEdit
	case "QueueClear":
		return c.QueueClear
	}
	return false
}

// fakeDialect 只用于回答能力表；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }
