package adapter

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	permowner "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	e2aacp "github.com/uapclaw/uapclaw-go/internal/swarm/e2a/acp"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// buildAgentRails 构建 Agent Rails 列表。
// Python: _build_agent_rails(config, config_base, mode) (line 2116-2212)
//
// 根据 mode 决定启用哪些 Rail，调用各 builder 组装列表。
func (d *DeepAdapter) buildAgentRails(config map[string]any, configBase map[string]any, mode string) []sainterfaces.AgentRail {
	var railsList []sainterfaces.AgentRail

	// ─── rail_infos 列表构建阶段（参数立即求值，对齐 Python L2127-2176）───
	// Python 在 build 循环之前构造 rail_infos 列表，params dict 中的
	// include_tools=self._skill_include_tools_for_profile() 在列表构建
	// 阶段立即求值：冷启动进入 _build_agent_rails 时 self._filesystem_rail
	// 仍是 __init__ 置的 None → include_tools=True（即使 filesystem rail
	// 已启用）；热重载（getCurrentAgentRails → buildAgentRails）进入时
	// _filesystem_rail 已非 None → False。
	// 故此求值必须发生在步骤 2 赋值 d.filesystemRail 之前——若放在赋值
	// 之后，冷启动也会得到 False，code 工具永不注册（Python 实际行为：
	// 冷启动 include_tools 恒为 True，除非 ACP tool profile）。
	skillIncludeTools := d.skillIncludeToolsForProfile(d.instanceOverrides)

	// ─── 以下注册顺序对齐 Python _build_agent_rails (interface_deep.py L2130-2156) ───

	// 步骤 1: runtimePromptRail — Python: _runtime_prompt_rail (index 0)
	rp := d.buildRuntimePromptRail()
	if rp != nil {
		d.runtimePromptRail = rp
		railsList = append(railsList, rp)
	}

	// 步骤 2: filesystemRail — Python: conditional insert at 1
	filesystemEnabled := d.filesystemRailEnabledForProfile(d.instanceOverrides)
	if filesystemEnabled {
		readOnly := mode == "agent.work.plan"
		fs := d.buildFilesystemRail(readOnly)
		if fs != nil {
			d.filesystemRail = fs
			railsList = append(railsList, fs)
		}
	}

	// 步骤 3: skillRail — Python: conditional insert at 2 (or 1 if no filesystem)
	// include_tools 已在函数开头（步骤 2 赋值 d.filesystemRail 之前）求值，
	// 对齐 Python rail_infos 列表构建时 params 立即求值的语义。
	skill := d.buildSkillRailWithIncludeTools(skillIncludeTools)
	if skill != nil {
		d.skillRail = skill
		railsList = append(railsList, skill)
	}

	// 步骤 4: responsePromptRail — Python: _response_prompt_rail
	resp := d.buildResponsePromptRail()
	if resp != nil {
		d.responsePromptRail = resp
		railsList = append(railsList, resp)
	}

	// 步骤 5: streamEventRail — Python: _stream_event_rail
	se := d.buildStreamEventRail()
	if se != nil {
		d.streamEventRail = se
		railsList = append(railsList, se)
	}

	// 步骤 6: taskPlanningRail — Python: _task_planning_rail
	tp := d.buildTaskPlanningRail(config, d.resolveRuntimeLanguage())
	if tp != nil {
		d.taskPlanningRail = tp
		railsList = append(railsList, tp)
	}

	// 步骤 7: securityRail — Python: _security_rail
	sec := d.buildSecurityRail(configBase)
	if sec != nil {
		d.securityRail = sec
		railsList = append(railsList, sec)
	}

	// 步骤 8: heartbeatRail — Python: _heartbeat_rail
	hb := d.buildHeartbeatRail()
	if hb != nil {
		d.heartbeatRail = hb
		railsList = append(railsList, hb)
	}

	// 步骤 9: avatarRail — Python: _avatar_rail
	av := d.buildAvatarRail()
	if av != nil {
		d.avatarRail = av
		railsList = append(railsList, av)
	}

	// 步骤 10: subagentRail — Python: _subagent_rail
	// buildSubagentRail 已返回具体类型，无需类型断言（原先的断言失败会静默丢 rail）
	sa := d.buildSubagentRail()
	d.subagentRail = sa
	railsList = append(railsList, sa)

	// 步骤 11: permissionRail — Python: _permission_rail
	perm := d.buildPermissionRail(configBase)
	if perm != nil {
		d.permissionRail = perm
		railsList = append(railsList, perm)
	}

	// 步骤 12: contextProcessorRail — Python: _context_processor_rail
	cp := d.buildContextProcessorRail(d.configCacheSnapshot())
	d.contextProcessorRail = cp
	railsList = append(railsList, cp)

	// ─── Go 独有 Rail（Python 中不存在或不同） ───

	// agentModeRail — plan 模式约束（Go 独有）
	if mode == "agent.work.plan" {
		am := d.buildAgentModeRail(nil)
		if am != nil {
			railsList = append(railsList, am)
		}
	}

	// mcpRail — MCP 资源浏览（Go 独有）
	mcp := d.buildMcpRail()
	if mcp != nil {
		railsList = append(railsList, mcp)
	}

	// progressiveToolRail — 渐进式工具（Go 独有）
	pt := d.buildProgressiveToolRail()
	if pt != nil {
		railsList = append(railsList, pt)
	}

	// contextAssembleRail — 上下文组装（Go 独有）
	ca := d.buildContextAssembleRail(mode)
	if ca != nil {
		d.contextAssembleRail = ca
		railsList = append(railsList, ca)
	}

	// ─── 不在冷启动时挂载的 Rail（由 updateRailsForMode 按需注册） ───

	// skillEvolutionRail / skillCreateRail — 冷启动既不构建也不注册。
	// 两条 rail 的字段语义是「非 nil ⇒ 已注册」，构建提前写字段会让
	// updatePlanModeRails 的判空条件恒为假、注册分支被跳过。
	// Python: interface_deep.py:2158「SkillEvolutionRail 不在冷启动时挂载」，
	// 字段保持 None，仅在 _update_plan_mode_rails / _ensure_evolution_rail_for_slash 内赋值。

	// memoryRail — 冷启动不构建（对齐 Python：_memory_rail 仅在
	// _handle_memory_rail_by_config 内构建，冷启动只置 None）。
	// 此前 Go 在冷启动构建并存字段，导致后续 handleMemoryRailByConfig
	// 误判"已注册且类型相同"而跳过 RegisterRail——MemoryRail 永不挂载。
	d.memoryRail = nil

	// externalMemoryRail — 仅构建实例存储到字段，不注册
	// Python: ExternalMemoryRail 由 _update_rails_for_mode 按 mode 按需注册/注销
	_ = d.buildExternalMemoryRail()

	// ─── 用户自定义 Hook Rail ───

	// userHookRail — 用户配置的 hooks，对齐 Python interface_deep.py L2200-2211
	// Python: try/except 包裹注册流程，失败时 warning 并继续
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("UserHookRail load failed, skipping")
			}
		}()
		hooksCfg := hookscfg.LoadHooksConfig(configBase)
		if len(hooksCfg.Events) > 0 {
			userHookRail := serverhooks.NewUserHookRail(*hooksCfg)
			railsList = append(railsList, userHookRail)
			logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail loaded")
		}
	}()

	logger.Info(logComponent).
		Str("mode", mode).
		Int("rails_count", len(railsList)).
		Msg("buildAgentRails completed")

	return railsList
}

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

