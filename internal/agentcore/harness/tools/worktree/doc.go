// Package worktree 提供 Git Worktree 隔离模型，用于在独立的工作树中执行任务，
// 避免并发操作对主仓库的干扰。
//
// 本包定义了 Worktree 的配置、运行时会话、创建结果和变更摘要等核心数据结构，
// 以及生命周期策略枚举。对齐 Python openjiuwen/harness/tools/worktree/models.py。
//
// 文件目录：
//
//	worktree/
//	├── doc.go           # 包文档
//	└── models.go        # 核心数据结构与枚举定义
//
// 对应 Python 代码：openjiuwen/harness/tools/worktree/models.py
package worktree
