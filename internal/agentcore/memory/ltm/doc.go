// Package ltm 提供长期记忆编排器（LongTermMemory），记忆系统的顶层入口。
//
// LongTermMemory 是 Singleton 模式的全局实例，不实现任何存储逻辑，
// 而是组装和协调已有的各子模块（manage/、process/、config/、migration/、codec/、common/），
// 对外提供统一的记忆写入（AddMessages）、检索（SearchUserMem）、删除、更新 API。
//
// 在 Agent 会话中的位置：
//   - 写入时机：每轮对话结束后调用 AddMessages 存消息 + LLM 提取记忆碎片
//   - 检索时机：下轮对话前调用 SearchUserMem 检索相关记忆注入 Prompt
//   - 变量读取：通过 GetVariables 读取用户变量注入 Prompt
//
// 文件目录：
//
//	ltm/
//	├── doc.go                 # 包文档
//	├── models.go              # MemInfo / MemResult / AddMemResult 返回值模型
//	├── options.go             # 函数式选项（AddMessagesOption / SearchOption）
//	├── long_term_memory.go    # LongTermMemory 结构体 + sync.Once 单例
//	├── register.go            # RegisterStore / RegisterPlugin / MigrateBetweenIndices
//	├── config_ops.go          # SetConfig / SetScopeConfig / GetScopeConfig / DeleteScopeConfig
//	├── add_messages.go        # AddMessages 核心写入流程
//	├── search_ops.go          # SearchUserMem / SearchUserHistorySummary / GetVariables
//	├── delete_ops.go          # DeleteMemByID / DeleteMemByUserID / DeleteMemByScope 等
//	├── update_ops.go          # UpdateMemByID / UpdateVariables
//	├── query_ops.go           # GetRecentMessages / GetMessageByID / GetUserMemByPage 等
//	├── scope_helpers.go       # getScopeLLM / getScopeConfig / applyScopeEmbedding 等私有方法
//	└── helpers.go             # checkMessages / getHistoryMessages / validateID / runMigration
//
// 对应 Python 代码：openjiuwen/core/memory/long_term_memory.py
package ltm