// buildSkillRail 构建技能使用护栏（include_tools 由调用方在 filesystemRail
// 赋值前求值后传入，见 buildAgentRails 步骤 3 注释）。
// ✅ 已回填：SkillUseRail（对齐 Python: _build_skill_rail() — SkillUseRail）
func (d *DeepAdapter) buildSkillRailWithIncludeTools(includeTools bool) (rail sainterfaces.AgentRail) {
	// Python: try-except 保护，失败返回 nil 不影响其他 Rail
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).Msg("buildSkillRail panicked, skipping")
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
		Msg("SkillUseRail created")
	return rail
}

// resolveSkillMode 校验配置的 skill_mode 并在无效值时安全回退。
// Python: _resolve_skill_mode(config)
func (d *DeepAdapter) resolveSkillMode() string {
	rawSkillMode := skillrails.SkillModeAll // 默认值
	if cache := d.configCacheSnapshot(); cache != nil {
		if sm, ok := cache["skill_mode"]; ok {
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
		Msg("invalid skill_mode, falling back to all")
	return skillrails.SkillModeAll
}

// evolutionAutoScan 计算 auto_scan：环境变量优先，其次 config，缺省关闭。
// 对齐 Python: interface_deep.py:1951-1955 与 2317-2328
// （注意 rail 构造默认值为 true，必须显式传入，不能省略）
func evolutionAutoScan(config map[string]any) bool {
	if env := os.Getenv("EVOLUTION_AUTO_SCAN"); env != "" {
		switch strings.ToLower(strings.TrimSpace(env)) {
		case "true", "1", "yes":
			return true
		default:
			return false
		}
	}
	evolutionConfig, _ := config["evolution"].(map[string]any)
	as, _ := evolutionConfig["auto_scan"].(bool)
	return as
}

// updateSkillEvolutionRail 热重载时就地更新演进 rail 的 LLM 与 auto_scan。
// Python: _get_current_agent_rails 顶部（interface_deep.py:2314-2328）——
// 该 rail 不重建、不重新注册，只换 LLM 与开关。
func (d *DeepAdapter) updateSkillEvolutionRail(config map[string]any) {
	if d.skillEvolutionRail == nil {
		return
	}
	d.skillEvolutionRail.UpdateLLM(d.modelSnapshot(), d.defaultModelName)
	d.skillEvolutionRail.SetAutoScan(evolutionAutoScan(config))
	logger.Info(logComponent).
		Str("model", d.defaultModelName).
		Bool("auto_scan", d.skillEvolutionRail.AutoScan()).
		Msg("SkillEvolutionRail hot-updated in place")
}

// buildSkillEvolutionRail 构建技能演进护栏。
// 对齐 Python: _build_skill_evolution_rail() (interface_deep.py:1948-1968)
//
// 与 Python 一致：本函数不判 evolution.enabled（门控在调用方），
// 不接 sharing 配置，auto_save 恒 false（需审批），auto_scan 缺省关闭。
func (d *DeepAdapter) buildSkillEvolutionRail() *evolution.SkillEvolutionRail {
	model := d.modelSnapshot()
	if model == nil {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: model is nil, skipping")
		return nil
	}

	cache := d.configCacheSnapshot()

	// 禁用技能列表
	var disabledSkills []string
	if d.skillManager != nil {
		disabledSkills = d.skillManager.ListExecutionDisabledSkills()
	}

	// skills 目录
	skillsDir := workspace.AgentSkillsDir()
	if skillsDir == "" {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: skills dir is empty, skipping")
		return nil
	}

	// model_name 兜底链：默认模型 → config.model_name → 模板默认值
	modelName := d.defaultModelName
	if modelName == "" {
		if v, ok := cache["model_name"].(string); ok && v != "" {
			modelName = v
		}
	}
	if modelName == "" {
		modelName = "qwen-max"
	}

	rail := evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		model,
		modelName,
		d.resolveRuntimeLanguage(),
		evolution.WithAutoScan(evolutionAutoScan(cache)),
		// auto_save 恒 false：rail 构造默认 true（skill_evolution_rail.go:192），
		// 省略该选项等于放开审批，必须显式传 false。
		evolution.WithAutoSave(false),
		evolution.WithDisabledSkillsSet(disabledSkills),
	)

	logger.Info(logComponent).
		Str("model", modelName).
		Bool("auto_scan", rail.AutoScan()).
		Msg("SkillEvolutionRail created")
	return rail
}

// buildSkillCreateRail 构建单 Agent 技能创建护栏。
// 对齐 Python: _build_skill_create_rail() (interface_deep.py:1972-1997)
//
// Python 语义：skill_create 未启用时返回 None 且不写字段；启用时 auto_trigger 恒 True。
func (d *DeepAdapter) buildSkillCreateRail() *evolution.SkillCreateRail {
	if !d.getSkillCreateEnabled() {
		logger.Debug(logComponent).Msg("skill_create disabled by config, skipping SkillCreateRail")
		return nil
	}

	skillsDir := d.getSkillsDir()
	if skillsDir == "" {
		logger.Warn(logComponent).Msg("buildSkillCreateRail: skillsDir is empty, skipping creation")
		return nil
	}

	rail := evolution.NewSkillCreateRail(skillsDir,
		evolution.WithSkillCreateAutoTrigger(true),
		evolution.WithSkillCreateLanguage(d.getLanguage()),
	)
	logger.Info(logComponent).
		Str("skills_dir", skillsDir).
		Bool("auto_trigger", rail.AutoTrigger()).
		Msg("SkillCreateRail created")
	return rail
}

// getSkillsDir 获取技能目录路径。
// 对齐 Python: get_agent_skills_dir() → agent workspace 下的 skills 子目录。
//
// 原实现优先读 react.skills_dir，但该键在出厂模板与 Python 侧均不存在
// （Python 直接调用 get_agent_skills_dir()），属无人消费的死分支，已删除。
func (d *DeepAdapter) getSkillsDir() string {
	return workspace.AgentSkillsDir()
}

// getLanguage 获取语言设置。
func (d *DeepAdapter) getLanguage() string {
	if cache := d.configCacheSnapshot(); cache != nil {
		if lang, ok := cache["language"].(string); ok && lang != "" {
			return lang
		}
	}
	return "cn"
}

// getSkillCreateEnabled 获取是否启用技能创建。
//
// Python: _get_skill_create_enabled(config) (interface_deep.py:268-280)
// 顺序：环境变量 SKILL_CREATE 优先 → react.evolution.skill_create → 默认 false。
// 原实现读的是 skill_create_enabled 且默认 true，与 Python 不一致：
// 模板里的 react.evolution.skill_create 因此完全不生效。
func (d *DeepAdapter) getSkillCreateEnabled() bool {
	if env := os.Getenv("SKILL_CREATE"); env != "" {
		switch strings.ToLower(env) {
		case "true", "1", "yes":
			return true
		default:
			return false
		}
	}
	evolutionConfig, _ := d.configCacheSnapshot()["evolution"].(map[string]any)
	enabled, _ := evolutionConfig["skill_create"].(bool)
	return enabled
}

// buildStreamEventRail 构建流事件护栏。
// 对齐 Python: _build_stream_event_rail() (line 2051-2080)
func (d *DeepAdapter) buildStreamEventRail() *commrails.JiuClawStreamEventRail {
	rail := commrails.NewJiuClawStreamEventRail()
	logger.Info(logComponent).Msg("JiuClawStreamEventRail created")
	return rail
}

// buildSubagentRail 构建子代理护栏。
// ✅ 已回填：SubagentRail（对齐 Python: _build_subagent_rail() — SubagentRail()）
// 返回具体类型（而非 AgentRail 接口）：调用方多需 *SubagentRail 存入字段，
// 接口返回会迫使调用点做类型断言，断言失败即静默丢 rail（S-01 同类缺陷）。
// 需要接口的地方直接赋值即可——Go 接口隐式满足，调用方无需改动。
func (d *DeepAdapter) buildSubagentRail() *subagent.SubagentRail {
	rail := subagent.NewSubagentRail()
	logger.Info(logComponent).Msg("SubagentRail created")
	return rail
}

// buildSecurityRail 构建安全护栏。
// ✅ 已回填：SafetyPromptRail（对齐 Python: _build_security_rail() — SecurityRail()）
//
// Python: _build_security_rail() (line 2033-2042)
// Python: try/except 包裹创建过程，失败时 warning 并返回 nil
func (d *DeepAdapter) buildSecurityRail(configBase map[string]any) sainterfaces.AgentRail {
	var rail sainterfaces.AgentRail
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("SafetyPromptRail creation failed, skipping")
			}
		}()
		rail = secrail.NewSafetyPromptRail()
	}()
	if rail != nil {
		logger.Info(logComponent).Msg("SafetyPromptRail created")
	}
	return rail
}

