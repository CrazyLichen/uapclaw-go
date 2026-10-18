// Package memory 提供 Swarm 侧的记忆相关功能。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/memory/，
// 是 agentcore lite 记忆包的产品侧完整 fork：workspace_dir 字符串 API、
// 配置自查（config.yaml embed/memory 段 + 环境变量链）、Mock 降级、
// INDEX_CACHE 全局缓存。消费者为 memory_rpc（TUI 通道）。
// 与 internal/agentcore/memory/lite/ 各自对齐各自 Python 基线；
// 唯一共享点：vec0 扩展加载器（lite.ResolveVec0Path/LoadVec0Extension，
// 对应 Python 两包共用第三方库 sqlite_vec）。
//
// 外接记忆（10.6.16）：engine 门禁 + external 段解析 + 四路 provider 分发
// builder 已从 adapter 内联实现迁入本包（external_memory_config.go /
// external_memory_builder.go，对齐 Python external_memory_config.py /
// external_memory_builder.py），adapter 侧 handleExternalMemoryRailByConfig
// 挂载链路保留、改调本包导出函数。
//
// 文件目录：
//
//	memory/
//	├── doc.go          # 包文档
//	├── types.go        # 记忆数据类型（MemorySearchResult/SearchOpts/FileEntry/MemoryChunk 等）
//	├── internal.go     # 纯函数（递归 ListMemoryFiles/ChunkMarkdown/FTS 构建等，swarm 语义）
//	├── config.go       # 配置（EmbedConfig/MemorySettings/mode 解析 + 包内缓存）
//	├── embeddings.go   # EmbeddingProvider 接口 + OpenAICompatible + Mock + 工厂（10.6.15）
//	├── manager.go      # MemoryIndexManager（sync/混合检索/文件监听/嵌入缓存）
//	├── external_memory_config.go # 外接记忆配置（engine 门禁/external 段解析/LTM 目录/openjiuwen 映射）
//	├── external_memory_builder.go # ExternalMemoryRail 构建入口（四路 provider 分发，失败返回 nil）
//	├── forbidden.go    # 记忆禁止配置与提示词生成
//	└── dreaming/       # Dreaming 记忆整理（Sweeper 业务管线）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory/
package memory
