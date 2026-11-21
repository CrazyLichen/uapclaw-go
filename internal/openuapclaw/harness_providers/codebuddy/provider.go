package codebuddy

import (
	"fmt"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodebuddyProvider 校验 provider 配置并创建一个未启动的 codebuddy harness。
// Python 无对应物（偏差 D-15.22-a）；三方法签名对齐 dsh/provider.py:13-41。
type CodebuddyProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 能力卡转发。
func (p *CodebuddyProvider) Card() harnessprotocol.HarnessCard { return codebuddyCard }

// Create 经 FromMapping 校验配置并创建未启动 harness。
func (p *CodebuddyProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	validated, err := CodebuddyConfigFromMapping(config)
	if err != nil {
		return nil, err
	}
	return NewCodebuddyHarness(validated)
}

// ModelConfig 把宿主模型选择映射到 model（--model 载体）。
// effort 与 fallback 在 codebuddy 无对应设置，显式报错——对齐 dsh
// provider.py:24-41 的拒绝分支语义。
func (p *CodebuddyProvider) ModelConfig(
	choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	if choice.Effort != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "codebuddy has no reasoning effort setting",
		}
	}
	if fallback != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "codebuddy has no authentication fallback",
		}
	}
	values := harnessprotocol.JsonObject{}
	if choice.Model != nil {
		values["model"] = *choice.Model
	}
	if choice.Endpoint != nil {
		if choice.Endpoint.APIBase != nil {
			values["base_url"] = *choice.Endpoint.APIBase
		}
		if choice.Endpoint.APIKey != nil {
			values["api_key"] = *choice.Endpoint.APIKey
		}
	}
	// dsh provider.py:40-41 filter-None → Go 零值省略
	result := harnessprotocol.JsonObject{}
	for key, value := range values {
		if value != nil && value != "" {
			result[key] = value
		}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 编译期注册：走第三方进程内注册缝（factory.go:137
// RegisterHarnessProvider），不改 ProviderNames 闭集（偏差 D-15.22-b）。
func init() {
	if err := providers.RegisterHarnessProvider("codebuddy", func() (harnessprotocol.HarnessProvider, error) {
		return &CodebuddyProvider{}, nil
	}); err != nil {
		panic(fmt.Sprintf("codebuddy provider 注册失败: %v", err))
	}
}
