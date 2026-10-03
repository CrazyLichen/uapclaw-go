// Package dreaming 提供后台记忆整理编排器。
//
// 本包实现 DreamingOrchestrator，负责定时触发记忆整理（sweep），
// 支持忙碌退避（busy backoff）和幂等启停。
//
// Dreaming 的含义是"睡觉整理记忆"——类似人睡眠期间大脑整理白天的记忆，
// 在 Agent 空闲时定期扫描历史会话，用 LLM 提取有价值的信息。
//
// 文件目录：
//
//	dreaming/
//	├── doc.go              # 包文档
//	└── orchestrator.go     # DreamingOrchestrator 后台定时编排器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/dreaming/
package dreaming
