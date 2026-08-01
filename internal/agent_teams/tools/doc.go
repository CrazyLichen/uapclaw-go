// Package tools 提供团队工具集。
//
// 本包定义团队协作所需的核心工具：TeamTaskManager（任务管理器）、TeamMessageManager（消息管理器）。
// 数据模型已迁移到 database 子包（Team/TeamMember/TeamTask），InMemoryTeamDatabase 也迁移到 database 子包。
// TeamTaskManager 为具体 struct 实现（含 PLAN_MODE），messager 以 any 占位、⤵️ 9.65 回填事件发布。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── task_manager.go      # TeamTaskManager 具体实现（20+ 方法） ✅ 9.65a-2
//	├── task_manager_test.go # TeamTaskManager 测试（85.2% 覆盖率） ✅ 9.65a-2
//	├── message_manager.go   # TeamMessageManager 接口                       ⤵️ 9.65a-3
//	└── database/
//	    ├── doc.go           # 数据库子包文档
//	    ├── config.go        # DatabaseConfig 配置
//	    ├── config_test.go   # 配置测试
//	    ├── models.go        # Team + TeamMember + TeamTask 数据模型 + 动态表常量 ✅ 9.65a-2
//	    ├── database.go      # TeamDatabase 门面接口 + TeamDao(18方法)/MemberDao DAO 接口 ✅ 9.65a-2
//	    ├── fsm.go           # FSM 状态转换表 + 校验函数（避免 database→schema 循环依赖）
//	    ├── engine.go        # 数据库引擎初始化函数 + GetCurrentTime/SanitizeSessionIDForTable
//	    ├── memory_impl.go   # InMemoryTeamDatabase 单体实现（含 TaskDao 18 方法） ✅ 9.65a-2
//	    ├── team_dao.go      # TeamDao 占位文件
//	    ├── member_dao.go    # MemberDao 占位文件
//	    ├── task_dao.go      # TaskDao 注释说明文件                           ✅ 9.65a-2
//	    ├── message_dao.go   # MessageDao 占位文件                          ⤵️ 9.65a-3
//	    ├── models_test.go   # 模型序列化测试
//	    ├── database_test.go # 接口满足性测试
//	    ├── memory_impl_test.go # InMemory DAO 全面测试（89.1% 覆盖率）
//	    └── engine_test.go   # engine 工具函数测试
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/
package tools
