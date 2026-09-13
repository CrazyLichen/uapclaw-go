package adapter

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	permowner "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAgentRails 构建 Agent Rails 列表。
// Python: _build_agent_rails(config, config_base, mode) (line 2116-2212)
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
	// Python: _build_agent_rails 中的 skill/stream_event/subagent/security 等分支

	// 步骤 7: skillRail
	skill := d.buildSkillRail()
	if skill != nil {
		d.skillRail = skill
		railsList = append(railsList, skill)
	}

	// 步骤 8: skillEvolutionRail
	evolve := d.buildSkillEvolutionRail()
	if evolve != nil {
		var ok bool
		d.skillEvolutionRail, ok = evolve.(*evolution.SkillEvolutionRail)
		if ok {
			railsList = append(railsList, d.skillEvolutionRail)
		}
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
	// buildSubagentRail 始终返回非 nil 的 SubagentRail
	if rail, ok := sa.(*subagent.SubagentRail); ok {
		d.subagentRail = rail
	}
	railsList = append(railsList, sa)

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

	// 步骤 21: userHookRail — 用户配置的 hooks，对齐 Python interface_deep.py L2200-2211
	// Python: try/except 包裹注册流程，失败时 warning 并继续
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("UserHookRail 加载失败，跳过")
			}
		}()
		hooksCfg := hookscfg.LoadHooksConfig(configBase)
		if len(hooksCfg.Events) > 0 {
			userHookRail := serverhooks.NewUserHookRail(*hooksCfg)
			railsList = append(railsList, userHookRail)
			logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
		}
	}()

	logger.Info(logComponent).
		Str("mode", mode).
		Int("rails_count", len(railsList)).
		Msg("buildAgentRails 完成")

	return railsList
}

// ──────────────────────────── 已实现 Rail Builder ────────────────────────────

// buildHeartbeatRail 构建心跳护栏。
// Python: _build_heartbeat_rail() (line 1632-1648)
func (d *DeepAdapter) buildHeartbeatRail() *rails.HeartbeatRail {
	return rails.NewHeartbeatRail()
}

// buildTaskPlanningRail 构建任务规划护栏。
// Python: _build_task_planning_rail() (line 1649-1710)
func (d *DeepAdapter) buildTaskPlanningRail(config map[string]any, language string) *rails.TaskPlanningRail {
	return rails.NewTaskPlanningRail(rails.WithLanguage(language))
}

// buildFilesystemRail 构建文件系统护栏。
// Python: _build_filesystem_rail() (line 1711-1768)
func (d *DeepAdapter) buildFilesystemRail(readOnly bool) *rails.SysOperationRail {
	return rails.NewSysOperationRail(rails.WithReadOnly(readOnly))
}

// buildAgentModeRail 构建模式约束护栏。
// Python: _build_agent_mode_rail() (line 1769-1812)
func (d *DeepAdapter) buildAgentModeRail(allowedTools []string) *rails.AgentModeRail {
	return rails.NewAgentModeRail(allowedTools)
}

// buildMcpRail 构建 MCP 资源浏览护栏。
// Python: _build_mcp_rail() (line 1813-1868)
func (d *DeepAdapter) buildMcpRail() *rails.McpRail {
	return rails.NewMcpRail()
}

// buildProgressiveToolRail 构建渐进式工具护栏。
// Python: _build_progressive_tool_rail() (line 1869-1915)
func (d *DeepAdapter) buildProgressiveToolRail() *rails.ProgressiveToolRail {
	// ProgressiveToolRail 由 CreateDeepAgent 内部自动创建（当 ProgressiveToolEnabled=true），
	// adapter 层不需要构建，此方法保留仅为接口兼容。
	return nil
}

// ──────────────────────────── 未实现 Rail Builder（⤵️ 10.6.3-10） ────────────────────────────

