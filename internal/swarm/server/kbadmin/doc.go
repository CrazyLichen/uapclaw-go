// Package kbadmin 知识库管理面（领域三十，WeKnora 对标）业务编排域。
//
// 职责：kb_info/kb_doc/kb_chunk_meta/kb_chunk_revision/kb_task 五表台账
// （SQLite，AutoMigrate 自建）、KBService 编排（三库类型 create/delete +
// retrieval 工厂接线）、DocService 文档管理编排（上传分帧 + 解析任务/
// 重解析/取消/列表）、ParseWorker 解析任务链（固定池 + 声明式 pipeline +
// ProgressSink 观察者）、kb.* 方法族 E2A 分发与 kb.task 通信面。
// chunk 正文与向量在向量库 collection（retrieval 所有），本域只管管理面元数据，
// 靠 kb_id/doc_id/chunk_id 与向量库对齐；retrieval 包零改动。
//
// 文件目录：
//
//	kbadmin/
//	├── doc.go           # 包文档
//	├── models.go        # 五表 GORM 结构体
//	├── store.go         # Store：打开 SQLite（busy_timeout+WAL）+ AutoMigrate
//	├── kbconfig.go      # KBConfig 解析（上限/chunker/embedding 配置）
//	├── kb_dao.go        # kb_info DAO
//	├── doc_dao.go       # kb_doc DAO
//	├── chunk_dao.go     # kb_chunk_meta + kb_chunk_revision DAO
//	├── task_dao.go      # kb_task DAO
//	├── kb_service.go    # KBService 三库类型 CRUD 编排 + RetrievalKBFactory
//	├── pipeline.go      # 声明式解析步骤计划（parse/chunk/embed/triple）
//	├── worker.go        # ParseWorker 固定池（CAS 认领/孤儿恢复/ctx 取消）
//	├── progress.go      # ProgressSink 观察者（stages_json/事件发布/停滞扫描）
//	├── doc_service.go   # DocService 解析任务编排（提交/重解析/取消/列表）
//	├── task_progress.go # kb.task.progress/subscribe 通信面（Domain 方法）
//	├── doc_upload.go    # DocService 上传分帧编排（发起/收尾挂点/取消/反向下载）
//	├── stub_services.go # Chunk/Search Service 骨架（30.6 回填）
//	├── upload_store.go  # 上传槽位表（TTL 清理/上限判定/sha256 收尾）
//	└── handle_kb.go     # HandleKBRequest 分发入口 + Domain 装配
//
// 对应 Python 代码：无（新增能力域，上游 Python 无对应物，对标 Tencent/WeKnora）。
package kbadmin