// buildMemoryRail 构建记忆护栏。
// ✅ 已回填：MemoryRail（对齐 Python: _build_memory_rail() — MemoryRail）
//
// Python: _build_memory_rail(mode) (line 2044-2072)
func (d *DeepAdapter) buildMemoryRail() sainterfaces.AgentRail {
	// Python: try/except 包裹创建过程
	var rail sainterfaces.AgentRail
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("MemoryRail creation failed, skipping")
			}
		}()

		// Python: embed_config = config.get("embed")
		// 注意：embed 是 config.yaml 顶级段（非 react 段），对齐 Python get_config().get("embed")
		embedConfig, _ := d.configBaseSnapshot()["embed"].(map[string]any)
		if embedConfig == nil {
			logger.Warn(logComponent).
				Msg("MemoryRail creation failed: no embed config")
			return
		}

		// Python: has_api_key / has_base_url / has_model
		apiKey, _ := embedConfig["embed_api_key"].(string)
		baseURL, _ := embedConfig["embed_base_url"].(string)
		if baseURL == "" {
			baseURL, _ = embedConfig["embed_api_base"].(string)
		}
		modelName, _ := embedConfig["embed_model"].(string)

		if apiKey == "" || baseURL == "" || modelName == "" {
			logger.Warn(logComponent).
				Str("has_api_key", boolStr(apiKey != "")).
				Str("has_base_url", boolStr(baseURL != "")).
				Str("has_model", boolStr(modelName != "")).
				Msg("MemoryRail creation failed: incomplete embed config")
		}

		// Python: self._is_proactive_memory = is_proactive_memory(mode, config)
		// 注意：buildMemoryRail 在冷启动时调用无 mode 上下文，isProactive 留默认 false，
		// 后续 handleMemoryRailByConfig 会按 mode 重建
		isProactive := false

		ec := &embedding.EmbeddingConfig{
			ModelName: modelName,
			BaseURL:   baseURL,
			APIKey:    apiKey,
		}
		rail = memrail.NewMemoryRail(ec, isProactive)
		logger.Info(logComponent).Msg("MemoryRail created")
	}()
	return rail
}

// buildExternalMemoryRail 构建外接记忆护栏。
// 对齐 Python: _build_external_memory_rail() — 委托 swarm memory 包 builder
// （user_id/scope_id 默认 __default__ 由 builder 处理，偏差 #4 修正后不再回退 agentName）
//
// S-11: 必须传 d.configBase（完整配置）而非 d.configCache（react 段快照）——
// memory.engine / memory.external 是 config.yaml 顶级段键，Python 侧用的是
// get_config()（interface_deep.py:5328-5331）；传 react 段会使
// GetMemoryEngine 恒回落 builtin，ExternalMemoryRail 永不启用。
func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
	return commmem.BuildExternalMemoryRail(d.configBaseSnapshot(), d.workspaceDir)
}