// buildSkillRail 构建技能使用护栏。
// ✅ 已回填：SkillUseRail（对齐 Python: _build_skill_rail() — SkillUseRail）
func (d *DeepAdapter) buildSkillRail() (rail sainterfaces.AgentRail) {
	// Python: try-except 保护，失败返回 nil 不影响其他 Rail
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).Msg("buildSkillRail panic，跳过")
			rail = nil
		}
	}()

	skillsDir := workspace.AgentSkillsDir()
	if skillsDir == "" {
		return nil
	}
	skillMode := d.resolveSkillMode()
	var disabled []string
	if d.skillManager != nil {
		disabled = d.skillManager.ListExecutionDisabledSkills()
	}
	// Python: include_tools = self._skill_include_tools_for_profile()
	includeTools := d.skillIncludeToolsForProfile(d.instanceOverrides)
	// Python: enable_read_image_multimodal = deep_config.enable_read_image_multimodal
	enableImageMultimodal := true
	if v, ok := d.instanceOverrides["enable_read_image_multimodal"]; ok {
		if b, ok := v.(bool); ok {
			enableImageMultimodal = b
		}
	}
	rail = skillrails.NewSkillUseRail(
		[]string{skillsDir},
		skillrails.WithSkillMode(skillMode),
		skillrails.WithIncludeTools(includeTools),
		skillrails.WithEnableImageMultimodal(enableImageMultimodal),
		skillrails.WithDisabledSkills(disabled),
	)
	logger.Info(logComponent).
		Str("skill_mode", skillMode).
		Bool("include_tools", includeTools).
		Bool("enable_image_multimodal", enableImageMultimodal).
		Int("disabled_count", len(disabled)).
		Msg("SkillUseRail create success")
	return rail
}

// resolveSkillMode 校验配置的 skill_mode 并在无效值时安全回退。
// Python: _resolve_skill_mode(config)
func (d *DeepAdapter) resolveSkillMode() string {
	rawSkillMode := skillrails.SkillModeAll // 默认值
	if d.configCache != nil {
		if sm, ok := d.configCache["skill_mode"]; ok {
			if s, ok := sm.(string); ok {
				rawSkillMode = s
			}
		}
	}
	if _, ok := skillrails.ValidSkillModes[rawSkillMode]; ok {
		return rawSkillMode
	}
	logger.Warn(logComponent).
		Str("raw_skill_mode", rawSkillMode).
		Str("fallback", skillrails.SkillModeAll).
		Msg("invalid skill_mode, fallback to all")
	return skillrails.SkillModeAll
}

// buildSkillEvolutionRail 构建技能演进护栏。
// ✅ 已回填：SkillEvolutionRail（对齐 Python: _build_skill_evolution_rail()）
func (d *DeepAdapter) buildSkillEvolutionRail() sainterfaces.AgentRail {
	if d.model == nil {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: model 为空，跳过")
		return nil
	}

	// 读取 evolution 配置
	evolutionConfig, _ := d.configCache["evolution"].(map[string]any)
	enabled, _ := evolutionConfig["enabled"].(bool)
	if !enabled {
		return nil
	}

	// 读取 auto_scan
	autoScan := true
	if as, ok := evolutionConfig["auto_scan"].(bool); ok {
		autoScan = as
	}
	// 环境变量覆盖
	if envAS := os.Getenv("EVOLUTION_AUTO_SCAN"); envAS != "" {
		autoScan = strings.ToLower(envAS) == "true"
	}

	// auto_save 默认 false（需要审批）
	autoSave := false
	if as, ok := evolutionConfig["auto_save"].(bool); ok {
		autoSave = as
	}

	// 禁用技能列表
	var disabledSkills []string
	if d.skillManager != nil {
		disabledSkills = d.skillManager.ListExecutionDisabledSkills()
	}

	// skills 目录
	skillsDir := workspace.AgentSkillsDir()
	if skillsDir == "" {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: skills 目录为空，跳过")
		return nil
	}

	modelName := d.defaultModelName
	if modelName == "" {
		modelName = "qwen-max"
	}

	opts := []evolution.SkillEvolutionRailOption{
		evolution.WithAutoScan(autoScan),
		evolution.WithAutoSave(autoSave),
		evolution.WithDisabledSkillsSet(disabledSkills),
	}

	// sharing 配置
	sharingConfig, _ := evolutionConfig["sharing"].(map[string]any)
	if sharingConfig != nil {
		opts = append(opts, evolution.WithSharingConfig(sharingConfig))
	}

	rail := evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		d.model,
		modelName,
		"cn",
		opts...,
	)

	logger.Info(logComponent).Msg("SkillEvolutionRail 创建成功")
	return rail
}

