// Package sections 提供 agent_evolving 提示词 section 构建器：技能演进与技能创建的
// 常驻协议段（before_model_call 注入 / uninit 移除，消费方为 harness rails skills 与
// evolution 两系 rail）。
//
// 设计要点：
//   - 段名常量（SectionNameXxx）与 Python harness/prompts/sections/__init__.py 的
//     SectionName 枚举一一对应；
//   - 文案常量从 Python 原文逐字拷贝，不做翻译改写；
//   - 偏差 G1：Python PromptSection 的 category/carrier 字段 Go 侧无对应字段
//     （全仓零消费方，Task 0 裁决不修，回收条件 = Go PromptSection 引入 category
//     或上游出现消费方）。
//
// 文件目录：
//
//	sections/
//	├── doc.go                  # 包文档
//	├── skill_creation.go       # 技能创建引导段（skill_creation.py 218 行 1:1）
//	└── evolution.go            # 技能演进协议段（evolution.py 218 行 1:1）
//
// 对应 Python 代码：openjiuwen/agent_evolving/prompts/sections/
package sections
