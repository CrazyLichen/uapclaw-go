// Package prompts 提供 Agent 簇（harness）的系统提示词构建器扩展，
// 在 single_agent/prompts 基础上增加 PromptMode 模式过滤和诊断报告能力。
// SystemPromptBuilder 继承基础构建器，根据 PromptMode（FULL/MINIMAL/NONE）
// 过滤节后拼接系统提示词；PromptReport 生成提示词诊断统计；
// sanitize 系列函数防御提示词注入攻击；attachment_*.go 提供会话级动态提示词投递
// （按内容哈希做快照/增量并按 provider 投影 role）。
// 文件目录：
//
//	prompts/
//	├── doc.go                 # 包文档
//	├── builder.go             # harness 扩展 SystemPromptBuilder + 语言/模式解析
//	├── report.go              # PromptReport 提示词诊断报告
//	├── sanitize.go            # 提示词注入防御函数
//	├── attachment_types.go    # 动态提示词片段类型、常量与写入选项
//	├── attachment_digest.go   # 规范化 JSON 与内容哈希
//	├── attachment_store.go    # 会话级存储（归一化/过期/稳定排序/更新族）
//	├── attachment_render.go   # snapshot/delta/removed 渲染与两级截断
//	├── attachment_delivery.go # 历史同步（快照/增量/基线回退）
//	├── attachment_rail.go     # 写历史前/建窗口前的同步装订点
//	└── attachment_mutator.go  # provider 矩阵与窗口投影
package prompts