// buildSkillCreateRail 构建技能创建护栏。
// ⤵️ 10.6.3-10: SkillCreateRail
// Python: _build_skill_create_rail() (line 2011-2050)
func (d *DeepAdapter) buildSkillCreateRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 SkillCreateRail
	return nil
}

// buildStreamEventRail 构建流事件护栏。
// ⤵️ 10.6.3-10: JiuClawStreamEventRail
// Python: _build_stream_event_rail() (line 2051-2080)
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 JiuClawStreamEventRail
	return nil
}

// buildSubagentRail 构建子代理护栏。
// ✅ 已回填：SubagentRail（对齐 Python: _build_subagent_rail() — SubagentRail()）
func (d *DeepAdapter) buildSubagentRail() sainterfaces.AgentRail {
	rail := subagent.NewSubagentRail()
	logger.Info(logComponent).Msg("SubagentRail 创建成功")
	return rail
}

// buildSecurityRail 构建安全护栏。
// ✅ 已回填：SafetyPromptRail（对齐 Python: _build_security_rail() — SecurityRail()）
//
// Python: _build_security_rail() (line 2033-2042)
// Python: try/except 包裹创建过程，失败时 warning 并返回 nil
func (d *DeepAdapter) buildSecurityRail(configBase map[string]any) sainterfaces.AgentRail {
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("SafetyPromptRail 创建失败，跳过")
			}
		}()
	}()
	rail := secrail.NewSafetyPromptRail()
	logger.Info(logComponent).Msg("SafetyPromptRail 创建成功")
	return rail
}

// buildMemoryRail 构建记忆护栏。
// ⤵️ 10.6.3-10: MemoryRail
// Python: _build_memory_rail() (line 2116-2130)
func (d *DeepAdapter) buildMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 MemoryRail
	return nil
}

// buildExternalMemoryRail 构建外接记忆护栏。
// ⤵️ 10.6.3-10: ExternalMemoryRail
// Python: _build_external_memory_rail() (line 2131-2145)
func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ExternalMemoryRail
	return nil
}

// buildAvatarRail 构建数字分身护栏。
// Python: _build_avatar_rail() (line 2146-2155)
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
	rail := commrails.NewAvatarPromptRail()
	logger.Info(logComponent).Msg("AvatarPromptRail 创建成功")
	return rail
}

// buildRuntimePromptRail 构建运行时提示词护栏。
// ⤵️ 10.6.3-10: RuntimePromptRail
// Python: _build_runtime_prompt_rail() (line 2156-2170)
func (d *DeepAdapter) buildRuntimePromptRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 RuntimePromptRail
	return nil
}

// buildResponsePromptRail 构建响应提示词护栏。
// ⤵️ 10.6.3-10: ResponsePromptRail
// Python: _build_response_prompt_rail() (line 2171-2180)
func (d *DeepAdapter) buildResponsePromptRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ResponsePromptRail
	return nil
}

// buildContextAssembleRail 构建上下文组装护栏。
// Python: _build_context_assemble_rail() (line 2181-2195)
func (d *DeepAdapter) buildContextAssembleRail(mode string) sainterfaces.AgentRail {
	d.contextAssembleMode = mode
	return cerails.NewContextAssembleRail()
}

// buildContextProcessorRail 构建上下文处理护栏。
// Python: _build_context_processor_rail() (line 2196-2212)
func (d *DeepAdapter) buildContextProcessorRail() sainterfaces.AgentRail {
	return cerails.NewContextProcessorRail()
}

