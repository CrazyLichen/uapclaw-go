// Package database 提供数据库工具配置、接口定义和 InMemory 实现。
//
// 本包定义数据库连接配置结构体、TeamDatabase 门面接口、DAO 接口（TeamDao/MemberDao/TaskDao/MessageDao）、
// FSM 状态转换表和校验函数，以及 InMemoryTeamDatabase 单体实现。
// 对齐 Python 端 openjiuwen/agent_teams/tools/database/ + tools/memory_database.py 的实现。
// 支持 SQLite、PostgreSQL、MySQL、Memory 四种数据库后端。
// TaskDao 接口已完成 ✅ 9.65a-2；MessageDao 接口为空接口占位 ⤵️ 9.65a-3。
//
// 文件目录：
//
//	database/
//	├── doc.go               # 包文档
//	├── config.go            # DatabaseConfig 配置结构体与构造函数
//	├── config_test.go       # 配置测试
//	├── models.go            # Team + TeamMember + TeamTaskBase + TeamTaskDependencyBase 数据模型 + 辅助类型（NewTaskSpec/EdgeSpec/GraphMutationResult）+ 动态表常量
//	├── database.go          # TeamDatabase 门面接口 + TeamDao/MemberDao/TaskDao/MessageDao DAO 接口
//	├── fsm.go               # FSM 状态转换表（MemberTransitions/ExecutionTransitions/TaskTransitions）+ 校验函数
//	├── engine.go            # 数据库引擎初始化函数 + GetCurrentTime/SanitizeSessionIDForTable
//	├── memory_impl.go       # InMemoryTeamDatabase 单体实现（TeamDatabase+TeamDao+MemberDao+TaskDao + 管线+终止传播）
//	├── team_dao.go          # TeamDao 占位文件（实现已在 memory_impl.go）
//	├── member_dao.go        # MemberDao 占位文件（实现已在 memory_impl.go）
//	├── task_dao.go          # TaskDao 注释说明文件（实现已在 memory_impl.go）
//	├── message_dao.go       # MessageDao 占位文件                           ⤵️ 9.65a-3
//	├── models_test.go       # 模型序列化测试
//	├── database_test.go     # 接口满足性测试
//	├── memory_impl_test.go  # InMemory DAO 全面测试（含 TaskDao）
//	└── engine_test.go       # engine 工具函数测试
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/database/ + openjiuwen/agent_teams/tools/memory_database.py
package database
