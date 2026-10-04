// Package indexing 提供文档级索引管线：解析 → 分块 → 嵌入 → 入库。
//
// 与 agentcore/foundation/store/index（7.36 用户记忆索引）不同，
// 本包处理外部文档（上传文件/链接/图片）的索引构建与维护。
// 用户记忆索引按用户/作用域增删改 KV+Vector 双写；
// 文档索引按 doc_id 整体替换，纯 Vector 存储。
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/
package indexing
