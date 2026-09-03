// Package tools 提供团队工具集。
//
// 本包定义团队协作所需的核心工具：TeamBackend（团队后端门面）、TeamTaskManager（任务管理器）、TeamMessageManager（消息管理器）。
// 数据模型已迁移到 database 子包（Team/TeamMember/TeamTask/TeamMessageBase），InMemoryTeamDatabase 也迁移到 database 子包。
// TeamBackend 组合 DB + TaskManager + MessageManager + Messager，提供团队级业务门面（30+ 方法）。
// Functional Options 构造模式，独立 RWMutex 仅保护 HITT 缓存，串行文件清理。
// TeamTaskManager 为具体 struct 实现（含 PLAN_MODE + 事件发布），messager 已类型化为 messager.Messager。
// 事件发布改用 schema.TypedEvent + schema.EventMessageFromEvent，sessionID 从 context 获取（schema.GetSessionID(ctx)）。
// TeamMessageManager 为具体 struct 实现（7 方法薄门面），委托 db.Message() 执行 DAO 操作。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── team_backend.go      # TeamBackend 门面（30+ 方法 + Functional Options） ✅ 9.65a-4
//	├── task_manager.go      # TeamTaskManager 具体实现（20+ 方法 + Add/WithPriority/AsTopPriority/Batch + 事件发布 + Option 模式） ✅ 9.65a-2 + 9.65-1
//	├── message_manager.go   # TeamMessageManager 具体实现（7 方法薄门面） ✅ 9.65a-3
//	└── database/
//	    ├── doc.go           # 数据库子包文档
//	    ├── config.go        # DatabaseConfig 配置
//	    ├── models.go        # Team + TeamMember + TeamTask + TeamMessageBase 数据模型 + 辅助类型 + 动态表常量
//	    ├── database.go      # TeamDatabase 门面接口 + TeamDao/MemberDao/TaskDao/MessageDao DAO 接口
//	    ├── fsm.go           # FSM 状态转换表 + 校验函数
//	    ├── engine.go        # 数据库引擎初始化函数 + GetCurrentTime/SanitizeSessionIDForTable
//	    ├── memory_impl.go   # InMemoryTeamDatabase 单体实现（含 TaskDao + MessageDao + 管线 + 终止传播）
//	    ├── sql_engine.go    # SqlTeamDatabase 门面 + newGormDB + DDL 建表/删表 + 清理
//	    ├── sql_team_dao.go  # SQLTeamDao（5 方法，静态表 team_info）
//	    ├── sql_member_dao.go # SQLMemberDao（8 方法，含 CAS，静态表 team_member）
//	    ├── sql_task_dao.go  # SQLTaskDao（18 方法 + 5 辅助函数 + 环检测，动态表 team_task_{suffix}）
//	    └── sql_message_dao.go # SQLMessageDao（7 方法，含重试 + watermark，动态表 team_message_{suffix}）
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/
package tools
