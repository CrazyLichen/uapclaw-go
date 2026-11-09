package harness

import (
	"fmt"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSysOperationSpec 把 hschema.SysOperationSpec 解析成活实例。
//
// 上游是 get-or-create（同一稳定 id 复用同一实例）；该语义由资源管理器
// 与调用方保证，这里只负责按 mode 构造。
//
// WorkConfig / GatewayConfig 在 Go 侧仍是 any 占位（见 hschema.SysOperationSpec 注释），
// 因此按 mode 分支取用：Local 看 WorkConfig，Sandbox 看 GatewayConfig。
func resolveSysOperationSpec(spec *hschema.SysOperationSpec) (sysop.SysOperation, error) {
	if spec == nil {
		return nil, nil
	}

	var mode sysop.OperationMode
	switch spec.Mode {
	case "local", "":
		mode = sysop.OperationModeLocal
	case "sandbox":
		mode = sysop.OperationModeSandbox
	default:
		return nil, fmt.Errorf("unknown sys_operation mode %q", spec.Mode)
	}

	opts := []sysop.SysOperationCardOption{sysop.WithSysOpMode(mode)}
	if spec.ID != "" {
		opts = append(opts, sysop.WithSysOpCustomID(spec.ID))
	}
	// 运行配置透传：Local 用 WorkConfig，Sandbox 用 GatewayConfig
	switch mode {
	case sysop.OperationModeLocal:
		if cfg, ok := spec.WorkConfig.(*sysop.LocalWorkConfig); ok {
			opts = append(opts, sysop.WithSysOpWorkConfig(cfg))
		}
	case sysop.OperationModeSandbox:
		if cfg, ok := spec.GatewayConfig.(*sysop.SandboxGatewayConfig); ok {
			opts = append(opts, sysop.WithSysOpGatewayConfig(cfg))
		}
	}

	card := sysop.NewSysOperationCard(opts...)
	return sysop.NewSysOperation(card)
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
