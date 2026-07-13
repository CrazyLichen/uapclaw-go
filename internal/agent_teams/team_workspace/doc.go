// Package team_workspace 提供团队共享工作空间的配置模型，
// 包括工作空间配置、文件锁、操作模式及冲突策略等数据结构。
//
// 本包对齐 Python openjiuwen/agent_teams/team_workspace/models.py，
// 支持多 Agent 并发编辑时的冲突检测与解决。
//
// 文件目录：
//
//	team_workspace/
//	├── doc.go           # 包文档
//	├── models.go        # 核心数据结构与枚举定义
//	└── models_test.go   # 模型单元测试
//
// 对应 Python 代码：openjiuwen/agent_teams/team_workspace/models.py
package team_workspace
