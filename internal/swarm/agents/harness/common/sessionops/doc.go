// Package sessionops 提供会话操作服务，包括会话分叉、回退、文件恢复、
// 上下文重建和状态复制等功能。
//
// 对齐 Python: jiuwenswarm/agents/harness/common/session_ops_service.py
//
// 7 个服务函数：
//   - ForkSession           — 分叉会话（sync）
//   - RewindSession         — 回退会话（sync）
//   - ListSessionTurns      — 列出用户 turn（sync）
//   - RestoreSessionFiles   — 恢复文件到指定 turn（sync）
//   - RewindSessionContext  — 重建 context_engine（async）
//   - CopySessionState      — 复制 DeepAgentState（async）
//   - CopySessionContext    — 复制内存上下文（async）
//
// 文件目录：
//
//	sessionops/
//	├── doc.go              # 包文档
//	└── session_ops.go      # 7 个服务函数 + 辅助函数
//
// 依赖说明（已知偏差，10.6.12 破环迁移遗留）：本包 import server/session
// （会话元数据 7 符号）与 server/utils（GetDiffService）。消费方
// gateway/message_handler 经本包对 server 形成传递依赖，违反项目规则
// "gateway 包不 import server/"。因 server 侧（handle_session.go）也消费
// 本包，sessionops 实质是 gateway/server 共享库，物理位置对齐 Python
// （agents/harness/common）。待 server↔agents 边界收敛工程统一处理。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/session_ops_service.py
package sessionops
