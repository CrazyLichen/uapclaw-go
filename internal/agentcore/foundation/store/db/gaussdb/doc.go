// Package gaussdb 提供 GaussDB 数据库的方言适配和存储实现。
//
// 本包完整对标 Python 的 openjiuwen/extensions/store/db/gauss_dialect.py，
// 通过自定义 GORM Dialector 处理 GaussDB 与 PostgreSQL 的不兼容点：
//   - 不支持 NOWAIT / SKIP LOCKED 锁选项
//   - 不支持原生 ENUM / UUID 类型
//   - 非 string 值绑定到 string 列时需要自动转换
//
// GaussDbStore 实现 db.BaseDbStore 接口，通过 GaussDialector 创建
// *gorm.DB 实例，供上层组件（SqlDbStore、SqlMessageStore 等）使用。
//
// 文件目录：
//
//	gaussdb/
//	├── doc.go          # 包文档
//	├── dialector.go    # GaussDB 方言定义
//	├── store.go        # GaussDbStore 存储实现
//	├── clause.go       # GaussDB LOCKING 子句构建器
//	├── serializer.go   # GaussDB 字符串序列化器
//	└── migrator.go     # GaussDB 迁移器
//
// 对应 Python 代码：
//
//	openjiuwen/extensions/store/db/gauss_db_store.py
//	openjiuwen/extensions/store/db/gauss_dialect.py
//
// 核心类型/接口索引：
//
//	GaussDialector  — GaussDB 数据库方言，基于 PostgreSQL 方言扩展
//	GaussMigrator   — GaussDB 迁移器，基于 PostgreSQL 迁移器扩展
//	GaussDbStore    — GaussDB 数据库存储，实现 db.BaseDbStore 接口
package gaussdb
