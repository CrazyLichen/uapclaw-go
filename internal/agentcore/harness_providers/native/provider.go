// Package native 的 provider 工厂：从可序列化配置构造未启动的 DeepAgentHarness。
// Python: openjiuwen/harness_providers/native/provider.py
package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// providerFactoryName factory 解析用的内置 provider 名（闭集成员）。
	// Python: factory.py:203 `provider == "native"`；卡片名 ProviderName
	// （"deepagent"）是协议身份，二者不同（harness.py:83 card.name）。
	providerFactoryName = "native"
)

// knownKeys create 允许的配置键。
// Python: provider.py:16 _KNOWN_KEYS
var knownKeys = map[string]bool{
	"deep_agent":            true,
	"agent_template":        true,
	"session_id":            true,
	"language":              true,
	"event_buffer_capacity": true,
}

// errStartupPrefix OpenSession 失败统一前缀（harness.py:307-310）。
const errStartupPrefix = "DeepAgent harness startup failed: "

// ──────────────────────────── 结构体 ────────────────────────────

// NativeHarnessProvider 从可序列化配置构造未启动 DeepAgentHarness 的工厂。
// Python: provider.py:19-64 NativeHarnessProvider
type NativeHarnessProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 返回 DeepAgentHarness 的静态能力卡。
// Python: provider.py:30-32 card property
func (p *NativeHarnessProvider) Card() harnessprotocol.HarnessCard {
	return AgentFactoryCard()
}

// Create 校验配置并构造未启动的 DeepAgentHarness。
// Python: provider.py:34-64 create
//
// config 键：deep_agent（DeepAgentSpec 字段）/ agent_template（可选模板，
// 首轮前热加载）/ session_id、language、event_buffer_capacity（旋钮）。
func (p *NativeHarnessProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	values := harnessprotocol.JsonValueToBuiltin(config)
	valuesMap, ok := values.(map[string]any)
	if !ok {
		// Python: TypeError("native harness config must be an object")
		return nil, errors.New("native harness config must be an object")
	}
	// 未知键拒绝（provider.py:39-41），键序确定性输出
	unknown := make([]string, 0, len(valuesMap))
	for key := range valuesMap {
		if !knownKeys[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown native harness configuration fields: %s", strings.Join(unknown, ", "))
	}

	// deep_agent 必须是对象；缺省空 spec（provider.py:42-45）
	var spec hschema.DeepAgentSpec
	if raw, present := valuesMap["deep_agent"]; present && raw != nil {
		specPayload, isMap := raw.(map[string]any)
		if !isMap {
			return nil, errors.New("native harness deep_agent config must be an object")
		}
		parsed, err := parseSpecPayload[hschema.DeepAgentSpec](specPayload)
		if err != nil {
			return nil, err
		}
		spec = parsed
	}

	// agent_template 可选（provider.py:46-47）
	var template *hschema.AgentTemplateSpec
	if raw, present := valuesMap["agent_template"]; present && raw != nil {
		templatePayload, isMap := raw.(map[string]any)
		if !isMap {
			return nil, errors.New("native harness agent_template config must be an object")
		}
		parsed, err := parseSpecPayload[hschema.AgentTemplateSpec](templatePayload)
		if err != nil {
			return nil, err
		}
		template = &parsed
	}

	// 模板兜底：card 与 model（provider.py:49-52）
	if template != nil && spec.Card == nil {
		spec.Card = &template.AgentCard
	}
	if template != nil && spec.Model == nil && template.Model != nil {
		spec.Model = template.Model
	}

	// 工厂：忽略 context，按 spec 构建未启动 agent（provider.py:54-56 spec.build）
	factory := func(_ context.Context, _ harnessprotocol.HarnessContext) (*harness.DeepAgent, error) {
		return buildDeepAgentFromSpec(&spec)
	}

	// 旋钮（provider.py:58-64）；session_id/language 走 _optional_str 校验
	opts := []HarnessOption{WithEventBufferCapacity(bufferCapacityOf(valuesMap))}
	if template != nil {
		opts = append(opts, WithAgentTemplate(template))
	}
	if raw, present := valuesMap["session_id"]; present && raw != nil {
		sessionID, valid := optionalString(raw)
		if !valid {
			return nil, errors.New("native harness string options must be non-empty strings")
		}
		opts = append(opts, WithSessionID(sessionID))
	}
	language := ""
	if raw, ok := valuesMap["language"]; ok && raw != nil {
		parsed, valid := optionalString(raw)
		if !valid {
			return nil, errors.New("native harness string options must be non-empty strings")
		}
		language = parsed
	}
	if language == "" {
		language = spec.Language
	}
	if language != "" {
		opts = append(opts, WithLanguage(language))
	}
	return NewDeepAgentHarness(factory, opts...), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// optionalString 非空字符串校验；其余值拒绝。
// Python: provider.py:67-72 _optional_str
func optionalString(value any) (string, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// bufferCapacityOf 有界缓冲容量，缺省 1024。
// Python: provider.py:63 int(values.get("event_buffer_capacity") or 1024)
func bufferCapacityOf(values map[string]any) int {
	if raw, ok := values["event_buffer_capacity"]; ok && raw != nil {
		switch n := raw.(type) {
		case float64:
			if n > 0 {
				return int(n)
			}
		case int:
			if n > 0 {
				return n
			}
		}
	}
	return 1024
}

// parseSpecPayload 把 builtin map 经 JSON 往返物化为 typed spec。
// Python: provider.py:45/47 的 pydantic model_validate；Go 无 pydantic，
// 以 json 往返等价（仓内先例：NewTool JSON round-trip）。
func parseSpecPayload[T any](payload map[string]any) (T, error) {
	var out T
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, fmt.Errorf("native harness config encode failed: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("native harness config decode failed: %w", err)
	}
	return out, nil
}

// buildDeepAgentFromSpec 物化未启动 DeepAgent。
// Python: provider.py:54-56 `_factory` 的 `spec.build()`
// （deep_agent_spec.py 的 DeepAgentSpec.build → ResolveDeepAgentSpecParts +
// ApplyDeepAgentParts，与 agent_teams/harness/native_harness.go:118-131 同链路）。
func buildDeepAgentFromSpec(spec *hschema.DeepAgentSpec) (*harness.DeepAgent, error) {
	parts, err := harness.ResolveDeepAgentSpecParts(spec, nil)
	if err != nil {
		return nil, fmt.Errorf("%s%s", errStartupPrefix, err.Error())
	}
	agent, err := harness.ApplyDeepAgentParts(context.Background(), parts)
	if err != nil {
		return nil, fmt.Errorf("%s%s", errStartupPrefix, err.Error())
	}
	return agent, nil
}

// init 编译期注册：等价 Python factory.py:203-206 对 native 的惰性 import。
// 导入本包即链接内置 provider "native"（resolve_provider 的内置分支语义）。
func init() {
	harness_providers.RegisterBuiltinProvider(providerFactoryName, func() (harnessprotocol.HarnessProvider, error) {
		return &NativeHarnessProvider{}, nil
	})
}
