// Package kbadmin 知识库管理面（领域三十，WeKnora 对标）业务编排域。
//
// 职责：kb_info/kb_doc/kb_chunk_meta/kb_chunk_revision/kb_task 五表台账
// （SQLite，AutoMigrate 自建）、KBService 编排（retrieval 工厂接线）、
// DocService/ChunkService/SearchService 骨架、kb.* 方法族 E2A 分发。
// chunk 正文与向量在向量库 collection（retrieval 所有），本域只管管理面元数据，
// 靠 kb_id/doc_id/chunk_id 与向量库对齐；retrieval 包零改动。
//
// 文件目录：
//
//	kbadmin/
//	├── doc.go           # 包文档
//	├── models.go        # 五表 GORM 结构体
//	├── store.go         # Store：打开 SQLite + AutoMigrate
//	├── kb_dao.go        # kb_info DAO
//	├── doc_dao.go       # kb_doc DAO
//	├── chunk_dao.go     # kb_chunk_meta + kb_chunk_revision DAO
//	├── task_dao.go      # kb_task DAO
//	├── kb_service.go    # KBService 真实 CRUD 编排
//	├── doc_upload.go    # DocService 上传分帧编排（发起/收尾/取消/反向下载，30.5）
//	├── stub_services.go # Doc/Chunk/Search Service 骨架（Doc 上传段 30.5 已摘）
//	├── upload_store.go  # 上传槽位表（TTL 清理/串行守卫/sha256 收尾，30.5）
//	└── handle_kb.go     # HandleKBRequest 分发入口
//
// 对应 Python 代码：无（新增能力域，上游 Python 无对应物，对标 Tencent/WeKnora）。
package kbadmin