// handleMemoryRailByConfig 按配置注册/注销 MemoryRail。
// ✅ 已回填：handleMemoryRailByConfig（对齐 Python: _handle_memory_rail_by_config()）
//
// Python: _handle_memory_rail_by_config(mode) (line 5296-5320)
//
// 注意：memory.mode / modes.agent.<mode>.memory / memory.engine 均为
// config.yaml 顶级段键（Python get_config() 返回完整顶级配置），
// 此处必须传 d.configBase 而非 d.configCache（react 段快照）。
func (d *DeepAdapter) handleMemoryRailByConfig(ctx context.Context, mode string) {
	configBase := d.configBaseSnapshot()

	// Python: if get_memory_mode(config) == "local":
	if commmem.GetMemoryMode(configBase) == "local" {
		// Python: builtin_on = is_builtin_memory_allowed(config) and is_memory_enabled(mode, config)
		builtinOn := commmem.IsBuiltinMemoryAllowed(configBase) && commmem.IsMemoryEnabled(mode, configBase)

		if builtinOn {
			// 开启记忆
			if d.memoryRail != nil {
				// Python: cur_memory_type = is_proactive_memory(mode, config)
				curProactive := commmem.IsProactiveMemory(mode, configBase)
				if d.isProactiveMemory != nil && *d.isProactiveMemory != curProactive {
					// 当前记忆类型（主动/被动）和之前注册的不一致，注销后重建
					if d.instance != nil {
						if err := d.instance.UnregisterRail(ctx, d.memoryRail); err != nil {
							logger.Error(logComponent).Err(err).Msg("Failed to unregister MemoryRail (type mismatch)")
						}
					}
					d.memoryRail = nil
				} else {
					// 已经注册，且记忆类型相同，无需其他操作
					return
				}
			}
			if d.memoryRail == nil {
				// 重建 MemoryRail（使用当前 mode 决定 isProactive）
				d.memoryRail = d.buildMemoryRailWithMode(mode)
			}
			if d.memoryRail != nil && d.instance != nil {
				if err := d.instance.RegisterRail(ctx, d.memoryRail); err != nil {
					logger.Error(logComponent).Err(err).Str("mode", mode).Msg("Failed to register MemoryRail")
				} else {
					logger.Info(logComponent).Str("mode", mode).Msg("MemoryRail registered")
				}
			}
		} else if d.memoryRail != nil {
			// Python: elif not builtin_on and self._memory_rail is not None:
			if d.instance != nil {
				if err := d.instance.UnregisterRail(ctx, d.memoryRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to unregister MemoryRail")
				}
			}
			d.memoryRail = nil
			d.isProactiveMemory = nil
			logger.Info(logComponent).Str("mode", mode).Msg("MemoryRail unregistered (config disabled)")
		}
	}
}

// handleExternalMemoryRailByConfig 按配置注册/注销 ExternalMemoryRail。
// 对齐 Python: _handle_external_memory_rail_by_config()
// engine 为 "external" 或 "both" 时注册，否则注销。
// S-11: 与 buildExternalMemoryRail 一致，配置判定必须读 configBase（完整配置）。
func (d *DeepAdapter) handleExternalMemoryRailByConfig(ctx context.Context) {
	if commmem.IsExternalMemoryEnabled(d.configBaseSnapshot()) {
		if !d.externalMemoryRailRegistered {
			rail := d.buildExternalMemoryRail()
			if rail != nil {
				d.externalMemoryRail = rail
				if d.instance != nil {
					if err := d.instance.RegisterRail(ctx, rail); err != nil {
						logger.Error(logComponent).Err(err).Msg("Failed to register ExternalMemoryRail")
					} else {
						d.externalMemoryRailRegistered = true
						logger.Info(logComponent).Msg("ExternalMemoryRail registered")
					}
				}
			}
		}
	} else {
		if d.externalMemoryRailRegistered {
			// 对齐 Python: 注销前先调 provider.on_session_end(messages)
			// 接受双重调用模式（此处带 messages，Uninit 中带 nil）
			if emRail, ok := d.externalMemoryRail.(*memrail.ExternalMemoryRail); ok {
				provider := emRail.Provider()
				if provider != nil {
					if err := provider.OnSessionEnd(ctx, nil); err != nil {
						logger.Warn(logComponent).Err(err).Msg("ExternalMemoryRail Provider OnSessionEnd failed")
					}
				}
			}
			if d.instance != nil {
				if err := d.instance.UnregisterRail(ctx, d.externalMemoryRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to unregister ExternalMemoryRail")
				}
			}
			d.externalMemoryRail = nil
			d.externalMemoryRailRegistered = false
			logger.Info(logComponent).Msg("ExternalMemoryRail unregistered (config disabled)")
		}
	}
}

// buildMemoryRailWithMode 构建带 mode 上下文的 MemoryRail（设置 isProactive）。
// Python: _build_memory_rail(mode) 中会调用 is_proactive_memory(mode, config)
func (d *DeepAdapter) buildMemoryRailWithMode(mode string) sainterfaces.AgentRail {
	configBase := d.configBaseSnapshot()
	var rail sainterfaces.AgentRail
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("buildMemoryRailWithMode creation failed, skipping")
			}
		}()

		// embed 为 config.yaml 顶级段（对齐 Python get_config().get("embed")）
		embedConfig, _ := configBase["embed"].(map[string]any)
		if embedConfig == nil {
			logger.Warn(logComponent).Msg("buildMemoryRailWithMode: no embed config")
			return
		}

		apiKey, _ := embedConfig["embed_api_key"].(string)
		baseURL, _ := embedConfig["embed_base_url"].(string)
		if baseURL == "" {
			baseURL, _ = embedConfig["embed_api_base"].(string)
		}
		modelName, _ := embedConfig["embed_model"].(string)

		isProactive := commmem.IsProactiveMemory(mode, configBase)
		d.isProactiveMemory = &isProactive

		ec := &embedding.EmbeddingConfig{
			ModelName: modelName,
			BaseURL:   baseURL,
			APIKey:    apiKey,
		}
		rail = memrail.NewMemoryRail(ec, isProactive)
		logger.Info(logComponent).
			Bool("is_proactive", isProactive).
			Msg("MemoryRail (with mode) created")
	}()
	return rail
}

// buildAvatarRail 构建数字分身护栏。
// Python: _build_avatar_rail() (line 2146-2155)
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
	rail := commrails.NewAvatarPromptRail()
	logger.Info(logComponent).Msg("AvatarPromptRail created")
	return rail
}

