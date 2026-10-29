// Package domain 放跨层的领域模型：一个聚合根一个文件。
package domain

// 设置键。集中在这里，避免字符串散落。
const (
	KeyTheme     = "ui.theme"      // light / dark / system
	KeyLang      = "ui.lang"       // zh-CN / en
	KeyWorkspace = "app.workspace" // 上次用的工作目录
	// KeyRecentWorkspaces 是最近用过的工作目录列表（JSON 数组，最多 RecentWorkspaceMax 条）。
	// 单独一个键而不是每次从会话反推：会话文件的扫描要开几百个文件头，
	// 而「最近在哪些目录干过活」是界面自己的记忆。
	KeyRecentWorkspaces = "app.recent_workspaces"
	KeyProfile          = "app.profile" // omp profile 名
	KeyActiveEngine     = "app.engine"  // 当前选中的引擎（omp / pi）
	// KeyEngineExePrefix 是「某引擎的显式路径」，键 = engine.path.omp / engine.path.pi。
	// 用前缀而不是两个常量：新增引擎时不必再改这里。
	KeyEngineExePrefix = "engine.path."
	// KeyAutoStart 是开机自启开关的镜像值（1 / 0）。
	// 真值在注册表里，界面上这个键只用来「显示得出来」——
	// 每次启动以注册表为准回写它，避免开关状态与系统实际不符。
	KeyAutoStart = "app.autostart"
	// KeyRestoreSession 决定启动时是否回到上次那个会话（1 / 0）。
	// 为 0 时打开的是空会话，但工作目录照旧恢复——两件事不一样。
	KeyRestoreSession = "app.restore_session"
	KeyLastSession    = "app.last_session"
	KeyFontScale      = "ui.font_scale"
	KeyReduceMotion   = "ui.reduce_motion"
	// KeyCloseAction 是「点窗口关闭按钮时做什么」：tray（默认）/ quit。
	//
	// 为什么要可配：关窗静默收托盘在第一次用时看起来就是「关不掉」。
	// 默认收托盘是为了不丢正在跑的对话，但用户必须能改成直接退。
	KeyCloseAction = "app.close_action"
	// KeyTrayHintShown 记录「收托盘」的首次提示是否已经看过（1 / 0）。
	KeyTrayHintShown = "app.tray_hint_shown"
)

// RecentWorkspaceMax 最近工作目录的保留条数。
//
// 上限存在的理由不是省空间：超过一屏的「最近」就不再是快捷方式，
// 而变成第二份需要浏览的列表。12 条在设置页里刚好一屏出头。
const RecentWorkspaceMax = 12

// WorkspaceRow 是最近用过的工作目录。
type WorkspaceRow struct {
	// WorkspacePath 目录绝对路径
	WorkspacePath string `json:"workspace_path"`
	// Exists 目录现在还在不在：删掉或拔掉的盘不该还列在「最近」里
	Exists bool `json:"exists"`
	// UpdatedAt 最后一次在这个目录里改动会话的时间（RFC3339）
	UpdatedAt string `json:"updated_at,omitempty"`
}

// 服务商视图：两个引擎的配置文件格式不同，但界面只认这一种形态。

// ProvidersView 是一个引擎的服务商全量视图。
type ProvidersView struct {
	// Providers 服务商列表
	Providers []ProviderView `json:"providers"`
	// ConfigPath 这份视图读出来的那个配置文件（omp 是 models.yml，pi 是 models.json）
	ConfigPath string `json:"config_path"`
}

// ProviderView 是单个服务商的展示形态（密钥已打码）。
type ProviderView struct {
	// EngineKind 这条服务商属于哪个引擎（omp / pi）
	//
	// 两边的配置文件完全独立，同一个服务商名可以在两边各有一份。
	// 界面在「全部」作用域下靠这个字段分组与路由写操作。
	EngineKind string `json:"engine_kind"`
	// ProviderName 服务商名，也是配置文件里那个键
	ProviderName string `json:"provider_name"`
	// BaseURL 接口地址
	BaseURL string `json:"base_url"`
	// API 接口协议（openai-completions / anthropic-messages …）
	API string `json:"api"`
	// Auth 认证方式（api-key / oauth …）
	Auth string `json:"auth"`
	// APIKey 打码后的密钥；HasKey 才是「配没配」的判断依据
	APIKey string `json:"api_key"`
	// HasKey 是不是真的配了密钥
	HasKey bool `json:"has_key"`
	// Models 这个服务商下的模型列表
	Models []ModelEntry `json:"models"`
}

// ModelEntry 是一个模型条目。
type ModelEntry struct {
	// ModelID 模型标识；界面按 provider + model_id 唯一定位一个模型
	ModelID string `json:"model_id"`
	// ModelName 展示名；多数服务商不提供，为空时界面显示 id
	ModelName string `json:"model_name,omitempty"`
	// API 单个模型覆盖协议时才有
	API string `json:"api,omitempty"`
	// Reasoning 是否推理模型
	Reasoning bool `json:"reasoning,omitempty"`
	// Input 支持的输入模态（text / image …）
	Input []string `json:"input,omitempty"`
	// ContextWindow 上下文窗口；MaxTokens 单次最大输出
	ContextWindow int `json:"context_window,omitempty"`
	MaxTokens     int `json:"max_tokens,omitempty"`
}
