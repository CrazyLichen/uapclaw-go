//go:build integration

// Package worktree 提供 Worktree 模块的集成测试。
//
// 测试覆盖：
//   - WorktreeManager 生命周期（Enter/Exit/CountChanges/EventHook）
//   - WorktreeSessionState Context 传播
//   - WorktreeRail Init 注册工具、BeforeAfterInvoke Session 持久化
//   - GitBackend 真实 git worktree 操作
//   - ValidateSlug 安全校验
//
// 文件目录：
//
//	worktree/
//	├── doc.go               # 包文档
//	└── worktree_test.go     # Worktree 集成测试
//
// 对应 Python 代码：openjiuwen/harness/tools/worktree/
package worktree
