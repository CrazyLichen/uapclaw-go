package dreaming

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// promptCodeZH code 模式中文提示词模板。
// Python: sweeper.py _PROMPT_CODE
// 格式化占位符：%s=existing_knowledge, %s=compressed_session, %d=max_items
var promptCodeZH = `你是技术经验提取助手。下面是开发者与 AI 编程助手的对话（已压缩）。

目标：提取**只有亲历这次任务才能得到**的经验、踩坑、结论，帮用户在未来类似任务中少走弯路。
绝大多数对话沉淀不了知识，输出 [] 是常态，不要凑数。

## 值得提取（必须带"为什么/教训"，不是平铺事实）

- 踩坑根因：症状 X，常规思路 W 为何不通，真正根因 Y，解法 Z
- 被验证的非显而易见行为：API / 库 / 工具在边界情况下与文档或直觉不符的实际表现
- 设计决策的"为什么"：在哪些约束下选 A 不选 B，放弃 B 的代价
- 项目隐含约定：跨模块契约、不可违反的不变量、grep 难发现的命名 / 结构规则
- 环境 / 部署 / 工具链的坑：版本组合、平台差异、CI 行为
- 被否决的方案及原因

## 严禁提取

- AI 助手自身运行机制：内置工具行为、系统提示规则、记忆 / 钩子 / 技能等内部机制
- 通用编程常识：入门资料里都有的内容
- 对话流水账：做了什么的过程描述，而非沉淀出的结论
- 静态可查事实：grep / 看代码立即可得的信息
- 一次性任务状态：PR 编号、当前进度、临时调试目标
- 已在下方"已有知识库"中记录过的

## 自检三问（每条候选都过一遍）

1. 一周后做类似任务，没这条会怎样？看代码 / 查文档能想起的 → 丢弃
2. 这是关于用户的代码 / 项目 / 领域，还是关于 AI 助手的工具 / 机制？后者一律丢弃
3. 能否写成"因为 X 所以 Y / 不要 Z"或"X 出过问题，根因是 Y"？只能写成平铺事实 → 丢弃

## 已有知识库

%s

## 对话记录

注：仅含用户提问和助手最终回复，无工具调用细节。无法判断时宁可不提取。

%s

## 输出

JSON 数组。**绝大多数情况输出 []**。最多 %d 条。

` + "```json" + `
[
  {
    "title": "简洁标题（10-30 字，点明经验 / 教训）",
    "content": "背景 + 现象 + 根因 / 为什么 + 结论 / 做法，含具体示例，脱离对话上下文可独立阅读"
  }
]
` + "```" + `

`

// promptAgentZH agent 模式中文提示词模板。
// Python: sweeper.py _PROMPT_AGENT
// 格式化占位符：%s=existing_knowledge, %s=compressed_session, %d=max_items
var promptAgentZH = `你是一个记忆整理助手。下面是一个用户与 AI 助手的对话记录（已压缩）。

请从中提取值得长期记住的信息。每条记忆应当脱离原始对话语境后仍然可读、可用。

## 提取标准

值得提取的：
- 用户明确表达的偏好（回复风格、语言习惯、关注重点等）
- 用户的个人背景信息（职业、技术栈、所在团队/公司、角色等）
- 用户提到的重要事实（项目名称、截止日期、关键人物等）
- 用户关心的领域知识或专业话题
- 用户纠正助手的地方（说明助手之前的认知有误）
- 反复出现的交互模式

不值得提取的：
- 一次性的事务请求
- 对话中的寒暄、确认、感谢
- 已在下方"已有记忆"中记录过的内容

## 已有记忆

%s

## 对话记录

%s

## 输出格式

输出 JSON 数组，每个元素是一条独立记忆。无值得提取的信息则输出 []。最多 %d 条。

` + "```json" + `
[
  {
    "title": "简洁的标题（10-30字）",
    "content": "完整的记忆描述，用陈述句描述事实，脱离对话上下文后可独立阅读"
  }
]
` + "```" + `

要求：每条记忆独立成篇，宁缺毋滥。
`

