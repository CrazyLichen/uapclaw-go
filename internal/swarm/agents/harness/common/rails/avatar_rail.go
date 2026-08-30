package rails

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AvatarPromptRail 数字分身 Rail — 处理所有 per-request 的 avatar 逻辑。
// 对齐 Python: AvatarPromptRail(DeepAgentRail)
//
// 职责:
// 1. BeforeModelCall: 根据 PermissionContext 动态注入/移除 avatar 相关 PromptSection
// 2. BeforeToolCall: 拦截群聊记忆禁写 + enable_memory=False 场景
type AvatarPromptRail struct {
	rails.DeepAgentRail
	// injectedSections 已注入的 PromptSection 名称集合
	injectedSections map[string]struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// avatarPromptPriority 数字分身提示词基础优先级
	// 对齐 Python: _AVATAR_PROMPT_PRIORITY = 110
	avatarPromptPriority = 110

	// avatarPromptRailPriority AvatarPromptRail 优先级
	// 对齐 Python: AvatarPromptRail.priority = 85
	avatarPromptRailPriority = 85
)

// ──────────────────────────── 全局变量 ────────────────────────────

// memoryWriteTools 记忆写入工具集合
// 对齐 Python: _MEMORY_WRITE_TOOLS = frozenset({"write_memory", "edit_memory"})
var memoryWriteTools = map[string]struct{}{
	"write_memory": {},
	"edit_memory":  {},
}

// memoryAllTools 所有记忆工具集合（记忆完全禁用时全部拦截）
var memoryAllTools = map[string]struct{}{
	"write_memory":  {},
	"edit_memory":   {},
	"read_memory":   {},
	"memory_search": {},
	"memory_get":    {},
}

// avatarLogComponent 日志组件标识
var avatarLogComponent = logger.ComponentAgentServer

// 编译时验证 AvatarPromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*AvatarPromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAvatarPromptRail 创建 AvatarPromptRail 实例。
// 对齐 Python: AvatarPromptRail.__init__()
func NewAvatarPromptRail() *AvatarPromptRail {
	r := &AvatarPromptRail{
		injectedSections: make(map[string]struct{}),
	}
	r.WithPriority(avatarPromptRailPriority)
	return r
}

// BeforeModelCall 模型调用前动态注入/移除 avatar 相关 PromptSection。
// 对齐 Python: AvatarPromptRail.before_model_call()
func (r *AvatarPromptRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 获取 SystemPromptBuilder
	// 对齐 Python L45-48: builder = getattr(getattr(self, "_deep_agent", None) or ctx.agent, "system_prompt_builder", None)
	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	// 清除上次注入的 sections
	// 对齐 Python L53-55: for name in list(self._injected_sections): builder.remove_section(name)
	for name := range r.injectedSections {
		builder.RemoveSection(name)
	}
	r.injectedSections = make(map[string]struct{})

	// 读取语言
	// 对齐 Python L57: language = getattr(builder, "language", "cn") or "cn"
	language := builder.Language()
	if language == "" {
		language = "cn"
	}

	// 1. 注入 forbidden_memory（优先级 113）
	// 对齐 Python L59-69: 尝试加载 forbidden_memory
	r.injectForbiddenMemory(builder, language)

	// 2. 从 context 获取 PermissionContext
	// 对齐 Python L73: perm_ctx = TOOL_PERMISSION_CONTEXT.get()
	permCtx := sschema.PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return nil
	}

	// 3. 判断数字分身模式
	// 对齐 Python L78: perm_ctx.group_digital_avatar and perm_ctx.avatar_mode
	isGroupDigitalAvatar := permCtx.GroupDigitalAvatar && permCtx.AvatarMode

	// 4. 数字分身身份提示词
	// 对齐 Python L78-87: avatar_identity section
	if isGroupDigitalAvatar {
		displayName := permCtx.AvatarPrincipalName
		if displayName == "" {
			displayName = permCtx.PrincipalUserID
		}
		content := buildAvatarPrompt(displayName, language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "avatar_identity",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority,
		})
		r.injectedSections["avatar_identity"] = struct{}{}
	}

	// 5. 群聊记忆禁写通知
	// 对齐 Python L89-108: group_chat_memory_notice section
	if isGroupDigitalAvatar {
		notice := buildGroupChatMemoryNotice(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "group_chat_memory_notice",
			Content:  map[string]string{language: notice},
			Priority: avatarPromptPriority + 1,
		})
		r.injectedSections["group_chat_memory_notice"] = struct{}{}
	}

	// 6. 记忆完全禁用
	// 对齐 Python L110-124: memory_fully_disabled section
	shouldDisableMemory := !permCtx.EnableMemory && permCtx.GroupDigitalAvatar && permCtx.AvatarMode
	if shouldDisableMemory {
		content := buildMemoryFullyDisabledPrompt(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "memory_fully_disabled",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority + 2,
		})
		r.injectedSections["memory_fully_disabled"] = struct{}{}
	}

	// 7. 多轮交互指引
	// 对齐 Python L126-134: interaction_guidance section
	if isGroupDigitalAvatar {
		content := buildInteractionPrompt(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "interaction_guidance",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority + 4,
		})
		r.injectedSections["interaction_guidance"] = struct{}{}
	}

	return nil
}

