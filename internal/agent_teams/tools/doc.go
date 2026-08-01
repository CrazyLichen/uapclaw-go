// Package tools 提供团队工具集。
//
// 本包定义团队协作所需的核心工具接口：TeamTaskManager、TeamMessageManager。
// 数据模型已迁移到 database 子包（Team/TeamMember），InMemoryTeamDatabase 也迁移到 database 子包。
// 当前为薄接口+空实现阶段，真实逻辑由领域 9.65a 回填。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── task_manager.go      # TeamTaskManager 接口                          ⤵️ 9.65a-2
//	├── message_manager.go   # TeamMessageManager 接口                       ⤵️ 9.65a-3
//	└── database/
//	    ├── doc.go           # 数据库子包文档
//	    ├── config.go        # DatabaseConfig 配置
//	    ├── config_test.go   # 配置测试
//	    ├── models.go        # Team + TeamMember 数据模型 + 动态表常量
//	    ├── database.go      # TeamDatabase 门面接口 + TeamDao/MemberDao DAO 接口
//	    ├── fsm.go           # FSM 状态转换表 + 校验函数（避免 database→schema 循环依赖）
//	    ├── engine.go        # 数据库引擎初始化函数 + GetCurrentTime/SanitizeSessionIDForTable
//	    ├── memory_impl.go   # InMemoryTeamDatabase 单体实现（TeamDatabase+TeamDao+MemberDao）
//	    ├── team_dao.go      # TeamDao 占位文件
//	    ├── member_dao.go    # MemberDao 占位文件
//	    ├── task_dao.go      # TaskDao 占位文件                             ⤵️ 9.65a-2
//	    ├── message_dao.go   # MessageDao 占位文件                          ⤵️ 9.65a-3
//	    ├── models_test.go   # 模型序列化测试
//	    ├── database_test.go # 接口满足性测试
//	    ├── memory_impl_test.go # InMemory DAO 全面测试
//	    └── engine_test.go   # engine 工具函数测试
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/
package tools