// buildPermissionRail 构建权限护栏。
// ✅ 已回填：PermissionInterruptRail（对齐 Python: _build_permission_rail() — build_permission_rail()）
//
// Python: build_permission_rail(config, llm, model_name) (interrupt_helpers.py L16-281)
// Python: _build_permission_rail() (interface_deep.py L2213-2250)
func (d *DeepAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
	permissionConfig, _ := configBase["permissions"].(map[string]any)
	if permissionConfig == nil {
		return nil
	}
	enabled, _ := permissionConfig["enabled"].(bool)
	if !enabled {
		logger.Info(logComponent).Msg("PermissionRail: permissions.enabled=false，跳过创建")
		return nil
	}

	toolNames := collectOptionalToolTags(permissionConfig)
	modelName := extractModelName(configBase)

	host := &harnesssecurity.ToolPermissionHost{
		GetPermissionsSnapshot:        d.getPermissionsSnapshot,
		PersistAllowRule:              d.persistAllowRule,
		ResolveWorkspaceDir:           d.resolveWorkspaceDir,
		PermissionYAMLPath:            d.getPermissionYAMLPath(),
		ToolPermissionChecksActive:    func() bool { return true },
		RequestPermissionConfirmation: d.requestPermissionConfirmation,
		PermissionSceneHook:           d.permissionSceneHook,
	}

	workspaceRoot := workspace.WorkspaceDir()

	rail := harnesssecurity.BuildPermissionInterruptRail(
		permissionConfig, nil, host, workspaceRoot, d.model, modelName,
		func(config map[string]any, engine *harnesssecurity.PermissionEngine, toolNames []string, llmModel *llm.Model, mn string, h *harnesssecurity.ToolPermissionHost) sainterfaces.AgentRail {
			return secrail.NewPermissionInterruptRail(config, engine, toolNames, llmModel, mn, h)
		},
	)
	if rail != nil {
		logger.Info(logComponent).
			Strs("tool_names", toolNames).
			Msg("PermissionInterruptRail 创建成功")
	} else {
		logger.Warn(logComponent).Msg("PermissionInterruptRail 创建失败")
	}
	return rail
}

// ──────────────────────────── Rail 模式切换（⤵️ 10.6.3-10） ────────────────────────────

// updateRailsForMode 按模式注册/注销 Rail。
// Python: _update_rails_for_mode() (line 2754-2896)
func (d *DeepAdapter) updateRailsForMode(mode string) {
	// Python: if mode == "agent.plan": await self._update_plan_mode_rails()
	// Python: else: await self._update_agent_mode_rails(mode)
	if mode == "agent.plan" {
		d.updatePlanModeRails()
	} else {
		d.updateAgentModeRails(mode)
	}
}

