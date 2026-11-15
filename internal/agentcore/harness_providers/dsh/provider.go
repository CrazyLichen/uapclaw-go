package dsh

import (
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DshHarnessProvider 校验 provider 配置并创建一个未启动的 DSH harness。
// Python: provider.py:13-22 DshHarnessProvider
type DshHarnessProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 能力卡转发。Python: provider.py:16-18 card
func (p *DshHarnessProvider) Card() harnessprotocol.HarnessCard { return dshCard }

// Create 校验 provider 配置并创建未启动 harness。
// Python: provider.py:20-22 create（DshHarnessConfig.from_mapping → DshHarness）
func (p *DshHarnessProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	values, err := DshHarnessConfigFromMapping(config)
	if err != nil {
		return nil, err
	}
	return NewDshHarness(values)
}

// ModelConfig 把宿主模型选择映射到 model/base_url/api_key。
// effort 或 fallback 在 DSH 无对应设置，显式报错（「认证回退」在 DSH 是
// 明确不支持的能力面）。
// Python: provider.py:24-41 model_config
func (p *DshHarnessProvider) ModelConfig(
	choice harnessprotocol.ModelChoice, fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	// Python: provider.py:31-32
	if choice.Effort != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH has no reasoning effort setting",
		}
	}
	// Python: provider.py:33-34
	if fallback != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH has no authentication fallback",
		}
	}
	values := map[string]any{}
	// Python: provider.py:35-41 endpoint → model/base_url/api_key
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
	frozen, err := harnessprotocol.FreezeJsonObject(values)
	if err != nil {
		return nil, err
	}
	return frozen, nil
}

// 编译期接口断言。Python 由类定义隐式满足；Go 显式钉住。
var (
	_ harnessprotocol.HarnessProvider     = (*DshHarnessProvider)(nil)
	_ harnessprotocol.ModelConfigProvider = (*DshHarnessProvider)(nil)
)

// init 编译期注册：等价 Python factory.py 对 dsh 的 entry-point 注册。
// 导入本包即链接内置 provider "dsh"。
func init() {
	hp.RegisterBuiltinProvider("dsh", func() (harnessprotocol.HarnessProvider, error) {
		return &DshHarnessProvider{}, nil
	})
}
