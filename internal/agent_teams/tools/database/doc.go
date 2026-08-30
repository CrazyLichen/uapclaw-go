// Package database 提供数据库工具配置、接口定义和 InMemory/SQL 双后端实现。
//
// 本包定义数据库连接配置结构体、TeamDatabase 门面接口、DAO 接口（TeamDao/MemberDao/TaskDao/MessageDao）、
// FSM 状态转换表和校验函数，以及 InMemoryTeamDatabase 和 SqlTeamDatabase 两种后端实现。
// 对齐 Python 端 openjiuwen/agent_teams/tools/database/ + tools/memory_database.py 的实现。
// 支持 SQLite、PostgreSQL、MySQL、Memory 四种数据库后端。
// 通过 NewTeamDatabase 工厂函数按 db_type 自动选择实现。
// SQL DAO 操作动态表（team_task_{suffix}/team_message_{suffix}），表名由 ctx 中 session_id 决定。
//
// 文件目录：
//
//	database/
//	├── doc.go               # 包文档
//	├── config.go            # DatabaseConfig 配置结构体与构造函数
//	├── models.go            # Team + TeamMember + TeamTaskBase + TeamMessageBase + MessageReadStatusBase 数据模型 + 辅助类型 + 动态表常量
//	├── database.go          # TeamDatabase 门面接口 + TeamDao/MemberDao/TaskDao/MessageDao DAO 接口 + NewTeamDatabase 工厂
//	├── fsm.go               # FSM 状态转换表（MemberTransitions/ExecutionTransitions/TaskTransitions）+ 校验函数
//	├── engine.go            # GetCurrentTime / SanitizeSessionIDForTable 工具函数 + GetSessionIDFunc 注入点
//	├── memory_impl.go       # InMemoryTeamDatabase 单体实现（TeamDatabase+TeamDao+MemberDao+TaskDao+MessageDao + 管线+终止传播）
//	├── sql_engine.go        # SqlTeamDatabase 门面 + newGormDB + DDL 建表/删表 + 清理 + WithTx
//	├── sql_team_dao.go      # SQLTeamDao（5 方法，静态表 team_info）
//	├── sql_member_dao.go    # SQLMemberDao（8 方法，含 CAS，静态表 team_member）
//	├── sql_task_dao.go      # SQLTaskDao（18 方法 + 5 辅助函数 + 环检测，动态表 team_task_{suffix}）
//	└── sql_message_dao.go   # SQLMessageDao（7 方法，含重试 + watermark，动态表 team_message_{suffix}）
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/database/ + openjiuwen/agent_teams/tools/memory_database.py
package database
