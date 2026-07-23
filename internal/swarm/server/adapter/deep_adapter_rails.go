package adapter

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAgentRails 构建 Agent Rails 列表。
// 对齐 Python: _build_agent_rails(config, config_base, mode) (line 2116-2212)
//
// 根据 mode 决定启用哪些 Rail，调用各 builder 组装列表。
func (d *DeepAdapter) buildAgentRails(config map[string]any, configBase map[string]any, mode string) []sainterfaces.AgentRail {
	var railsList []sainterfaces.AgentRail

	// 步骤 1: heartbeatRail — 心跳
	hb := d.buildHeartbeatRail()
	if hb != nil {
		d.heartbeatRail = hb
		railsList = append(railsList, hb)
	}

	// 步骤 2: taskPlanningRail — 任务规划
	tp := d.buildTaskPlanningRail(config, d.resolveRuntimeLanguage())
	if tp != nil {
		d.taskPlanningRail = tp
		railsList = append(railsList, tp)
	}

	// 步骤 3: filesystemRail — 文件系统（非 ACP 模式启用）
	if d.filesystemRailEnabledForProfile(d.instanceOverrides) {
		readOnly := mode == "agent.plan"
		fs := d.buildFilesystemRail(readOnly)
		if fs != nil {
			d.filesystemRail = fs
			railsList = append(railsList, fs)
		}
	}

	// 步骤 4: agentModeRail — 模式约束（plan 模式）
	if mode == "agent.plan" {
		am := d.buildAgentModeRail(nil)
		if am != nil {
			railsList = append(railsList, am)
		}
	}

	// 步骤 5: mcpRail — MCP 资源浏览
	mcp := d.buildMcpRail()
	if mcp != nil {
		railsList = append(railsList, mcp)
	}

	// 步骤 6: progressiveToolRail — 渐进式工具
	pt := d.buildProgressiveToolRail()
	if pt != nil {
		railsList = append(railsList, pt)
	}

	// 步骤 7-19: 未实现 Rail builder（⤵️ 10.6.3-10）
	// 对齐 Python: _build_agent_rails 中的 skill/stream_event/subagent/security 等分支

	// 步骤 7: skillRail
	skill := d.buildSkillRail()
	if skill != nil {
		d.skillRail = skill
		railsList = append(railsList, skill)
	}

	// 步骤 8: skillEvolutionRail
	evolve := d.buildSkillEvolutionRail()
	if evolve != nil {
		d.skillEvolutionRail = evolve
		railsList = append(railsList, evolve)
	}

	// 步骤 9: skillCreateRail
	create := d.buildSkillCreateRail()
	if create != nil {
		d.skillCreateRail = create
		railsList = append(railsList, create)
	}

	// 步骤 10: streamEventRail
	se := d.buildStreamEventRail()
	if se != nil {
		d.streamEventRail = se
		railsList = append(railsList, se)
	}

	// 步骤 11: subagentRail
	sa := d.buildSubagentRail()
	if sa != nil {
		d.subagentRail = sa
		railsList = append(railsList, sa)
	}

	// 步骤 12: securityRail
	sec := d.buildSecurityRail(configBase)
	if sec != nil {
		d.securityRail = sec
		railsList = append(railsList, sec)
	}

	// 步骤 13: memoryRail
	mem := d.buildMemoryRail()
	if mem != nil {
		d.memoryRail = mem
		railsList = append(railsList, mem)
	}

	// 步骤 14: externalMemoryRail
	emem := d.buildExternalMemoryRail()
	if emem != nil {
		d.externalMemoryRail = emem
		railsList = append(railsList, emem)
	}

	// 步骤 15: avatarRail
	av := d.buildAvatarRail()
	if av != nil {
		d.avatarRail = av
		railsList = append(railsList, av)
	}

	// 步骤 16: runtimePromptRail
	rp := d.buildRuntimePromptRail()
	if rp != nil {
		d.runtimePromptRail = rp
		railsList = append(railsList, rp)
	}

	// 步骤 17: responsePromptRail
	resp := d.buildResponsePromptRail()
	if resp != nil {
		d.responsePromptRail = resp
		railsList = append(railsList, resp)
	}

	// 步骤 18: contextAssembleRail
	ca := d.buildContextAssembleRail(mode)
	if ca != nil {
		d.contextAssembleRail = ca
		railsList = append(railsList, ca)
	}

	// 步骤 19: contextProcessorRail
	// buildContextProcessorRail() 始终返回非 nil，无需 nil 检查
	cp := d.buildContextProcessorRail()
	d.contextProcessorRail = cp
	railsList = append(railsList, cp)

	// 步骤 20: permissionRail
	perm := d.buildPermissionRail(configBase)
	if perm != nil {
		d.permissionRail = perm
		railsList = append(railsList, perm)
	}

	logger.Info(logComponent).
		Str("mode", mode).
		Int("rails_count", len(railsList)).
		Msg("buildAgentRails 完成")

	return railsList
}

