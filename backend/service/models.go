package service

// 模型与服务商：读写引擎自己的 models 配置，以及在界面里发起登录。
//
// 两个引擎的配置文件格式不同（omp 是 YAML 节点、pi 是 JSONC 键），
// 但界面只认一种形态——各自的读写细节在 env/ompcfg 与 env/picfg，
// 这里只做「读出来 → 归一成视图 → 写回去」的编排。

import (
	"context"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/ompcfg"
	"OhMyPiDesktop/backend/env/picfg"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- 模型与服务商 ----------------

// ModelRow 是可用模型在界面上的形态。
type ModelRow struct {
	Kind          engine.Kind `json:"kind"`
	Provider      string      `json:"provider"`
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	API           string      `json:"api"`
	ContextWindow int         `json:"context_window"`
	MaxTokens     int         `json:"max_tokens"`
	Reasoning     bool        `json:"reasoning"`
	Input         []string    `json:"input"`
}

// Models 列出引擎眼下的可用模型。
func (a *App) Models(ctx context.Context, kind engine.Kind) ([]ModelRow, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	e := a.EngineFor(kind, a.Workspace())
	list, err := e.AvailableModels(ctx)
	if err != nil {
		return []ModelRow{}, nil
	}
	out := make([]ModelRow, 0, len(list))
	for _, m := range list {
		out = append(out, ModelRow{
			Kind: kind, Provider: m.Provider, ID: m.ID, Name: m.Name, API: m.API,
			ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
			Reasoning: m.Reasoning, Input: m.Input,
		})
	}
	return out, nil
}

// Providers 返回某引擎的服务商视图（密钥打码）。
//
// 两个引擎的配置文件格式完全不同，但视图形态一致：前端只认 ProvidersView。
func (a *App) Providers(kind engine.Kind) (*domain.ProvidersView, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil, err
		}
		out := &domain.ProvidersView{Providers: []domain.ProviderView{}, ConfigPath: layout.Models}
		for _, name := range f.Names() {
			p := f.Providers[name]
			v := domain.ProviderView{
				ProviderName: name,
				BaseURL:      p.BaseURL,
				API:          p.API,
				Auth:         p.OAuth,
				APIKey:       picfg.MaskKey(p.APIKey),
				HasKey:       p.APIKey != "",
				Models:       []domain.ModelEntry{},
			}
			for _, m := range p.Models {
				v.Models = append(v.Models, domain.ModelEntry{
					ModelID: m.ID, ModelName: m.Name, API: m.API, Reasoning: m.Reasoning,
					Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
				})
			}
			out.Providers = append(out.Providers, v)
		}
		return out, nil
	}
	f, err := ompcfg.ReadModels(layout.Models)
	if err != nil {
		return nil, err
	}
	view := f.View(layout.Models)
	out := &domain.ProvidersView{Providers: []domain.ProviderView{}, ConfigPath: view.Path}
	for _, p := range view.Providers {
		out.Providers = append(out.Providers, domain.ProviderView{
			ProviderName: p.Name, BaseURL: p.BaseURL, API: p.API, Auth: p.Auth,
			APIKey: p.APIKey, HasKey: p.HasKey, Models: toDomainModels(p.Models),
		})
	}
	return out, nil
}

// UpsertProvider 写一个服务商。
func (a *App) UpsertProvider(kind engine.Kind, name string, p domain.ProviderView) (*domain.ProvidersView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "服务商名字不能为空", "")
	}
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil, err
		}
		next := picfg.Provider{
			Name: name, BaseURL: p.BaseURL, APIKey: p.APIKey, API: p.API, OAuth: p.Auth,
		}
		for _, m := range p.Models {
			next.Models = append(next.Models, picfg.Model{
				ID: m.ModelID, Name: m.ModelName, API: m.API, Reasoning: m.Reasoning,
				Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
			})
		}
		if prev, ok := f.Providers[name]; ok {
			next = picfg.Merge(prev, next)
		}
		if err := picfg.Upsert(layout.Models, name, next); err != nil {
			return nil, err
		}
		return a.Providers(kind)
	}
	next := ompcfg.Provider{
		BaseURL: p.BaseURL, APIKey: p.APIKey, API: p.API, Auth: p.Auth,
	}
	for _, m := range p.Models {
		next.Models = append(next.Models, ompcfg.ModelEntry{
			ID: m.ModelID, Name: m.ModelName, API: m.API, Reasoning: m.Reasoning,
			Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
		})
	}
	if f, err := ompcfg.ReadModels(layout.Models); err == nil {
		if prev, ok := f.Providers[name]; ok {
			next = ompcfg.MergeProvider(prev, next)
		}
	}
	if err := ompcfg.UpsertProvider(layout.Models, name, next); err != nil {
		return nil, err
	}
	return a.Providers(kind)
}

// DeleteProvider 删一个服务商。
func (a *App) DeleteProvider(kind engine.Kind, name string) (*domain.ProvidersView, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		if err := picfg.Delete(layout.Models, name); err != nil {
			return nil, err
		}
	} else if err := ompcfg.DeleteProvider(layout.Models, name); err != nil {
		return nil, err
	}
	return a.Providers(kind)
}

// LoginProviders 返回可登录的服务商（仅 omp 有 RPC 面）。
func (a *App) LoginProviders(ctx context.Context, kind engine.Kind) ([]engine.LoginProvider, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	if !a.Identity(kind).Caps.Login {
		return []engine.LoginProvider{}, nil
	}
	var out struct {
		Providers []engine.LoginProvider `json:"providers"`
	}
	if err := a.EngineFor(kind, a.Workspace()).Call(ctx, "get_login_providers", nil, &out, 20*time.Second); err != nil {
		return []engine.LoginProvider{}, nil
	}
	if out.Providers == nil {
		out.Providers = []engine.LoginProvider{}
	}
	return out.Providers, nil
}

// Login 发起 OAuth（仅 omp）。
func (a *App) Login(ctx context.Context, kind engine.Kind, provider string) error {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	if !a.Identity(kind).Caps.Login {
		return pkg.New(pkg.CodeEngineUnsupported,
			"pi 的登录在终端里做：pi auth", "")
	}
	return a.EngineFor(kind, a.Workspace()).Call(ctx, "login", engine.LoginParams(provider), nil, 600*time.Second)
}
