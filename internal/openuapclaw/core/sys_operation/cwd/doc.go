// Package cwd 提供每 Agent CWD 状态管理。
// Python: openjiuwen/core/sys_operation/cwd.py
// 通过 context.Context 传播 CwdState（可变容器模式），
// 实现 Agent 间隔离（InitCwd 创建新实例 + WithCwdState 派生新 ctx），
// Agent 内共享（同一 CwdState 引用的 goroutine 共享修改）。
// 三层 CWD 模型：
//
//	Layer 1 — ProjectRoot:  项目身份锚点（设置一次，沙箱边界依据）
//	Layer 2 — OriginalCwd:  会话起始点（worktree 退出时的恢复目标）
//	Layer 3 — Cwd:          当前工作目录（高频更新，所有工具的路径锚点）
//
// 辅助字段：Workspace（per-agent artifact 目录）、TeamWorkspace（团队共享目录）
// 读取优先级：Cwd -> OriginalCwd -> os.Getwd()
//
// # 唯一入口与 error 传播
//
// 与 Python 一致，本包只提供唯一的 get_cwd() 等价物：
// GetCwd(ctx) (string, error) / GetProjectRoot(ctx) (string, error)。
// 三级回退（Cwd -> OriginalCwd -> os.Getwd()）全部内联在模块级函数里，
// CwdState 是纯数据容器（仅保留字段快照与 setter，无读取 getter）。
//
// 最后一级 os.Getwd() 失败时返回 error，对应 Python os.getcwd() 抛 OSError。
// Python 的调用方多数不捕获、让异常自然上冒；Go 侧调用方须显式处理或传播
// error（登记为偏差 V-10，见 9.90d spec §五）。
//
// 历史：9.90d-1 曾短暂存在过吞异常的兼容壳 GetCwd(ctx) string 与
// GetCwdE/GetProjectRootE 变体，已按规则 8 全部删除，不留残留。
//
// 文件目录：
//
//	cwd/
//	├── doc.go           # 包文档
//	└── cwd.go           # CwdState 数据容器 + 模块级唯一读取入口
//
// 对应 Python 代码：openjiuwen/core/sys_operation/cwd.py
package cwd
