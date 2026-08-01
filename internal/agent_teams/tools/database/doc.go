// Package database 提供数据库工具配置和接口定义。
//
// 本包定义数据库连接配置结构体和 TeamDatabase 门面接口，
// 对齐 Python 端 openjiuwen/agent_teams/tools/database/ 的实现。
// 支持 SQLite、PostgreSQL、MySQL、Memory 四种数据库后端。
// 当前 TeamDatabase/DAO 接口为薄接口阶段，真实逻辑由领域 9.65a 回填。
//
// 文件目录：
//
//	database/
//	├── doc.go           # 包文档
//	├── config.go        # DatabaseConfig 配置结构体与构造函数
//	├── config_test.go   # 配置测试
//	├── database.go      # TeamDatabase 门面接口 + TeamDao/MemberDao/TaskDao/MessageDao  ⤵️ 9.65a
//	├── engine.go        # 数据库引擎初始化函数                                          ⤵️ 9.65a
//	├── team_dao.go      # TeamDao 占位文件                                              ⤵️ 9.65a
//	├── member_dao.go    # MemberDao 占位文件                                            ⤵️ 9.65a
//	├── task_dao.go      # TaskDao 占位文件                                              ⤵️ 9.65a
//	└── message_dao.go   # MessageDao 占位文件                                           ⤵️ 9.65a
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/database/
package database
