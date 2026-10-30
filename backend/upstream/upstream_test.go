package upstream

// 向上游服务商问模型清单：只测拼地址与解析这两种会静默出错的事。
//
// 拼错地址的表现是「点了没反应」；解析错的表现是「清单是空的」——
// 而空清单会被界面当成「这个服务商没有模型」。

import (
	"testing"
)

// 拼地址的两种常见形状都要覆盖：
// 用户填的地址有的带 /v1，有的只到域名，而模型清单的位置不一样。
func TestModelsEndpoint(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"裸域名补 v1", "https://api.example.com", "https://api.example.com/v1/models"},
		{"已有 v1", "https://api.example.com/v1", "https://api.example.com/v1/models"},
		{"末尾斜杠", "https://api.example.com/v1/", "https://api.example.com/v1/models"},
		{"已经是 models", "https://api.example.com/v1/models", "https://api.example.com/v1/models"},
		{"带子路径", "https://gw.example.com/proxy", "https://gw.example.com/proxy/v1/models"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := modelsEndpoint(c.base)
			if err != nil {
				t.Fatalf("拼地址失败：%v", err)
			}
			if got != c.want {
				t.Fatalf("拼出 %q，期望 %q", got, c.want)
			}
		})
	}
}

// 响应形状有三种兼容写法，都要能挑出模型 id；空清单要报错而不是交一张空表。
func TestParseModels(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"openai 形状", `{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`, []string{"gpt-4o", "gpt-4o-mini"}},
		{"model 字段变体", `{"data":[{"model":"claude-x"}]}`, []string{"claude-x"}},
		{"models 顶层", `{"models":[{"id":"m1"}]}`, []string{"m1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entries, err := parseModels(c.body)
			if err != nil {
				t.Fatalf("解析失败：%v", err)
			}
			if len(entries) != len(c.want) {
				t.Fatalf("解析出 %d 条，期望 %d 条", len(entries), len(c.want))
			}
			for i, id := range c.want {
				if entries[i].ModelID != id {
					t.Fatalf("第 %d 条是 %q，期望 %q", i, entries[i].ModelID, id)
				}
			}
		})
	}

	t.Run("空清单要报错", func(t *testing.T) {
		if _, err := parseModels(`{"data":[]}`); err == nil {
			t.Fatal("空清单应该报错")
		}
	})
}
