// Package memory 提供团队记忆功能配置与管理。
//
// 本包定义团队记忆（TeamMemory）的配置结构体、管理器、共享记忆管理器、
// 成员记忆工具集和记忆提取器，对齐 Python 端 openjiuwen/agent_teams/memory/
// 的实现。团队记忆支持场景化记忆提取、共享记忆、成员记忆提示等能力。
// 当前管理器和工具集为薄接口+空实现阶段，真实逻辑由领域 7.x 回填。
//
// 文件目录：
//
//	memory/
//	├── doc.go                   # 包文档
//	├── config.go                # TeamMemoryConfig 配置 + ResolveEmbeddingConfig       ⤴️ 9.64 回填完成
//	├── config_test.go           # 配置测试
//	├── manager_params.go        # TeamMemoryManagerParams + 类型别名                    ← 新建
//	├── manager.go               # TeamMemoryManager 5个生命周期方法                     ⤵️ 7.1+7.2+9.65a
//	├── shared_memory.go         # SharedMemoryManager（真实实现）                       ← 新建
//	├── shared_memory_test.go    # SharedMemoryManager 测试
//	├── member_memory_toolkit.go # MemberMemoryToolkit + 工具创建                        ⤵️ 7.2+7.3
//	└── extractor.go             # ExtractTeamMemories + ExtractionAgentPrompt           ⤵️ 7.2+9.65a
//
// 对应 Python 代码：openjiuwen/agent_teams/memory/
package memory
