package engine

import "encoding/json"

// 两引擎共有的 RPC 视图类型。
//
// 字段名一律取引擎原始形态（camelCase）：协议是引擎的私有契约，改名就等于
// 自造第二套 schema，引擎一升级就对不上。界面拿到的字段名由 ChatState 之类
// 的投影类型决定，不在这里翻译。

// Model 是引擎报上来的模型描述（只取界面要显示的字段）。
type Model struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	API           string     `json:"api"`
	Provider      string     `json:"provider"`
	BaseURL       string     `json:"baseUrl,omitempty"`
	Reasoning     bool       `json:"reasoning"`
	Input         []string   `json:"input,omitempty"`
	ContextWindow int        `json:"contextWindow"`
	MaxTokens     int        `json:"maxTokens"`
	Cost          *ModelCost `json:"cost,omitempty"`
	Thinking      *struct {
		Mode    string   `json:"mode"`
		Efforts []string `json:"efforts"`
		Default string   `json:"defaultLevel,omitempty"`
	} `json:"thinking,omitempty"`
}

// PromptPayload 是一轮对话要发给引擎的全部内容。
//
// 两个引擎的 prompt 命令形状相同：message 是正文，images 是图片数组，
// 每张图片是 {type,data,mimeType}。图片只能走这条路——没有别的字段能传二进制。
type PromptPayload struct {
	// Message 正文（@ 引用的文件清单也拼在这里）
	Message string `json:"message"`
	// Images 图片附件；为空时整个字段不发，避免引擎收到一个空数组
	Images []ImageContent `json:"images,omitempty"`
}

// ImageContent 是一张图。type 恒为 image，写在构造处而不是字段上。
type ImageContent struct {
	// Type 固定为 "image"，两个引擎的判别字段
	Type string `json:"type"`
	// Data base64 编码的图片字节（不含 data: 前缀）
	Data string `json:"data"`
	// MimeType 如 image/png / image/jpeg
	MimeType string `json:"mimeType"`
}

// NewImage 造一张图。是否该发由调用方判断（模型的 input 里有没有 image）。
func NewImage(mimeType, data string) ImageContent {
	return ImageContent{Type: "image", Data: data, MimeType: mimeType}
}

// ModelCost 是每百万 token 的价格。
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// ContextUsage 是上下文水位。两个引擎都提供，来源不同：
// omp 在 get_state.contextUsage，pi 在 get_session_stats.contextUsage。
type ContextUsage struct {
	Tokens        *int     `json:"tokens"`
	ContextWindow int      `json:"contextWindow"`
	Percent       *float64 `json:"percent"`
}

// SessionState 是 get_state 的视图面（只解界面用得到的键）。
//
// 两家共有的键都收在这里；omp 独有的键（tokensPerSecond / queuedMessages /
// contextUsage / todoPhases）由 omp 侧用私有结构再取一次。
type SessionState struct {
	Model           *Model            `json:"model"`
	ThinkingLevel   string            `json:"thinkingLevel"`
	IsStreaming     bool              `json:"isStreaming"`
	IsCompacting    bool              `json:"isCompacting"`
	IsSettled       bool              `json:"isSettled"`
	SessionID       string            `json:"sessionId"`
	SessionFile     string            `json:"sessionFile"`
	SessionName     string            `json:"sessionName"`
	MessageCount    int               `json:"messageCount"`
	PendingCount    int               `json:"pendingMessageCount"`
	AutoCompaction  bool              `json:"autoCompactionEnabled"`
	SteeringMode    string            `json:"steeringMode"`
	FollowUpMode    string            `json:"followUpMode"`
	ContextUsage    *ContextUsage     `json:"contextUsage"`
	TokensPerSecond *float64          `json:"tokensPerSecond"`
	Queued          *QueuedMessages   `json:"queuedMessages"`
	TodoPhases      []json.RawMessage `json:"todoPhases"`
}

// QueuedMessages 是排队消息快照。
type QueuedMessages struct {
	Steering    []string `json:"steering"`
	FollowUp    []string `json:"followUp"`
	LiveSteered int      `json:"liveSteered"`
}

// SessionStats 是会话累计用量。两引擎字段一致，
// 唯一差别是 omp 多 reasoning 与 premiumRequests。
type SessionStats struct {
	SessionID         string        `json:"sessionId"`
	UserMessages      int           `json:"userMessages"`
	AssistantMessages int           `json:"assistantMessages"`
	ToolCalls         int           `json:"toolCalls"`
	ToolResults       int           `json:"toolResults"`
	TotalMessages     int           `json:"totalMessages"`
	Tokens            TokenCounts   `json:"tokens"`
	Cost              float64       `json:"cost"`
	PremiumRequests   int           `json:"premiumRequests"`
	ContextUsage      *ContextUsage `json:"contextUsage"`
}

