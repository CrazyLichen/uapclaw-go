//go:build integration

package milvus

// 集成测试占位文件。
// 需要真实 Milvus 服务器才能运行。
//
// 运行方式: go test -tags=integration ./internal/agentcore/store/graph/milvus/...
//
// TODO: 补充以下集成测试：
// - TestMilvusGraphStore_真实创建集合
// - TestMilvusGraphStore_真实写入和查询
// - TestMilvusGraphStore_真实混合搜索
// - TestMilvusGraphStore_真实BFS扩展
// - TestMilvusGraphStore_真实删除
// - TestMilvusClientGraphAdapter_真实连接