// promptCodeEN code 模式英文提示词模板。
// Python: sweeper.py _PROMPT_CODE_EN
// 格式化占位符：%s=existing_knowledge, %s=compressed_session, %d=max_items
var promptCodeEN = `You are a technical experience extraction assistant. Below is a compressed conversation between a developer and an AI coding assistant.

Goal: Extract experiences, pitfalls, and conclusions that can **only be gained by personally going through this task**, helping the user avoid detours in similar future tasks.
Most conversations yield no extractable knowledge — outputting [] is normal, don't force entries.

## Worth Extracting (must include "why/lesson learned", not just plain facts)

- Pitfall root causes: symptom X, why the obvious approach W didn't work, actual root cause Y, solution Z
- Verified non-obvious behaviors: actual behavior of API/library/tool in edge cases that contradicts docs or intuition
- Design decision rationale: why choose A over B under what constraints, what is sacrificed by not choosing B
- Project implicit conventions: cross-module contracts, inviolable invariants, naming/structure rules hard to find via grep
- Environment/deployment/toolchain pitfalls: version combinations, platform differences, CI behaviors
- Rejected approaches and reasons

## Strictly Forbidden

- AI assistant's own operating mechanisms: built-in tool behaviors, system prompt rules, internal mechanisms like memory/hooks/skills
- General programming common sense: content readily available in introductory materials
- Conversation logs: process descriptions of what was done, rather than distilled conclusions
- Statically searchable facts: information immediately obtainable by grep/reading code
- One-time task state: PR numbers, current progress, temporary debugging goals
- Content already recorded in the "Existing Knowledge Base" below

## Self-check Three Questions (run each candidate through)

1. A week from now doing a similar task, what if this entry were missing? If you could figure it out from code/docs → discard
2. Is this about the user's code/project/domain, or about the AI assistant's tools/mechanisms? The latter → always discard
3. Can you phrase it as "Because X, therefore Y / Don't do Z" or "X caused problems, root cause was Y"? If it can only be plain facts → discard

## Existing Knowledge Base

%s

## Conversation Log

Note: Contains only user questions and assistant final responses, no tool call details. When in doubt, don't extract.

%s

## Output

JSON array. **Most cases output []**. Max %d entries.

` + "```json" + `
[
  {
    "title": "Concise title (10-30 chars, highlighting experience/lesson)",
    "content": "Background + phenomenon + root cause / why + conclusion / approach, with concrete examples, readable independently from conversation context"
  }
]
` + "```" + `

`

// promptAgentEN agent 模式英文提示词模板。
// Python: sweeper.py _PROMPT_AGENT_EN
// 格式化占位符：%s=existing_knowledge, %s=compressed_session, %d=max_items
var promptAgentEN = `You are a memory organization assistant. Below is a compressed conversation between a user and an AI assistant.

Please extract information worth remembering long-term. Each memory entry should be readable and usable independently from the original conversation context.

## Extraction Criteria

Worth extracting:
- User's explicitly expressed preferences (response style, language habits, focus areas, etc.)
- User's personal background information (profession, tech stack, team/company, role, etc.)
- Important facts mentioned by the user (project names, deadlines, key people, etc.)
- Domain knowledge or professional topics the user cares about
- Places where the user corrected the assistant (indicating the assistant's previous understanding was wrong)
- Recurring interaction patterns

Not worth extracting:
- One-time transactional requests
- Small talk, acknowledgments, thanks in conversation
- Content already recorded in "Existing Memories" below

## Existing Memories

%s

## Conversation Log

%s

## Output Format

Output a JSON array, each element is an independent memory entry. Output [] if nothing worth extracting. Max %d entries.

` + "```json" + `
[
  {
    "title": "Concise title (10-30 characters)",
    "content": "Complete memory description in declarative sentences, describing facts, readable independently from conversation context"
  }
]
` + "```" + `

Requirements: Each memory entry is self-contained. Better to have nothing than to force entries.
`

// promptMap 提示词模板映射：key = (mode, language)。
// Python: self._prompt_map
var promptMap = map[[2]string]string{
	{"code", "zh"}:  promptCodeZH,
	{"code", "en"}:  promptCodeEN,
	{"agent", "zh"}: promptAgentZH,
	{"agent", "en"}: promptAgentEN,
}

// sysMsgMap 系统消息映射：key = (mode, language)。
// Python: self._sys_msg_map
var sysMsgMap = map[[2]string]string{
	{"code", "zh"}:  "你是技术经验提取助手。严格输出 JSON 数组。",
	{"code", "en"}:  "You are a technical knowledge extractor. Output JSON array strictly.",
	{"agent", "zh"}: "你是记忆整理助手。严格输出 JSON 数组。",
	{"agent", "en"}: "You are a memory organizer. Output JSON array strictly.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPrompt 根据 mode 和 language 返回提示词模板，回退到 code/zh。
// Python: prompt_template = self._prompt_map.get(key, _PROMPT_CODE)
func GetPrompt(mode, language string) string {
	key := [2]string{mode, language}
	if tmpl, ok := promptMap[key]; ok {
		return tmpl
	}
	return promptMap[[2]string{"code", "zh"}]
}

// GetSysMsg 根据 mode 和 language 返回系统消息，回退到 code/zh。
// Python: sys_msg = self._sys_msg_map.get(key, self._sys_msg_map[("code", "zh")])
func GetSysMsg(mode, language string) string {
	key := [2]string{mode, language}
	if msg, ok := sysMsgMap[key]; ok {
		return msg
	}
	return sysMsgMap[[2]string{"code", "zh"}]
}

// ──────────────────────────── 非导出函数 ────────────────────────────
