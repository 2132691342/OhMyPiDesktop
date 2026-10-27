package engine

import "testing"

// 参数名写错是**静默失败**：引擎把它当成缺参，回 success:false 或者干脆不动，
// 界面上就是「点了没反应」。所以对着引擎的命令表钉死。
//
// 期望值是引擎命令表里逐字抄来的 params 声明。
func TestRPCParamNames(t *testing.T) {
	cases := []struct {
		cmd    string
		params map[string]any
		want   []string
		ban    []string
	}{
		{"steer_subagent", SteerSubagentParams("a", "m"), []string{"subagentId", "message"}, []string{"id"}},
		{"cancel_subagent", CancelSubagentParams("a"), []string{"subagentId"}, []string{"id"}},
		{"get_subagent_messages", SubagentMessagesParams("a", 7), []string{"subagentId", "fromByte"}, []string{"id", "from"}},
		{"login", LoginParams("anthropic"), []string{"providerId"}, []string{"provider"}},
	}
	for _, c := range cases {
		for _, k := range c.want {
			if _, ok := c.params[k]; !ok {
				t.Errorf("%s 缺参数 %q（实际 %v）", c.cmd, k, sortedKeys(c.params))
			}
		}
		for _, k := range c.ban {
			if _, bad := c.params[k]; bad {
				t.Errorf("%s 不该用参数名 %q", c.cmd, k)
			}
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
