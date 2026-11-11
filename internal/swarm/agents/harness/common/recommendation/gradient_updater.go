package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 梯度更新 prompt 模板集合。
//
// 三组（统一 / 显式反馈专用 / 隐式反馈专用）各含 zh/en 两版，全部**逐字复刻上游**。
// 显式与隐式不共用统一模板：显式只能生成 target 规则，隐式可生成任意类型，
// 约束不同故 prompt 不同（上游即如此，不可合并）。
//
// Go 侧保留 `{{` / `}}` 写法（与 Python format 转义一致），
// 由 RenderPromptTemplate 统一替换。
//
// Python: jiuwenswarm/agents/harness/common/recommendation/gradient_updater.py
const (
	// gradientUpdatePromptZH 统一梯度更新 prompt 中文版
	// Python: gradient_updater.py GRADIENT_UPDATE_PROMPT_ZH
	gradientUpdatePromptZH = "你是推荐策略分析师。根据用户反馈，更新推荐策略规则。\n" +
		"\n" +
		"【用户反馈】\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"【已有策略规则】\n" +
		"{existing_rules_text}\n" +
		"\n" +
		"请分析用户反馈，提取改进建议并更新策略规则。\n" +
		"\n" +
		"⚠️ 反馈类型与规则生成的对应关系：\n" +
		"\n" +
		"1. 显式反馈（点赞/点踩）：\n" +
		"   - 点赞表示用户对该推荐话题感兴趣\n" +
		"   - 点踩表示用户对该推荐话题不感兴趣\n" +
		"   - 只能生成 target 类型规则（关于\"推什么/不推什么\"）\n" +
		"   - 示例：用户点赞\"翻译技能\" → \"用户对翻译类话题感兴趣\"\n" +
		"   - 示例：用户点踩\"日程提醒\" → \"避免推荐日程类话题\"\n" +
		"\n" +
		"2. 隐式反馈（用户文本回复）：\n" +
		"   - 用户明确表达了意见或建议\n" +
		"   - 可以生成任意类型规则（target/relation/tone/structure）\n" +
		"   - 必须基于用户明确表达的内容，不要猜测\n" +
		"\n" +
		"⚠️ 规则分类：\n" +
		"- target: 关于\"推什么\"（如\"不要推翻译类\"、\"优先推荐效率类\"）\n" +
		"- relation: 关于\"怎么关联\"（如\"关联日程要有实际关联\"、\"引用用户原话\"）\n" +
		"- tone: 关于\"语气风格\"（如\"简洁克制\"、\"不要过度热情\"）\n" +
		"- structure: 关于\"话术结构\"（如\"1-2句话\"、\"不要解释功能\"）\n" +
		"\n" +
		"每条规则一句话，不超过25字。⚠️ 规则必须用简体中文书写。\n" +
		"\n" +
		"⚠️ 避免语义重复：如果新反馈与已有规则相关，优先 revise 已有规则，而非 add 新规则。\n" +
		"\n" +
		"输出 JSON：\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"新规则\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"修正后的规则\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"字段说明：\n" +
		"- action: \"add\" | \"revise\" | \"drop\"\n" +
		"- rule: 规则内容（add/revise 必填）\n" +
		"- category: 规则分类（add/revise 必填）\n" +
		"- gradient_id: 要修改/删除的规则ID（revise/drop 必填）\n" +
		"\n" +
		"如果没有需要更新的，返回 {{\"operations\": []}}。\n"

	// gradientUpdatePromptEN 统一梯度更新 prompt 英文版
	// Python: gradient_updater.py GRADIENT_UPDATE_PROMPT_EN
	gradientUpdatePromptEN = "You are a recommendation-strategy analyst. Update the recommendation strategy \\\n" +
		"rules based on user feedback.\n" +
		"\n" +
		"[User feedback]\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"[Existing strategy rules]\n" +
		"{existing_rules_text}\n" +
		"\n" +
		"Analyze the feedback, extract improvement suggestions, and update the rules.\n" +
		"\n" +
		"⚠️ Feedback type → rule mapping:\n" +
		"\n" +
		"1. Explicit feedback (like/dislike):\n" +
		"   - A like means the user is interested in the recommended topic\n" +
		"   - A dislike means the user is not interested in that topic\n" +
		"   - Only target-type rules may be generated (what to recommend / not recommend)\n" +
		"   - Example: user likes \"translation skill\" → \"user is interested in translation topics\"\n" +
		"   - Example: user dislikes \"schedule reminders\" → \"avoid recommending schedule topics\"\n" +
		"\n" +
		"2. Implicit feedback (the user's text reply):\n" +
		"   - The user clearly expressed an opinion or suggestion\n" +
		"   - Any rule type may be generated (target/relation/tone/structure)\n" +
		"   - Base rules only on what the user explicitly said; do not guess\n" +
		"\n" +
		"⚠️ Rule categories:\n" +
		"- target: what to push (e.g. \"don't push translation\", \"prefer productivity topics\")\n" +
		"- relation: how to relate (e.g. \"schedule links must be real\", \"quote the user's words\")\n" +
		"- tone: style (e.g. \"concise and restrained\", \"don't be overly enthusiastic\")\n" +
		"- structure: copy structure (e.g. \"1-2 sentences\", \"don't explain features\")\n" +
		"\n" +
		"One sentence per rule, max 25 words. ⚠️ Rules MUST be written in English.\n" +
		"\n" +
		"⚠️ Avoid semantic duplicates: if new feedback relates to an existing rule, prefer \\\n" +
		"revising it over adding a new one.\n" +
		"\n" +
		"Output JSON:\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"new rule\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"revised rule\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"Field notes:\n" +
		"- action: \"add\" | \"revise\" | \"drop\"\n" +
		"- rule: rule content (required for add/revise)\n" +
		"- category: rule category (required for add/revise)\n" +
		"- gradient_id: rule ID to modify/delete (required for revise/drop)\n" +
		"\n" +
		"If nothing needs updating, return {{\"operations\": []}}.\n"

	// explicitGradientPromptEN 显式反馈梯度 prompt 英文版
	// Python: gradient_updater.py:321-385 内联 f-string
	explicitGradientPromptEN = "You are a recommendation-strategy analyst. Update the recommendation strategy \\\n" +
		"rules based on the user's like/dislike feedback.\n" +
		"\n" +
		"[User feedback]\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"[Existing strategy rules]\n" +
		"{existing_text}\n" +
		"\n" +
		"⚠️ Key constraints:\n" +
		"- A like means the user is interested in that recommendation; a dislike means not.\n" +
		"- Only target-type rules may be generated (what to recommend / not recommend).\n" +
		"- Do NOT generate tone/structure/relation rules.\n" +
		"- Do not guess why the user liked/disliked.\n" +
		"\n" +
		"⚠️ Conservative generalization (critical):\n" +
		"- Decide whether the user dislikes \"this specific recommendation scenario\" or \"this \\\n" +
		"whole class of skill/topic\" — prefer the concrete scenario in the recommendation \\\n" +
		"content (rec_content), e.g. \"writing skill pushed for an NBA PPT\". Record the rule \\\n" +
		"for the specific scenario; do not generalize to a whole category by skill name alone.\n" +
		"- Single feedback: record the specific target + scenario only, e.g.:\n" +
		"  · user dislikes \"general-writing (NBA PPT copy scenario)\" → \"writing skill for \\\n" +
		"NBA PPT is unwelcome\" (NOT \"avoid copywriting topics\" — that would wrongly suppress \\\n" +
		"other scenarios that need copywriting)\n" +
		"  · user likes \"translation skill (foreign-document scenario)\" → \"translation skill \\\n" +
		"for translating documents is well received\"\n" +
		"- Only when [Existing strategy rules] already records several same-kind feedbacks, \\\n" +
		"revise into a class preference: e.g. several dislikes on different copywriting \\\n" +
		"skills → merge into \"avoid copywriting topics\"\n" +
		"\n" +
		"Examples:\n" +
		"- Single feedback stays specific: user likes \"translation skill\" → \"user is \\\n" +
		"interested in translation skills for translating documents\" (do not generalize to \\\n" +
		"\"translation in general\")\n" +
		"- Several same-kind feedbacks generalize: existing \"dislike on translation skill A\" \\\n" +
		"+ \"dislike on translation skill B\" → revise into \"avoid translation topics\"\n" +
		"\n" +
		"Rules MUST be written in English (legacy Chinese rules already stored may remain; \\\n" +
		"revise them into English when new feedback touches them).\n" +
		"\n" +
		"Output JSON:\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"new rule\", \"category\": \"target\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"revised rule\", \"category\": \"target\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"If nothing needs updating, return {{\"operations\": []}}.\n"

	// explicitGradientPromptZH 显式反馈梯度 prompt 中文版
	// Python: gradient_updater.py:387-431 内联 f-string
	explicitGradientPromptZH = "你是推荐策略分析师。根据用户的点赞/点踩反馈，更新推荐策略规则。\n" +
		"\n" +
		"【用户反馈】\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"【已有策略规则】\n" +
		"{existing_text}\n" +
		"\n" +
		"⚠️ 重要约束：\n" +
		"- 点赞表示用户对该推荐感兴趣，点踩表示对该推荐不感兴趣\n" +
		"- 只能生成 target 类型规则（关于\"推什么/不推什么\"）\n" +
		"- 不要生成 tone/structure/relation 类型的规则\n" +
		"- 不要猜测用户为什么点赞/点踩\n" +
		"\n" +
		"⚠️ 泛化保守（关键）：\n" +
		"- 判断用户是对\"这个具体推荐场景\"不感兴趣，还是对\"这类 skill/话题\"整体不感兴趣——\n" +
		"  优先看「推荐内容」(rec_content) 里的具体场景（如\"为做 NBA PPT 推写作技能\"），\n" +
		"  按具体场景记规则，不要仅凭 skill 名泛化成大类。\n" +
		"- 单次反馈：只记具体 target + 场景，例如：\n" +
		"  · 用户点踩\"general-writing（NBA PPT 文案场景）\" → \"为 NBA PPT 推写作技能不受欢迎\"\n" +
		"    （不泛化成\"避免文案生成类话题\"——那会误伤其他需要文案的场景）\n" +
		"  · 用户点赞\"翻译技能（翻外语文档场景）\" → \"翻译技能用于翻文档受欢迎\"\n" +
		"- 仅当「已有策略规则」里已有多条同类反馈的记录时，才 revise 成类偏好：\n" +
		"  例如已有多条\"不同文案类技能点踩\" → 才合并成\"避免文案生成类话题\"\n" +
		"\n" +
		"示例：\n" +
		"- 单次反馈记具体：用户点赞\"翻译技能\" → \"用户对翻译技能用于翻文档感兴趣\"（不泛化成\"翻译类\"）\n" +
		"- 多次同类才泛化：已有\"翻译技能点踩\"+\"其他翻译技能点踩\" → revise 成\"避免翻译类话题\"\n" +
		"\n" +
		"规则必须用简体中文书写（存量旧语言的规则可保留；新反馈触及它们时 revise 成中文）。\n" +
		"\n" +
		"输出 JSON：\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"新规则\", \"category\": \"target\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"修正后的规则\", \"category\": \"target\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"如果没有需要更新的，返回 {{\"operations\": []}}。\n"

	// implicitGradientPromptEN 隐式反馈梯度 prompt 英文版
	// Python: gradient_updater.py:477-563 内联 f-string
	implicitGradientPromptEN = "You are a recommendation-strategy analyst. Update the recommendation strategy \\\n" +
		"rules based on the user's text replies.\n" +
		"\n" +
		"[User feedback]\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"[Existing strategy rules]\n" +
		"{existing_text}\n" +
		"\n" +
		"⚠️ Key constraints:\n" +
		"- In [User feedback], feedback_type \"implicit\" means the raw text of the user's \\\n" +
		"reply, with sentiment NOT pre-classified (keyword-based classification was removed \\\n" +
		"due to high misjudgment). First judge the sentiment (positive/negative/neutral) \\\n" +
		"from the user_reply text itself, then decide whether to generate rules. Neutral \\\n" +
		"replies → return empty operations.\n" +
		"- Base rules only on what the user explicitly expressed; do not guess intent.\n" +
		"- Any rule type may be generated (target/relation/tone/structure).\n" +
		"\n" +
		"⚠️ Relevance check (paired with the removed time window):\n" +
		"- user_reply may be unrelated to the recommendation (the user moved on to another \\\n" +
		"topic). If the reply has no connection to the recommendation content, return empty \\\n" +
		"operations — do not force a link.\n" +
		"\n" +
		"⚠️ Conservative generalization (critical):\n" +
		"- Prefer the concrete scenario in the recommendation content (rec_content); record \\\n" +
		"rules for the specific scenario, not whole categories by skill name/topic.\n" +
		"- Single feedback records a specific target + scenario: e.g. user replies \"not \\\n" +
		"needed\" to \"writing skill pushed for an NBA PPT\" → \"writing skill for NBA PPT is \\\n" +
		"unwelcome\" (NOT \"avoid copywriting\").\n" +
		"- Only when [Existing strategy rules] already records several same-kind feedbacks, \\\n" +
		"revise into a class preference.\n" +
		"\n" +
		"Examples:\n" +
		"- User says \"be more concise\" → rule: \"keep the tone concise and restrained\" (tone)\n" +
		"- User says \"stop pushing translation\" → rule: \"avoid recommending translation \\\n" +
		"topics\" (target — the user explicitly said so)\n" +
		"- Reply unrelated to the recommendation → return empty operations\n" +
		"\n" +
		"Rules MUST be written in English (legacy Chinese rules already stored may remain; \\\n" +
		"revise them into English when new feedback touches them).\n" +
		"\n" +
		"Output JSON:\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"new rule\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"revised rule\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"If nothing needs updating, return {{\"operations\": []}}.\n"

	// implicitGradientPromptZH 隐式反馈梯度 prompt 中文版
	// Python: gradient_updater.py:565-615 内联 f-string
	implicitGradientPromptZH = "你是推荐策略分析师。根据用户的文本回复，更新推荐策略规则。\n" +
		"\n" +
		"【用户反馈】\n" +
		"{feedbacks_text}\n" +
		"\n" +
		"【已有策略规则】\n" +
		"{existing_text}\n" +
		"\n" +
		"⚠️ 重要约束：\n" +
		"- 【用户反馈】里的\"反馈类型: implicit\"表示这是用户的文本回复原文，情感未预分类\n" +
		"  （此前用关键词规则分类误判率高，已移除）。你需先从 user_reply 原文判断其情感\n" +
		"  倾向（正面/负面/中性），再决定是否生成规则。中性回复返回空 operations。\n" +
		"- 必须基于用户明确表达的内容生成规则\n" +
		"- 不要猜测用户的意图\n" +
		"- 可以生成任意类型规则（target/relation/tone/structure）\n" +
		"\n" +
		"⚠️ 相关性判断（配合砍掉的时间窗）：\n" +
		"- user_reply 可能和推荐无关（用户聊到别的话题）。若回复与「推荐内容」无关联，\n" +
		"  返回空 operations，不产生梯度——不要硬凑关联。\n" +
		"\n" +
		"⚠️ 泛化保守（关键）：\n" +
		"- 优先看「推荐内容」(rec_content) 里的具体场景，按具体场景记规则，不要仅凭\n" +
		"  skill 名/话题泛化成大类。\n" +
		"- 单次反馈只记具体 target + 场景，例如用户对\"为 NBA PPT 推的写作技能\"回复\n" +
		"  \"不需要\" → \"为 NBA PPT 推写作技能不受欢迎\"（不泛化成\"避免文案类\"）。\n" +
		"- 仅当「已有策略规则」里已有多条同类反馈时，才 revise 成类偏好。\n" +
		"\n" +
		"示例：\n" +
		"- 用户说\"简洁点\" → 生成规则：\"语气简洁克制\"（tone 类型）\n" +
		"- 用户说\"不要推翻译类\" → 生成规则：\"避免推荐翻译类话题\"（target 类型，因用户明确说了\"翻译类\"）\n" +
		"- 用户回复和推荐无关（如聊到别的话题）→ 返回空 operations\n" +
		"\n" +
		"规则必须用简体中文书写（存量旧语言的规则可保留；新反馈触及它们时 revise 成中文）。\n" +
		"\n" +
		"输出 JSON：\n" +
		"{{\n" +
		"  \"operations\": [\n" +
		"    {{\"action\": \"add\", \"rule\": \"新规则\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"revise\", \"gradient_id\": \"g_xxx\", \"rule\": \"修正后的规则\", \"category\": \"target|relation|tone|structure\"}},\n" +
		"    {{\"action\": \"drop\", \"gradient_id\": \"g_yyy\"}}\n" +
		"  ]\n" +
		"}}\n" +
		"\n" +
		"如果没有需要更新的，返回 {{\"operations\": []}}。\n"
)

const (
	// gradientDecisionCategoryTarget 决策层分类：推什么
	gradientDecisionCategoryTarget = "target"
	// gradientDecisionCategoryRelation 决策层分类：怎么关联
	gradientDecisionCategoryRelation = "relation"
	// gradientStyleCategoryTone 话术层分类：语气
	gradientStyleCategoryTone = "tone"
	// gradientStyleCategoryStructure 话术层分类：结构
	gradientStyleCategoryStructure = "structure"
	// maxGradientsFedToLLM 喂 LLM 的梯度条数上限（留最新）
	// Python: gradient_updater.py:651 decision_g[-10:] / :676 style_g[-10:]
	maxGradientsFedToLLM = 10
	// defaultGradientCategory add 操作缺分类时的默认值
	// Python: gradient_updater.py:250 op.get("category", "tone")
	defaultGradientCategory = "tone"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// decisionCategories 决策层分类集合。
// Python: gradient_updater.py:617 DECISION_CATEGORIES
var decisionCategories = map[string]struct{}{
	gradientDecisionCategoryTarget:   {},
	gradientDecisionCategoryRelation: {},
}

// styleCategories 话术层分类集合。
// Python: gradient_updater.py:618 STYLE_CATEGORIES
var styleCategories = map[string]struct{}{
	gradientStyleCategoryTone:      {},
	gradientStyleCategoryStructure: {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GradientUpdatePrompt 按语言返回统一梯度更新 prompt 模板（未渲染）。
// Python: gradient_updater.py:135 gradient_update_prompt
func GradientUpdatePrompt(language string) string {
	if strings.ToLower(strings.TrimSpace(language)) == "en" {
		return gradientUpdatePromptEN
	}
	return gradientUpdatePromptZH
}

// ApplyOperations 把 LLM 给出的操作应用到梯度列表，返回更新后的列表。
//
// 三种操作：
//   - add：新建梯度
//   - revise：修改已有梯度，并**挪到末尾**——这样「最近被新反馈更新过的规则」
//     反映新近度，配合存储/喂 LLM 截断留最新（-N:），被 revise 的规则不会被
//     「留最早」逻辑误淘汰，且优先被喂。
//   - drop：删除已有梯度
//
// Python: gradient_updater.py:229 apply_operations
func ApplyOperations(gradients []map[string]any, operations []map[string]any) []map[string]any {
	// Python 用 dict 保持插入序；Go 用 order 切片显式维护顺序
	gradientMap := make(map[string]map[string]any, len(gradients))
	order := make([]string, 0, len(gradients))
	for _, g := range gradients {
		id := stringField(g, "gradient_id")
		if id == "" {
			continue
		}
		gradientMap[id] = g
		order = append(order, id)
	}

	for _, op := range operations {
		action := stringField(op, "action")
		switch action {
		case "add":
			// 过滤空 rule：LLM 可能返回空规则，渲染成 "- [target] " 白占 context 且可能误导
			ruleText := strings.TrimSpace(stringField(op, "rule"))
			if ruleText == "" {
				logger.Debug(logComponent).Msg("add op with empty rule, skipped")
				continue
			}
			gid := fmt.Sprintf("g_%d_%d", time.Now().UnixMilli(), len(gradientMap))
			category := stringFieldDefault(op, "category", defaultGradientCategory)
			gradientMap[gid] = map[string]any{
				"gradient_id": gid,
				"rule":        ruleText,
				"category":    category,
			}
			order = append(order, gid)

		case "revise":
			gid := stringField(op, "gradient_id")
			existing, exists := gradientMap[gid]
			if !exists {
				continue
			}
			// 挪到末尾：从 order 移除再追加
			for i, id := range order {
				if id == gid {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
			revised := map[string]any{
				"gradient_id": gid,
				"rule":        stringFieldDefault(op, "rule", stringField(existing, "rule")),
				"category":    stringFieldDefault(op, "category", stringField(existing, "category")),
			}
			gradientMap[gid] = revised
			order = append(order, gid)

		case "drop":
			gid := stringField(op, "gradient_id")
			if _, exists := gradientMap[gid]; !exists {
				continue
			}
			delete(gradientMap, gid)
			for i, id := range order {
				if id == gid {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
	}

	result := make([]map[string]any, 0, len(order))
	for _, id := range order {
		if g, ok := gradientMap[id]; ok {
			result = append(result, g)
		}
	}
	return result
}

// UpdateGradients 统一梯度更新流程。
//
// existingGradients 为空 → 一阶梯度（从反馈提炼）
// existingGradients 非空 → 二阶梯度（修正已有规则）
//
// Python: gradient_updater.py:275 update_gradients
func UpdateGradients(
	ctx context.Context,
	feedbacks []map[string]any,
	existingGradients []map[string]any,
	proactiveAgent *harness.DeepAgent,
) []map[string]any {
	if len(feedbacks) == 0 {
		return existingGradients
	}

	// 分离显式和隐式反馈
	var explicitFeedbacks, implicitFeedbacks []map[string]any
	for _, fb := range feedbacks {
		if isExplicitFeedback(stringField(fb, "feedback_type")) {
			explicitFeedbacks = append(explicitFeedbacks, fb)
		}
		if stringField(fb, "user_reply") != "" {
			implicitFeedbacks = append(implicitFeedbacks, fb)
		}
	}

	result := existingGradients
	// 处理显式反馈：只生成 target 类型规则
	if len(explicitFeedbacks) > 0 {
		result = updateGradientsFromExplicitFeedback(ctx, explicitFeedbacks, result, proactiveAgent)
	}
	// 处理隐式反馈：可以生成任意类型规则
	if len(implicitFeedbacks) > 0 {
		result = updateGradientsFromImplicitFeedback(ctx, implicitFeedbacks, result, proactiveAgent)
	}
	return result
}

// AttributeGradients 把梯度归到决策层与话术层。
// 未知分类默认归决策层。
// Python: gradient_updater.py:621 attribute_gradients
func AttributeGradients(gradients []map[string]any) (decision []map[string]any, style []map[string]any) {
	decision = make([]map[string]any, 0, len(gradients))
	style = make([]map[string]any, 0, len(gradients))
	for _, g := range gradients {
		category := stringField(g, "category")
		if _, ok := decisionCategories[category]; ok {
			decision = append(decision, g)
			continue
		}
		if _, ok := styleCategories[category]; ok {
			style = append(style, g)
			continue
		}
		// 未知分类默认归决策层
		decision = append(decision, g)
	}
	return decision, style
}

// RenderDecisionRules 渲染决策层规则，供分析 prompt 注入。
// 留最新 10 条：与存储留最新（-20:）方向一致，反映最近反馈学到的偏好。
// Python: gradient_updater.py:646 render_decision_rules
func RenderDecisionRules(gradients []map[string]any, language string) string {
	decisionG, _ := AttributeGradients(gradients)
	if len(decisionG) == 0 {
		if isEnglish(language) {
			return "(none yet)"
		}
		return "（暂无）"
	}
	// Python: for g in decision_g[-10:]
	start := 0
	if len(decisionG) > maxGradientsFedToLLM {
		start = len(decisionG) - maxGradientsFedToLLM
	}
	lines := make([]string, 0, len(decisionG)-start)
	for _, g := range decisionG[start:] {
		lines = append(lines, "- ["+stringFieldDefault(g, "category", "unknown")+"] "+stringField(g, "rule"))
	}
	return strings.Join(lines, "\n")
}

// RenderStyleRules 渲染话术层规则，供 directive prompt 注入。
// 无话术层规则时返回空串（调用方据此决定是否加该段）。
// Python: gradient_updater.py:661 render_style_rules
func RenderStyleRules(gradients []map[string]any, language string) string {
	_, styleG := AttributeGradients(gradients)
	if len(styleG) == 0 {
		return ""
	}
	heading := "【话术风格要求】（基于用户历史反馈）"
	if isEnglish(language) {
		heading = "[Style rules for the copy] (from the user's past feedback)"
	}
	lines := []string{heading}
	start := 0
	if len(styleG) > maxGradientsFedToLLM {
		start = len(styleG) - maxGradientsFedToLLM
	}
	for _, g := range styleG[start:] {
		lines = append(lines, "- "+stringField(g, "rule"))
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateGradientsFromExplicitFeedback 从显式反馈生成 target 类型规则。
// Python: gradient_updater.py:315 _update_gradients_from_explicit_feedback
func updateGradientsFromExplicitFeedback(
	ctx context.Context,
	feedbacks []map[string]any,
	existingGradients []map[string]any,
	proactiveAgent *harness.DeepAgent,
) []map[string]any {
	language := PreferredLanguage()
	feedbacksText := renderFeedbacks(feedbacks, language)
	existingText := renderGradients(existingGradients)

	template := explicitGradientPromptZH
	if isEnglish(language) {
		template = explicitGradientPromptEN
	}
	// 注意：显式/隐式专用模板的占位符是 {existing_text}（上游 :335/:387/:486/:539），
	// 与统一模板的 {existing_rules_text}（:33/:87）不同，不可混用。
	prompt := RenderPromptTemplate(template, map[string]string{
		"feedbacks_text": feedbacksText,
		"existing_text":  existingText,
	})

	convID := fmt.Sprintf("gradient_update_explicit_%d", time.Now().UnixMilli())
	defer releaseCheckpointID(ctx, convID)

	content, ok := invokeGradientAgent(ctx, proactiveAgent, prompt, convID)
	if !ok {
		logger.Warn(logComponent).Msg("no JSON in response for explicit feedback")
		return existingGradients
	}

	operations, ok := parseGradientOperations(content)
	if !ok {
		return existingGradients
	}

	// 强制过滤：只保留 target 类型或 drop 操作
	filtered := make([]map[string]any, 0, len(operations))
	for _, op := range operations {
		if stringField(op, "category") == gradientDecisionCategoryTarget || stringField(op, "action") == "drop" {
			filtered = append(filtered, op)
		}
	}
	if len(filtered) == 0 {
		logger.Info(logComponent).Msg("no target operations from explicit feedback")
		return existingGradients
	}

	updated := ApplyOperations(existingGradients, filtered)
	logger.Info(logComponent).Int("operations", len(filtered)).
		Msg("applied operations from explicit feedback")
	return updated
}

// updateGradientsFromImplicitFeedback 从隐式反馈生成任意类型规则。
// Python: gradient_updater.py:463 _update_gradients_from_implicit_feedback
func updateGradientsFromImplicitFeedback(
	ctx context.Context,
	feedbacks []map[string]any,
	existingGradients []map[string]any,
	proactiveAgent *harness.DeepAgent,
) []map[string]any {
	language := PreferredLanguage()
	feedbacksText := renderFeedbacks(feedbacks, language)
	existingText := renderGradients(existingGradients)

	template := implicitGradientPromptZH
	if isEnglish(language) {
		template = implicitGradientPromptEN
	}
	// 同显式分支：专用模板占位符是 {existing_text}
	prompt := RenderPromptTemplate(template, map[string]string{
		"feedbacks_text": feedbacksText,
		"existing_text":  existingText,
	})

	convID := fmt.Sprintf("gradient_update_implicit_%d", time.Now().UnixMilli())
	defer releaseCheckpointID(ctx, convID)

	content, ok := invokeGradientAgent(ctx, proactiveAgent, prompt, convID)
	if !ok {
		logger.Warn(logComponent).Msg("no JSON in response for implicit feedback")
		return existingGradients
	}

	operations, ok := parseGradientOperations(content)
	if !ok {
		return existingGradients
	}
	if len(operations) == 0 {
		logger.Info(logComponent).Msg("no operations from implicit feedback")
		return existingGradients
	}

	updated := ApplyOperations(existingGradients, operations)
	logger.Info(logComponent).Int("operations", len(operations)).
		Msg("applied operations from implicit feedback")
	return updated
}

// renderFeedbacks 渲染反馈记录供 prompt 使用（标签按语言切换）。
// Python: gradient_updater.py:143 _render_feedbacks
func renderFeedbacks(feedbacks []map[string]any, language string) string {
	english := isEnglish(language)
	if len(feedbacks) == 0 {
		if english {
			return "(no feedback)"
		}
		return "（无反馈）"
	}
	lines := make([]string, 0, len(feedbacks))
	for i, fb := range feedbacks {
		feedbackType := stringFieldDefault(fb, "feedback_type", "unknown")
		recContent := stringField(fb, "rec_content")
		userReply := stringField(fb, "user_reply")
		idx := strconv.Itoa(i + 1)

		var line string
		if english {
			line = idx + ". feedback type: " + feedbackType
			if recContent != "" {
				line += "\n   recommendation content: " + recContent
			}
			if userReply != "" {
				line += "\n   user reply: " + userReply
			}
		} else {
			line = idx + ". 反馈类型: " + feedbackType
			if recContent != "" {
				line += "\n   推荐内容: " + recContent
			}
			if userReply != "" {
				line += "\n   用户回复: " + userReply
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderGradients 渲染已有梯度供 prompt 使用。
// Python: gradient_updater.py:173 _render_gradients
func renderGradients(gradients []map[string]any) string {
	if len(gradients) == 0 {
		return "（暂无）"
	}
	lines := make([]string, 0, len(gradients))
	for _, g := range gradients {
		lines = append(lines, "- ["+stringFieldDefault(g, "gradient_id", "unknown")+
			"] ("+stringFieldDefault(g, "category", "unknown")+") "+stringField(g, "rule"))
	}
	return strings.Join(lines, "\n")
}

// extractGradientJSON 从 LLM 响应中提取 JSON（支持 markdown 代码块）。
//
// fallback 用逐字符解析一段合法 JSON（等价 Python `json.JSONDecoder().raw_decode`），
// **不是**贪婪正则。贪婪正则从首个 `{` 匹配到末个 `}`，若 LLM 在 JSON 后附了含 `}`
// 的文本会截到无效 JSON 致解析失败；raw_decode 从首个 `{` 解析出一段合法 JSON 即止，
// 忽略尾部文本。
//
// ⚠️ 与 proactive_actions 的 ExtractJSONFromResponse（贪婪正则版）是**两套不同实现**，
// 上游即如此（gradient_updater.py:204-228 的 docstring 明确说明为何不用贪婪正则），
// **禁止合并**。
//
// Python: gradient_updater.py:204 _extract_json_from_response
func extractGradientJSON(text string) string {
	if m := jsonFencePattern.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	// raw_decode 等价：从 start 起解析一段完整 JSON 对象，忽略尾部文本
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	var obj any
	if err := decoder.Decode(&obj); err != nil {
		return ""
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(data)
}

// invokeGradientAgent 调用 proctive agent 做一次梯度分析，返回提取出的文本。
func invokeGradientAgent(
	ctx context.Context,
	proactiveAgent *harness.DeepAgent,
	prompt, convID string,
) (string, bool) {
	if proactiveAgent == nil {
		return "", false
	}
	result, err := proactiveAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": convID,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("gradient update llm call failed")
		return "", false
	}
	content := ExtractOutputText(result)
	jsonStr := extractGradientJSON(content)
	if jsonStr == "" {
		return "", false
	}
	return jsonStr, true
}

// parseGradientOperations 解析 {"operations": [...]}。
func parseGradientOperations(jsonStr string) ([]map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil, false
	}
	rawOps, ok := payload["operations"].([]any)
	if !ok {
		// operations 缺失或非数组 → 视为无操作（对齐 Python data.get("operations", [])）
		return nil, true
	}
	ops := make([]map[string]any, 0, len(rawOps))
	for _, item := range rawOps {
		if m, isMap := item.(map[string]any); isMap {
			ops = append(ops, m)
		}
	}
	return ops, true
}

// releaseCheckpointID 释放一次性会话的 checkpointer 记录。
func releaseCheckpointID(ctx context.Context, convID string) {
	if err := checkpointer.GetCheckpointer().Release(ctx, convID); err != nil {
		logger.Debug(logComponent).Err(err).Str("conversation_id", convID).
			Msg("checkpoint release failed")
	}
}

// isEnglish 语言是否为英文。
func isEnglish(language string) bool {
	return strings.ToLower(strings.TrimSpace(language)) == "en"
}
