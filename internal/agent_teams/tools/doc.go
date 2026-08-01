// Package tools 提供团队工具集。
//
// 本包定义团队协作所需的核心工具接口：TeamDatabase、TeamTaskManager、
// TeamMessageManager，以及数据模型 TeamInfo、TeamMemberInfo。
// 当前为薄接口+空实现阶段，真实逻辑由领域 9.65a 回填。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── models.go            # TeamInfo + TeamMemberInfo 数据模型              ⤵️ 9.65a
//	├── task_manager.go      # TeamTaskManager 接口                          ⤵️ 9.65a
//	├── message_manager.go   # TeamMessageManager 接口                       ⤵️ 9.65a
//	├── memory_database.go   # InMemoryTeamDatabase 接口                     ⤵️ 9.65a
//	└── database/
//	    ├── doc.go           # 数据库子包文档
//	    ├── config.go        # DatabaseConfig 配置
//	    ├── config_test.go   # 配置测试
//	    ├── database.go      # TeamDatabase 门面接口 + DAO 接口             ⤵️ 9.65a
//	    ├── engine.go        # 数据库引擎初始化函数                          ⤵️ 9.65a
//	    ├── team_dao.go      # TeamDao 占位文件                              ⤵️ 9.65a
//	    ├── member_dao.go    # MemberDao 占位文件                            ⤵️ 9.65a
//	    ├── task_dao.go      # TaskDao 占位文件                              ⤵️ 9.65a
//	    └── message_dao.go   # MessageDao 占位文件                           ⤵️ 9.65a
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/
package tools
