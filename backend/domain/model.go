// Package domain 放跨层的领域模型：一个聚合根一个文件。
package domain

// 设置键。集中在这里，避免字符串散落。
const (
	KeyTheme        = "ui.theme"      // light / dark / system
	KeyLang         = "ui.lang"       // zh-CN / en
	KeyWorkspace    = "app.workspace" // 上次用的工作目录
	KeyProfile      = "app.profile"   // omp profile 名
	KeyActiveEngine = "app.engine"    // 当前选中的引擎（omp / pi）
	KeyEnginePath   = "engine.path"   // 已废弃：早先只有 omp 一份，保留读兼容
	// KeyEngineExePrefix 是「某引擎的显式路径」，键 = engine.path.omp / engine.path.pi。
	// 用前缀而不是两个常量：新增引擎时不必再改这里。
	KeyEngineExePrefix = "engine.path."
	KeyAutoStart       = "app.autostart"
	KeyLastSession     = "app.last_session"
	KeyFontScale       = "ui.font_scale"
	KeyReduceMotion    = "ui.reduce_motion"
)

// WorkspaceRow 是最近用过的工作目录。
type WorkspaceRow struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// 服务商视图：两个引擎的配置文件格式不同，但界面只认这一种形态。

// ProvidersView 是一个引擎的服务商全量视图。
type ProvidersView struct {
	Providers []ProviderView `json:"providers"`
	Path      string         `json:"path"`
}

// ProviderView 是单个服务商的展示形态（密钥已打码）。
type ProviderView struct {
	Name    string       `json:"name"`
	BaseURL string       `json:"base_url"`
	API     string       `json:"api"`
	Auth    string       `json:"auth"`
	APIKey  string       `json:"api_key"`
	HasKey  bool         `json:"has_key"`
	Models  []ModelEntry `json:"models"`
}

// ModelEntry 是一个模型条目。
type ModelEntry struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	API           string   `json:"api,omitempty"`
	Reasoning     bool     `json:"reasoning,omitempty"`
	Input         []string `json:"input,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
}