// updatePlanModeRails plan 模式：注册 plan 专属 rails。
// Python: _update_plan_mode_rails() (line 2761-2852)
func (d *DeepAdapter) updatePlanModeRails() {
	ctx := context.Background()

	// 1. TaskPlanningRail — plan 模式下注册
	if d.taskPlanningRail == nil {
		rail := d.buildTaskPlanningRail(d.configCache, d.resolveRuntimeLanguage())
		if rail != nil && d.instance != nil {
			if err := d.instance.RegisterRail(ctx, rail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注册 TaskPlanningRail 失败")
			} else {
				d.taskPlanningRail = rail
				logger.Info(logComponent).Msg("TaskPlanningRail 注册成功（plan 模式）")
			}
		}
	}

	// 2. 卸载 multi-session 工具
	// Python: for existing in list(self._instance.ability_manager.list() or []):
	//   if getattr(existing, "name", "").startswith(("session_new", "session_cancel", "session_list")):
	//     self._instance.ability_manager.remove(existing.name)
	if d.instance != nil {
		tools := d.instance.AbilityManager().List()
		for _, t := range tools {
			name := t.AbilityName()
			if strings.HasPrefix(name, "session_new") ||
				strings.HasPrefix(name, "session_cancel") ||
				strings.HasPrefix(name, "session_list") {
				d.instance.AbilityManager().Remove(name)
			}
		}
	}

	// 3. 记忆 rail 处理
	// Python: await self._handle_memory_rail_by_config("plan")
	// ⤵️ 待回填: handleMemoryRailByConfig

	// 4. 外接记忆 rail
	// Python: await self._handle_external_memory_rail_by_config()
	// ⤵️ 待回填: handleExternalMemoryRailByConfig

	// 5. ContextAssembleRail — plan 模式专属实例
	if d.contextAssembleRail == nil || d.contextAssembleMode != "agent.plan" {
		if d.contextAssembleRail != nil && d.instance != nil {
			if err := d.instance.UnregisterRail(ctx, d.contextAssembleRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注销 ContextAssembleRail 失败")
			}
			d.contextAssembleRail = nil
		}
		d.contextAssembleRail = d.buildContextAssembleRail("agent.plan")
		d.contextAssembleMode = "agent.plan"
		if d.contextAssembleRail != nil && d.instance != nil {
			if err := d.instance.RegisterRail(ctx, d.contextAssembleRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注册 ContextAssembleRail 失败")
			} else {
				logger.Info(logComponent).Msg("ContextAssembleRail 注册成功（plan 模式）")
			}
		}
	}

	// 6. ContextProcessorRail — 按配置启用/禁用
	contextEngineConfig, _ := d.configCache["context_engine_config"].(map[string]any)
	contextEnabled, _ := contextEngineConfig["enabled"].(bool)
	if contextEnabled {
		if d.contextProcessorRail == nil {
			d.contextProcessorRail = d.buildContextProcessorRail()
			if d.contextProcessorRail != nil && d.instance != nil {
				if err := d.instance.RegisterRail(ctx, d.contextProcessorRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("注册 ContextProcessorRail 失败")
				} else {
					logger.Info(logComponent).Msg("ContextProcessorRail 注册成功（plan 模式）")
				}
			}
		}
	} else {
		if d.contextProcessorRail != nil && d.instance != nil {
			if err := d.instance.UnregisterRail(ctx, d.contextProcessorRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注销 ContextProcessorRail 失败")
			}
			d.contextProcessorRail = nil
			logger.Info(logComponent).Msg("ContextProcessorRail 已注销（plan 模式，配置禁用）")
		}
	}

	// 7. SkillEvolutionRail — 按 evolution 配置注册/注销
	evolutionConfig, _ := d.configCache["evolution"].(map[string]any)
	evolutionEnabled, _ := evolutionConfig["enabled"].(bool)
	if evolutionEnabled {
		if d.skillEvolutionRail == nil {
			rail := d.buildSkillEvolutionRail()
			if rail != nil {
				var ok bool
				d.skillEvolutionRail, ok = rail.(*evolution.SkillEvolutionRail)
				if ok && d.skillEvolutionRail != nil && d.instance != nil {
					if err := d.instance.RegisterRail(ctx, d.skillEvolutionRail); err != nil {
						logger.Error(logComponent).Err(err).Msg("注册 SkillEvolutionRail 失败")
					} else {
						logger.Info(logComponent).Msg("SkillEvolutionRail 注册成功（plan 模式）")
					}
				}
			}
		}
	} else {
		if d.skillEvolutionRail != nil {
			if d.instance != nil {
				if err := d.instance.UnregisterRail(ctx, d.skillEvolutionRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("注销 SkillEvolutionRail 失败")
				}
			}
			d.skillEvolutionRail = nil
			logger.Info(logComponent).Msg("SkillEvolutionRail 已注销（evolution.enabled=false）")
		}
	}

	// 8. SkillCreateRail
	// Python: skill_create_enabled = _get_skill_create_enabled(self._config_cache)
	// ⤵️ 待回填: SkillCreateRail 处理

	// 9. SubagentRail — plan 模式下注册
	if d.subagentRail == nil {
		rail := d.buildSubagentRail()
		if rail != nil {
			var ok bool
			d.subagentRail, ok = rail.(*subagent.SubagentRail)
			if ok && d.subagentRail != nil && d.instance != nil {
				if err := d.instance.RegisterRail(ctx, d.subagentRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("注册 SubagentRail 失败")
				} else {
					logger.Info(logComponent).Msg("SubagentRail 注册成功（plan 模式）")
				}
			}
		}
	}

	logger.Info(logComponent).Msg("updatePlanModeRails 执行完成")
}

