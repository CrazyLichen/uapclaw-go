// Package lite 提供轻量级记忆系统的接口定义。
//
// 本包对齐 Python openjiuwen/core/memory/lite/ 的目录结构，
// 定义 MemoryIndexManager、MemorySettings、工具上下文、工具操作等接口。
// 当前为薄接口+空实现阶段，真实逻辑由领域 7.x 各章节回填。
//
// 文件目录：
//
//	lite/
//	├── doc.go                       # 包文档
//	├── config.go                    # MemorySettings + IsMemoryEnabled + CreateMemorySettings    ⤵️ 7.4
//	├── types.go                     # MemoryChunk 数据类                                          ⤵️ 7.4
//	├── internal.go                  # 纯计算工具函数（部分真实实现）                               ⤵️ 7.4
//	├── frontmatter.go               # frontmatter 解析/验证/重建                                  ⤵️ 7.5
//	├── conflict_types.go            # WriteMode + WriteResult（真实实现）                         ⤵️ 7.2
//	├── embeddings.go                # EmbeddingProvider 接口 + Mock + resolve_from_env           ⤵️ 7.4
//	├── manager.go                   # MemoryIndexManager 接口 + Params + SessionDeltaState       ⤵️ 7.1
//	├── tool_context_base.go         # LiteMemoryToolContextBase                                   ⤵️ 7.3
//	├── tool_context.go              # MemoryToolContext                                           ⤵️ 7.3
//	├── coding_memory_tool_context.go # CodingMemoryToolContext                                   ⤵️ 7.3
//	├── tool_ops.go                  # memory_search/read/write/edit_with_context                  ⤵️ 7.2
//	├── coding_memory_tool_ops.go    # coding_memory_read/write/edit_with_context                  ⤵️ 7.2
//	└── tools.go                     # InitMemoryManagerAsync                                      ⤵️ 7.2
//
// 对应 Python 代码：openjiuwen/core/memory/lite/
package lite
