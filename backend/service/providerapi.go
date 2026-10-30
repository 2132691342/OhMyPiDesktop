package service

// 服务商的上游探测：问「有哪些模型」与「这条配置通不通」。两个动作不写盘（写盘是 UpsertProvider 的事），
// 只帮用户确认地址与密钥对不对、这个服务商底下有哪些模型可填。怎么问上游归 upstream 包，
// 这里只做「读配置里的密钥 → 问 → 把结果交出去」。

import (
	"context"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/ompcfg"
	"OhMyPiDesktop/backend/env/picfg"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/upstream"
)

// ProviderModelsREQ 是「拉取模型清单」的入参。密钥可以不给：不给就用已保存的（编辑已有服务商时），
// 给了就用给的（新建、换密钥时）。
type ProviderModelsREQ struct {
	// EngineKind 读哪个引擎的配置（omp / pi）
	EngineKind string `json:"engine_kind"`
	// ProviderName 服务商名；给了就从配置里取地址与密钥
	ProviderName string `json:"provider_name"`
	// BaseURL 接口地址；不给就用配置里的
	BaseURL string `json:"base_url"`
	// API 协议；不给就用配置里的
	API string `json:"api"`
	// APIKey 密钥；不给就用配置里的
	APIKey string `json:"api_key"`
	// Auth 认证方式；不给就用配置里的
	Auth string `json:"auth"`
}

// ProviderModelsRESP 是「拉取模型清单」的回执。
type ProviderModelsRESP struct {
	// Models 问回来的模型清单
	Models []domain.ModelEntry `json:"models"`
	// ConfigPath 这份配置读自哪个文件
	ConfigPath string `json:"config_path"`
}

// FetchProviderModels 向上游问一次模型清单。「不给参数就用配置里的」是关键：编辑已有服务商时
// 界面上只有打码的密钥（没有原值），不从配置里取密钥这一按就必然 401。
func (a *App) FetchProviderModels(ctx context.Context, req ProviderModelsREQ) (*ProviderModelsRESP, error) {
	kind := engine.Kind(req.EngineKind)
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}

	saved := a.savedProvider(kind, layout.Models, req.ProviderName)
	base := firstNonEmpty(strings.TrimSpace(req.BaseURL), saved.BaseURL)
	api := firstNonEmpty(strings.TrimSpace(req.API), saved.API)
	key := firstNonEmpty(strings.TrimSpace(req.APIKey), saved.APIKey)
	auth := firstNonEmpty(strings.TrimSpace(req.Auth), saved.Auth)

	entries, err := upstream.FetchModels(upstream.Request{
		BaseURL: base, API: api, APIKey: key, Auth: auth,
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.ModelEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, domain.ModelEntry{ModelID: e.ModelID, ModelName: e.ModelName})
	}
	return &ProviderModelsRESP{Models: out, ConfigPath: layout.Models}, nil
}

// savedProvider 读出一个已保存服务商的地址、协议与密钥。读不到时交空值，调用方用请求参数补上；
// 密钥取原值（不打码）——要拿它去问上游。
func (a *App) savedProvider(kind engine.Kind, configPath, name string) domain.ProviderView {
	out := domain.ProviderView{EngineKind: string(kind), ProviderName: name}
	if name == "" {
		return out
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(configPath)
		if err != nil {
			return out
		}
		p, ok := f.Providers[name]
		if !ok {
			return out
		}
		out.BaseURL = p.BaseURL
		out.API = p.API
		out.Auth = p.OAuth
		out.APIKey = p.APIKey
		return out
	}
	f, err := ompcfg.ReadModels(configPath)
	if err != nil {
		return out
	}
	p, ok := f.Providers[name]
	if !ok {
		return out
	}
	out.BaseURL = p.BaseURL
	out.API = p.API
	out.Auth = p.Auth
	out.APIKey = p.APIKey
	return out
}

// ProviderTestREQ 是「测一下这条配置通不通」的入参。
type ProviderTestREQ struct {
	// EngineKind 读哪个引擎的配置
	EngineKind string `json:"engine_kind"`
	// ProviderName 服务商名
	ProviderName string `json:"provider_name"`
	// BaseURL 接口地址；不给就用配置里的
	BaseURL string `json:"base_url"`
	// API 协议；不给就用配置里的
	API string `json:"api"`
	// APIKey 密钥；不给就用配置里的
	APIKey string `json:"api_key"`
	// Auth 认证方式；不给就用配置里的
	Auth string `json:"auth"`
}

// ProviderTestRESP 是「测一下这条配置通不通」的回执。
type ProviderTestRESP struct {
	// OK 通不通
	OK bool `json:"ok"`
	// ModelCount 上游报了多少个模型；不通时是 0
	ModelCount int `json:"model_count"`
	// Message 不通时的原因原话；通了就是空串
	Message string `json:"message"`
}

// TestProvider 测一条服务商配置通不通。判据是「上游的模型清单能不能问出来」——
// 它同时验证了地址、协议与密钥，这三样是配错时最容易混在一起的三件事。
func (a *App) TestProvider(ctx context.Context, req ProviderTestREQ) (*ProviderTestRESP, error) {
	models, err := a.FetchProviderModels(ctx, ProviderModelsREQ{
		EngineKind: req.EngineKind, ProviderName: req.ProviderName,
		BaseURL: req.BaseURL, API: req.API, APIKey: req.APIKey, Auth: req.Auth,
	})
	if err != nil {
		appErr := pkg.As(err)
		return &ProviderTestRESP{OK: false, Message: appErr.Message}, nil
	}
	return &ProviderTestRESP{OK: true, ModelCount: len(models.Models)}, nil
}
