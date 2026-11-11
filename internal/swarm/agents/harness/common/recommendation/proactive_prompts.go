package recommendation

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

// 决策与话术 prompt 模板集合。
//
// 模板按 preferred_language 双语化（zh/en）。中文语境的约束说明（引用原话、去重锚点等）
// 用中文表述最精确；英文语境给等义英文版。两个版本必须同步维护——改一处语义必须同步另一处。
//
// ⚠️ 内容逐字复刻上游 Python，禁止本地化或改写措辞（规则「提示词一比一复刻」）。
// Go 侧模板仍保留 `{{` / `}}` 写法（与 Python 的 format 转义一致），
// 由 RenderPromptTemplate 统一替换为单个花括号。
//
// Python: jiuwenswarm/agents/harness/common/recommendation/proactive_prompts.py
const (
	// unifiedAnalysisPromptZH 决策 prompt 中文版
	// Python: proactive_prompts.py _UNIFIED_ANALYSIS_PROMPT_ZH
	unifiedAnalysisPromptZH = "你是用户洞察与推荐助手。以下是用户的综合情境信息（含当前对话、历史推荐、日程与候选 skill）。\n" +
		"\n" +
		"{conversation_summary}\n" +
		"\n" +
		"📊 推荐策略规则（基于用户历史反馈）：\n" +
		"{decision_rules_text}\n" +
		"\n" +
		"请基于以上信息，决定是否需要主动与用户发起推荐。输出 JSON。\n" +
		"\n" +
		"⚠️ 重要：只从「当前对话」的 `[User]:` 消息中理解用户意图。\n" +
		"- `[Assistant]:` 是系统回复（含推荐话术、skill 介绍），不是用户意图\n" +
		"- 「候选 Skill」是系统安装的工具清单，不是用户兴趣\n" +
		"- 「历史推荐记录」是系统已推过的内容，不是用户意图。按类型区分对待已推 target：\n" +
		"  · need_exploration：探索方向是一次性话题，禁止再选已推过的 target（含换皮重复：\n" +
		"    同主题换措辞也算重复，应推真正不同的方向）。\n" +
		"  · skill_recommend：去重锚点是「skill + 它用来做的事」，不是 skill 名本身。同 skill\n" +
		"    用来做不同的事 = 可再推（如翻译技能先翻文档、后翻游戏 UI）；只有同 skill + 同一件\n" +
		"    事时才算重复（别同一任务反复推同一 skill）。\n" +
		"  · task_reminder：同一待办未闭环时再提醒是合理的，不算重复。\n" +
		"- 参考「推荐策略规则」调整推荐方式，如规则与当前情境明显冲突可忽略\n" +
		"\n" +
		"推荐类型（优先级从高到低）：\n" +
		"\n" +
		"a. \"task_reminder\"：提醒用户没做完、没闭环或快到点的事。\n" +
		"   - 优先取「即将到来的日程」中近期事件（会议、约会、截止），target 为事件标题。\n" +
		"   - 其次取当前对话中用户明确表达但尚未完成的承诺（\"回头整理\"\"待会儿发给你\"），\n" +
		"     target 为该待办事项，reason 必须引用对应的 [User]: 原话。\n" +
		"\n" +
		"b. \"skill_recommend\"：从解决用户已有任务、或可能需要做的事的角度，推荐能帮上手的已安装 Skill。\n" +
		"   - 先从当前对话 + 日程事件中推断用户「要做的事 / 可能要做的事」（一个具体场景/任务，\n" +
		"     可概括推断，不必用户原话明说），再在「候选 Skill」里找能应对该事的 skill——不是泛泛推工具。\n" +
		"   - reason 说明推断出的「要做的事」是什么、这个 skill 怎么帮上忙（可概括推断，\n" +
		"     不必逐字引用原话；有合适的 [User]: 原话可引用时优先引用，但不是硬约束）。\n" +
		"\n" +
		"c. \"need_exploration\"：从已有内容延申出用户可能感兴趣的新话题。\n" +
		"   - 从当前对话/日程已有的内容出发，往一个顺理成章、用户可能感兴趣但没主动提过\n" +
		"     的方向延申；target 是探索方向（非 skill 名）。\n" +
		"   - reason 说明从哪段已有内容延申、为什么用户可能感兴趣（可概括，不必逐字引用原话）。\n" +
		"   - 只要能从已有内容合理延申即可，不必拘泥于用户\"明确表达过兴趣\"。\n" +
		"\n" +
		"⚠️ 约束：\n" +
		"- decision.type 为 \"skill_recommend\" 时，target 必须是「候选 Skill」列表里实际存在的 skill 名称。\n" +
		"- task_reminder 的 reason 必须逐字引用依据来源（[User]: 原话用「」括起；日程事件用事件\n" +
		"  标题），禁止概括改写——待办事项需精确指代用户具体哪件事，主 agent 据此生成话术、\n" +
		"  不回翻自己上下文窗口核实，若 reason 概括而非原话，主 agent 在窗口里找不到对应字面\n" +
		"  内容会误判为凭空推荐并当场否定，产出自相矛盾的话术。\n" +
		"- skill_recommend / need_exploration 的 reason 允许概括推断（推断出要做的事 / 延申的\n" +
		"  方向本就是主观判断，不必逐字引用原话）；有合适 [User]: 原话时可优先引用，非硬约束。\n" +
		"- 如当前无合适推荐，decision 返回 null。\n" +
		"- target 与 reason 使用简体中文书写（系统语言设置）。skill 名、待办标题、引用用户\n" +
		"  原话的短语可保留原文，其余必须是中文——下游话术按系统语言生成并原样嵌入这些字段。\n" +
		"\n" +
		"输出 JSON 格式：\n" +
		"{{\n" +
		"  \"decision\": {{\n" +
		"    \"type\": \"skill_recommend|task_reminder|need_exploration\",\n" +
		"    \"target\": \"skill名称/待办事项/探索方向\",\n" +
		"    \"reason\": \"推荐原因（引用 [User]: 对话内容或日程事件）\",\n" +
		"    \"urgency\": 0.0-1.0\n" +
		"  }} | null\n" +
		"}}"

	// unifiedAnalysisPromptEN 决策 prompt 英文版
	// Python: proactive_prompts.py _UNIFIED_ANALYSIS_PROMPT_EN
	unifiedAnalysisPromptEN = "You are a user-insight and recommendation assistant. Below is the user's situation \\\n" +
		"report (current conversation, past recommendations, schedule, and candidate skills).\n" +
		"\n" +
		"{conversation_summary}\n" +
		"\n" +
		"📊 Recommendation strategy rules (from the user's past feedback):\n" +
		"{decision_rules_text}\n" +
		"\n" +
		"Based on the above, decide whether to proactively reach out to the user with a \\\n" +
		"recommendation. Output JSON.\n" +
		"\n" +
		"⚠️ Important: understand user intent ONLY from `[User]:` messages in the \"current \\\n" +
		"conversation\".\n" +
		"- `[Assistant]:` messages are system replies (recommendation copy, skill intros), \\\n" +
		"not user intent.\n" +
		"- \"Candidate Skills\" is the list of installed tools, not the user's interests.\n" +
		"- \"Past recommendations\" are things the system already suggested, not user intent. \\\n" +
		"Treat already-pushed targets differently by type:\n" +
		"  · need_exploration: an exploration direction is a one-shot topic. Never pick an \\\n" +
		"already-pushed target again (including paraphrased repeats — same topic reworded \\\n" +
		"still counts; push a genuinely different direction).\n" +
		"  · skill_recommend: the dedup anchor is \"the skill + what it is used for\", not the \\\n" +
		"skill name alone. The same skill for a different task = pushable again (e.g. a \\\n" +
		"translation skill for documents, then for game UI); only the same skill + the same \\\n" +
		"task counts as a duplicate.\n" +
		"  · task_reminder: re-reminding an unclosed todo is legitimate, not a duplicate.\n" +
		"- Consult the \"recommendation strategy rules\" to adjust your approach; ignore them \\\n" +
		"if they clearly conflict with the current situation.\n" +
		"\n" +
		"Recommendation types (highest priority first):\n" +
		"\n" +
		"a. \"task_reminder\": remind the user of something unfinished, unclosed, or due soon.\n" +
		"   - Prefer recent events from \"upcoming schedule\" (meetings, appointments, \\\n" +
		"deadlines); target is the event title.\n" +
		"   - Otherwise pick commitments the user explicitly made in the current conversation \\\n" +
		"but has not completed (\"I'll organize it later\", \"I'll send it to you soon\"); target \\\n" +
		"is that todo item, and reason MUST quote the corresponding [User]: text verbatim.\n" +
		"\n" +
		"b. \"skill_recommend\": recommend an installed Skill that helps with something the \\\n" +
		"user is doing or may need to do.\n" +
		"   - First infer from the conversation + schedule events \"what the user is about to \\\n" +
		"do / may need to do\" (a concrete scenario or task; inference is allowed, the user \\\n" +
		"need not have said it verbatim), then find a candidate Skill that handles it — not \\\n" +
		"a generic tool plug.\n" +
		"   - reason explains the inferred task and how the skill helps (inference allowed; \\\n" +
		"quoting a fitting [User]: line is welcome but not mandatory).\n" +
		"\n" +
		"c. \"need_exploration\": extend from existing content toward a new topic the user may \\\n" +
		"find interesting.\n" +
		"   - Starting from conversation/schedule content, extend in one natural direction \\\n" +
		"the user hasn't raised but would plausibly care about; target is the direction \\\n" +
		"(not a skill name).\n" +
		"   - reason explains what existing content it extends and why the user might care \\\n" +
		"(summary allowed, no verbatim quoting required).\n" +
		"\n" +
		"⚠️ Constraints:\n" +
		"- When decision.type is \"skill_recommend\", target MUST be a skill name that exists \\\n" +
		"in the \"Candidate Skills\" list.\n" +
		"- For task_reminder, reason MUST quote its source verbatim ([User]: text wrapped in \\\n" +
		"quotes; schedule events by their exact title) — no paraphrasing. The todo must be \\\n" +
		"precisely attributable to the user's specific matter: the main agent generates copy \\\n" +
		"from reason without re-checking its own context window; if reason paraphrases \\\n" +
		"instead of quoting, the main agent cannot find the literal text, judges the \\\n" +
		"recommendation fabricated, and produces self-contradicting copy.\n" +
		"- For skill_recommend / need_exploration, reason may be inferential (inferring the \\\n" +
		"task / direction is inherently subjective); quoting a fitting [User]: line is \\\n" +
		"preferred when available, but not mandatory.\n" +
		"- If nothing fits right now, decision returns null.\n" +
		"- ⚠️ Write target and reason in ENGLISH (the system language setting). Skill names, \\\n" +
		"todo titles, and short verbatim quotes from the user's conversation may stay in \\\n" +
		"their original language, but everything else in target/reason must be English — \\\n" +
		"the downstream copy is generated in English and embeds these fields verbatim.\n" +
		"\n" +
		"Output JSON format:\n" +
		"{{\n" +
		"  \"decision\": {{\n" +
		"    \"type\": \"skill_recommend|task_reminder|need_exploration\",\n" +
		"    \"target\": \"skill name / todo item / exploration direction\",\n" +
		"    \"reason\": \"why to recommend (quoting [User]: text or a schedule event)\",\n" +
		"    \"urgency\": 0.0-1.0\n" +
		"  }} | null\n" +
		"}}"

	// directivePromptZH 话术指令 prompt 中文版
	// Python: proactive_prompts.py _DIRECTIVE_PROMPT_ZH
	directivePromptZH = "[主动推荐指令]\n" +
		"推荐类型：{rec_type}\n" +
		"推荐内容：{target}\n" +
		"推荐依据：{reason}\n" +
		"\n" +
		"{style_rules_section}\n" +
		"\n" +
		"请基于以上信息，以助手身份自然地向用户发起这条推荐。要求：\n" +
		"- 2-3句话，口语化，不像广告\n" +
		"- 不要直接说\"这是系统推荐\"，自然融入对话\n" +
		"- ⚠️ 必须使用简体中文生成话术（这是系统层面的语言设置，即使用户消息是其他语言）。\n" +
		"- ⚠️ 「推荐依据」是本次推荐的素材来源，直接据此生成话术，不必回翻自己的上下文窗口核实。\n" +
		"- ⚠️ 话术必须自然地包含「推荐内容」的字面内容，这是硬约束：\n" +
		"  · skill_recommend：必须出现「{target}」这个 skill 名（用户后续说\"装上/用用看\"时，\n" +
		"    助手要能从对话历史里知道推荐的是哪个 skill——directive 指令本身不进历史，\n" +
		"    skill 名只能靠这条话术带进 context。自然地嵌入即可，不要生硬报名字）。\n" +
		"  · task_reminder：必须出现「{target}」这个待办/事件名。\n" +
		"  · need_exploration：必须出现「{target}」这个探索方向。\n" +
		"- 给出行动引导（如\"要不要现在试试\" / \"需要我帮你开始吗\"）\n" +
		"- 语气按推荐类型调整：\n" +
		"  · task_reminder：关切提醒，像贴心的助手\n" +
		"  · skill_recommend：从用户痛点切入，自然引出工具；落到行动上倾向于\"用它把这件事办了\"，\n" +
		"    让用户感受到是直接上手解决问题，而不是先走一道准备/配置\n" +
		"  · need_exploration：像同事间的建议，让用户觉得这个方向有意思\n" +
		"- 如有「话术风格要求」，请遵循这些规则调整语气和结构\n" +
		"输出纯文本话术，不要 JSON，不要标题。\n"

	// directivePromptEN 话术指令 prompt 英文版
	// Python: proactive_prompts.py _DIRECTIVE_PROMPT_EN
	directivePromptEN = "[Proactive recommendation directive]\n" +
		"Recommendation type: {rec_type}\n" +
		"Recommendation content: {target}\n" +
		"Rationale: {reason}\n" +
		"\n" +
		"{style_rules_section}\n" +
		"\n" +
		"Based on the above, naturally initiate this recommendation to the user as their \\\n" +
		"assistant. Requirements:\n" +
		"- 2-3 sentences, conversational, not ad-like\n" +
		"- Don't say \"this is a system recommendation\"; blend it naturally into the dialog\n" +
		"- ⚠️ You MUST write the copy in English (this is the system-level language setting, \\\n" +
		"even if user messages are in other languages).\n" +
		"- ⚠️ The \"Rationale\" is the source material for this recommendation — generate the \\\n" +
		"copy directly from it; do not go re-verify against your own context window.\n" +
		"- ⚠️ The copy must naturally contain the literal text of \"Recommendation content\". \\\n" +
		"This is a hard constraint:\n" +
		"  · skill_recommend: the skill name \"{target}\" must appear (when the user later \\\n" +
		"says \"install it / let me try\", the assistant must be able to tell from conversation \\\n" +
		"history which skill was meant — the directive itself never enters history, so the \\\n" +
		"skill name can only reach context via this copy. Weave it in naturally, don't \\\n" +
		"blurt the name out stiffly).\n" +
		"  · task_reminder: the todo/event name \"{target}\" must appear.\n" +
		"  · need_exploration: the exploration direction \"{target}\" must appear.\n" +
		"- Include a call to action (e.g. \"want to try it now?\" / \"shall I get started for you?\")\n" +
		"- Adjust the tone by recommendation type:\n" +
		"  · task_reminder: caring reminder, like a considerate assistant\n" +
		"  · skill_recommend: start from the user's pain point and lead naturally to the \\\n" +
		"tool; lean toward \"use it to get this done now\" so the user feels they are directly \\\n" +
		"solving the problem rather than going through setup first\n" +
		"  · need_exploration: suggest it like a colleague sharing something interesting\n" +
		"- If \"style rules\" are provided above, follow them to adjust tone and structure.\n" +
		"Output plain-text copy only — no JSON, no headings.\n"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UnifiedAnalysisPrompt 按语言返回决策 prompt 模板（未渲染，含 {conversation_summary} 等占位符）。
// Python: proactive_prompts.py:164 unified_analysis_prompt
func UnifiedAnalysisPrompt(language string) string {
	if strings.ToLower(strings.TrimSpace(language)) == "en" {
		return unifiedAnalysisPromptEN
	}
	return unifiedAnalysisPromptZH
}

// DirectivePrompt 按语言返回话术指令 prompt 模板（未渲染，含 {rec_type} 等占位符）。
// Python: proactive_prompts.py:238 directive_prompt
func DirectivePrompt(language string) string {
	if strings.ToLower(strings.TrimSpace(language)) == "en" {
		return directivePromptEN
	}
	return directivePromptZH
}

// RenderPromptTemplate 单次扫描渲染模板，等价 Python str.format 的命名占位替换。
//
// 语义要点（与 str.format 对齐）：
//   - `{{` → 字面量 `{`，`}}` → 字面量 `}`
//   - `{name}` → replacements[name]；未提供的 name 原样保留 `{name}`
//   - **替换值不做二次展开**：值是原样输出的，不会因含 `{x}` 再被替换
//
// 实现必须单次扫描：曾经先 ReplaceAll 占位符再还原哨兵的写法，会因 map 遍历顺序
// 让先替换进去的值被后一轮替换命中，产生与 str.format 不同的结果。
func RenderPromptTemplate(template string, replacements map[string]string) string {
	var out strings.Builder
	out.Grow(len(template))

	for i := 0; i < len(template); i++ {
		ch := template[i]
		if ch != '{' && ch != '}' {
			out.WriteByte(ch)
			continue
		}
		// 双花括号 = 转义，输出单个花括号
		if i+1 < len(template) && template[i+1] == ch {
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == '}' {
			// 孤立右花括号：str.format 会报错，此处按字面量输出（模板本身不含此情形）
			out.WriteByte(ch)
			continue
		}
		// 尝试匹配 {name}：name 内不得出现 '{'（str.format 的字段名规则），
		// 否则会把嵌套结构里的 { 到远端 } 误当成一个占位符名。
		end := -1
		for j := i + 1; j < len(template); j++ {
			if template[j] == '{' {
				break
			}
			if template[j] == '}' {
				end = j
				break
			}
		}
		if end < 0 {
			out.WriteByte(ch)
			continue
		}
		name := template[i+1 : end]
		if value, ok := replacements[name]; ok {
			out.WriteString(value)
			i = end
			continue
		}
		// 未知占位符原样保留
		out.WriteString(template[i : end+1])
		i = end
	}
	return out.String()
}