// updateAgentModeRails agent 模式：卸载 plan 专属 rails，按需注册 agent 专属 rails。
// Python: _update_agent_mode_rails() (line 2854-2895)
func (d *DeepAdapter) updateAgentModeRails(mode string) {
	ctx := context.Background()

	// 1. 卸载 plan 专属 rails
	// Python: rail_specs = (("_task_planning_rail", "TaskPlanningRail"), ...)
	if d.taskPlanningRail != nil && d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.taskPlanningRail); err != nil {
			logger.Error(logComponent).Err(err).Str("rail", "TaskPlanningRail").Msg("注销 Rail 失败")
		} else {
			logger.Info(logComponent).Str("mode", mode).Str("rail", "TaskPlanningRail").Msg("Rail 已注销")
		}
		d.taskPlanningRail = nil
	}
	if d.skillEvolutionRail != nil && d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.skillEvolutionRail); err != nil {
			logger.Error(logComponent).Err(err).Str("rail", "SkillEvolutionRail").Msg("注销 Rail 失败")
		} else {
			logger.Info(logComponent).Str("mode", mode).Str("rail", "SkillEvolutionRail").Msg("Rail 已注销")
		}
		d.skillEvolutionRail = nil
	}
	if d.skillCreateRail != nil && d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.skillCreateRail); err != nil {
			logger.Error(logComponent).Err(err).Str("rail", "SkillCreateRail").Msg("注销 Rail 失败")
		} else {
			logger.Info(logComponent).Str("mode", mode).Str("rail", "SkillCreateRail").Msg("Rail 已注销")
		}
		d.skillCreateRail = nil
	}
	if d.subagentRail != nil && d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.subagentRail); err != nil {
			logger.Error(logComponent).Err(err).Str("rail", "SubagentRail").Msg("注销 Rail 失败")
		} else {
			logger.Info(logComponent).Str("mode", mode).Str("rail", "SubagentRail").Msg("Rail 已注销")
		}
		d.subagentRail = nil
	}

	// 2. 记忆 rail 处理
	// Python: await self._handle_memory_rail_by_config("fast")
	// ⤵️ 待回填: handleMemoryRailByConfig

	// 3. 外接记忆 rail
	// Python: await self._handle_external_memory_rail_by_config()
	// ⤵️ 待回填: handleExternalMemoryRailByConfig

	// 4. ContextAssembleRail — agent.fast 模式专属实例
	if d.contextAssembleRail == nil || d.contextAssembleMode == "agent.plan" {
		if d.contextAssembleRail != nil && d.instance != nil {
			if err := d.instance.UnregisterRail(ctx, d.contextAssembleRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注销 ContextAssembleRail 失败")
			}
			d.contextAssembleRail = nil
		}
		d.contextAssembleRail = d.buildContextAssembleRail("agent.fast")
		d.contextAssembleMode = "agent.fast"
		if d.contextAssembleRail != nil && d.instance != nil {
			if err := d.instance.RegisterRail(ctx, d.contextAssembleRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注册 ContextAssembleRail 失败")
			}
		}
	}

	// 5. ContextProcessorRail — agent 模式下按需注册
	if d.contextProcessorRail == nil {
		d.contextProcessorRail = d.buildContextProcessorRail()
		if d.contextProcessorRail != nil && d.instance != nil {
			if err := d.instance.RegisterRail(ctx, d.contextProcessorRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("注册 ContextProcessorRail 失败")
			} else {
				logger.Info(logComponent).Str("mode", mode).Msg("ContextProcessorRail 注册成功")
			}
		}
	}

	logger.Info(logComponent).Str("mode", mode).Msg("updateAgentModeRails 执行完成")
}

// updatePromptForMode 按模式更新系统提示词语言。
// Python: _update_prompt_for_mode() (line 3091-3097)
//
// ⤵️ 10.6.3-10: 依赖 RuntimePromptRail
func (d *DeepAdapter) updatePromptForMode(mode string) {
	// ⤵️ 10.6.3-10: 同步 system_prompt_builder 语言
	logger.Info(logComponent).Str("mode", mode).Msg("updatePromptForMode 等待 10.6.3-10 回填")
}

// ──────────────────────────── ToolPermissionHost 回调方法 ────────────────────────────

// getPermissionsSnapshot 返回当前权限配置快照。
// Python: get_permissions_snapshot = lambda: get_config().get("permissions", {}) (interrupt_helpers.py L215)
func (d *DeepAdapter) getPermissionsSnapshot() map[string]any {
	if d.configCache == nil {
		return map[string]any{}
	}
	perm, _ := d.configCache["permissions"].(map[string]any)
	if perm == nil {
		return map[string]any{}
	}
	return perm
}