// 已实现 Rail Builder

// buildHeartbeatRail 构建心跳护栏。
// 对齐 Python: _build_heartbeat_rail() (line 1632-1648)
func (d *DeepAdapter) buildHeartbeatRail() *rails.HeartbeatRail {
	return rails.NewHeartbeatRail()
}

// buildTaskPlanningRail 构建任务规划护栏。
// 对齐 Python: _build_task_planning_rail() (line 1649-1710)
func (d *DeepAdapter) buildTaskPlanningRail(config map[string]any, language string) *rails.TaskPlanningRail {
	return rails.NewTaskPlanningRail(rails.WithLanguage(language))
}

// buildFilesystemRail 构建文件系统护栏。
// 对齐 Python: _build_filesystem_rail() (line 1711-1768)
func (d *DeepAdapter) buildFilesystemRail(readOnly bool) *rails.SysOperationRail {
	return rails.NewSysOperationRail(rails.WithReadOnly(readOnly))
}

// buildAgentModeRail 构建模式约束护栏。
// 对齐 Python: _build_agent_mode_rail() (line 1769-1812)
func (d *DeepAdapter) buildAgentModeRail(allowedTools []string) *rails.AgentModeRail {
	return rails.NewAgentModeRail(allowedTools)
}

// buildMcpRail 构建 MCP 资源浏览护栏。
// 对齐 Python: _build_mcp_rail() (line 1813-1868)
func (d *DeepAdapter) buildMcpRail() *rails.McpRail {
	return rails.NewMcpRail()
}

// buildProgressiveToolRail 构建渐进式工具护栏。
// 对齐 Python: _build_progressive_tool_rail() (line 1869-1915)
func (d *DeepAdapter) buildProgressiveToolRail() *rails.ProgressiveToolRail {
	// ⤵️ agentcore: 需要 DeepAgentConfig，实例化后回填
	return nil
}

// 未实现 Rail Builder（⤵️ 10.6.3-10）

// buildSkillRail 构建技能使用护栏。
// ⤵️ 10.6.3-10: SkillUseRail
// 对齐 Python: _build_skill_rail() (line 1916-1960)
func (d *DeepAdapter) buildSkillRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SkillUseRail
	return nil
}

// buildSkillEvolutionRail 构建技能演进护栏。
// ⤵️ 10.6.3-10: SkillEvolutionRail
// 对齐 Python: _build_skill_evolution_rail() (line 1961-2010)
func (d *DeepAdapter) buildSkillEvolutionRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SkillEvolutionRail
	return nil
}

// buildSkillCreateRail 构建技能创建护栏。
// ⤵️ 10.6.3-10: SkillCreateRail
// 对齐 Python: _build_skill_create_rail() (line 2011-2050)
func (d *DeepAdapter) buildSkillCreateRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SkillCreateRail
	return nil
}

// buildStreamEventRail 构建流事件护栏。
// ⤵️ 10.6.3-10: JiuClawStreamEventRail
// 对齐 Python: _build_stream_event_rail() (line 2051-2080)
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 JiuClawStreamEventRail
	return nil
}