// buildRuntimePromptRail 构建运行时提示词护栏。
// ✅ 已回填：RuntimePromptRail（对齐 Python: _build_runtime_prompt_rail() — RuntimePromptRail(language, channel)）
//
// Python: _build_runtime_prompt_rail() (line 2156-2170)
func (d *DeepAdapter) buildRuntimePromptRail() *commrails.RuntimePromptRail {
	// Python: default_channel = "acp" if self._is_acp_tool_profile(self._instance_overrides) else self._resolve_prompt_channel()
	defaultChannel := "web"
	if d.isAcpToolProfile(d.instanceOverrides) {
		defaultChannel = "acp"
	} else {
		// Python: self._resolve_prompt_channel() 读 self._session_id
		// Go: CreateInstance 时无 sessionID，等价于 Python 调用 _resolve_prompt_channel("") → "web"
		defaultChannel = resolvePromptChannel("")
	}
	// Python: rail = RuntimePromptRail(language=self._resolve_runtime_language(), channel=default_channel)
	rail := commrails.NewRuntimePromptRail(d.resolveRuntimeLanguage(), defaultChannel)
	logger.Info(logComponent).Msg("RuntimePromptRail created")
	return rail
}

// buildResponsePromptRail 构建响应提示词护栏。
// ✅ 已回填：ResponsePromptRail（对齐 Python: _build_response_prompt_rail() — ResponsePromptRail()）
//
// Python: _build_response_prompt_rail() (line 2171-2180)
func (d *DeepAdapter) buildResponsePromptRail() sainterfaces.AgentRail {
	rail := commrails.NewResponsePromptRail()
	logger.Info(logComponent).Msg("ResponsePromptRail created")
	return rail
}

// buildContextAssembleRail 构建上下文组装护栏。
// Python: _build_context_assemble_rail() (line 2181-2195)
func (d *DeepAdapter) buildContextAssembleRail(mode string) sainterfaces.AgentRail {
	d.contextAssembleMode = mode
	return cerails.NewContextAssembleRail()
}

// buildContextProcessorRail 构建上下文处理护栏，并把 yaml 里的 processor 配置透传进去。
//
// Python: _build_context_processor_rail(config) (interface_deep.py L346-395)
//
// 覆盖机制走 ProcessorSpec.ConfigOverrides（字段级部分覆盖），因此 yaml 里的绝对阈值会先
// 合并进 preset，再在 rail.Init 中被比例收口（spec §6.6）。Python 只认四段，本实现额外
// 支持 full_compact_processor_config（设计 D9，Python 侧待同构）与
// tool_result_budget_processor_config（session_memory 预设下的轮内工具结果预算，
// 此前只吃默认值、yaml 无法调参）。
func (d *DeepAdapter) buildContextProcessorRail(reactCfg map[string]any) sainterfaces.AgentRail {
	ctxCfg, _ := reactCfg["context_engine_config"].(map[string]any)
	userProcessors := make([]ceiface.ProcessorSpec, 0, 5)
	specs := []struct {
		configKey string
		typeName  string
	}{
		{"message_summary_offloader_config", "MessageSummaryOffloader"},
		{"dialogue_compressor_config", "DialogueCompressor"},
		{"current_round_compressor_config", "CurrentRoundCompressor"},
		{"round_level_compressor_config", "RoundLevelCompressor"},
		{"full_compact_processor_config", "FullCompactProcessor"},
		{"tool_result_budget_processor_config", "ToolResultBudgetProcessor"},
	}
	for _, spec := range specs {
		if m, _ := ctxCfg[spec.configKey].(map[string]any); len(m) > 0 {
			userProcessors = append(userProcessors, ceiface.ProcessorSpec{
				Type:            spec.typeName,
				ConfigOverrides: utils.DeepCopyMap(m),
			})
		}
	}

	opts := []cerails.ContextProcessorRailOption{cerails.WithUserProcessors(userProcessors)}
	// session_memory 开关：对齐 Python _build_context_processor_rail 的
	// _resolve_session_memory_config(context_engine_cfg) → ContextProcessorRail(session_memory=...)。
	// 传了就启用 session_memory preset（三件套），未传则走默认 4 个处理器。
	if smCfg, ok := resolveSessionMemoryConfig(ctxCfg); ok {
		opts = append(opts, cerails.WithSessionMemoryConfig(smCfg))
	}
	return cerails.NewContextProcessorRail(opts...)
}

// resolveSessionMemoryConfig 解析 context_engine_config 里的 session memory 配置。
//
// Python: _resolve_session_memory_config (interface_deep.py:334-343)
//
//	raw = cfg.get("session_memory_config") or cfg.get("session_memory")
//	True → {}（全默认）；dict → 原样；其余视为关闭
//
// 返回 (配置, 是否启用)。
func resolveSessionMemoryConfig(ctxCfg map[string]any) (*cecontext.SessionMemoryConfig, bool) {
	if ctxCfg == nil {
		return nil, false
	}
	raw := firstTruthyValue(ctxCfg["session_memory_config"], ctxCfg["session_memory"])
	switch v := raw.(type) {
	case nil:
		return nil, false
	case bool:
		// True → 使用默认配置；False 已被 firstTruthyValue 过滤
		cfg := cecontext.NewSessionMemoryConfig()
		return &cfg, v
	case map[string]any:
		cfg, err := cecontext.SessionMemoryConfigFromMap(v)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("invalid session_memory_config, session memory disabled")
			return nil, false
		}
		return &cfg, true
	default:
		logger.Warn(logComponent).
			Str("type", fmt.Sprintf("%T", v)).
			Msg("session_memory_config must be bool or dict, session memory disabled")
		return nil, false
	}
}

// firstTruthyValue 复刻 Python `a or b` 的取值语义（None/False/空串/空容器视为假）。
func firstTruthyValue(values ...any) any {
	for _, v := range values {
		switch t := v.(type) {
		case nil:
			continue
		case bool:
			if t {
				return v
			}
		case string:
			if t != "" {
				return v
			}
		case map[string]any:
			if len(t) > 0 {
				return v
			}
		case []any:
			if len(t) > 0 {
				return v
			}
		case int:
			if t != 0 {
				return v
			}
		case int64:
			if t != 0 {
				return v
			}
		case float64:
			if t != 0 {
				return v
			}
		default:
			return v
		}
	}
	return nil
}

