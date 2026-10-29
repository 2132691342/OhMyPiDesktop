package service

// 模型清单的降级来源：不拉起引擎，直接读配置。
//
// 为什么需要它：引擎的 get_available_models 是运行时状态（按凭据过滤过），
// 而拿到它必须先拉起一个引擎进程——omp 冷启动实测 11 秒，pi 起一次 Node 3 秒。
// 于是「打开对话页看一眼模型下拉」这种零风险操作，代价是等一个进程冷启动。
//
// 降级策略：引擎没在跑的时候读它自己的配置文件。
// 代价是拿不到内置目录（引擎自带的那份模型表）与凭据过滤，
// 所以降级结果要标出来，界面据此说明「这是配置里配的，不是引擎当前可用的」。
//
// 一条事实一个落点：配置怎么读归 env/ompcfg 与 env/picfg，
// 这里只把它们的结构归一成同一份视图。

import (
	"context"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/ompcfg"
	"OhMyPiDesktop/backend/env/picfg"
)

// ModelsFromConfig 从配置文件里读出模型清单。
//
// kind 为空时两个引擎都读。读不出来（文件不存在 / 格式有误）就少一条，
// 不返回错误：模型清单拉不到时界面仍应可用，只是下拉里少了几个选项。
func (a *App) ModelsFromConfig(kind engine.Kind) []ModelRow {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := make([]ModelRow, 0, 16)
	for _, k := range kinds {
		out = append(out, a.modelsFromConfigOf(k)...)
	}
	return out
}

// modelsFromConfigOf 读单个引擎的配置模型清单。
func (a *App) modelsFromConfigOf(kind engine.Kind) []ModelRow {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil
		}
		out := make([]ModelRow, 0, 16)
		for name, p := range f.Providers {
			for _, m := range p.Models {
				out = append(out, ModelRow{
					Kind: kind, Provider: name, ID: m.ID, Name: m.Name, API: m.API,
					ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
					Reasoning: m.Reasoning, Input: m.Input,
				})
			}
		}
		return out
	}
	f, err := ompcfg.ReadModels(layout.Models)
	if err != nil {
		return nil
	}
	out := make([]ModelRow, 0, 16)
	for name, p := range f.Providers {
		for _, m := range p.Models {
			out = append(out, ModelRow{
				Kind: kind, Provider: name, ID: m.ID, Name: m.Name, API: m.API,
				ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
				Reasoning: m.Reasoning, Input: m.Input,
			})
		}
	}
	return out
}

// AvailableModels 是「模型清单」的统一出口。
//
// 引擎在跑就用引擎的（它知道自己有哪些凭据、哪些模型真的能调）；
// 引擎没跑就用配置的（不为了看个下拉去起一个进程）。
func (a *App) AvailableModels(ctx context.Context, kind engine.Kind) ([]ModelRow, error) {
	e := a.EngineFor(kind, a.Workspace())
	if e.Live() {
		list, err := e.AvailableModels(ctx)
		if err == nil {
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
	}
	// 引擎没跑或问不出来：退到配置文件。
	return a.ModelsFromConfig(kind), nil
}