// buildSubagentRail 构建子代理护栏。
// ⤵️ 10.6.3-10: SubagentRail
// 对齐 Python: _build_subagent_rail() (line 2081-2100)
func (d *DeepAdapter) buildSubagentRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SubagentRail
	return nil
}

// buildSecurityRail 构建安全护栏。
// ⤵️ 10.6.3-10: SecurityRail
// 对齐 Python: _build_security_rail() (line 2101-2115)
func (d *DeepAdapter) buildSecurityRail(configBase map[string]any) sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SecurityRail
	return nil
}

// buildMemoryRail 构建记忆护栏。
// ⤵️ 10.6.3-10: MemoryRail
// 对齐 Python: _build_memory_rail() (line 2116-2130)
func (d *DeepAdapter) buildMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 MemoryRail
	return nil
}

// buildExternalMemoryRail 构建外接记忆护栏。
// ⤵️ 10.6.3-10: ExternalMemoryRail
// 对齐 Python: _build_external_memory_rail() (line 2131-2145)
func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ExternalMemoryRail
	return nil
}

// buildAvatarRail 构建头像护栏。
// ⤵️ 10.6.3-10: AvatarRail
// 对齐 Python: _build_avatar_rail() (line 2146-2155)
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 AvatarRail
	return nil
}

// buildRuntimePromptRail 构建运行时提示词护栏。
// ⤵️ 10.6.3-10: RuntimePromptRail
// 对齐 Python: _build_runtime_prompt_rail() (line 2156-2170)
func (d *DeepAdapter) buildRuntimePromptRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 RuntimePromptRail
	return nil
}

// buildResponsePromptRail 构建响应提示词护栏。
// ⤵️ 10.6.3-10: ResponsePromptRail
// 对齐 Python: _build_response_prompt_rail() (line 2171-2180)
func (d *DeepAdapter) buildResponsePromptRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ResponsePromptRail
	return nil
}

// buildContextAssembleRail 构建上下文组装护栏。
// 对齐 Python: _build_context_assemble_rail() (line 2181-2195)
func (d *DeepAdapter) buildContextAssembleRail(mode string) sainterfaces.AgentRail {
	d.contextAssembleMode = mode
	return cerails.NewContextAssembleRail()
}

// buildContextProcessorRail 构建上下文处理护栏。
// 对齐 Python: _build_context_processor_rail() (line 2196-2212)
func (d *DeepAdapter) buildContextProcessorRail() sainterfaces.AgentRail {
	return cerails.NewContextProcessorRail()
}

// buildPermissionRail 构建权限护栏。
// ⤵️ 10.6.3-10: PermissionInterruptRail
// 对齐 Python: _build_permission_rail() (line 2213-2250)
func (d *DeepAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 PermissionInterruptRail
	return nil
}

// Rail 模式切换（⤵️ 10.6.3-10）

// updateRailsForMode 按模式注册/注销 Rail。
// 对齐 Python: _update_rails_for_mode() (line 2754-2896)
//
// ⤵️ 10.6.3-10: 依赖未实现 Rail
func (d *DeepAdapter) updateRailsForMode(mode string) {
	// ⤵️ 10.6.3-10: 按 mode 分支注册/注销 Rail
	// agent.plan → 启用 AgentModeRail（只读模式）
	// agent.fast → 禁用 AgentModeRail
	// code → 启用 LspRail/CodeAgentRail 等
	logger.Info(logComponent).Str("mode", mode).Msg("updateRailsForMode 等待 10.6.3-10 回填")
}

// updatePromptForMode 按模式更新系统提示词语言。
// 对齐 Python: _update_prompt_for_mode() (line 3091-3097)
//
// ⤵️ 10.6.3-10: 依赖 RuntimePromptRail
func (d *DeepAdapter) updatePromptForMode(mode string) {
	// ⤵️ 10.6.3-10: 同步 system_prompt_builder 语言
	logger.Info(logComponent).Str("mode", mode).Msg("updatePromptForMode 等待 10.6.3-10 回填")
}