// buildPermissionRail 构建权限护栏。
// ✅ 已回填：PermissionInterruptRail（对齐 Python: _build_permission_rail() — build_permission_rail()）
//
// Python: build_permission_rail(config, llm, model_name) (interrupt_helpers.py L16-281)
// Python: _build_permission_rail() (interface_deep.py L2213-2250)
func (d *DeepAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
	permissionConfig, _ := configBase["permissions"].(map[string]any)
	// Python: logger.info("[InterruptHelpers] build_permission_rail called: enabled=%s", ...)
	logger.Info(logComponent).
		Bool("enabled", permissionConfig != nil && isPermissionEnabled(permissionConfig)).
		Msg("build_permission_rail called")
	if permissionConfig == nil {
		return nil
	}
	enabled, _ := permissionConfig["enabled"].(bool)
	if !enabled {
		logger.Info(logComponent).Msg("PermissionRail: permissions.enabled=false, skipping creation")
		return nil
	}

	toolNames := collectOptionalToolTags(permissionConfig)
	modelName := extractModelName(configBase)

	// Python: logger.info("[InterruptHelpers] tools_config keys: %s, rail tool_names (with rules): %s", ...)
	var toolsConfigKeys []string
	if toolsCfg, ok := permissionConfig["tools"].(map[string]any); ok {
		for k := range toolsCfg {
			toolsConfigKeys = append(toolsConfigKeys, k)
		}
	}
	sort.Strings(toolsConfigKeys)
	logger.Info(logComponent).
		Strs("tools_config_keys", toolsConfigKeys).
		Strs("tool_names", toolNames).
		Msg("permission rail tools_config keys and rail tool_names")

	// Python: logger.info("[InterruptHelpers] Building PermissionInterruptRail with tool_names=%s llm=%s model_name=%s", ...)
	permissionModel := d.modelSnapshot()
	logger.Info(logComponent).
		Strs("tool_names", toolNames).
		Bool("llm", permissionModel != nil).
		Str("model_name", modelName).
		Msg("Building PermissionInterruptRail")

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
		permissionConfig, nil, host, workspaceRoot, permissionModel, modelName,
		func(config map[string]any, engine *harnesssecurity.PermissionEngine, toolNames []string, llmModel *llm.Model, mn string, h *harnesssecurity.ToolPermissionHost) sainterfaces.AgentRail {
			return secrail.NewPermissionInterruptRail(config, engine, toolNames, llmModel, mn, h)
		},
	)
	if rail != nil {
		logger.Info(logComponent).
			Strs("tool_names", toolNames).
			Msg("PermissionInterruptRail created")
	} else {
		logger.Warn(logComponent).Msg("PermissionInterruptRail creation failed")
	}
	return rail
}

// UpdateAgentRails 幂等补挂 agent 模式 rail（plan/fast 已合并为单一 agent 模式）。
//
// Python: _update_agent_rails（interface_deep.py:13981-14075）。合并后不再按子模式分叉：
// 任务规划/子代理/演进 rail 恒在，记忆固定读 modes.agent，上下文 rail 统一 agent 实例。
func (d *DeepAdapter) UpdateAgentRails(ctx context.Context, sessionID, mode string) {
	d.ensureTaskPlanningRail(ctx)
	d.ensureSubagentRail(ctx)
	d.syncMultiSessionTools()
	d.handleMemoryRailByConfig(ctx, "agent")
	d.handleExternalMemoryRailByConfig(ctx)
	d.ensureContextAssembleRail(ctx)
	d.ensureContextProcessorRail(ctx)
	d.ensureSkillEvolutionRail(ctx)
	d.ensureSkillCreateRail(ctx)

	logger.Info(logComponent).
		Str("mode", mode).
		Str("session_id", sessionID).
		Msg("UpdateAgentRails completed")
}

// ensureTaskPlanningRail 确保 todo rail 常驻（plan/fast 合并后两种模式都需要）。
//
// Python: TaskPlanningRail 属默认 rail 集合，inject_prompt=False（由 rail 自身控制）。
func (d *DeepAdapter) ensureTaskPlanningRail(ctx context.Context) {
	if d.taskPlanningRail != nil || d.instance == nil {
		return
	}
	rail := d.buildTaskPlanningRail(d.configCacheSnapshot(), d.resolveRuntimeLanguage())
	if rail == nil {
		return
	}
	if err := d.instance.RegisterRail(ctx, rail); err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to register TaskPlanningRail")
		return
	}
	d.taskPlanningRail = rail
	logger.Info(logComponent).Msg("TaskPlanningRail registered")
}

// ensureSubagentRail 确保 SubagentRail 已注册。
//
// 上游把 SubagentRail 放进冷启动 rail 集合（interface_deep.py:11545），因此这里做
// 「缺失则补挂」，不再随 plan/normal 切换注册或卸载。
func (d *DeepAdapter) ensureSubagentRail(ctx context.Context) {
	if d.subagentRail != nil || d.instance == nil {
		return
	}
	rail := d.buildSubagentRail()
	if rail == nil {
		return
	}
	if err := d.instance.RegisterRail(ctx, rail); err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to register SubagentRail")
		return
	}
	d.subagentRail = rail
	logger.Info(logComponent).Msg("SubagentRail registered")
}

// syncMultiSessionTools 清理历史遗留的 multi-session 临时工具。
//
// Python: _update_tools_for_mode（interface_deep.py:14138-14150）。plan/fast 合并后
// session_new / session_cancel 这类临时协程工具不再注册；session_list 等
// 产品会话工具保留。
func (d *DeepAdapter) syncMultiSessionTools() {
	if d.instance == nil {
		return
	}
	am := d.instance.AbilityManager()
	if am == nil {
		return
	}
	for _, t := range am.List() {
		name := t.AbilityName()
		if name == "session_new" || name == "session_cancel" {
			am.Remove(name)
			logger.Info(logComponent).Str("tool_name", name).Msg("multi-session tool removed")
		}
	}
}

// ensureContextAssembleRail 确保 ContextAssembleRail 是 agent 实例（幂等）。
//
// Python: _update_agent_rails 中 `if self._context_assemble_rail is None or
// self._context_assemble_mode != "agent"` 才重建。
func (d *DeepAdapter) ensureContextAssembleRail(ctx context.Context) {
	const agentMode = "agent"
	if d.contextAssembleRail != nil && d.contextAssembleMode == agentMode {
		return
	}
	if d.contextAssembleRail != nil {
		if d.instance != nil {
			if err := d.instance.UnregisterRail(ctx, d.contextAssembleRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("Failed to unregister ContextAssembleRail")
			}
		}
		d.contextAssembleRail = nil
	}
	d.contextAssembleRail = d.buildContextAssembleRail(agentMode)
	d.contextAssembleMode = agentMode
	if d.contextAssembleRail != nil && d.instance != nil {
		if err := d.instance.RegisterRail(ctx, d.contextAssembleRail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to register ContextAssembleRail")
			return
		}
		logger.Info(logComponent).Msg("ContextAssembleRail registered")
	}
}

