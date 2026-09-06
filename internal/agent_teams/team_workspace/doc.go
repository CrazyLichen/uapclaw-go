// Package team_workspace 提供团队共享工作空间管理，
// 包括工作空间配置、文件锁、版本控制、符号链接挂载及冲突检测等功能。
//
// 本包对齐 Python openjiuwen/agent_teams/team_workspace/，
// 支持多 Agent 并发编辑时的冲突检测与解决。
// 两种操作模式：LOCAL（本地模式）和 DISTRIBUTED（分布式模式，Phase 3）。
//
// 文件目录：
//
//	team_workspace/
//	├── doc.go           # 包文档
//	├── manager.go       # TeamWorkspaceManager 核心管理器
//	├── models.go        # 核心数据结构与枚举定义
//	├── rail.go          # TeamWorkspaceRail 透明拦截工具调用施加锁检查和版本控制
//	└── tool.go          # WorkspaceMetaTool 锁管理和版本历史查询工具
//
// 对应 Python 代码：openjiuwen/agent_teams/team_workspace/
package team_workspace