// TokenCounts 是 token 分段。
type TokenCounts struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	Reasoning  int `json:"reasoning"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
	Total      int `json:"total"`
}

// Usage 是单条助手消息的用量（含花费）。
type Usage struct {
	Input       int        `json:"input"`
	Output      int        `json:"output"`
	CacheRead   int        `json:"cacheRead"`
	CacheWrite  int        `json:"cacheWrite"`
	TotalTokens int        `json:"totalTokens"`
	Cost        *UsageCost `json:"cost,omitempty"`
}

// UsageCost 是单条消息的花费分段。
type UsageCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// ContentBlock 是消息内容块。type 决定哪个字段有效。
type ContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	Signature  string          `json:"thinkingSignature,omitempty"`
	Summary    bool            `json:"summary,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	PartialArg string          `json:"partialArgs,omitempty"`
	Intent     string          `json:"intent,omitempty"`
	Data       string          `json:"data,omitempty"`
	MimeType   string          `json:"mimeType,omitempty"`
	ImageURL   string          `json:"url,omitempty"`
}

// Message 是会话里的单条消息。
type Message struct {
	Role        string         `json:"role"`
	Content     []ContentBlock `json:"content"`
	Provider    string         `json:"provider,omitempty"`
	Model       string         `json:"model,omitempty"`
	Usage       *Usage         `json:"usage,omitempty"`
	StopReason  string         `json:"stopReason,omitempty"`
	Timestamp   int64          `json:"timestamp,omitempty"`
	ToolCallID  string         `json:"toolCallId,omitempty"`
	ToolName    string         `json:"toolName,omitempty"`
	IsError     bool           `json:"isError,omitempty"`
	Attribution string         `json:"attribution,omitempty"`
	// Details 工具结果的结构化细节；改文件的工具把差异放在这里
	Details json.RawMessage `json:"details,omitempty"`
	// DurationMs 工具执行耗时（毫秒）
	DurationMs int64 `json:"durationMs,omitempty"`
}

// SlashCommand 是引擎报上来的可用命令。
//
// omp 的 source 是 builtin/extension/skill，pi 是 extension/prompt/skill，
// 且 pi 的技能命令带 `skill:` 前缀——两者都原样透出，界面按前缀染色。
type SlashCommand struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Input       *struct {
		Hint string `json:"hint,omitempty"`
	} `json:"input,omitempty"`
	Subcommands []struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Usage       string `json:"usage,omitempty"`
	} `json:"subcommands,omitempty"`
}

// UIRequest 是扩展要界面配合的请求（提问 / 通知 / 状态条 / 小部件）。
//
// 两引擎的扩展 API 同源，方法名一致；omp 多的 open_url 与 ask 由 Cap 决定是否出现。
type UIRequest struct {
	ID            string   `json:"id"`
	Method        string   `json:"method"`
	Title         string   `json:"title,omitempty"`
	Message       string   `json:"message,omitempty"`
	Placeholder   string   `json:"placeholder,omitempty"`
	Prefill       string   `json:"prefill,omitempty"`
	Options       []string `json:"options,omitempty"`
	OptionDetails []struct {
		Description string `json:"description,omitempty"`
	} `json:"optionDetails,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	TargetID  string `json:"targetId,omitempty"`
	Questions []struct {
		ID          string `json:"id"`
		Question    string `json:"question"`
		Header      string `json:"header,omitempty"`
		Multi       bool   `json:"multi,omitempty"`
		Recommended *int   `json:"recommended,omitempty"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description,omitempty"`
			Preview     string `json:"preview,omitempty"`
		} `json:"options"`
	} `json:"questions,omitempty"`
	// 展示类方法
	NotifyType      string   `json:"notifyType,omitempty"`
	StatusKey       string   `json:"statusKey,omitempty"`
	StatusText      *string  `json:"statusText,omitempty"`
	WidgetKey       string   `json:"widgetKey,omitempty"`
	WidgetLines     []string `json:"widgetLines,omitempty"`
	WidgetPlacement string   `json:"widgetPlacement,omitempty"`
	URL             string   `json:"url,omitempty"`
	LaunchURL       string   `json:"launchUrl,omitempty"`
	Instructions    string   `json:"instructions,omitempty"`
	Text            string   `json:"text,omitempty"`
}

// UIResponse 是回写给引擎的界面应答。
type UIResponse struct {
	Type      string     `json:"type"`
	ID        string     `json:"id"`
	Value     *string    `json:"value,omitempty"`
	Confirmed *bool      `json:"confirmed,omitempty"`
	Cancelled bool       `json:"cancelled,omitempty"`
	TimedOut  bool       `json:"timedOut,omitempty"`
	Answers   []UIAnswer `json:"answers,omitempty"`
}

// UIAnswer 是 ask 工具单题的答案。
type UIAnswer struct {
	ID              string   `json:"id"`
	SelectedOptions []string `json:"selectedOptions"`
	CustomInput     string   `json:"customInput,omitempty"`
}

// LoginProvider 是可登录的服务商（仅 omp 有 RPC 面）。
type LoginProvider struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Available     bool   `json:"available"`
	Authenticated bool   `json:"authenticated"`
}

// Ready 是 omp 的握手首帧。pi 没有这一帧。
type Ready struct {
	Type                      string `json:"type"`
	ProtocolVersion           int    `json:"protocolVersion"`
	SupportedProtocolVersions []int  `json:"supportedProtocolVersions"`
	MaxFrameBytes             int    `json:"maxFrameBytes"`
	MaxReassembledFrameBytes  int    `json:"maxReassembledFrameBytes"`
}
