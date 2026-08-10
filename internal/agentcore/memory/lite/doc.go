// Package lite 提供轻量级记忆系统的核心实现。
//
// 本包对齐 Python openjiuwen/core/memory/lite/ 的目录结构，
// 实现 MemoryIndexManager（记忆索引管理器）、混合搜索（向量+FTS5）、
// 文件监听同步、Embedding 缓存等核心功能。
//
// 文件目录：
//
//	lite/
//	├── doc.go                       # 包文档
//	├── config.go                    # MemorySettings + IsMemoryEnabled + CreateMemorySettings
//	├── types.go                     # MemoryChunk 数据类
//	├── internal.go                  # 纯计算工具函数（FTS5 查询构建、BM25 分数转换等）
//	├── frontmatter.go               # frontmatter 解析/验证/重建
//	├── conflict_types.go            # WriteMode + WriteResult + ToDict（对齐 Python）
//	├── embeddings.go                # EmbeddingProvider 接口 + Mock + baseEmbeddingAdapter
//	├── vec_loader.go                # vec0.so 加载器（ResolveVec0Path + LoadVec0Extension）
//	├── manager.go                   # MemoryIndexManager 接口 + Params + 导出函数
//	├── manager_impl.go              # MemoryIndexManager 实现（Initialize + Sync + Search + Close）
//	├── tool_context_base.go         # LiteMemoryToolContextBase + EnsureManager
//	├── tool_context.go              # MemoryToolContext
//	├── coding_memory_tool_context.go # CodingMemoryToolContext
//	├── tool_ops.go                  # memory_search/read/write/edit_with_context
//	├── coding_memory_tool_ops.go    # coding_memory_read/write/edit_with_context + 并发控制 + 索引更新
//	└── tools.go                     # InitMemoryManagerAsync + InitCodingMemoryManagerAsync
//
// 对应 Python 代码：openjiuwen/core/memory/lite/
package lite
