package prompts

import (
	"fmt"
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfo 团队元数据，用于构建 team_info section。
// Python: team_info: dict[str, Any]（TeamBackend.get_team_info 返回值）
type TeamInfo struct {
	// TeamName 团队唯一标识
	TeamName string
	// DisplayName 团队展示名
	DisplayName string
	// Description 团队目标与指令
	Description string
}

// TeamMember 团队成员信息，用于构建 team_members section。
// Python: team_members: list[dict[str, str]] 中的单个 dict
type TeamMember struct {
	// MemberName 成员名（语义 slug）
	MemberName string
	// DisplayName 展示名
	DisplayName string
	// Description 成员描述
	Description string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// Section 名称常量，对齐 Python TeamSectionName。
// Python: class TeamSectionName (openjiuwen/agent_teams/prompts/sections.py)
const (
	// SectionRole 角色策略 + 执行模式（P:11）
	SectionRole = "team_role"
	// SectionHITT HITT 人类成员协作规则（P:12）
	SectionHITT = "team_hitt"
	// SectionWorkflow Leader 工作流（P:13）
	SectionWorkflow = "team_workflow"
	// SectionLifecycle 团队生命周期策略（P:14）
	SectionLifecycle = "team_lifecycle"
	// SectionExtra 用户自定义 base prompt（P:17）
	SectionExtra = "team_extra"
	// SectionInboundTags 入站/事件/上下文 XML 标签说明（P:18）
	// Python: TeamSectionName.INBOUND_TAGS (sections.py L73)
	SectionInboundTags = "team_inbound_tags"
	// SectionBootstrap leader 起手段（P:11）
	// Python: TeamSectionName.BOOTSTRAP (sections.py L62)
	SectionBootstrap = "team_bootstrap"
	// SectionPrebuilt 框架建队 leader 起手段（P:11）
	// Python: TeamSectionName.PREBUILT (sections.py L63)
	SectionPrebuilt = "team_prebuilt"
	// SectionBoundGroup leader 绑定的专家团（P:12）
	// Python: TeamSectionName.BOUND_GROUP (sections.py L64)
	SectionBoundGroup = "team_bound_group"
	// SectionBridge Bridge Agent 自契约（P:12）
	// Python: TeamSectionName.BRIDGE (sections.py L67)
	SectionBridge = "team_bridge"
	// SectionDispatch 任务下发与获取（P:15）
	// Python: TeamSectionName.DISPATCH (sections.py L69)
	SectionDispatch = "team_dispatch"
	// SectionTaskState 任务状态流转（P:16）
	// Python: TeamSectionName.TASK_STATE (sections.py L70)
	SectionTaskState = "team_task_state"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// workflowTemplateName 工作流模板名映射。
// Python: _WORKFLOW_TEMPLATES (openjiuwen/agent_teams/prompts/sections.py)
var workflowTemplateName = map[string]string{
	"default":    "leader_workflow",
	"predefined": "leader_workflow_predefined",
	"hybrid":     "leader_workflow_hybrid",
}

// dispatchRoleSlugs 参与任务下发的角色到模板 slug 的映射。
//
// 只有 LEADER 与 TEAMMATE 参与下发：HUMAN_AGENT 的「等指派」契约已在它的
// HITT section 里，BRIDGE_AGENT 是纯转发 avatar、不承担看板工作。
// EXTERNAL_CLI 归入 teammate（它是完整的 teammate，只是内容来自远端执行器）。
//
// Python: _DISPATCH_ROLE_SLUGS (prompts/sections.py L370-375)
var dispatchRoleSlugs = map[atschema.TeamRole]string{
	atschema.TeamRoleLeader:      "leader",
	atschema.TeamRoleTeammate:    "teammate",
	atschema.TeamRoleExternalCLI: "teammate",
}

// dispatchModes 合法的调度模式集合。
// Python: _DISPATCH_MODES (prompts/sections.py L365)
var dispatchModes = map[string]struct{}{
	"autonomous": {},
	"scheduled":  {},
}

// labels 双语标签字典，对齐 Python _LABELS。
// Python: _LABELS: dict[str, dict[str, str]] (openjiuwen/agent_teams/prompts/sections.py)
var labels = map[string]map[string]string{
	"cn": {
		"role_heading":       "# 团队角色",
		"workflow_heading":   "# 工作流程",
		"dispatch_heading":   "# 任务下发与获取",
		"task_state_heading": "# 任务状态流转",
		"lifecycle_heading":  "# 团队生命周期",
		"persona_heading":    "# 当前人设",
		"info_heading":       "# 团队信息",
		"team_name_label":    "team_name（团队唯一标识）",
		"display_name_label": "display_name（团队展示名）",
		"team_desc":          "团队目标与指令",
		"team_workspace":     "团队共享工作空间",
		"team_workspace_purpose": "用于存放团队共享文件（方案、设计、交付成果），" +
			"所有成员通过该路径前缀读写同一份文件，系统自动管理版本和文件锁",
		"team_workspace_abs": "绝对路径",
		"members_heading":    "# 成员关系",
		"leader_mode_plan": "团队成员执行模式: plan_mode（成员选择或接到任务后需直接通过 submit_plan 提交计划，" +
			"由你通过 approve_plan 审批后才能执行）",
		"leader_mode_build": "团队成员执行模式: build_mode（成员领取任务后自主执行并直接完成，无需你审批计划）",
		"teammate_mode_plan": "你的执行模式: plan_mode（选择或接到任务后必须先通过 submit_plan 提交计划，" +
			"该工具会认领任务；等待 leader 通过 approve_plan 审批后才能开始执行）",
		"teammate_mode_build": "你的执行模式: build_mode（领取任务后可自主执行并直接标记完成，无需 leader 审批计划）",
	},
	"en": {
		"role_heading":       "# Team Role",
		"workflow_heading":   "# Workflow",
		"dispatch_heading":   "# Task Dispatch",
		"task_state_heading": "# Task State Transitions",
		"lifecycle_heading":  "# Team Lifecycle",
		"persona_heading":    "# Current Persona",
		"info_heading":       "# Team Info",
		"team_name_label":    "team_name (unique identifier)",
		"display_name_label": "display_name (human-readable label)",
		"team_desc":          "Team Goal & Directives",
		"team_workspace":     "Team Shared Workspace",
		"team_workspace_purpose": "Holds team-shared files (plans, designs, deliverables); " +
			"all members read/write the same files through this path prefix. " +
			"Versioning and file locks are managed automatically",
		"team_workspace_abs": "Absolute path",
		"members_heading":    "# Relationships",
		"leader_mode_plan": "Teammate execution mode: plan_mode (teammates must submit a plan " +
			"with submit_plan after selecting or receiving a task; " +
			"that tool reserves the task, then teammates wait for your exact plan_id approval via approve_plan " +
			"before executing)",
		"leader_mode_build": "Teammate execution mode: build_mode (teammates execute and " +
			"complete tasks autonomously without plan approval)",
		"teammate_mode_plan": "Your execution mode: plan_mode (after selecting or receiving a task you must " +
			"submit a plan via submit_plan; that tool reserves the task. Wait for the leader to approve " +
			"that plan_id via approve_plan before executing)",
		"teammate_mode_build": "Your execution mode: build_mode (after claiming a task you " +
			"execute autonomously and mark it completed without leader plan " +
			"approval)",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTeamRoleSection 构建角色策略 + 执行模式 section。
//
// 成员的 member_name **不在此渲染**——它是唯一的逐成员值，住在
// team_identity 状态车道（由 TeamContextTracker 投递）。渲染进来会让同一
// 角色的所有成员前缀不再逐字节相同，KV 缓存前缀就失效了。
//
// HUMAN_AGENT 取独立策略（human_agent_policy）：avatar 的对话方与输出通道
// 和 teammate 最要紧的那点不同——它的 controller 直接读纯文本输出，而
// teammate 策略把 send_message(to="user") 定成必须的回复通道，套给 avatar 会
// 让它用「给 user 发消息」来回答自己的 controller，那是另一个真人。
// BRIDGE_AGENT 沿用 teammate 策略：它是货真价实的 teammate，只是内容来自
// 远端执行器。
//
// 参数：
//   - role: 角色枚举 TeamRole
//   - teammateMode: 队友执行模式 "build_mode" / "plan_mode"
//   - workspacePromptVariant: "native"（.team 挂载）/ "external"（路径式共享工作区）
//   - language: 提示词语言 "cn" 或 "en"
//
// Python: build_team_role_section (prompts/sections.py L254-325)
func BuildTeamRoleSection(role atschema.TeamRole, teammateMode, workspacePromptVariant, language string) (*saprompt.PromptSection, error) {
	lbl := sectionsLabelsFor(language)

	var policyName string
	switch {
	case role == atschema.TeamRoleLeader:
		policyName = "leader_policy"
	case role == atschema.TeamRoleHumanAgent:
		policyName = "human_agent_policy"
	case workspacePromptVariant == "external":
		policyName = "teammate_policy_external"
	default:
		policyName = "teammate_policy"
	}
	roleTpl, err := LoadTemplate(policyName, language)
	if err != nil {
		return nil, err
	}
	// 模板缺失直接返回错误，不再注入 Python 中不存在的「加载失败」占位文案
	roleText := strings.TrimSpace(roleTpl.Content())

	// avatar 不自己规划任务，它只按 controller 的指令行动，所以执行模式行
	// 对它什么都不说。
	// 模式行只说「任务到了成员手上之后」怎么办，从不说怎么到达——
	// 认领（autonomous）还是调度器拉起（scheduled）是 dispatch section 的事，
	// 本节两种模式共用。
	modeLine := ""
	if role != atschema.TeamRoleHumanAgent {
		isPlanMode := teammateMode == "plan_mode"
		var modeLabelKey string
		if role == atschema.TeamRoleLeader {
			if isPlanMode {
				modeLabelKey = "leader_mode_plan"
			} else {
				modeLabelKey = "leader_mode_build"
			}
		} else {
			if isPlanMode {
				modeLabelKey = "teammate_mode_plan"
			} else {
				modeLabelKey = "teammate_mode_build"
			}
		}
		modeLine = lbl[modeLabelKey] + "\n\n"
	}
	body := lbl["role_heading"] + "\n\n" + modeLine + roleText + "\n"

	section := saprompt.NewPromptSection(SectionRole, map[string]string{language: body}, 11)
	return &section, nil
}

// BuildTeamHITTSection 构建 HITT 协作契约 section（仅规则，不含名册）。
//
// 门控在 HITT **能力开关**上，不在实时人类名册上——这样人类 agent 还没
// spawn 时规则就已就绪；人类成员本身在统一名册里以 [human] 标记出现
// （由 exposeHumanAgentsToTeammates 控制是否向 teammate 暴露）。
// 因此本节是静态的、字节稳定，可以进系统提示词前缀。
//
// 文本因角色而异：LEADER 拿指派规则，TEAMMATE 拿角色中立协作习惯
// （默认匿名，exposeHumanAgentsToTeammates 切到名册感知变体），
// HUMAN_AGENT 拿 avatar 自契约（需注入自身 member_name）。
//
// 参数：
//   - role: 角色枚举
//   - hittEnabled: HITT 是否为本团队启用；false → 返回 nil
//   - language: "cn" 或 "en"
//   - selfMemberName: 当前成员自身名（仅 HUMAN_AGENT 契约使用）
//   - exposeHumanAgentsToTeammates: TEAMMATE 在匿名（默认）与名册感知两模板间切换
//
// Python: build_team_hitt_section (prompts/sections.py L642-685)
func BuildTeamHITTSection(
	role atschema.TeamRole,
	hittEnabled bool,
	language string,
	selfMemberName string,
	exposeHumanAgentsToTeammates bool,
) (*saprompt.PromptSection, error) {
	if !hittEnabled {
		return nil, nil
	}
	body, ok, err := hittContractBody(role, selfMemberName, exposeHumanAgentsToTeammates, language)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	section := saprompt.NewPromptSection(SectionHITT, map[string]string{language: body}, 12)
	return &section, nil
}

// BuildTeamWorkflowSection 构建 Leader 工作流 section。
// Python: build_team_workflow_section() (openjiuwen/agent_teams/prompts/sections.py)
//
// 仅 LEADER 返回非 nil。teamMode 支持 "default"/"predefined"/"hybrid"。
func BuildTeamWorkflowSection(role atschema.TeamRole, teamMode string, language string) (*saprompt.PromptSection, error) {
	if role != atschema.TeamRoleLeader {
		return nil, nil
	}
	lbl := sectionsLabelsFor(language)
	templateName := "leader_workflow"
	if mapped, ok := workflowTemplateName[teamMode]; ok {
		templateName = mapped
	}
	workflowTpl, err := LoadTemplate(templateName, language)
	if err != nil {
		return nil, err
	}
	workflowText := strings.TrimSpace(workflowTpl.Content())
	body := lbl["workflow_heading"] + "\n\n" + workflowText + "\n"

	section := saprompt.NewPromptSection(SectionWorkflow, map[string]string{language: body}, 13)
	return &section, nil
}

// BuildTeamLifecycleSection 构建 Leader 生命周期策略 section。
// Python: build_team_lifecycle_section() (openjiuwen/agent_teams/prompts/sections.py)
//
// 仅 LEADER 返回非 nil。lifecycle 支持 "persistent"/"temporary"。
func BuildTeamLifecycleSection(role atschema.TeamRole, lifecycle string, language string) (*saprompt.PromptSection, error) {
	if role != atschema.TeamRoleLeader {
		return nil, nil
	}
	lbl := sectionsLabelsFor(language)
	templateName := "lifecycle_temporary"
	if lifecycle == "persistent" {
		templateName = "lifecycle_persistent"
	}
	lifecycleTpl, err := LoadTemplate(templateName, language)
	if err != nil {
		return nil, err
	}
	lifecycleText := strings.TrimSpace(lifecycleTpl.Content())
	body := lbl["lifecycle_heading"] + "\n\n" + lifecycleText + "\n"

	section := saprompt.NewPromptSection(SectionLifecycle, map[string]string{language: body}, 14)
	return &section, nil
}

// BuildTeamExtraSection 构建用户自定义 base prompt section。
// Python: build_team_extra_section() (openjiuwen/agent_teams/prompts/sections.py)
//
// 不加标题头，basePrompt 为空或纯空白时返回 nil。
func BuildTeamExtraSection(basePrompt string, language string) *saprompt.PromptSection {
	trimmed := strings.TrimSpace(basePrompt)
	if trimmed == "" {
		return nil
	}

	section := saprompt.NewPromptSection(SectionExtra, map[string]string{language: trimmed + "\n"}, 16)
	return &section
}

// BuildTeamInboundTagsSection 构建入站标签通知 section。
// 告诉模型 <team-inbound> / <team-note> / <team-event> / <team-context> /
// <team-policy> 这套 XML 的含义与嵌套规则。模板是静态的，每个成员（含外部 CLI）
// 都有，所以它进 system prompt 前缀、不随团队状态变化。
// language 参数**不使用**：content 同时携带中英两份，语言选择发生在渲染时
// （上游显式 `del language`，保留参数只为签名一致）。
// Python: build_team_inbound_tags_section (sections.py L562-590)
func BuildTeamInboundTagsSection(language string) (*saprompt.PromptSection, error) {
	_ = language // 与上游 `del language` 对应：content 携带双语，语言选择在渲染侧
	cnTpl, err := LoadTemplate("inbound_tags", "cn")
	if err != nil {
		return nil, fmt.Errorf("加载 cn/inbound_tags.md 失败: %w", err)
	}
	enTpl, err := LoadTemplate("inbound_tags", "en")
	if err != nil {
		return nil, fmt.Errorf("加载 en/inbound_tags.md 失败: %w", err)
	}
	section := saprompt.NewPromptSection(SectionInboundTags, map[string]string{
		"cn": cnTpl.Content(),
		"en": enTpl.Content(),
	}, 18)
	return &section, nil
}

// BuildTeamDispatchSection 构建任务下发 section（LEADER + TEAMMATE）。
//
// 调度模式与 team_mode 正交：team_mode 决定名册能否增长，本轴决定任务
// 如何到达执行它的成员。拆成两个 section 避免了 3×2 的模板矩阵——每个维度
// 各自贡献一份文件。
//
// 参数：
//   - role: 角色；不在 dispatchRoleSlugs 里的返回 nil
//   - dispatchMode: "autonomous"（成员自己从看板认领）/ "scheduled"（leader 指派、调度器拉起）
//   - memberMessaging: scheduled 成员之间是否可互发消息（F_126）
//   - memberSubtasks: scheduled 成员是否可把任务拆成子任务（F_128）
//   - language: "cn" 或 "en"
//
// Python: build_team_dispatch_section (prompts/sections.py L377-446)
func BuildTeamDispatchSection(
	role atschema.TeamRole,
	dispatchMode string,
	memberMessaging, memberSubtasks bool,
	language string,
) (*saprompt.PromptSection, error) {
	slug, ok := dispatchRoleSlugs[role]
	if !ok {
		return nil, nil
	}
	mode := dispatchMode
	if _, valid := dispatchModes[mode]; !valid {
		mode = "autonomous"
	}
	lbl := sectionsLabelsFor(language)
	tpl, err := LoadTemplate("dispatch_"+mode+"_"+slug, language)
	if err != nil {
		return nil, err
	}
	rendered := tpl.Content()
	if mode == "scheduled" {
		// 槽位在模板里都结束一列表行，所以规则自己作为列表项加入；
		// 空槽位不留空行。
		memberSubtasksText := ""
		if memberSubtasks {
			sub, err := LoadTemplate("dispatch_scheduled_"+slug+"_subtasks", language)
			if err != nil {
				return nil, err
			}
			memberSubtasksText = "\n" + strings.TrimSpace(sub.Content())
		}
		messaging, err := scheduledMemberMessaging(slug, memberMessaging, language)
		if err != nil {
			return nil, err
		}
		// 两次 Render 照抄上游的两次 format：先填两个能力槽位，
		// 再填 inline_member（未提供的 key 保留占位符原文）。
		rendered = (&PromptTemplate{content: rendered}).Render(map[string]string{
			"member_messaging": messaging,
			"member_subtasks":  memberSubtasksText,
		})
		if slug == "leader" {
			// 任务级内联成员（F_118）在硬开关后面；关闭时教 member / lifetime /
			// 休眠的那段散文一起消失。
			inlineMember := ""
			if atschema.InlineTaskMemberEnabled {
				seg, err := LoadTemplate("dispatch_scheduled_leader_inline_member", language)
				if err != nil {
					return nil, err
				}
				inlineMember = strings.TrimSpace(seg.Content()) + "\n\n"
			}
			rendered = (&PromptTemplate{content: rendered}).Render(map[string]string{"inline_member": inlineMember})
		}
	}
	body := lbl["dispatch_heading"] + "\n\n" + strings.TrimSpace(rendered) + "\n"
	section := saprompt.NewPromptSection(SectionDispatch, map[string]string{language: body}, 15)
	return &section, nil
}

// BuildTeamTaskStateSection 构建任务状态机 section（仅 LEADER）。
//
// 状态机不是模式中立的，所以按调度模式各一份模板，而不是一份模板里塞模式
// caveat。有两条边直接不同——谁驱动 pending → in_progress、以及到底有没有
// 验证闸——一份同时描述两者的文本必须每行都打补丁，自主认领模式的 leader
// 就是这么被教成去调 create_task(reviewer=[...]) 的：那个模式的 create_task
// 根本没这个参数。
//
// 所以 autonomous 不描述验证闸。那不是遗漏：TaskCreateTool 不暴露 reviewer，
// 手工也到不了那道闸——验证者机制由调度运行时驱动，只有 scheduled 模式的
// leader 才有；在 autonomous 里把任务推进 in_review 只会卡住、没人裁决。
//
// Python: build_team_task_state_section (prompts/sections.py L463-507)
func BuildTeamTaskStateSection(role atschema.TeamRole, dispatchMode, language string) (*saprompt.PromptSection, error) {
	if role != atschema.TeamRoleLeader {
		return nil, nil
	}
	mode := dispatchMode
	if _, valid := dispatchModes[mode]; !valid {
		mode = "autonomous"
	}
	lbl := sectionsLabelsFor(language)
	tpl, err := LoadTemplate("task_state_"+mode, language)
	if err != nil {
		return nil, err
	}
	body := lbl["task_state_heading"] + "\n\n" + strings.TrimSpace(tpl.Content()) + "\n"
	// priority=16 硬编码：上游 SECTION_PRIORITY 表里没有 TASK_STATE 项。
	section := saprompt.NewPromptSection(SectionTaskState, map[string]string{language: body}, 16)
	return &section, nil
}

// BuildTeamBridgeSection 构建 Bridge Agent 自契约 section（仅 BRIDGE_AGENT）。
//
// 从其他每个成员的视角看，bridge 就是普通 teammate——在统一名册里不带标记、
// 也没有面向同伴的 section。只有 bridge avatar 本身收到这份调度自契约
// （怎么转达远端执行器的输出）。{{self_line}} 携带 avatar 自己的 member_name。
//
// Python: build_team_bridge_section (prompts/sections.py L687-720)
func BuildTeamBridgeSection(role atschema.TeamRole, language, selfMemberName string) (*saprompt.PromptSection, error) {
	if role != atschema.TeamRoleBridgeAgent {
		return nil, nil
	}
	tpl, err := LoadTemplate("bridge_agent", language)
	if err != nil {
		return nil, err
	}
	selfLine := selfMemberLine(selfMemberName, language)
	// 不 TrimSpace：上游取 loader(...).format(...).content 原样
	body := tpl.Render(map[string]string{"self_line": selfLine})
	section := saprompt.NewPromptSection(SectionBridge, map[string]string{language: body}, 12)
	return &section, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// scheduledMemberMessaging 渲染 scheduled dispatch 模板的 {{member_messaging}} 槽位。
//
// leader 永远拿到一条规则（成员能联系到谁，决定它怎么路由信息）。
// teammate 的「关闭」规则已经写在它的 send_message 描述里，所以那里塌成
// 空串，模板把槽位留在最后一行让 TrimSpace 吸收。
//
// Python: _scheduled_member_messaging (prompts/sections.py L448-461)
func scheduledMemberMessaging(slug string, enabled bool, language string) (string, error) {
	if enabled {
		tpl, err := LoadTemplate("dispatch_scheduled_"+slug+"_messaging_open", language)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(tpl.Content()), nil
	}
	if slug == "leader" {
		tpl, err := LoadTemplate("dispatch_scheduled_leader_messaging_closed", language)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(tpl.Content()), nil
	}
	return "", nil
}

// hittTemplateName 选角色的 HITT 契约模板。
// TEAMMATE 与 EXTERNAL_CLI 同一分支（external CLI 是完整的 teammate，
// 只是内容来自远端执行器）。
// Python: _hitt_template_name (prompts/sections.py L601-615)
func hittTemplateName(role atschema.TeamRole, exposeHumanAgentsToTeammates bool) (string, bool) {
	switch role {
	case atschema.TeamRoleLeader:
		return "hitt_leader", true
	case atschema.TeamRoleTeammate, atschema.TeamRoleExternalCLI:
		if exposeHumanAgentsToTeammates {
			return "hitt_teammate", true
		}
		return "hitt_teammate_anonymous", true
	case atschema.TeamRoleHumanAgent:
		return "hitt_human_agent", true
	default:
		return "", false
	}
}

// hittContractBody 渲染 HITT 协作契约 markdown（仅规则）。
//
// 人类名册**不内联**在这里——人类成员在统一名册里以 [human] 标记出现。
// 四个模板里只有 hitt_human_agent.md 含 {{self_line}} 占位符，其余三个是
// 纯文本、不调 format。
//
// Python: _hitt_contract_body (prompts/sections.py L617-640)
func hittContractBody(
	role atschema.TeamRole,
	selfMemberName string,
	exposeHumanAgentsToTeammates bool,
	language string,
) (string, bool, error) {
	name, ok := hittTemplateName(role, exposeHumanAgentsToTeammates)
	if !ok {
		return "", false, nil
	}
	tpl, err := LoadTemplate(name, language)
	if err != nil {
		return "", false, err
	}
	if role == atschema.TeamRoleHumanAgent {
		selfLine := selfMemberLine(selfMemberName, language)
		return tpl.Render(map[string]string{"self_line": selfLine}), true, nil
	}
	return tpl.Content(), true, nil
}

// selfMemberLine 渲染「你的 member_name 是 X」这一行，未设置时返回空串。
// Python: _self_member_line (prompts/sections.py L592-599)
func selfMemberLine(selfName, language string) string {
	if selfName == "" {
		return ""
	}
	if language == "cn" {
		return "你的 member_name 是 `" + selfName + "`。\n"
	}
	return "Your member_name is `" + selfName + "`.\n"
}

// sectionsLabelsFor 返回指定语言的标签字典，默认回退到中文。
// 加 sections 前缀是因为 Go 侧本包内还有 messages.go 的同名函数：上游
// sections.py 的 _labels_for 与 messages.py 的 labels_for 分属两个模块、
// 名字本就不同，落到同一个 Go 包后两个函数的导出名会撞成 labelsFor。
// 两张表必须独立（同名字段取值不同），故按来源加前缀区分。
// Python: _labels_for(language) (openjiuwen/agent_teams/prompts/sections.py)
func sectionsLabelsFor(language string) map[string]string {
	if lbl, ok := labels[language]; ok {
		return lbl
	}
	return labels["cn"]
}
