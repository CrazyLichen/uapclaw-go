//go:build integration

// Package context_engine 提供上下文引擎集成测试。
//
// 测试覆盖范围：
//   - ContextEngine 门面：创建/配置/上下文池管理/ClearContext 三粒度/SaveContexts 持久化
//   - SessionModelContext 核心：AddMessages/GetContextWindow/窗口截断/Statistic/ClearMessages/SaveLoadState/PopMessages/OffloadMessages
//   - 处理器注册表：已注册处理器类型验证
//   - MicroCompactProcessor：创建配置/触发判断/清除旧工具结果/SaveLoadState
//   - MessageOffloader：创建配置/触发判断/SaveLoadState
//   - KVCacheManager：创建/Release 无 Model/首次快照/连续相同窗口
//   - ProcessorStateRecorder：压缩历史记录
//   - CompressContext：无处理器 noop/上下文不存在错误
//   - 进程重启恢复：生产装配（CreateCheckpointer → SQLite 持久化检查点）落盘 checkpoint.db →
//     新检查点器/新会话 → 新引擎 CreateContext 从检查点还原消息/工具链；不同 sessionID 不串味
//
// 文件目录：
//
//	context_engine/
//	├── doc.go                          # 包文档
//	├── context_engine_e2e_test.go      # ContextEngine E2E 集成测试（36 测试）
//	└── context_restart_reload_test.go  # 进程重启后从 checkpointer 恢复上下文（2 测试）
//
// 对应 Python 代码：openjiuwen/core/context_engine/
package context_engine
