// Package recommendation 承载主动推荐的「提示词建议」可独立部分。
//
// 本包对应上游 jiuwenswarm/agents/harness/common/recommendation/ 的
// prompt_suggestion.py 与 switches.py，以及 jiuwenswarm/common/
// recommendation_switches.py。完整的 topic 流水线（35 文件 / 43821 行）不在本包，
// 由 10.3.31a~e 单独排期；_fork 的结果落库与 ready 推送由窄接口预留，
// 待 10.7 的 TopicStarterStore 注入。
//
// 主动引擎（proactive_engine / proactive_actions / proactive_prompts /
// situation_report / calendar_source / profile_extractor / gradient_updater /
// feedback_collector，共 3058 行）由 10.3.31a 排期，当前尚未落地。
//
// 内置技能源目录不在本包，而在发布包资源
// resources/agent/workspace/skills/（10.3.31a-pre 原封拷贝上游 34 目录 /
// 415 文件），由 internal/swarm/server/runtime/skill 的 getBuiltinSkillsDir 解析。
//
// 文件目录：
//
//	recommendation/
//	├── doc.go               # 包文档
//	├── switches.go          # 推荐开关与同意 epoch
//	├── prompt_suggestion.go # MainCall / PromptSuggestionForks / ForkRail / _fork
//	├── suggestion_filter.go # 建议过滤
//	└── fork_rail.go         # PromptSuggestionForkRail
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/prompt_suggestion.py
//
//	jiuwenswarm/common/recommendation_switches.py
//	jiuwenswarm/agents/harness/common/recommendation/switches.py
package recommendation