// persistAllowRule 将合并后的 permissions 配置写回 config.yaml。
// Python: _persist_allow_rule(permissions) (interrupt_helpers.py L68-84)
func (d *DeepAdapter) persistAllowRule(permissions map[string]any) bool {
	yamlPath := d.getPermissionYAMLPath()
	if yamlPath == "" {
		logger.Warn(logComponent).Msg("persistAllowRule: config path 为空")
		return false
	}
	ok := harnesssecurity.WritePermissionsSectionToAgentConfigYAML(yamlPath, permissions)
	if !ok {
		logger.Warn(logComponent).Str("yaml_path", yamlPath).Msg("persistAllowRule 写盘失败")
	}
	return ok
}

// resolveWorkspaceDir 返回 workspace 根目录。
// Python: get_workspace_dir (interrupt_helpers.py L221)
func (d *DeepAdapter) resolveWorkspaceDir() string {
	return workspace.WorkspaceDir()
}

// getPermissionYAMLPath 返回 Agent 配置文件路径。
// Python: get_config_file (interrupt_helpers.py L222)
func (d *DeepAdapter) getPermissionYAMLPath() string {
	return workspace.ConfigFile()
}

// requestPermissionConfirmation 对 ASK 级别征求用户确认。
// Python: _request_permission_confirmation (interrupt_helpers.py L126-208)
//
// 非 ACP 通道返回 ConfirmActionInterrupt（走内置中断），ACP 通道走 JSON-RPC。
// 当前 ACP output manager 尚未实现，统一降级为 ConfirmActionInterrupt。
func (d *DeepAdapter) requestPermissionConfirmation(req harnesssecurity.PermissionConfirmationRequest) (*harnesssecurity.PermissionConfirmResponse, error) {
	// Python: channel = TOOL_PERMISSION_CHANNEL_ID.get() or "web"
	channelID := sschema.ToolPermissionChannelIDFromCtx(req.GoCtx)
	if channelID == "" {
		channelID = "web"
	}
	// Python: if channel != "acp": return "interrupt"
	if channelID != "acp" {
		return &harnesssecurity.PermissionConfirmResponse{
			Action: harnesssecurity.ConfirmActionInterrupt,
		}, nil
	}
	// ⤵️ ACP: ACP output manager 尚未实现，降级为 ConfirmActionInterrupt
	toolName := ""
	if req.ToolCall != nil {
		toolName = req.ToolCall.Name
	}
	if toolName == "" {
		toolName = "unknown"
	}
	logger.Warn(logComponent).
		Str("channel_id", channelID).
		Str("tool_name", toolName).
		Msg("ACP 通道权限确认尚未实现，降级为 interrupt")
	return &harnesssecurity.PermissionConfirmResponse{
		Action: harnesssecurity.ConfirmActionInterrupt,
	}, nil
}

