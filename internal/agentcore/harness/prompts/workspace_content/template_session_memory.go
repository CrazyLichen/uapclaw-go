package workspace_content

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SessionMemoryMDCN 会话记忆中文模板
	SessionMemoryMDCN = "# 会话标题\n" +
		"_用 5-10 个字写一个简短、明确、信息密度高的会话标题，不要空话。_\n\n" +
		"# 当前状态\n" +
		"_当前正在做什么？有哪些还没完成的任务？下一步最直接要做什么？_\n\n" +
		"# 任务说明\n" +
		"_用户到底让你做什么？有哪些设计决策、限制条件、背景说明需要保留？_\n\n" +
		"# 文件与函数\n" +
		"_哪些文件最重要？它们分别包含什么？为什么和当前任务有关？_\n\n" +
		"# 工作流程\n" +
		"_这个任务通常会按什么顺序执行命令？命令输出该怎么理解？如果不明显请写清楚。_\n\n" +
		"# 错误与修正\n" +
		"_出现过哪些错误？后来怎么修的？用户纠正了什么？哪些做法已经证明不该再试？_\n\n" +
		"# 代码库与系统说明\n" +
		"_这个系统里有哪些关键组件？它们之间怎么协作？有哪些必须记住的架构信息？_\n\n" +
		"# 经验与结论\n" +
		"_有哪些经验已经验证有效？哪些无效？哪些坑要避开？不要和其他章节重复。_\n\n" +
		"# 关键结果\n" +
		"_如果用户要求过明确产物，比如答案、表格、文档、结论，请把最终结果原样或高保真记录在这里。_\n\n" +
		"# 工作记录\n" +
		"_按步骤简要记录做过什么。每一步尽量短，但要能帮助后续快速接上。_\n"

	// SessionMemoryMDEN 会话记忆英文模板
	SessionMemoryMDEN = "# Session Title\n" +
		"_A short and distinctive 5-10 word descriptive title for the session. Super info dense, no filler._\n\n" +
		"# Current State\n" +
		"_What is actively being worked on right now? Pending tasks not yet completed. Immediate next steps._\n\n" +
		"# Task specification\n" +
		"_What did the user ask to build? Any design decisions or other explanatory context._\n\n" +
		"# Files and Functions\n" +
		"_What are the important files? In short, what do they contain and why are they relevant?_\n\n" +
		"# Workflow\n" +
		"_What bash commands are usually run and in what order? How should their output be interpreted if that is not obvious?_\n\n" +
		"# Errors & Corrections\n" +
		"_Errors encountered and how they were fixed. What did the user correct? What approaches failed and should not be tried again?_\n\n" +
		"# Codebase and System Documentation\n" +
		"_What are the important system components? How do they work and fit together?_\n\n" +
		"# Learnings\n" +
		"_What has worked well? What has not? What should be avoided? Do not duplicate items from other sections._\n\n" +
		"# Key results\n" +
		"_If the user asked for a concrete output such as an answer, table, document, or conclusion, repeat the final result here with high fidelity._\n\n" +
		"# Worklog\n" +
		"_Step by step, what was attempted and completed? Keep each step terse, but useful for resuming work quickly._\n"
)
