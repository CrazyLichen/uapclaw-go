// Package recommendation 承载主动推荐能力（提示词建议 + 主动引擎）。
//
// 本包对应上游 jiuwenswarm/agents/harness/common/recommendation/ 的
// prompt_suggestion.py、switches.py、proactive_*.py、situation_report.py、
// calendar_source.py、profile_extractor.py、gradient_updater.py、
// feedback_collector.py，以及 jiuwenswarm/common/recommendation_switches.py。
// 完整的 topic 流水线（35 文件 / 43821 行）不在本包，由 10.3.31b~d 单独排期；
// _fork 的结果落库与 ready 推送由窄接口预留，待 10.7 的 TopicStarterStore 注入。
//
// 两个子系统：
//   - 提示词建议（10.3.30 已交付）：switches.go / prompt_suggestion.go /
//     suggestion_filter.go / fork_rail.go —— 每个成功轮结束后预测下一条用户输入。
//   - 主动引擎（10.3.31a 第 1/2 段已交付）：profile_extractor.go /
//     proactive_prompts.go / proactive_actions.go / situation_report.go ——
//     定时 tick 聚合情境 → 单轮 LLM 决策 → 让主 agent 生成推荐话术。
//     第 3 段（calendar_source / gradient_updater / feedback_collector /
//     proactive_engine / proactive_adapter）尚未落地。
//
// 内置技能源目录不在本包，而在发布包资源
// resources/agent/workspace/skills/（10.3.31a-pre 原封拷贝上游 34 目录 /
// 415 文件），由 internal/swarm/server/runtime/skill 的 getBuiltinSkillsDir 解析。
//
// 文件目录：
//
//	recommendation/
//	├── doc.go                          # 包文档
//	├── switches.go                     # 推荐开关与同意 epoch
//	├── prompt_suggestion.go            # MainCall / PromptSuggestionForks / _fork
//	├── suggestion_filter.go            # 建议过滤
//	├── fork_rail.go                    # PromptSuggestionForkRail
//	├── profile_extractor.go            # RecommendationState 持久化（recommendation.json）
//	├── proactive_prompts.go            # 决策/话术 prompt 模板（zh/en，逐字复刻上游）
//	├── proactive_actions.go            # 决策类型/限流/技能发现/LLM 分析/触发主 agent
//	└── situation_report.go             # 情境报告（多会话扫描/历史压缩/渲染）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/
//
//	jiuwenswarm/common/recommendation_switches.py
package recommendation
