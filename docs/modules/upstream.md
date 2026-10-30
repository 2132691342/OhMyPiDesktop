# 上游探测（服务商）

## 定位

回答一个问题：**「我填的这个服务商底下有哪些模型」**。

它**不做**：不写配置、不管凭据管理、不猜模型能力、不替用户做选择。

存在的原因：模型清单在引擎里是一份运行时事实（按凭据过滤过），
而配一个服务商时用户手上只有地址与密钥。「有哪些模型」这个问题只有上游知道。

## 设计

```
backend/upstream/           ← 能力域，只依赖 pkg
└── models.go    modelsEndpoint（拼地址）/ applyAuth（加头）/ parseModels（归一响应）
```

### 为什么地址要拼而不是照抄

用户填的地址形状不一：

| 用户填的 | 实际要问的 |
|---|---|
| `https://api.example.com` | `…/v1/models` |
| `https://api.example.com/v1` | `…/v1/models` |
| `https://api.example.com/v1/` | `…/v1/models` |
| `https://api.example.com/v1/models` | 原样（别再拼一次） |
| `https://gw.example.com/proxy` | `…/proxy/v1/models` |

照抄一定有一半是错的，而错的形态是 404——用户看不出是自己地址写歪了，
还是这个服务商不支持模型清单。

### 响应形状

兼容三种，因为兼容服务商的形状真的不统一：

```json
{"data": [{"id": "gpt-4o"}]}      // OpenAI / Anthropic
{"data": [{"model": "claude-x"}]}   // 一些兼容服务商的变体
{"models": [{"id": "m1"}]}          // 部分网关
```

解析出来只有两样：**模型 id** 与 **展示名**。上游给的 `owned_by` / `created` /
`permission` 一律丢掉——界面的「拉模型」只关心「有哪些 id、叫什么」，
别的字段（上下文窗口、是否推理）引擎自己知道。

### 空清单是错误不是结果

上游返回空列表时接口报错，而不是交一张空表。
理由：空表会被界面当成「这个服务商没有模型」，
而真实情况是「这个地址没返回模型清单」——后者要换地址或换协议，前者不用。

## 契约

```go
// upstream.FetchModels
func FetchModels(req Request) ([]ModelEntry, error)
```

`Request`：

| 字段 | 说明 |
|---|---|
| `BaseURL` | 服务商地址 |
| `API` | 协议（`openai-completions` / `anthropic-messages` / …） |
| `APIKey` | 密钥；空表示这个服务商靠环境变量或不需要密钥 |
| `Auth` | 认证方式：`api-key`（默认）/ `oauth` / `none` |

`ModelEntry`：

| 字段 | 说明 |
|---|---|
| `ModelID` | 模型标识，写进配置就是它 |
| `ModelName` | 展示名；上游没给就是空 |

## 流程

```
界面「拉模型」/「测一下」
  → service.FetchProviderModels
      → 从已保存的服务商配置里补齐没给的字段（密钥尤其）
      → upstream.FetchModels
          → 拼地址 → 加 Authorization → GET → 解析
  → 界面
```

**密钥为什么必须从配置里取**：界面上拿到的是掩码（`sk-240••••••734f`），
原值只在后端的配置文件里。编辑一个已存在的服务商时界面上根本没有原值，
不从配置里取就必然 401。

## 不变量

- **只读**：这个包不改任何文件。要写配置走 `service.UpsertProvider`。
- **不缓存**：每次问都是真的问上游。缓存的是引擎那份清单（见 `service/modelcfg.go`），
  两者不是一回事：这里问的是「这个地址现在有什么」，那里是「引擎能用哪些」。
- **超时 20 秒**：上游慢时一分钟太长，界面会像卡住了。
- **响应体上限 8 MB**：不设上限的话，一个坏地址能让我们吃下几百 MB。

## 取舍

- **不试多个协议**：接口地址错了就是错了，挨个试一遍会让一次点击等好几秒。
  协议由用户选，选错了换个选就是。
- **不发 `owned_by` 之类的字段回界面**：那些字段没有界面在用，
  传过去只是让契约多出一堆没人读的键。
