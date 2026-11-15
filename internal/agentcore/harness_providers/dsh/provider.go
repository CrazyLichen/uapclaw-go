package dsh

import (
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DshHarnessProvider 校验 provider 配置并创建一个未启动的 DSH harness。
// Python: provider.py:13-22 DshHarnessProvider
type DshHarnessProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 能力卡转发。
// Python: provider.py:16-18 card
func (p *DshHarnessProvider) Card() harnessprotocol.HarnessCard { return dshCard }

// Create 经 FromMapping 校验配置并创建未启动的 harness。
// Python: provider.py:20-22 create
func (p *DshHarnessProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	validated, err := DshHarnessConfigFromMapping(config)
	if err != nil {
		return nil, err
	}
	return NewDshHarness(validated)
}

// ModelConfig 把宿主模型选择映射到 model/base_url/api_key。
//
// effort 与 fallback 在 DSH 无对应设置，显式报错——「认证回退」在 dsh 的
// 能力面就是 model_config 的拒绝分支（计划行描述的「认证回退」实为此语义）。
// Python: provider.py:24-41 model_config
func (p *DshHarnessProvider) ModelConfig(
	choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	if choice.Effort != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH has no reasoning effort setting",
		}
	}
	if fallback != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH has no authentication fallback",
		}
	}
	values := harnessprotocol.JsonObject{"model": derefString(choice.Model)}
	if choice.Endpoint != nil {
		if choice.Endpoint.APIBase != nil {
			values["base_url"] = *choice.Endpoint.APIBase
		}
		if choice.Endpoint.APIKey != nil {
			values["api_key"] = *choice.Endpoint.APIKey
		}
	}
	// Python: provider.py:40-41 filter-None → Go 零值省略
	result := harnessprotocol.JsonObject{}
	for key, value := range values {
		if value != nil && value != "" {
			result[key] = value
		}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 编译期注册：等价 Python factory.py:203-206 对 dsh 的惰性 import。
func init() {
	harness_providers.RegisterBuiltinProvider("dsh", func() (harnessprotocol.HarnessProvider, error) {
		return &DshHarnessProvider{}, nil
	})
}
