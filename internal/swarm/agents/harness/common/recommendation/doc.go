// Package recommendation 承载主动推荐的「提示词建议」可独立部分。
//
// 本包对应上游 jiuwenswarm/agents/harness/common/recommendation/ 的
// prompt_suggestion.py 与 switches.py，以及 jiuwenswarm/common/
// recommendation_switches.py。完整的 topic 流水线（35 文件 / 43821 行）不在本包，
// 由 10.3.31a~e 单独排期；_fork 的结果落库与 ready 推送由窄接口预留，
// 待 10.7 的 TopicStarterStore 注入。
//
// 文件目录：
//
//	recommendation/
//	├── doc.go               # 包文档
//	├── switches.go          # 推荐开关与同意 epoch
//	└── prompt_suggestion.go # MainCall / PromptSuggestionForks / ForkRail / _fork
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/prompt_suggestion.py
//
//	jiuwenswarm/common/recommendation_switches.py
//	jiuwenswarm/agents/harness/common/recommendation/switches.py
package recommendation
