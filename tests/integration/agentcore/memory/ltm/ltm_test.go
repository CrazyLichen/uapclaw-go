//go:build integration

package ltm_test

// TODO: 补充 LongTermMemory 集成测试
//   - RegisterStore 完整流程（需要真实 InMemoryKVStore + InMemoryVectorStore）
//   - SetConfig 初始化所有 manager（需要真实 db_store + vector_store）
//   - addMessagesImpl 完整写入流程（需要真实 LLM + 消息存储）
//   - MigrateBetweenIndices 跨索引迁移
//   - SearchUserMem 真实搜索
//   - GetVariables 真实变量读写
//   - DeleteMemByID / DeleteMemByUserID 真实删除
//   - UpdateMemByID / UpdateVariables 真实更新
//   - GetRecentMessages / GetMessageByID 真实读取
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/memory/ltm/...
