//go:build integration

// Package session_test 提供 Session 生命周期集成测试。
//
// 覆盖 Session 创建、状态读写、PreRun/PostRun 幂等、
// InMemoryCheckpointer 持久化、环境变量传播等场景。
// 对齐 Python: tests/cli/e2e/test_session_persist.py
//
// 文件目录：
//
//	session/
//	├── doc.go            # 包文档
//	└── session_test.go   # Session 生命周期测试
package session_test
