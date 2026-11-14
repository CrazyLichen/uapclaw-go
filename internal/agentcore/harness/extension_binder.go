package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	toolskillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	subagentrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	resources_manager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 插件/模板扩展热绑定：把 resolve 产物挂到 live DeepAgent 上，并支持逆序卸载。
// 逐符号 1:1 对齐 Python openjiuwen/harness/extension_binder.py。
//
// 绑定记账（硬编码）：同名/同标识资源已存在 → 报错（整批失败）；
// AgentTemplate identity 段 → 替换 + 快照；新资源 → 绑定并记 ref。
// 任一绑定失败时对本次已绑 refs 逆序 unapply 后重抛。

const logComponentBinder = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyExtensionHot 把解析后的 Plugin/AgentTemplate Parts 热绑到 live DeepAgent。
//
// 绑定顺序硬编码：tools → mcps → rails → prompt_sections → skills → subagents
// →（有 subagents 时）ensureSubagentRailReady；任一失败对本次已绑 refs 逆序
// unapply 后重抛。
//
// Python: extension_binder.py:27-62 apply_extension_hot
func ApplyExtensionHot(ctx context.Context, agent *DeepAgent, parts hres.ExtensionParts) ([]hres.ResourceRef, error) {
	refs := []hres.ResourceRef{}
	for i := range parts.Tools {
		ref, err := bindTool(agent, parts.Tools[i])
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	for i := range parts.Mcps {
		ref, err := bindMCP(ctx, agent, &parts.Mcps[i])
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	for _, rail := range parts.Rails {
		ref, err := bindRail(ctx, agent, rail)
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	for i := range parts.PromptSections {
		ref, err := bindPromptSection(ctx, agent, parts.PromptSections[i])
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	for i := range parts.Skills {
		ref, err := bindSkill(ctx, agent, parts.Skills[i])
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	for i := range parts.Subagents {
		ref, err := bindSubagent(agent, &parts.Subagents[i])
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		refs = append(refs, ref)
	}
	if len(parts.Subagents) > 0 {
		ensureRef, err := ensureSubagentRailReady(ctx, agent)
		if err != nil {
			unapplyAndFail(ctx, agent, refs, err)
			return nil, err
		}
		if ensureRef != nil {
			refs = append(refs, *ensureRef)
		}
	}
	return refs, nil
}

// UnapplyExtensionHot 逆序撤销 refs 记录的绑定，返回标签列表。
// Python: extension_binder.py:64-74 unapply_extension_hot
func UnapplyExtensionHot(ctx context.Context, agent *DeepAgent, refs []hres.ResourceRef) ([]string, error) {
	unloaded := []string{}
	for i := len(refs) - 1; i >= 0; i-- {
		if err := unbind(ctx, agent, refs[i]); err != nil {
			return unloaded, err
		}
		unloaded = append(unloaded, fmt.Sprintf("%s:%s", refs[i].Kind, refs[i].Identity))
	}
	return unloaded, nil
}

// unapplyAndFail 失败路径共用：对已绑 refs 逆序撤销后记录错误日志。
// Python: apply_extension_hot 的 except 分支（:58-62，回滚后 re-raise）。
func unapplyAndFail(ctx context.Context, agent *DeepAgent, refs []hres.ResourceRef, cause error) {
	if _, err := UnapplyExtensionHot(ctx, agent, refs); err != nil {
		logger.Error(logComponentBinder).Err(err).
			Msg("extension hot apply rollback failed")
	}
	logger.Error(logComponentBinder).Err(cause).
		Msg("extension hot apply failed, rolled back bound refs")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requireDeepConfig 取 DeepAgentConfig；未配置时报错。
// Python: extension_binder.py:76-81 _require_deep_config
func requireDeepConfig(agent *DeepAgent) (*hschema.DeepAgentConfig, error) {
	config := agent.DeepConfig()
	if config == nil {
		return nil, fmt.Errorf("DeepAgentConfig is required. Call configure() first.")
	}
	return config, nil
}

// toolIdentity 计算工具在本 agent 下的注册标识。
// Python: extension_binder.py:83-87 _tool_identity
func toolIdentity(agent *DeepAgent, card *tool.ToolCard) string {
	if !card.Stateless && agent.Card().GetID() != "" {
		qualified := ability.QualifyToolID(card, agent.Card().GetID())
		if qualified != nil && qualified.ID != "" {
			return qualified.ID
		}
	}
	if card.ID != "" {
		return card.ID
	}
	return card.Name
}

// bindTool 绑定单个工具能力（Tool 实例或 ToolCard，经 ToolResource union 替身）。
// Python: extension_binder.py:89-109 _bind_tool
// 归一校验已在 resolver 侧（toolResourceOf）前置；此处 Instance 优先。
func bindTool(agent *DeepAgent, resource tool.ToolResource) (hres.ResourceRef, error) {
	ref := hres.NewResourceRef(hres.ResourceKindTool, "")
	switch {
	case resource.Instance != nil:
		typed := resource.Instance
		card := typed.Card()
		if agent.AbilityManager().Get(card.Name) != nil {
			return ref, fmt.Errorf("Tool already bound: %s", card.Name)
		}
		identity := toolIdentity(agent, card)
		// Python: agent.ability_manager.add_ability(card, resource)——结果码由
		// AddAbilityResult 表达（重复注册等），失败时不中断本批（对齐 Python
		// add_ability 的返回值语义，不加 error 分支）。
		_ = agent.AbilityManager().AddAbility(card, typed)
		if err := appendConfigTool(agent, card); err != nil {
			return ref, err
		}
		ref.Identity = identity
		ref.Extra["ability_names"] = []string{card.Name}
		return ref, nil
	case resource.Card != nil:
		card := resource.Card
		if agent.AbilityManager().Get(card.Name) != nil {
			return ref, fmt.Errorf("Tool already bound: %s", card.Name)
		}
		identity := toolIdentity(agent, card)
		// Python: agent.ability_manager.add(card)（结果码语义与 add_ability 同）
		_ = agent.AbilityManager().Add(card)
		if err := appendConfigTool(agent, card); err != nil {
			return ref, err
		}
		ref.Identity = identity
		ref.Extra["ability_names"] = []string{card.Name}
		return ref, nil
	default:
		return ref, fmt.Errorf("Unsupported tool resource: empty ToolResource")
	}
}

// appendConfigTool 把工具卡片记入 DeepAgentConfig.Tools（config commit last）。
// Python: extension_binder.py:102-106 config.tools 追加
func appendConfigTool(agent *DeepAgent, card *tool.ToolCard) error {
	config, err := requireDeepConfig(agent)
	if err != nil {
		return err
	}
	agent.configMu.Lock()
	defer agent.configMu.Unlock()
	tools := make([]*tool.ToolCard, 0, len(config.Tools)+1)
	tools = append(tools, config.Tools...)
	tools = append(tools, card)
	config.Tools = tools
	return nil
}

// bindMCP 绑定 MCP 服务器配置：先副作用、后提交 deep_config；
// 失败时自摘（外层回滚无需为本步未完成绑定记 ref）。
// Python: extension_binder.py:111-148 _bind_mcp
func bindMCP(ctx context.Context, agent *DeepAgent, config *mcptypes.McpServerConfig) (hres.ResourceRef, error) {
	deepConfig, err := requireDeepConfig(agent)
	if err != nil {
		return hres.ResourceRef{}, err
	}
	agent.configMu.RLock()
	for _, item := range deepConfig.Mcps {
		if item.ServerID == config.ServerID {
			agent.configMu.RUnlock()
			return hres.ResourceRef{}, fmt.Errorf("MCP server already bound: %s", config.ServerID)
		}
	}
	agent.configMu.RUnlock()
	if agent.AbilityManager().Get(config.ServerName) != nil {
		return hres.ResourceRef{}, fmt.Errorf("MCP ability already bound: %s", config.ServerName)
	}

	// 副作用在前，deep_config 提交在后；失败先 unbind 自身再抛。
	ref := hres.NewResourceRef(hres.ResourceKindMCP, config.ServerID)
	ref.Extra["server_name"] = config.ServerName
	if err := bindMCPMutations(ctx, agent, config); err != nil {
		if unbindErr := unbind(ctx, agent, ref); unbindErr != nil {
			logger.Error(logComponentBinder).Err(unbindErr).
				Str("server_id", config.ServerID).
				Msg("mcp bind self-unbind failed")
		}
		return ref, err
	}

	agent.configMu.Lock()
	mcps := make([]*mcptypes.McpServerConfig, 0, len(deepConfig.Mcps)+1)
	mcps = append(mcps, deepConfig.Mcps...)
	mcps = append(mcps, config)
	deepConfig.Mcps = mcps
	agent.configMu.Unlock()
	return ref, nil
}

// bindMCPMutations MCP 绑定的副作用段（resource_mgr + ability_manager）。
// Python: extension_binder.py:129-141 的 try 体
func bindMCPMutations(ctx context.Context, agent *DeepAgent, config *mcptypes.McpServerConfig) error {
	resourceMgr := runner.GetResourceMgr()
	existingConfig, getErr := resourceMgr.GetMcpServerConfig(config.ServerID)
	if getErr != nil || existingConfig == nil {
		if _, addErr := resourceMgr.AddMcpServer(ctx, config,
			resources_manager.WithMcpTag(resources_manager.Tag(agent.Card().GetID()))); addErr != nil {
			return addErr
		}
	} else {
		if _, tagErr := resourceMgr.AddResourceTag(config.ServerID,
			[]resources_manager.Tag{resources_manager.Tag(agent.Card().GetID())}); tagErr != nil {
			return tagErr
		}
		for _, toolID := range resourceMgr.GetMcpToolIDs(config.ServerID) {
			if _, tagErr := resourceMgr.AddResourceTag(toolID,
				[]resources_manager.Tag{resources_manager.Tag(agent.Card().GetID())}); tagErr != nil {
				return tagErr
			}
		}
	}
	agent.AbilityManager().Add(config)
	return nil
}

// bindRail 绑定单个 rail：identity 取物化类名；同名冲突报错；注册失败先
// unregister 清理已挂运行时工具（外层回滚看不到本 ref）再抛。
// Python: extension_binder.py:150-161 _bind_rail
func bindRail(ctx context.Context, agent *DeepAgent, rail agentinterfaces.AgentRail) (hres.ResourceRef, error) {
	identity := railTypeName(rail)
	if agent.FindRailByName(identity) != nil {
		return hres.ResourceRef{}, fmt.Errorf("Rail already bound: %s", identity)
	}
	if err := agent.RegisterRail(ctx, rail); err != nil {
		// init 可能在选择性路由失败前已注册运行时工具；本实例尚无 ResourceRef，
		// 外层回滚看不到它，须在此清理。
		if unregErr := agent.UnregisterRail(ctx, rail); unregErr != nil {
			logger.Error(logComponentBinder).Err(unregErr).
				Str("rail", identity).
				Msg("rail bind cleanup unregister failed")
		}
		return hres.ResourceRef{}, err
	}
	ref := hres.NewResourceRef(hres.ResourceKindRail, identity)
	return ref, nil
}

// bindPromptSection 绑定提示词段落：同段名冲突按 ReplaceExisting 决定报错或替换+快照。
// Python: extension_binder.py:164-184 _bind_prompt_section
func bindPromptSection(ctx context.Context, agent *DeepAgent, resolved hres.ResolvedPromptSection) (hres.ResourceRef, error) {
	builder := agent.SystemPromptBuilder()
	if builder == nil {
		return hres.ResourceRef{}, fmt.Errorf("Cannot bind prompt section before DeepAgent.configure()")
	}
	section := resolved.Section
	existing := builder.GetSection(section.Name)
	if existing != nil && !resolved.ReplaceExisting {
		return hres.ResourceRef{}, fmt.Errorf("Prompt section already bound: %s", section.Name)
	}

	ref := hres.NewResourceRef(hres.ResourceKindPromptSection, section.Name)
	if resolved.ReplaceExisting {
		ref.Extra["previous_exists"] = existing != nil
		if existing != nil {
			// 快照既有段落（卸载时恢复）
			snapshotContent := make(map[string]string, len(existing.Content))
			for k, v := range existing.Content {
				snapshotContent[k] = v
			}
			ref.Extra["previous_snapshot"] = map[string]any{
				"name":     existing.Name,
				"content":  snapshotContent,
				"priority": existing.Priority,
			}
		}
	}
	builder.AddSection(section)
	if err := agent.ApplyPromptBuilderToReactAgent(ctx); err != nil {
		return ref, err
	}
	return ref, nil
}

// skillPathsToRailMounts 把技能路径列表拆成挂载根与显式启用名。
// Python: extension_binder.py:186-202 _skill_paths_to_rail_mounts
func skillPathsToRailMounts(skillPaths []string) ([]string, []string) {
	roots := []string{}
	enabledNames := []string{}
	for _, rawPath := range skillPaths {
		path, err := filepath.Abs(expandUser(rawPath))
		if err != nil {
			path = rawPath
		}
		if isSkillLeafDir(path) {
			roots = append(roots, filepath.Dir(path))
			enabledNames = append(enabledNames, filepath.Base(path))
		} else {
			roots = append(roots, path)
			enabledNames = append(enabledNames, "")
		}
	}
	// Python：root 重复跳过；enabled name 空串跳过（leaf 才有名字）
	dedupRoots := []string{}
	seenRoots := map[string]struct{}{}
	for _, root := range roots {
		if _, ok := seenRoots[root]; ok {
			continue
		}
		seenRoots[root] = struct{}{}
		dedupRoots = append(dedupRoots, root)
	}
	dedupNames := []string{}
	seenNames := map[string]struct{}{}
	for _, name := range enabledNames {
		if name == "" {
			continue
		}
		if _, ok := seenNames[name]; ok {
			continue
		}
		seenNames[name] = struct{}{}
		dedupNames = append(dedupNames, name)
	}
	return dedupRoots, dedupNames
}

// skillValues 把 config.skills 归一为字符串列表（nil → 空列表）。
// Python: extension_binder.py:204-210 _skill_values
func skillValues(raw []string) []string {
	if raw == nil {
		return []string{}
	}
	return append([]string(nil), raw...)
}

// isSkillLeafDir 目录含 SKILL.md 即为技能叶子目录。
// Python: extension_binder.py:212-214 _is_skill_leaf_dir
func isSkillLeafDir(directory string) bool {
	info, err := os.Stat(filepath.Join(directory, "SKILL.md"))
	return err == nil && !info.IsDir()
}

// bindSkill 把一个技能挂载到 agent 既有的 SkillUseRail 上。
// 不自动创建 rail：agent 应已携带（factory._make_skill_rail / enable_skill_discovery）。
// Python: extension_binder.py:216-289 _bind_skill
func bindSkill(ctx context.Context, agent *DeepAgent, skill hres.ResolvedSkill) (hres.ResourceRef, error) {
	config, err := requireDeepConfig(agent)
	if err != nil {
		return hres.ResourceRef{}, err
	}
	directory, err := filepath.Abs(expandUser(skill.Directory))
	if err != nil {
		directory = skill.Directory
	}
	isLeaf := isSkillLeafDir(directory)
	mountRoot := directory
	if isLeaf {
		mountRoot = filepath.Dir(directory)
	}

	skillRails := agent.FindRailsByType(reflect.TypeOf(&toolskillrails.SkillUseRail{}))
	if len(skillRails) == 0 {
		return hres.ResourceRef{}, fmt.Errorf("No SkillUseRail registered on the agent; cannot bind a skill.")
	}
	target, ok := skillRails[0].(*toolskillrails.SkillUseRail)
	if !ok {
		return hres.ResourceRef{}, fmt.Errorf("No SkillUseRail registered on the agent; cannot bind a skill.")
	}

	roots, _ := skillPathsToRailMounts([]string{skill.Directory})

	previousDirs := skillValues(target.SkillsDir())
	currentDirs := map[string]struct{}{}
	for _, item := range previousDirs {
		resolved, rerr := filepath.Abs(expandUser(item))
		if rerr == nil {
			currentDirs[resolved] = struct{}{}
		}
	}

	if _, exists := currentDirs[mountRoot]; exists && !isLeaf {
		return hres.ResourceRef{}, fmt.Errorf("Skill already bound: %s", mountRoot)
	}

	previousEnabled := target.EnabledSkillsSnapshot()

	// 插入位置：已绑包根之后、宿主根之前。SkillUseRail 先载入的名字优先，
	// 包技能因此压过同名的 workspace 技能。leaf 名不得写入全局白名单，
	// 否则会把宿主技能整体遮蔽。
	packageRoots := map[string]struct{}{}
	for _, record := range agent.LoadRecordsSnapshot() {
		for _, ref := range record.Refs {
			if ref.Kind != hres.ResourceKindSkill {
				continue
			}
			rawDir, _ := ref.Extra["directory"].(string)
			if rawDir == "" {
				rawDir = ref.Identity
			}
			path, rerr := filepath.Abs(expandUser(rawDir))
			if rerr != nil {
				continue
			}
			if isSkillLeafDir(path) {
				packageRoots[filepath.Dir(path)] = struct{}{}
			} else {
				packageRoots[path] = struct{}{}
			}
		}
	}
	packageDirs := []string{}
	hostDirs := []string{}
	for _, item := range previousDirs {
		resolved, rerr := filepath.Abs(expandUser(item))
		if rerr == nil {
			if _, isPkg := packageRoots[resolved]; isPkg {
				packageDirs = append(packageDirs, item)
				continue
			}
		}
		hostDirs = append(hostDirs, item)
	}
	newSkillDirs := append([]string{}, packageDirs...)
	for _, root := range roots {
		if _, exists := currentDirs[root]; !exists {
			newSkillDirs = append(newSkillDirs, root)
		}
	}
	newSkillDirs = append(newSkillDirs, hostDirs...)
	target.SetSkillsDir(newSkillDirs)
	target.SetEnableCache(false)
	target.ClearSkills()
	if err := target.ReloadSkills(ctx); err != nil {
		// reload 失败恢复原状再抛
		target.SetSkillsDir(previousDirs)
		target.RestoreEnabledSkills(previousEnabled)
		target.SetEnableCache(false)
		target.ClearSkills()
		return hres.ResourceRef{}, err
	}

	// rail 副作用成功后才提交 config。
	agent.configMu.Lock()
	skills := skillValues(config.Skills)
	for _, root := range roots {
		found := false
		for _, item := range skills {
			if item == root {
				found = true
				break
			}
		}
		if !found {
			skills = append(skills, root)
		}
	}
	config.Skills = skills
	agent.configMu.Unlock()

	ref := hres.NewResourceRef(hres.ResourceKindSkill, skill.Directory)
	ref.Extra["mode"] = skill.Mode
	ref.Extra["directory"] = skill.Directory
	return ref, nil
}

// subagentKey 子代理去重键：card name 优先，空则 card id。
// Python: extension_binder.py:291-294 _subagent_key
func subagentKey(subagent *hschema.SubAgentConfig) string {
	card := subagent.AgentCard
	if card == nil {
		return ""
	}
	if card.GetName() != "" {
		return card.GetName()
	}
	return card.GetID()
}

// bindSubagent 绑定子代理配置并注册其卡片为能力。
// Python: extension_binder.py:296-309 _bind_subagent
func bindSubagent(agent *DeepAgent, subagent *hschema.SubAgentConfig) (hres.ResourceRef, error) {
	identity := subagentKey(subagent)
	config, err := requireDeepConfig(agent)
	if err != nil {
		return hres.ResourceRef{}, err
	}
	agent.configMu.RLock()
	for _, item := range config.Subagents {
		if item.SpecName() == identity {
			agent.configMu.RUnlock()
			return hres.ResourceRef{}, fmt.Errorf("Subagent already bound: %s", identity)
		}
	}
	agent.configMu.RUnlock()
	name := ""
	if subagent.AgentCard != nil {
		name = subagent.AgentCard.GetName()
	}
	if name != "" && agent.AbilityManager().Get(name) != nil {
		return hres.ResourceRef{}, fmt.Errorf("Subagent ability already bound: %s", identity)
	}
	agent.configMu.Lock()
	subagents := make([]hschema.SubagentSpec, 0, len(config.Subagents)+1)
	subagents = append(subagents, config.Subagents...)
	subagents = append(subagents, subagent)
	config.Subagents = subagents
	agent.configMu.Unlock()
	agent.AbilityManager().Add(subagent.AgentCard)
	ref := hres.NewResourceRef(hres.ResourceKindSubagent, identity)
	return ref, nil
}

// ensureSubagentRailReady subagent 绑定后确保 SubagentRail 存在且任务/会话工具就绪。
// 非同名冲突场景：共享引导设施按触发者无关的方式幂等复用/创建。
// Python: extension_binder.py:311-340 _ensure_subagent_rail_ready
func ensureSubagentRailReady(ctx context.Context, agent *DeepAgent) (*hres.ResourceRef, error) {
	config, err := requireDeepConfig(agent)
	if err != nil {
		return nil, err
	}
	if len(config.Subagents) == 0 {
		return nil, nil
	}

	subagentRails := agent.FindRailsByType(reflect.TypeOf(&subagentrails.SubagentRail{}))
	var rail *subagentrails.SubagentRail
	created := false
	if len(subagentRails) > 0 {
		if typed, ok := subagentRails[0].(*subagentrails.SubagentRail); ok {
			rail = typed
		}
	}
	if rail == nil {
		// Python: enable_subagent_runtime 取 config.enable_subagent_runtime；
		// Go DeepAgentConfig 无该字段（登记偏差），恒 false。
		rail = subagentrails.NewSubagentRail(
			subagentrails.WithEnableAsyncSubagent(config.EnableAsyncSubagent),
		)
		if err := agent.RegisterRail(ctx, rail); err != nil {
			return nil, err
		}
		created = true
	}
	// Python: if not getattr(rail, "tools", None): rail.init(agent)
	//         else: rail.refresh_available_agents(agent)
	if !rail.HasTools() {
		if err := rail.Init(ctx, agent); err != nil {
			return nil, err
		}
	} else {
		rail.RefreshAvailableAgents(agent)
	}
	if created {
		ref := hres.NewResourceRef(hres.ResourceKindRail, railTypeName(rail))
		return &ref, nil
	}
	return nil, nil
}

// unbind 按 ref 类别撤销单个绑定。Python: extension_binder.py:342-466 _unbind
func unbind(ctx context.Context, agent *DeepAgent, ref hres.ResourceRef) error {
	switch ref.Kind {
	case hres.ResourceKindTool:
		names, _ := ref.Extra["ability_names"].([]string)
		toolName := ref.Identity
		if len(names) > 0 {
			toolName = names[0]
		}
		if toolName != "" {
			agent.AbilityManager().Remove(toolName)
			config, err := requireDeepConfig(agent)
			if err != nil {
				return err
			}
			agent.configMu.Lock()
			if config.Tools != nil {
				var kept []*tool.ToolCard
				for _, card := range config.Tools {
					if card.Name != toolName {
						kept = append(kept, card)
					}
				}
				config.Tools = kept
			}
			agent.configMu.Unlock()
		}
		return nil
	case hres.ResourceKindMCP:
		return unbindMCP(ctx, agent, ref)
	case hres.ResourceKindRail:
		rail := agent.FindRailByName(ref.Identity)
		if rail != nil && agent.IsRegisteredRail(rail) {
			return agent.UnregisterRail(ctx, rail)
		} else if rail != nil {
			agent.RemovePendingRail(rail)
		}
		return nil
	case hres.ResourceKindPromptSection:
		return unbindPromptSection(ctx, agent, ref)
	case hres.ResourceKindSkill:
		return unbindSkill(ctx, agent, ref)
	case hres.ResourceKindSubagent:
		return unbindSubagent(agent, ref)
	default:
		return fmt.Errorf("Unsupported hot unbind: kind=%s", ref.Kind)
	}
}

// unbindMCP 撤销 MCP 绑定：仅当本 agent 是最后一个 tag 持有者时移除 server。
// Python: extension_binder.py:354-387
func unbindMCP(ctx context.Context, agent *DeepAgent, ref hres.ResourceRef) error {
	serverID := ref.Identity
	serverName, _ := ref.Extra["server_name"].(string)
	config, err := requireDeepConfig(agent)
	if err != nil {
		return err
	}
	agent.configMu.Lock()
	if config.Mcps != nil {
		var kept []*mcptypes.McpServerConfig
		for _, item := range config.Mcps {
			if item.ServerID != serverID {
				kept = append(kept, item)
			}
		}
		config.Mcps = kept
	}
	agent.configMu.Unlock()
	if serverName != "" && agent.AbilityManager().Get(serverName) != nil {
		agent.AbilityManager().Remove(serverName)
	}

	resourceMgr := runner.GetResourceMgr()
	existingConfig, getErr := resourceMgr.GetMcpServerConfig(serverID)
	if getErr != nil || existingConfig == nil {
		return nil
	}
	if _, tagErr := resourceMgr.RemoveResourceTag(serverID,
		[]resources_manager.Tag{resources_manager.Tag(agent.Card().GetID())},
		resources_manager.WithTagSkipIfNotExists()); tagErr != nil {
		return tagErr
	}
	tags := resourceMgr.GetResourceTag(serverID)
	if len(tags) > 0 {
		return nil
	}
	if _, rmErr := resourceMgr.RemoveMcpServer(ctx, serverID,
		resources_manager.WithMcpSkipIfNotExists()); rmErr != nil {
		return rmErr
	}
	return nil
}

// unbindPromptSection 撤销提示词段落：有快照恢复原文，无快照直接移除。
// Python: extension_binder.py:389-408
func unbindPromptSection(ctx context.Context, agent *DeepAgent, ref hres.ResourceRef) error {
	builder := agent.SystemPromptBuilder()
	if builder == nil {
		return nil
	}
	previousExists, _ := ref.Extra["previous_exists"].(bool)
	if previousExists {
		snapshot, _ := ref.Extra["previous_snapshot"].(map[string]any)
		if snapshot != nil {
			name, _ := snapshot["name"].(string)
			if name == "" {
				name = ref.Identity
			}
			content, _ := snapshot["content"].(map[string]string)
			priority := 100
			if p, ok := snapshot["priority"].(int); ok {
				priority = p
			}
			builder.AddSection(saprompts.PromptSection{
				Name: name, Content: content, Priority: priority,
			})
		}
	} else {
		builder.RemoveSection(ref.Identity)
	}
	return agent.ApplyPromptBuilderToReactAgent(ctx)
}

// unbindSkill 撤销技能挂载：从所有挂载了该根的 SkillUseRail 摘除并 reload；
// 兄弟 leaf 场景只摘本名，其余保持。
// Python: extension_binder.py:410-455
func unbindSkill(ctx context.Context, agent *DeepAgent, ref hres.ResourceRef) error {
	directory, _ := ref.Extra["directory"].(string)
	if directory == "" {
		directory = ref.Identity
	}
	path, err := filepath.Abs(expandUser(directory))
	if err != nil {
		path = directory
	}
	isLeaf := isSkillLeafDir(path)
	root := path
	if isLeaf {
		root = filepath.Dir(path)
	}
	config, err := requireDeepConfig(agent)
	if err != nil {
		return err
	}
	skillRails := agent.FindRailsByType(reflect.TypeOf(&toolskillrails.SkillUseRail{}))
	for _, railAny := range skillRails {
		rail, ok := railAny.(*toolskillrails.SkillUseRail)
		if !ok {
			continue
		}
		current := skillValues(rail.SkillsDir())
		mounted := map[string]struct{}{}
		for _, item := range current {
			resolved, rerr := filepath.Abs(expandUser(item))
			if rerr == nil {
				mounted[resolved] = struct{}{}
			}
		}
		if _, isMounted := mounted[root]; !isMounted {
			continue
		}

		// 兄弟 leaf 卸载：只摘本技能名，其余保持。
		if isLeaf {
			snapshot := rail.EnabledSkillsSnapshot()
			if len(snapshot) > 0 {
				rail.DiscardEnabledSkill(path2Name(path))
				if len(rail.EnabledSkillsSnapshot()) > 0 {
					if agent.IsRegisteredRail(rail) {
						rail.SetEnableCache(false)
						rail.ClearSkills()
						if err := rail.ReloadSkills(ctx); err != nil {
							return err
						}
					}
					return nil
				}
			}
		}

		agent.configMu.Lock()
		if config.Skills != nil {
			var kept []string
			for _, item := range skillValues(config.Skills) {
				resolved, rerr := filepath.Abs(expandUser(item))
				if rerr == nil && resolved == root {
					continue
				}
				kept = append(kept, item)
			}
			config.Skills = kept
		}
		agent.configMu.Unlock()
		var keptDirs []string
		for _, item := range current {
			resolved, rerr := filepath.Abs(expandUser(item))
			if rerr == nil && resolved == root {
				continue
			}
			keptDirs = append(keptDirs, item)
		}
		rail.SetSkillsDir(keptDirs)
		if len(keptDirs) == 0 {
			// 无可挂载内容：reload_skills 要求非空 skills_dir，只清缓存。
			rail.ClearSkills()
		} else if agent.IsRegisteredRail(rail) {
			rail.SetEnableCache(false)
			rail.ClearSkills()
			if err := rail.ReloadSkills(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// unbindSubagent 撤销子代理绑定并刷新 SubagentRail 的可用代理文案。
// Python: extension_binder.py:457-466
func unbindSubagent(agent *DeepAgent, ref hres.ResourceRef) error {
	config, err := requireDeepConfig(agent)
	if err != nil {
		return err
	}
	agent.configMu.Lock()
	var matched hschema.SubagentSpec
	var kept []hschema.SubagentSpec
	for _, item := range config.Subagents {
		if item.SpecName() == ref.Identity {
			matched = item
			continue
		}
		kept = append(kept, item)
	}
	if matched == nil {
		agent.configMu.Unlock()
		return nil
	}
	config.Subagents = kept
	agent.configMu.Unlock()

	name := ""
	if card := matchedCard(matched); card != nil {
		name = card.GetName()
	}
	if name != "" && agent.AbilityManager().Get(name) != nil {
		agent.AbilityManager().Remove(name)
	}
	// Python: for rail in agent.find_rails_by_type((SubagentRail,)):
	//             rail.refresh_available_agents(agent)  (:456-458)
	for _, rail := range agent.FindRailsByType(reflect.TypeOf(&subagentrails.SubagentRail{})) {
		if typed, ok := rail.(*subagentrails.SubagentRail); ok {
			typed.RefreshAvailableAgents(agent)
		}
	}
	return nil
}

// matchedCard 从 SubagentSpec 接口取 AgentCard（仅 SubAgentConfig 携带）。
func matchedCard(spec hschema.SubagentSpec) *saschema.AgentCard {
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok {
		return cfg.AgentCard
	}
	return nil
}

// path2Name 取路径末段目录名。
func path2Name(path string) string {
	return filepath.Base(path)
}

// expandUser 展开 ~ 前缀（Python Path.expanduser 等价）。
func expandUser(raw string) string {
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if raw == "~" {
				return home
			}
			return filepath.Join(home, raw[2:])
		}
	}
	return raw
}

// ApplyPromptBuilderToReactAgent 把当前 system_prompt_builder 重渲染进内层 ReActAgent。
// Python: DeepAgent.apply_prompt_builder_to_react_agent (deep_agent.py:859-882)
//
// 模板只携带 identity 段（ReActAgent 每轮会把 prompt_template 重读进自己的
// identity 节；DeepAgent 与 ReActAgent 共享 builder，其余节由 builder 自身
// 继续贡献——整份渲染会把 configure 时的所有段折进 identity 并重复输出）。
func (d *DeepAgent) ApplyPromptBuilderToReactAgent(ctx context.Context) error {
	builder := d.SystemPromptBuilder()
	if builder == nil || d.reactAgent == nil {
		return nil
	}
	language := hprompts.ResolveLanguage(d.deepConfigLanguage())
	identitySection := builder.GetSection(sections.SectionIdentity)
	identityPrompt := ""
	if identitySection != nil {
		identityPrompt = identitySection.Render(language)
	}
	currentConfig := d.reactAgent.Config()
	reactCfg, ok := currentConfig.(*saconfig.ReActAgentConfig)
	if !ok {
		return nil
	}
	newReactConfig := *reactCfg
	newReactConfig.PromptTemplate = []map[string]any{
		{"role": "system", "content": identityPrompt},
	}
	if err := d.reactAgent.Configure(ctx, &newReactConfig); err != nil {
		return err
	}
	return nil
}

// deepConfigLanguage 取 deep_config.language；未配置返回空。
func (d *DeepAgent) deepConfigLanguage() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.deepConfig == nil {
		return ""
	}
	return d.deepConfig.Language
}
