//go:build integration

// Package dreaming 提供 Sweeper / DreamingConfig / StartDreaming/StopDreaming 集成测试。
//
// 对齐 Python: tests/system_tests/test_dreaming_e2e.py
// 覆盖：Sweeper E2E 管线（Scan + Compress + Promote）、
// Checkpoint 持久化、DreamingConfig 环境变量加载、
// StartDreaming/StopDreaming API 生命周期、
// 增量语义（两次 RunSweep 的增量窗口、标题与内容哈希去重、checkpoint 推进）、
// adapter→sweeper 接线（CodeAdapter / DeepAdapter 真跑 CreateInstance）。
//
// 文件目录：
//
//	dreaming/
//	├── doc.go                        # 包文档
//	├── start_wiring_test.go          # 启停生命周期 + adapter 真跑 CreateInstance 接线
//	├── sweeper_e2e_test.go           # Sweeper E2E（Init/Scan/Promote/Checkpoint）
//	└── sweeper_incremental_test.go   # 增量管线（RunSweep 两次扫描/去重/checkpoint 推进）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory/dreaming/
package dreaming
