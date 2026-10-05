//go:build integration

// Package dreaming 提供 Sweeper / DreamingConfig / StartDreaming/StopDreaming 集成测试。
//
// 对齐 Python: tests/system_tests/test_dreaming_e2e.py
// 覆盖：Sweeper E2E 管线（Scan + Compress + Promote）、
// Checkpoint 持久化、DreamingConfig 环境变量加载、
// StartDreaming/StopDreaming API 生命周期。
//
// 文件目录：
//
//	dreaming/
//	├── doc.go               # 包文档
//	└── sweeper_e2e_test.go  # Sweeper E2E 集成测试
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory/dreaming/
package dreaming
