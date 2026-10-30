// 插件（引擎叫 plugin / extension / package，界面统一叫插件）。
package domain

// PluginRowVO 是一条已装插件在界面上的形态。
// 「已装」的判据是引擎自己的清单里登记了它，不是目录里碰巧有个包；
// 清单里有、目录不在，说明用户删过目录或换过机器——那时 Installed 为假，界面给「重装」而不是「卸载」。
type PluginRowVO struct {
	// EngineKind 归属引擎（omp / pi）
	EngineKind string `json:"engine_kind"`
	// PluginName 包名（npm 包名或目录名）
	PluginName string `json:"plugin_name"`
	// PluginVersion 清单里登记的版本；没锁版本时为空
	PluginVersion string `json:"plugin_version"`
	// PluginSource 清单里那一行原文（npm:xxx@1.2.3 / git:… / 本地路径）
	PluginSource string `json:"plugin_source"`
	// PluginPath 包目录绝对路径；空表示目录不在盘上
	PluginPath string `json:"plugin_path"`
	// PluginDesc 包的 description
	PluginDesc string `json:"plugin_desc,omitempty"`
	// ResourceKinds 这个包提供的资源种类（extensions / skills / prompts / themes）
	ResourceKinds []string `json:"resource_kinds"`
	// ScopeKind 登记在哪一层：user / project
	ScopeKind string `json:"scope_kind"`
	// Enabled 是否被停用（omp 认锁文件里的位，pi 没有这个开关）
	Enabled bool `json:"enabled"`
	// Installed 包目录是否真的存在
	Installed bool `json:"installed"`
}

// PluginListVO 是一个引擎（或全部）的插件清单。
type PluginListVO struct {
	// Plugins 插件行
	Plugins []PluginRowVO `json:"plugins"`
	// RegistryPath 这份清单读出来的那个文件（settings.json / plugins/package.json）
	RegistryPath string `json:"registry_path"`
	// ReadError 读清单时遇到的问题；为空表示一切正常
	ReadError string `json:"read_error,omitempty"`
}

// PluginActionREQ 装 / 卸一个插件。
type PluginActionREQ struct {
	// EngineKind 目标引擎；空表示当前选中的
	EngineKind string `json:"engine_kind"`
	// PluginSource 包名或仓库地址（npm:xxx / git:… / 本地路径）
	PluginSource string `json:"plugin_source"`
}
