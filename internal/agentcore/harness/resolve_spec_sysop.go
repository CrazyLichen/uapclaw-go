package harness

import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSysOperationSpec 把 hschema.SysOperationSpec 解析成活实例。
//
// 构造逻辑已收敛到 schema 的 SysOperationSpec.Resolve/BuildCard
// （Python deep_agent_spec.py:216-249），此处仅保留 nil 语义并转调。
func resolveSysOperationSpec(spec *hschema.SysOperationSpec) (sysop.SysOperation, error) {
	if spec == nil {
		return nil, nil
	}
	return spec.Resolve()
}

// convertToConfigSubagents 把 spec 形态的子代理转成配置形态。
//
// hschema.SubagentSpec 是只要求 SpecName() 的窄接口（config.go:26-29），
// 这里用 specSubagentAdapter 包一层满足它。
//
// ⤵️ 领域十五：上游此处由 hschema.SubAgentSpec.build 产出 SubAgentConfig；
// 本版只做字段搬运，provider 注册表就绪后替换为 provider 调用。
func convertToConfigSubagents(specs []hschema.SubAgentSpec) []hschema.SubagentSpec {
	out := make([]hschema.SubagentSpec, 0, len(specs))
	for i := range specs {
		out = append(out, &specSubagentAdapter{spec: &specs[i]})
	}
	return out
}

// specSubagentAdapter 把 hschema.SubAgentSpec 适配成 config 侧的 hschema.SubagentSpec 接口。
type specSubagentAdapter struct {
	// spec 被适配的子代理规格
	spec *hschema.SubAgentSpec
}

// SpecName 返回规格名称（取卡片名，无卡片时用工厂名兜底）。
func (a *specSubagentAdapter) SpecName() string {
	if a.spec == nil {
		return ""
	}
	if a.spec.AgentCard != nil && a.spec.AgentCard.GetName() != "" {
		return a.spec.AgentCard.GetName()
	}
	return a.spec.FactoryName
}