// BeforeToolCall 工具调用前拦截记忆工具。
// 对齐 Python: AvatarPromptRail.before_tool_call()
func (r *AvatarPromptRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L146-149: tool_name = ctx.inputs.tool_name; perm_ctx = TOOL_PERMISSION_CONTEXT.get()
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	permCtx := sschema.PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return nil
	}

	// 对齐 Python L157-162: should_disable_memory 判断
	shouldDisableMemory := !permCtx.EnableMemory && permCtx.GroupDigitalAvatar && permCtx.AvatarMode

	// 场景2：记忆完全禁用 — 拒绝所有记忆工具
	// 对齐 Python L164-171
	if shouldDisableMemory {
		if _, exists := memoryAllTools[toolInputs.ToolName]; exists {
			r.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] 记忆系统已禁用，禁止访问")
			return nil
		}
	}

	// 场景1：群聊数字分身 — 只拒绝写入
	// 对齐 Python L173-176
	isGroupDigitalAvatar := permCtx.GroupDigitalAvatar && permCtx.AvatarMode
	if isGroupDigitalAvatar {
		if _, exists := memoryWriteTools[toolInputs.ToolName]; exists {
			r.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] 群聊模式下禁止写入/编辑记忆文件")
			return nil
		}
	}

	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall + BeforeToolCall。
func (r *AvatarPromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// injectForbiddenMemory 注入 forbidden_memory PromptSection。
// 对齐 Python L59-69: 尝试加载 forbidden_memory
func (r *AvatarPromptRail) injectForbiddenMemory(builder saprompt.SystemPromptBuilderInterface, language string) {
	forbidden := commmem.GetForbiddenMemoryPrompt(language)
	if forbidden == "" {
		return
	}
	builder.AddSection(saprompt.PromptSection{
		Name:     "forbidden_memory",
		Content:  map[string]string{language: forbidden},
		Priority: avatarPromptPriority + 3,
	})
	r.injectedSections["forbidden_memory"] = struct{}{}
}

// rejectTool 跳过工具执行，设置拒绝消息。
// 对齐 Python L178-185: AvatarPromptRail._reject_tool()
func (r *AvatarPromptRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, toolInputs *agentinterfaces.ToolCallInputs, message string) {
	toolCallID := ""
	if toolInputs.ToolCall != nil {
		toolCallID = toolInputs.ToolCall.ID
	}
	cbc.Extra()["_skip_tool"] = true
	toolInputs.ToolResult = message
	toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, message)
	logger.Info(avatarLogComponent).
		Str("tool_name", toolInputs.ToolName).
		Str("message", message).
		Msg("AvatarPromptRail 拦截工具")
}

