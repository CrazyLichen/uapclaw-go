// Package worktree 提供团队侧的 worktree 所有权管理层。
//
// 与 agentcore 通用工具箱（openjiuwen/harness/tools/worktree，怎么建/删/算 diff/锁）
// 分工：本包回答"团队里谁在哪个 worktree、干完了怎么收、散会怎么清"——
// 成员进场三重门、复用还是新建、写成员 DB 行、散会收编与孤儿清扫。
//
// 文件目录：
//
//	worktree/
//	├── doc.go            # 包文档
//	├── naming.go        # 会话级成员 worktree 命名（team-member-哈希 slug）
//	├── session_scope.go # 严格会话/项目所有权 scope 与 manager 视图
//	├── member_state.go  # 成员 worktree 元数据与 scope 匹配解析
//	├── lifecycle.go     # TeammateWorktreeLifecycle 管家（ensure/finalize/清扫）
//	├── session_cleanup.go # 会话 worktree 目录清理
//	└── worktree_remote.go # 远端节点分布式 worktree 后端（RemoteWorktreeBackend）
//
// 对应 Python 代码：openjiuwen/agent_teams/worktree/ 与 worktree_remote.py
package worktree
