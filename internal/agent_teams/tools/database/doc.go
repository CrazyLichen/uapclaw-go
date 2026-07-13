// Package database 提供数据库工具配置。
//
// 本包定义数据库连接配置结构体，对齐 Python 端数据库配置的实现。
// 支持多种数据库后端：SQLite、PostgreSQL、MySQL、Memory。
//
// 文件目录：
//
//	database/
//	├── doc.go           # 包文档
//	├── config.go        # DatabaseConfig 等配置结构体与构造函数
//	└── config_test.go   # 配置相关测试
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/database/
package database