// buildAvatarPrompt 构建数字分身身份提示词。
// 对齐 Python: _build_avatar_prompt(principal_user_id, language)
func buildAvatarPrompt(principalName string, language string) string {
	if language == "cn" {
		var identity, perspective, boundary string
		if principalName != "" {
			identity = fmt.Sprintf("你当前正在群聊场景中作为 **%s** 的数字分身发言。", principalName)
			perspective = fmt.Sprintf("1. **第一人称视角**：始终以 %s 的身份和口吻回复，使用\"我\"而非\"他/她\"。", principalName)
			boundary = fmt.Sprintf("2. **承诺边界**：不要代替 %s 做出超出已知信息范围的承诺或决定。如果上下文不足，坦诚说明需要确认，而非编造事实。", principalName)
		} else {
			identity = "你当前正在群聊场景中作为用户本人的数字分身发言。"
			perspective = "1. **第一人称视角**：始终以用户本人的身份和口吻回复，使用\"我\"而非\"他/她\"。"
			boundary = "2. **承诺边界**：不要代替用户做出超出已知信息范围的承诺或决定。如果上下文不足，坦诚说明需要确认，而非编造事实。"
		}
		return "---\n\n# 数字分身模式\n\n" + identity + "以下规则**仅约束你在群聊中的对外回复方式**，不影响你的任何内部能力。\n\n## 回复风格约束\n\n" + perspective + "\n" + boundary + "\n3. **自然简洁**：回复风格应贴近真人在群聊中的表达习惯，简短、自然，避免过度正式或冗长。\n4. **不暴露身份**：不要提及自己是 AI、数字分身或机器人。\n\n## 能力不受影响\n\n数字分身模式**不限制**你的任何 agent 能力，以下功能照常执行：\n- **工具调用**：所有工具（文件操作、搜索、代码执行等）正常使用\n- **待办管理**：todo_create / todo_complete / todo_insert 等正常使用\n- **定时任务**：可以正常创建并执行 cron 任务和 heartbeat 任务\n- **技能调用**：所有已注册技能正常使用\n"
	}
	// 英文
	var identity, perspective, boundary string
	if principalName != "" {
		identity = fmt.Sprintf("You are currently acting as **%s**'s digital avatar in a group chat.", principalName)
		perspective = fmt.Sprintf("1. **First-person perspective**: Always reply as %s, using \"I\" instead of \"he/she\".", principalName)
		boundary = fmt.Sprintf("2. **Commitment boundary**: Do not make commitments or decisions beyond known information on behalf of %s.", principalName)
	} else {
		identity = "You are currently acting as the user's digital avatar in a group chat."
		perspective = "1. **First-person perspective**: Always reply as the user, using \"I\" instead of \"he/she\"."
		boundary = "2. **Commitment boundary**: Do not make commitments or decisions beyond known information on behalf of the user."
	}
	return "---\n\n# Digital Avatar Mode\n\n" + identity + " The rules below **only constrain your outward reply style** in group chat.\n\n## Reply Style Constraints\n\n" + perspective + "\n" + boundary + "\n3. **Natural and concise**: Reply style should resemble a real person's expression in group chat.\n4. **Do not reveal identity**: Never mention that you are an AI, digital avatar, or bot.\n"
}

// buildGroupChatMemoryNotice 构建群聊记忆禁写通知。
// 对齐 Python L96-108: 内联构建 group_chat_memory_notice
func buildGroupChatMemoryNotice(language string) string {
	if language == "cn" {
		return "\n[群聊模式：禁止调用 write_memory/edit_memory]\n"
	}
	return "\n[Group chat mode: write_memory/edit_memory calls are prohibited]\n"
}

// buildMemoryFullyDisabledPrompt 构建记忆完全禁用提示词。
// 对齐 Python: _build_memory_fully_disabled_prompt(language)
func buildMemoryFullyDisabledPrompt(language string) string {
	if language == "cn" {
		return "## 记忆系统 - 已完全禁用\n\n**记忆系统当前已完全禁用。**\n\n- **禁止** 使用任何记忆工具：\n  - 写入工具：write_memory、edit_memory\n  - 读取工具：read_memory、memory_search、memory_get\n- 如果用户询问历史信息或要求记住某些内容，回复：\"记忆系统当前已禁用，我无法访问历史记录或保存新信息。\"\n"
	}
	return "## Memory System - Fully Disabled\n\n**The memory system is currently fully disabled.**\n\n- **Do NOT** use any memory tools:\n  - Write tools: write_memory, edit_memory\n  - Read tools: read_memory, memory_search, memory_get\n- If the user asks about historical information or requests to remember something, reply: \"The memory system is currently disabled. I cannot access historical records or save new information.\"\n"
}

