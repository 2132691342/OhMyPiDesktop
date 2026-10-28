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
	KeyEnginePath       = "engine.path" // 已废弃：早先只有 omp 一份，保留读兼容
	// KeyEngineExePrefix 是「某引擎的显式路径」，键 = engine.path.omp / engine.path.pi。
	// 用前缀而不是两个常量：新增引擎时不必再改这里。
	KeyEngineExePrefix = "engine.path."
	KeyAutoStart       = "app.autostart"
	KeyLastSession     = "app.last_session"
	KeyFontScale       = "ui.font_scale"
	KeyReduceMotion    = "ui.reduce_motion"
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
	Providers []ProviderView `json:"providers"`
	// ConfigPath 这份视图读出来的那个配置文件（omp 是 models.yml，pi 是 models.json）
	ConfigPath string `json:"config_path"`
}

// ProviderView 是单个服务商的展示形态（密钥已打码）。
type ProviderView struct {
	// ProviderName 服务商名，也是配置文件里那个键
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	// API 接口协议（openai-completions / anthropic-messages …）
	API string `json:"api"`
	// Auth 认证方式（api-key / oauth …）
	Auth string `json:"auth"`
	// APIKey 打码后的密钥；HasKey 才是「配没配」的判断依据
	APIKey string       `json:"api_key"`
	HasKey bool         `json:"has_key"`
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
