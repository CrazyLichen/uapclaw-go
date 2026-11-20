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

// （历史注记：convertToConfigSubagents/specSubagentAdapter 字段搬运壳已随
// 15.16f 删除——SubAgentSpec.Build 的 factory 分支激活后由 buildSubagentsFromSpecs
// 直接产出 SubAgentConfig，规则 8，锚点 grep 0 命中。）