// buildInteractionPrompt 构建多轮交互追问指引提示词。
// 对齐 Python: _build_interaction_prompt(language)
func buildInteractionPrompt(language string) string {
	if language == "cn" {
		return "## 多轮交互指引\n\n在以下情况，你必须通过追问来明确需求，不要自行假设或跳过：\n\n### 何时必须追问\n1. **缺少关键参数**：任务需要具体参数但用户未提供（如订会议室但没说楼层、时间）\n2. **需求模糊或宽泛**：用户请求范围太大或方向不明确，直接执行可能偏离意图（如\"帮我写个报告\"\"做个调研\"\"整理一下\"）\n3. **存在多种理解**：请求可以有多种解读方式，不同理解会导致完全不同的执行结果\n4. **需要确认授权**：需要 principal（你代替的人）确认或授权才能执行\n\n### 群聊追问\n如果缺少的信息可以由群聊中的某位用户提供，在回复开头加上 `[群聊追问@用户名]`：\n- 例：`[群聊追问@张三] 请问需要预约哪个楼层的会议室？`\n- 系统会自动在群聊中 @张三 并追踪回复\n\n如果缺少的信息由发送请求的人自己补充即可，在回复开头加上 `[群聊追问]`（不带@）：\n- 例：`[群聊追问] 请问会议主题是什么？`\n- 例：`[群聊追问] 你说的调研报告是关于哪个方向的？需要覆盖哪些内容？`\n- 系统会自动追踪发送者的回复\n\n### 私聊追问\n如果需要 principal（你代替的人）确认或授权，在回复开头加上 `[私聊追问]`：\n- 例：`[私聊追问] 张三要订会议室，你确认吗？`\n- 系统会自动私聊 principal 并在群聊中发送简短确认\n\n### 注意事项\n- 需求模糊时**必须追问**，不要自行猜测用户意图后直接执行，否则很可能白做\n- 追问时给出具体选项或方向提示，帮助用户快速回复（如\"是A方向还是B方向？\"而非\"你要什么？\"）\n- 追问前缀必须放在回复的最开头\n- 收到追问的回答后，继续完成任务即可，不需要再加前缀\n- 收到追问回答后，只针对当前追问的任务继续处理，不要与之前的其他任务混淆\n- 如果群聊历史中存在多个不同的任务，务必根据追问上下文区分，只处理当前任务\n"
	}
	return "## Multi-turn Interaction Guidance\n\nYou MUST follow up to clarify requirements in these situations — do NOT assume or skip:\n\n### When You Must Follow Up\n1. **Missing key parameters**: The task requires specific parameters the user hasn't provided (e.g., booking a room without specifying floor or time)\n2. **Vague or broad requests**: The request is too broad or unclear — executing directly may miss the user's intent (e.g., \"write a report\", \"do some research\", \"organize this\")\n3. **Ambiguous interpretation**: The request could be understood in multiple ways, leading to very different outcomes\n4. **Need confirmation**: You need the principal (the person you represent) to confirm or authorize\n\n### Group Follow-up\nIf the missing information can be provided by someone in the group chat, prefix your reply with `[群聊追问@Username]`:\n- Example: `[群聊追问@张三] Which floor meeting room do you need?`\n- The system will automatically @mention the user and track their reply\n\nIf the sender can provide the missing information themselves, prefix your reply with `[群聊追问]` (without @):\n- Example: `[群聊追问] What is the meeting topic?`\n- Example: `[群聊追问] What direction should the research report cover? What topics should it include?`\n- The system will automatically track the sender's reply\n\n### DM Follow-up\nIf you need the principal (the person you represent) to confirm or authorize, prefix your reply with `[私聊追问]`:\n- Example: `[私聊追问] 张三 wants to book a meeting room, do you confirm?`\n- The system will automatically DM the principal and send a brief acknowledgment in the group\n\n### Notes\n- When the request is vague, you **MUST follow up** — do NOT guess the user's intent and execute, or you'll likely waste effort\n- When following up, provide specific options or directional hints to help the user reply quickly (e.g., \"Direction A or Direction B?\" rather than \"What do you want?\")\n- The follow-up prefix must be at the very beginning of your reply\n- After receiving the answer, continue completing the task without any prefix\n- After receiving the answer, only process the current task from the follow-up, do not mix with previous tasks\n- If the group chat history contains multiple different tasks, distinguish them based on the follow-up context and only handle the current one\n"
}
