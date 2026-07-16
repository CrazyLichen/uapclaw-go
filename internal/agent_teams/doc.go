// Package agent_teams 提供生产级团队 Agent 系统。
//
// agent_teams 是多 Agent 协作的完整应用层，包含：
//   - TeamAgent：可充当 Leader 或 Teammate 的核心编排节点
//   - CoordinationKernel：协调内核（事件总线 + 调度器 + 生命周期）
//   - SpawnManager/RecoveryManager/SessionManager/StreamController：专职 Manager
//   - TeamBackend/Messager/Memory/Workspace：团队基础设施
//
// 对齐 Python: openjiuwen/agent_teams/
//
// 文件目录：
//
//	agent_teams/
//	├── doc.go              # 包文档
//	├── constants.go        # 保留名常量
//	├── context.go          # session_id contextvar
//	├── i18n.go             # 多语言 i18n 支持
//	├── harness.go          # TeamHarness 团队适配层（9.57）
//	├── agent/              # TeamAgent 核心实现（9.55）
//	├── schema/             # 团队级 Schema 定义（9.55）
//	├── interaction/        # ⤵️ 回填: 9.59 团队交互
//	├── memory/             # ⤵️ 回填: 9.64 团队记忆
//	├── messager/           # ⤵️ 回填: 9.65 团队消息总线
//	├── models/             # ⤵️ 回填: 9.64 模型池/分配器
//	├── monitor/            # ⤵️ 回填: 9.67 团队监控
//	├── observability/      # ⤵️ 回填: 9.67 OpenTelemetry
//	├── rails/              # ⤵️ 回填: 9.68 团队级 Rails
//	├── prompts/            # ⤵️ 回填: 9.69 团队提示词
//	├── runtime/            # ⤵️ 回填: 9.62 团队运行时
//	├── spawn/              # 进程内生成 + 共享资源（9.58）
//	│   ├── doc.go          # 包文档
//	│   ├── handle.go       # SpawnHandle 统一接口
//	│   ├── inprocess_handle.go # InProcessSpawnHandle
//	│   ├── inprocess_spawn.go  # InProcessSpawn + SpawnableAgent
//	│   └── shared_resources.go # 进程级全局单例
//	├── team_workspace/     # ⤵️ 回填: 9.66 团队工作空间
//	├── tools/              # ⤵️ 回填: 9.58 团队工具（TeamBackend 等）
//	└── cli/                # ⤵️ 回填: 9.54 CLI
//
// 对应 Python 代码：openjiuwen/agent_teams/
package agent_teams
