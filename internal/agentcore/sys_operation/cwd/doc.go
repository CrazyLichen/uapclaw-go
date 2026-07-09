// Package cwd 提供每 Agent CWD 状态管理。
//
// 对齐 Python: openjiuwen/core/sys_operation/cwd.py
// 通过 context.Context 传播 CwdState（可变容器模式），
// 实现 Agent 间隔离（InitCwd 创建新实例 + WithCwdState 派生新 ctx），
// Agent 内共享（同一 CwdState 引用的 goroutine 共享修改）。
//
// 三层 CWD 模型：
//
//	Layer 1 — ProjectRoot:  项目身份锚点（设置一次，沙箱边界依据）
//	Layer 2 — OriginalCwd:  会话起始点（worktree 退出时的恢复目标）
//	Layer 3 — Cwd:          当前工作目录（高频更新，所有工具的路径锚点）
//
// 辅助字段：Workspace（per-agent artifact 目录）、TeamWorkspace（团队共享目录）
//
// 读取优先级：Cwd -> OriginalCwd -> os.Getwd()
//
// 文件目录：
//
//	cwd/
//	├── doc.go           # 包文档
//	└── cwd.go           # CwdState 结构体及全部方法
//
// 对应 Python 代码：openjiuwen/core/sys_operation/cwd.py
package cwd