// ensureContextProcessorRail 按 context_engine_config.enabled 注册/注销 ContextProcessorRail。
func (d *DeepAdapter) ensureContextProcessorRail(ctx context.Context) {
	cache := d.configCacheSnapshot()
	contextEngineConfig, _ := cache["context_engine_config"].(map[string]any)
	contextEnabled, _ := contextEngineConfig["enabled"].(bool)
	if contextEnabled {
		if d.contextProcessorRail != nil {
			return
		}
		d.contextProcessorRail = d.buildContextProcessorRail(cache)
		if d.contextProcessorRail == nil || d.instance == nil {
			return
		}
		if err := d.instance.RegisterRail(ctx, d.contextProcessorRail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to register ContextProcessorRail")
			d.contextProcessorRail = nil
			return
		}
		logger.Info(logComponent).Msg("ContextProcessorRail registered")
		return
	}
	if d.contextProcessorRail == nil {
		return
	}
	if d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.contextProcessorRail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to unregister ContextProcessorRail")
		}
	}
	d.contextProcessorRail = nil
	logger.Info(logComponent).Msg("ContextProcessorRail unregistered (context_engine disabled)")
}

// ensureSkillEvolutionRail 按 evolution.enabled 注册/注销 SkillEvolutionRail。
func (d *DeepAdapter) ensureSkillEvolutionRail(ctx context.Context) {
	cache := d.configCacheSnapshot()
	evolutionConfig, _ := cache["evolution"].(map[string]any)
	evolutionEnabled, _ := evolutionConfig["enabled"].(bool)
	if evolutionEnabled {
		if d.skillEvolutionRail != nil {
			return
		}
		rail := d.buildSkillEvolutionRail()
		if rail == nil || d.instance == nil {
			return
		}
		if err := d.instance.RegisterRail(ctx, rail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to register SkillEvolutionRail")
			return
		}
		d.skillEvolutionRail = rail
		logger.Info(logComponent).Msg("SkillEvolutionRail registered")
		return
	}
	if d.skillEvolutionRail == nil {
		return
	}
	if d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.skillEvolutionRail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to unregister SkillEvolutionRail")
		}
	}
	d.skillEvolutionRail = nil
	logger.Info(logComponent).Msg("SkillEvolutionRail unregistered (evolution.enabled=false)")
}

// ensureSkillCreateRail 按 skill_create 开关注册/注销 SkillCreateRail。
func (d *DeepAdapter) ensureSkillCreateRail(ctx context.Context) {
	if d.getSkillCreateEnabled() {
		if d.skillCreateRail != nil {
			return
		}
		rail := d.buildSkillCreateRail()
		if rail == nil || d.instance == nil {
			return
		}
		if err := d.instance.RegisterRail(ctx, rail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to register SkillCreateRail")
			return
		}
		d.skillCreateRail = rail
		logger.Info(logComponent).Msg("SkillCreateRail registered")
		return
	}
	if d.skillCreateRail == nil {
		return
	}
	if d.instance != nil {
		if err := d.instance.UnregisterRail(ctx, d.skillCreateRail); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to unregister SkillCreateRail")
		}
	}
	d.skillCreateRail = nil
	logger.Info(logComponent).Msg("SkillCreateRail unregistered (skill_create=false)")
}

// updatePromptForMode 按模式更新系统提示词语言。
// ✅ 已回填：对齐 Python _update_prompt_for_mode() (line 3091-3097)
//
// Python: _update_prompt_for_mode(mode, resolved_language)
//   - if self._instance.system_prompt_builder is not None: system_prompt_builder.language = resolved_language
//   - if self._instance.deep_config is not None: deep_config.language = resolved_language
func (d *DeepAdapter) updatePromptForMode(mode string) {
	resolvedLanguage := d.resolveRuntimeLanguage()

	// Python: if self._instance.system_prompt_builder is not None: builder.language = resolved_language
	if d.instance != nil {
		if spb := d.instance.SystemPromptBuilder(); spb != nil {
			spb.SetLanguage(resolvedLanguage)
		}
		// Python: if self._instance.deep_config is not None: deep_config.language = resolved_language
		if deepCfg := d.instance.DeepConfig(); deepCfg != nil {
			deepCfg.Language = resolvedLanguage
		}
	}

	logger.Info(logComponent).
		Str("mode", mode).
		Str("language", resolvedLanguage).
		Msg("updatePromptForMode completed")
}

