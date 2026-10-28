// MCP 服务器配置：两个引擎共用 `mcp.json` 的 `mcpServers` 形状。
package domain

// McpServerVO 是一个 MCP 服务器在界面上的形态。
//
// 只暴露「界面能表达」的那几个字段：连接方式是命令还是 URL、启没启用、
// 从哪份文件来的。其余未知字段（auth / oauth / headers / toolExposure …）
// 由读写层原样保留，界面不碰——碰了就是在替引擎做它自己的事。
type McpServerVO struct {
	// ServerName 服务器名，也是配置里那个键
	ServerName string `json:"server_name"`
	// TransportKind 连接方式：stdio / http / sse；空表示按 URL 自动推断
	TransportKind string `json:"transport_kind"`
	// CommandLine stdio 的命令与参数拼成的一行（给人看）
	CommandLine string `json:"command_line,omitempty"`
	// ServerURL http / sse 的地址
	ServerURL string `json:"server_url,omitempty"`
	// Desc 配置里的 description
	Desc string `json:"desc,omitempty"`
	// Enabled 当前是否启用（综合 entry.enabled 与顶层 allow/deny 清单）
	Enabled bool `json:"enabled"`
	// DisabledByDeny 被顶层 disabledServers 名单压住；界面要说清为什么点不动
	DisabledByDeny bool `json:"disabled_by_deny"`
	// ScopeKind 定义在哪一层：user / project
	ScopeKind string `json:"scope_kind"`
	// ConfigPath 这份定义来自哪个文件
	ConfigPath string `json:"config_path"`
	// LiveStatus 引擎报告的连接状态（connected / failed / unknown）；只在探测时才有
	LiveStatus string `json:"live_status,omitempty"`
	// LiveError 引擎报告的错误原文
	LiveError string `json:"live_error,omitempty"`
	// ToolNames 引擎报告的工具列表
	ToolNames []string `json:"tool_names,omitempty"`
}

// McpListVO 是一个引擎的 MCP 清单。
type McpListVO struct {
	// EngineKind 归属引擎
	EngineKind string `json:"engine_kind"`
	// Servers 服务器列表
	Servers []McpServerVO `json:"servers"`
	// UserPath 用户级 mcp.json 的路径（界面要点「打开」）
	UserPath string `json:"user_path"`
	// ProjectPath 项目级 mcp.json；不在 git 仓库里时为空
	ProjectPath string `json:"project_path,omitempty"`
	// ReadErrors 读配置时引擎会报的解析错误；为空表示一切正常
	ReadErrors []string `json:"read_errors,omitempty"`
	// LiveChecked 这份清单里的状态是否带上了引擎的实时探测结果
	LiveChecked bool `json:"live_checked"`
}

// McpSaveREQ 新增或覆盖一个 MCP 服务器。
type McpSaveREQ struct {
	// EngineKind 目标引擎；空表示当前选中的
	EngineKind string `json:"engine_kind"`
	// ServerName 服务器名
	ServerName string `json:"server_name"`
	// CommandText stdio 的整条命令行（第一个 token 是命令，其余是参数）
	CommandText string `json:"command_text,omitempty"`
	// ServerURL http / sse 的地址
	ServerURL string `json:"server_url,omitempty"`
	// TransportKind http / sse；留空且给了 URL 时按 http 处理
	TransportKind string `json:"transport_kind,omitempty"`
	// Desc 一句话说明
	Desc string `json:"desc,omitempty"`
	// EnvList 形如 `KEY=VALUE` 的环境变量
	EnvList []string `json:"env_list,omitempty"`
	// WorkDir stdio 服务器的工作目录
	WorkDir string `json:"work_dir,omitempty"`
	// TargetProject 写到项目级 .omp/.pi/mcp.json 而不是用户级
	TargetProject bool `json:"target_project"`
}

// McpToggleREQ 启停一个 MCP 服务器。
type McpToggleREQ struct {
	// EngineKind 目标引擎；空表示当前选中的
	EngineKind string `json:"engine_kind"`
	// ServerName 服务器名
	ServerName string `json:"server_name"`
	// Enabled 目标状态
	Enabled bool `json:"enabled"`
}

// McpRemoveREQ 删除一个 MCP 服务器。
type McpRemoveREQ struct {
	// EngineKind 目标引擎；空表示当前选中的
	EngineKind string `json:"engine_kind"`
	// ServerName 服务器名
	ServerName string `json:"server_name"`
}