// permissionSceneHook 宿主场景钩子（数字分身/owner_scopes）。
// Python: _permission_scene_hook (interrupt_helpers.py L210-254)
//
// 返回 nil 表示继续走引擎 tiered 判定；
// 返回 ("approve",) 直接放行；("reject", msg) 拒绝。
func (d *DeepAdapter) permissionSceneHook(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
	// Python: perm_ctx = TOOL_PERMISSION_CONTEXT.get()
	permCtx := sschema.PermissionContextFromCtx(input.GoCtx)
	if permCtx == nil {
		return nil, nil
	}

	// Python: if getattr(perm_ctx, "scene", None) == "group_digital_avatar":
	if permCtx.Scene() == "group_digital_avatar" {
		if input.UserInput != nil {
			return []string{"reject", "[PERMISSION_DENIED] 数字分身场景不支持交互审批"}, nil
		}
		// Python: level = await check_avatar_permission(normalized_tool_name, tool_args, channel_id=..., session_id=None)
		level, err := permowner.CheckAvatarPermission(
			d.getPermissionsSnapshot(),
			input.NormalizedToolName,
			input.ToolArgs,
			permCtx.ChannelID,
			"",
			permCtx.PrincipalUserID,
		)
		if err != nil {
			return nil, err
		}
		if level == "allow" {
			return []string{"approve"}, nil
		}
		return []string{"reject", "[PERMISSION_DENIED] 该工具未被授权在数字分身场景下使用"}, nil
	}

	// Python: owner_scopes 场景
	principalUID := strings.TrimSpace(permCtx.PrincipalUserID)
	chID := strings.TrimSpace(permCtx.ChannelID)
	if principalUID == "" || chID == "" {
		return nil, nil
	}

	permAll := d.getPermissionsSnapshot()
	ownerScopes, _ := permAll["owner_scopes"].(map[string]any)
	if len(ownerScopes) == 0 {
		return nil, nil
	}

	chScopes, _ := ownerScopes[chID].(map[string]any)
	scopeCfg, _ := chScopes[principalUID].(map[string]any)

	ownerLevel := permowner.ResolveOwnerScopeLevel(scopeCfg, input.NormalizedToolName, input.ToolArgs)
	if ownerLevel == "" {
		return nil, nil
	}
	if ownerLevel == "allow" {
		return []string{"approve"}, nil
	}
	return []string{"reject", fmt.Sprintf("[PERMISSION_DENIED] 该工具未被授权 (owner_scopes: %s)", ownerLevel)}, nil
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// collectOptionalToolTags 从 permissions 配置中收集工具名标签（仅作展示/日志用）。
// Python: _collect_optional_tool_tags() (interrupt_helpers.py L56-80)
func collectOptionalToolTags(permissionConfig map[string]any) []string {
	names := make(map[string]struct{})
	if toolsCfg, ok := permissionConfig["tools"].(map[string]any); ok {
		for k := range toolsCfg {
			if s := strings.TrimSpace(k); s != "" {
				names[s] = struct{}{}
			}
		}
	}
	if rules, ok := permissionConfig["rules"].([]any); ok {
		for _, entry := range rules {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			rawTools := m["tools"]
			switch v := rawTools.(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					names[s] = struct{}{}
				}
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						if s = strings.TrimSpace(s); s != "" {
							names[s] = struct{}{}
						}
					}
				}
			}
		}
	}
	result := make([]string, 0, len(names))
	for n := range names {
		result = append(result, n)
	}
	sort.Strings(result)
	return result
}

// extractModelName 从 configBase 提取默认模型名称。
// Python: config_base.get("models", {}).get("default", {}).get("model_client_config", {}).get("model_name", "gpt-4")
func extractModelName(configBase map[string]any) string {
	modelsCfg, _ := configBase["models"].(map[string]any)
	if modelsCfg == nil {
		return "gpt-4"
	}
	defaultCfg, _ := modelsCfg["default"].(map[string]any)
	if defaultCfg == nil {
		return "gpt-4"
	}
	clientCfg, _ := defaultCfg["model_client_config"].(map[string]any)
	if clientCfg == nil {
		return "gpt-4"
	}
	modelName, _ := clientCfg["model_name"].(string)
	if modelName == "" {
		return "gpt-4"
	}
	return modelName
}

// buildDynamicRail 根据方法名调用对应的 Rail builder。
// Python: method = getattr(self, method_name, None)
func (d *DeepAdapter) buildDynamicRail(methodName string, configBase map[string]any) sainterfaces.AgentRail {
	switch methodName {
	case "buildFilesystemRail":
		return d.buildFilesystemRail(false)
	case "buildSkillRail":
		return d.buildSkillRail()
	case "buildHeartbeatRail":
		return d.buildHeartbeatRail()
	case "buildAvatarRail":
		return d.buildAvatarRail()
	case "buildTaskPlanningRail":
		return d.buildTaskPlanningRail(d.configCache, d.resolveRuntimeLanguage())
	case "buildSubagentRail":
		return d.buildSubagentRail()
	case "buildContextAssembleRail":
		return d.buildContextAssembleRail(d.mode)
	case "buildContextProcessorRail":
		return d.buildContextProcessorRail()
	case "buildSkillEvolutionRail":
		return d.buildSkillEvolutionRail()
	case "buildWorktreeRail":
		// CodeAdapter 专有，DeepAdapter 不实现
		return nil
	case "buildCodeAgentRail":
		// CodeAdapter 专有，DeepAdapter 不实现
		return nil
	default:
		logger.Warn(logComponent).Str("method", methodName).
			Msg("未知的动态 Rail builder 方法名")
		return nil
	}
}
