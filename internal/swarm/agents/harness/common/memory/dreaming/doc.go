// Package dreaming 提供后台记忆整理（Dreaming）的业务实现。
//
// 本包实现 Sweeper 管线和公共 API（StartDreaming/StopDreaming/GetDreamingOrchestrator），
// 对齐 Python jiuwenswarm/agents/harness/common/memory/dreaming/。
//
// # Sweeper 管线流程：Scan → Compress → LLM Extract → Promote
//
// 文件目录：
//
//	dreaming/
//	├── doc.go          # 包文档
//	├── sweeper.go      # Sweeper + DreamingConfig + 辅助类型
//	├── prompts.go      # LLM 提示词模板（4 套：code/agent × zh/en）
//	└── api.go          # StartDreaming / StopDreaming / GetDreamingOrchestrator
//
// 对应 Python 代码：
//
//	jiuwenswarm/agents/harness/common/memory/dreaming/
package dreaming
