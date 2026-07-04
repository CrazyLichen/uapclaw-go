// Package workspace 提供 DeepAgent 工作空间目录结构管理。
//
// 工作空间定义了 DeepAgent 运行时所需的目录和文件布局，包括基础配置（AGENT.md）、
// 人格定义（SOUL.md）、心跳日志（HEARTBEAT.md）、身份凭证（IDENTITY.md）、
// 用户数据（USER.md）、记忆模块（memory/coding_memory）、待办事项（todo）、
// 消息历史（messages）、技能库（skills）、子智能体（agents）和上下文（context）。
//
// 本包支持中文（cn）和英文（en）两种默认工作空间模式，并提供目录节点的
// 增删查改、校验和默认补全功能。同时提供团队链接（.team/）和工作树链接
// （.worktree/）的符号链接管理方法。
//
// 文件目录：
//
//	workspace/
//	├── doc.go           # 包文档
//	└── workspace.go     # Workspace 结构体、WorkspaceNode 枚举、目录管理方法及链接管理方法
//
// 对应 Python 代码：openjiuwen/harness/workspace/
package workspace