// getPermissionsSnapshot 返回当前权限配置快照。
// Python: get_permissions_snapshot = lambda: get_config().get("permissions", {}) (interrupt_helpers.py L215)
func (d *DeepAdapter) getPermissionsSnapshot() map[string]any {
	cache := d.configCacheSnapshot()
	if cache == nil {
		return map[string]any{}
	}
	perm, _ := cache["permissions"].(map[string]any)
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
		logger.Warn(logComponent).Msg("persistAllowRule: config path is empty")
		return false
	}
	ok := harnesssecurity.WritePermissionsSectionToAgentConfigYAML(yamlPath, permissions)
	if !ok {
		logger.Warn(logComponent).Str("yaml_path", yamlPath).Msg("persistAllowRule write to disk failed")
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
// 非 ACP 通道返回 ConfirmActionInterrupt（走内置中断），ACP 通道经
// AcpOutputManager 发 session/request_permission JSON-RPC 请求。
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
	// Python L133-135: session_id = _resolve_session_id(req.ctx); if not session_id: return None
	sessionID := resolvePermissionSessionID(req.Ctx)
	if sessionID == "" {
		return nil, nil
	}

	// Python L139-142: 工具调用字段提取（name/arguments/id 兜底）
	toolName := ""
	var toolArgsRaw any
	toolCallID := ""
	if req.ToolCall != nil {
		toolName = req.ToolCall.Name
		toolArgsRaw = req.ToolCall.Arguments
		toolCallID = strings.TrimSpace(req.ToolCall.ID)
	}
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("permission_%s", firstNonEmptyPermStr(toolName, "tool"))
	}
	// Python L143-149: descriptor = build_acp_tool_descriptor(..., status="pending", kind="other")
	descriptor := e2aacp.BuildAcpToolDescriptor(toolName, toolArgsRaw,
		e2aacp.WithToolCallID(toolCallID),
		e2aacp.WithStatus("pending"),
		e2aacp.WithKind("other"))
	// Python L150-152: title 组装（reason 追加）
	title, _ := descriptor["title"].(string)
	if title == "" {
		title = fmt.Sprintf("Approve `%s`", toolName)
	}
	if req.Result != nil && req.Result.Reason != "" {
		title = fmt.Sprintf("%s: %s", title, req.Result.Reason)
	}
	descriptor["title"] = title

	// Python L154-164: 请求参数组装
	toolCall := map[string]any{}
	for k, v := range descriptor {
		toolCall[k] = v
	}
	toolCall["title"] = title
	requestParams := map[string]any{
		"toolCall": toolCall,
		"options": []any{
			map[string]any{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
			map[string]any{"optionId": "allow-always", "name": "Always allow", "kind": "allow_always"},
			map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
		},
	}

	// Python L166-174: 发送请求；异常仅 Warn 并返回 None
	response, err := swarmtools.GetAcpOutputManager().SendJsonrpcRequest(
		req.GoCtx, "session/request_permission", requestParams,
		swarmtools.WithAcpSessionID(sessionID))
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("[InterruptHelpers] ACP permission request failed")
		return nil, nil
	}

	// Python L176-179: 错误响应 → 拒绝
	if errPayload, ok := response["error"].(map[string]any); ok {
		message := "Permission request failed"
		if m, ok := errPayload["message"].(string); ok && m != "" {
			message = m
		}
		return &harnesssecurity.PermissionConfirmResponse{
			Approved: false,
			Feedback: fmt.Sprintf("[PERMISSION_DENIED] %s", message),
		}, nil
	}

	// Python L186-200: outcome 解析
	resultPayload, _ := response["result"].(map[string]any)
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	outcome, _ := resultPayload["outcome"].(map[string]any)
	if outcome == nil {
		outcome = map[string]any{}
	}
	outcomeKind := strings.ToLower(strings.TrimSpace(strOrEmptyPerm(outcome["outcome"])))
	optionID := strings.ToLower(strings.TrimSpace(strOrEmptyPerm(outcome["optionId"])))

	switch outcomeKind {
	case "selected":
		switch optionID {
		case "allow-once":
			return &harnesssecurity.PermissionConfirmResponse{Approved: true}, nil
		case "allow-always":
			return &harnesssecurity.PermissionConfirmResponse{Approved: true, AutoConfirm: true}, nil
		default:
			return &harnesssecurity.PermissionConfirmResponse{
				Feedback: "[PERMISSION_REJECTED] User rejected the request.",
			}, nil
		}
	case "cancelled":
		return &harnesssecurity.PermissionConfirmResponse{
			Feedback: "[PERMISSION_REJECTED] Permission request was cancelled.",
		}, nil
	}
	// Python L208: 其他 outcome → None
	return nil, nil
}

// resolvePermissionSessionID 从回调上下文解析会话 ID。
// Python: _resolve_session_id (interrupt_helpers.py L112-124)
func resolvePermissionSessionID(ctx *sainterfaces.AgentCallbackContext) string {
	if ctx == nil {
		return ""
	}
	session := ctx.Session()
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.GetSessionID())
}

// firstNonEmptyPermStr 依序取第一个非空串。
func firstNonEmptyPermStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// strOrEmptyPerm 安全字符串化（nil→""）。
func strOrEmptyPerm(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// isPermissionEnabled 检查 permissions 配置中 enabled 字段是否为真值。
func isPermissionEnabled(permissionConfig map[string]any) bool {
	enabled, ok := permissionConfig["enabled"]
	if !ok {
		return false
	}
	if b, ok := enabled.(bool); ok {
		return b
	}
	return false
}

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

// resolveModeMemory/getMemoryMode/isMemoryEnabled/isProactiveMemory 四函数
// 已迁移至 swarm 侧记忆包导出版：commmem.ResolveModeMemory/GetMemoryMode/IsMemoryEnabled/IsProactiveMemory
// （对齐 Python：这些函数定义在 jiuwenswarm/agents/harness/common/memory/config.py）
//
// isBuiltinMemoryAllowed、getMemoryEngine、isExternalMemoryEnabled、getExternalMemoryConfig、
// buildOpenJiuwenProviderConfig、BuildExternalMemoryRail 六函数亦已迁移至 commmem 包
// （对齐 Python：external_memory_config.py / external_memory_builder.py）

// boolStr 将 bool 转为 "true"/"false" 字符串（用于日志）。
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// buildDynamicRail 根据方法名调用对应的 Rail builder。
// Python: method = getattr(self, method_name, None)
func (d *DeepAdapter) buildDynamicRail(methodName string, configBase map[string]any) sainterfaces.AgentRail {
	switch methodName {
	case "buildFilesystemRail":
		return d.buildFilesystemRail(false)
	case "buildSkillRail":
		// 热重载路径：现场求值 include_tools（对齐 Python _get_current_agent_rails
		// 中 _build_skill_rail(include_tools=self._skill_include_tools_for_profile())，
		// 此时 _filesystem_rail 通常已赋值）
		return d.buildSkillRailWithIncludeTools(d.skillIncludeToolsForProfile(d.instanceOverrides))
	case "buildHeartbeatRail":
		return d.buildHeartbeatRail()
	case "buildAvatarRail":
		return d.buildAvatarRail()
	case "buildTaskPlanningRail":
		return d.buildTaskPlanningRail(d.configCacheSnapshot(), d.resolveRuntimeLanguage())
	case "buildSubagentRail":
		return d.buildSubagentRail()
	case "buildContextAssembleRail":
		return d.buildContextAssembleRail(d.mode)
	case "buildContextProcessorRail":
		return d.buildContextProcessorRail(d.configCacheSnapshot())
	case "buildSkillEvolutionRail":
		rail := d.buildSkillEvolutionRail()
		// 动态路径（code 模式 rails 配置点名）只负责构建并登记字段；
		// 注册由本次 CreateDeepAgent 装配完成，装配后不变量成立。
		if rail != nil {
			d.skillEvolutionRail = rail
		}
		return rail
	case "buildWorktreeRail":
		// CodeAdapter 专有，DeepAdapter 不实现
		return nil
	case "buildCodeAgentRail":
		// CodeAdapter 专有，DeepAdapter 不实现
		return nil
	default:
		logger.Warn(logComponent).Str("method", methodName).
			Msg("unknown dynamic Rail builder method name")
		return nil
	}
}
