// Package evoschema 提供演进 experience 域的 schema 叶包：subject/草稿归一化契约。
//
// 叶包定位（方案 B，2026-12-24 用户拍板）：review runtime 与 experience submission
// 双向依赖本包符号（NormalizeSubject/EvolutionSubject/草稿类型），Go 禁环，
// 下沉为独立叶包使 review→叶、experience→叶、submission→review 全部无环——
// 从而 ExperienceSubmissionService.PrepareEvolveSubmission 可一比一 Python
// 直传 review runtime（消窄接口 ProposalResolver，偏差 D-15.24b-4 回收）。
// 偏差登记：Python draft_schema.py 与 experience 同包，Go 拆包（先例 23.8 D-4
// rootcontext 叶包拆分，用户拍板同款手法）。
//
// 文件目录：
//
//	evoschema/
//	├── doc.go              # 包文档
//	└── draft_schema.go    # subject/草稿归一化契约（draft_schema.py 283 行 1:1）
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/draft_schema.py
package evoschema
