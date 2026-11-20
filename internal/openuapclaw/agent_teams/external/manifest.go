// Python 源：openjiuwen/agent_teams/external/manifest.py（104 行 1:1）
//
// 把解析出的 AgentTemplate runtime 映射到既有 external CLI 配置。

package external

import (
	"encoding/json"
	"fmt"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	"sort"
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

var (
	// manifestLogComponent 组件日志。
	manifestLogComponent = logger.ComponentAgentServer

	// providerToCLI AgentTemplate runtime provider → CLI kind。
	// Python: _PROVIDER_TO_CLI (manifest.py:12)
	providerToCLI = map[string]string{"codex": "codex", "claudecode": "claude"}

	// configFields runtime.config 允许覆写的字段闭集。
	// Python: _CONFIG_FIELDS (manifest.py:13-22)
	configFields = map[string]bool{
		"skill_conflict":                     true,
		"system_prompt_mode":                 true,
		"mcp_default_tools_approval_mode":    true,
		"codex_bypass_approvals_and_sandbox": true,
		"codex_turn_idle_timeout_s":          true,
		"codex_turn_idle_retries":            true,
		"claude_turn_idle_timeout_s":         true,
		"claude_max_buffer_size":             true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CLIAgentForProvider AgentTemplate runtime provider 对应的 CLI kind；
// 不支持得空串。Python: cli_agent_for_provider (manifest.py:25-27)
func CLIAgentForProvider(provider string) string {
	return providerToCLI[provider]
}

// ExternalCliAgentSpecFromTemplate 把 manifest.runtime 翻译成 Team 的
// external-CLI 配置；runtime 缺失得 nil。
// Python: external_cli_agent_spec_from_template (manifest.py:30-71)
func ExternalCliAgentSpecFromTemplate(template *hschema.AgentTemplateSpec) (*atschema.ExternalCliAgentSpec, error) {
	runtime := template.Runtime
	if runtime == nil {
		return nil, nil
	}
	provider := runtime.ProviderName
	if _, ok := providerToCLI[provider]; !ok {
		return nil, fmt.Errorf("runtime.provider_name must be 'codex' or 'claudecode'")
	}
	// Python: resolve_provider(provider).card.implementation_version——已安装
	// provider 卡片的实现版本。provider 闭集仅 codex/claudecode（上方
	// providerToCLI；dsh 是 harness provider 执行域，非成员后端，不进此翻译器），
	// 两者的 SDK provider 15.7/15.8 注册后校验自动生效，未链接时降级空串
	// 保持「声明版本必须为空」语义（与原恒空串行为等价）。
	expectedVersion := BuiltinProviderImplementationVersion(provider)
	if runtime.ProviderVersion != "" && runtime.ProviderVersion != expectedVersion {
		return nil, fmt.Errorf("runtime requires %s %q; installed version is %q", provider, runtime.ProviderVersion, expectedVersion)
	}
	if len(runtime.SdkPaths) > 0 {
		return nil, fmt.Errorf("runtime.sdk_paths is not supported for built-in Codex/Claude Code providers")
	}
	unknownConfig := []string{}
	for key := range runtime.Config {
		if !configFields[key] {
			unknownConfig = append(unknownConfig, key)
		}
	}
	if len(unknownConfig) > 0 {
		sort.Strings(unknownConfig)
		return nil, fmt.Errorf("runtime.config cannot set: %s", strings.Join(unknownConfig, ", "))
	}
	unsupported := []string{}
	for _, name := range []string{"tools", "rails", "subagents", "mcps"} {
		if templateHasAny(template, name) {
			unsupported = append(unsupported, name)
		}
	}
	if len(unsupported) > 0 {
		return nil, fmt.Errorf("external runtime does not support: %s", strings.Join(unsupported, ", "))
	}

	// Python: ExternalCliAgentSpec.model_validate({...})——skills 序列化为
	// template 声明的 json 形态。Go ExternalCliAgentSpec 见 schema/team。
	spec := &atschema.ExternalCliAgentSpec{
		CLIAgent:      providerToCLI[provider],
		SkillConflict: "append",
	}
	for _, skill := range template.Skills {
		spec.Skills = append(spec.Skills, skill)
	}
	// runtime.config 的白名单键覆写（对齐 Python **runtime.config 展开）。
	applyRuntimeConfig(spec, runtime.Config)
	return spec, nil
}

// MergeExternalCliConfig 专家模板孵出的 external-CLI 成员的启动配置：
// 团队对该 CLI kind 的声明配置是基座——承载 host 部署知识（command、cwd、env、
// SSH transport、内置模型目录…）；模板只叠加属于专家的部分：其 skills（追加
// 在声明之后）、skill_conflict、以及它设为非默认值的 runtime.config 键。
// 两配置 CLI kind 不同时报错。
// Python: merge_external_cli_config (manifest.py:74-104)
func MergeExternalCliConfig(declared *atschema.ExternalCliAgentSpec, templateConfig *atschema.ExternalCliAgentSpec) (*atschema.ExternalCliAgentSpec, error) {
	if declared.CLIAgent != templateConfig.CLIAgent {
		return nil, fmt.Errorf("template runs on cli_agent %q, declared config is for %q", templateConfig.CLIAgent, declared.CLIAgent)
	}
	overlay := map[string]any{"skill_conflict": templateConfig.SkillConflict}
	// Python: _CONFIG_FIELDS - {"skill_conflict"} 的非默认值才进 overlay。
	for name := range configFields {
		if name == "skill_conflict" {
			continue
		}
		if !isDefaultExternalCLIField(templateConfig, name) {
			overlay[name] = externalCLIFieldValue(templateConfig, name)
		}
	}
	skills := append([]any{}, declared.Skills...)
	for _, skill := range templateConfig.Skills {
		dup := false
		for _, existing := range skills {
			if sameSkillJSON(existing, skill) {
				dup = true
				break
			}
		}
		if !dup {
			skills = append(skills, skill)
		}
	}
	merged := *declared
	merged.SkillConflict = templateConfig.SkillConflict
	merged.Skills = skills
	applyOverlay(&merged, overlay)
	return &merged, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// templateHasAny 模板某能力面是否非空（Python getattr(template, name)）。
func templateHasAny(template *hschema.AgentTemplateSpec, name string) bool {
	switch name {
	case "tools":
		return len(template.Tools) > 0
	case "rails":
		return len(template.Rails) > 0
	case "subagents":
		return len(template.Subagents) > 0
	case "mcps":
		return len(template.Mcps) > 0
	}
	return false
}

// BuiltinProviderImplementationVersion 内置 provider 的实现版本查询——经
// harness_providers 注册表（ResolveProvider）取卡片实现版本。
// Python: resolve_provider(provider).card.implementation_version
//   - provider 闭集仅 codex/claudecode（providerToCLI；dsh 是 harness provider
//     执行域，非成员后端，不进此翻译器）——两者 SDK provider 15.7/15.8 注册
//     前未链接，降级空串保持「声明版本必须为空」语义（与原恒空串行为等价，
//     注册后自动生效）。
func BuiltinProviderImplementationVersion(provider string) string {
	resolved, err := harnessproviders.ResolveProvider(provider)
	if err != nil {
		return ""
	}
	return resolved.Card().ImplementationVersion
}

// applyRuntimeConfig 白名单键覆写 ExternalCliAgentSpec（Python **runtime.config）。
func applyRuntimeConfig(spec *atschema.ExternalCliAgentSpec, config map[string]any) {
	for key, value := range config {
		applyExternalCLIField(spec, key, value)
	}
}

// applyOverlay overlay 键写回（merge 阶段）。
func applyOverlay(spec *atschema.ExternalCliAgentSpec, overlay map[string]any) {
	for key, value := range overlay {
		applyExternalCLIField(spec, key, value)
	}
}

// applyExternalCLIField 按键写单个字段（反射面收窄为显式分支，禁 any 反射）。
func applyExternalCLIField(spec *atschema.ExternalCliAgentSpec, key string, value any) {
	switch key {
	case "skill_conflict":
		if s, ok := value.(string); ok {
			spec.SkillConflict = s
		}
	case "system_prompt_mode":
		if s, ok := value.(string); ok {
			spec.SystemPromptMode = s
		}
	case "mcp_default_tools_approval_mode":
		if s, ok := value.(string); ok {
			spec.MCPDefaultToolsApprovalMode = s
		}
	case "codex_bypass_approvals_and_sandbox":
		if b, ok := value.(bool); ok {
			spec.CodexBypassApprovalsAndSandbox = &b
		}
	case "codex_turn_idle_timeout_s":
		if f, ok := value.(float64); ok {
			spec.CodexTurnIdleTimeoutS = &f
		}
	case "codex_turn_idle_retries":
		if n, ok := value.(float64); ok {
			i := int(n)
			spec.CodexTurnIdleRetries = &i
		}
	case "claude_turn_idle_timeout_s":
		if f, ok := value.(float64); ok {
			spec.ClaudeTurnIdleTimeoutS = &f
		}
	case "claude_max_buffer_size":
		if n, ok := value.(float64); ok {
			i := int(n)
			spec.ClaudeMaxBufferSize = &i
		}
	}
}

// externalCLIFieldValue 读模板配置单字段值（merge 时判断非默认）。
func externalCLIFieldValue(spec *atschema.ExternalCliAgentSpec, key string) any {
	switch key {
	case "skill_conflict":
		return spec.SkillConflict
	case "system_prompt_mode":
		return spec.SystemPromptMode
	case "mcp_default_tools_approval_mode":
		return spec.MCPDefaultToolsApprovalMode
	case "codex_bypass_approvals_and_sandbox":
		return spec.CodexBypassApprovalsAndSandbox
	case "codex_turn_idle_timeout_s":
		return spec.CodexTurnIdleTimeoutS
	case "codex_turn_idle_retries":
		return spec.CodexTurnIdleRetries
	case "claude_turn_idle_timeout_s":
		return spec.ClaudeTurnIdleTimeoutS
	case "claude_max_buffer_size":
		return spec.ClaudeMaxBufferSize
	}
	return nil
}

// isDefaultExternalCLIField 字段是否等于 schema 默认值
// （Python ExternalCliAgentSpec.model_fields[name].get_default()）。
func isDefaultExternalCLIField(spec *atschema.ExternalCliAgentSpec, key string) bool {
	v := externalCLIFieldValue(spec, key)
	switch v.(type) {
	case nil:
		return true
	case string:
		return v.(string) == ""
	case *bool:
		return v.(*bool) == nil
	case *float64:
		return v.(*float64) == nil
	case *int:
		return v.(*int) == nil
	}
	return false
}

// sameSkillJSON skills 去重的 JSON 相等判断（Python `skill not in skills`
// 的 pydantic 相等——json 形态 dict 相等）。
func sameSkillJSON(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}
