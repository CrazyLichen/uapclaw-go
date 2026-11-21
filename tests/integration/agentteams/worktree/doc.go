// Package worktree 25.7 worktree 隔离集成测试（基准包名占位；测试体在
// worktree_test 包）。
//
// 覆盖 25.7 spec §7.2 的集成用例：全链（ensure → DAO 登记 → 运行时上下文注入）、
// 复用、finalize 贡献分类、散会清理与孤儿清扫、非 git 报错、
// shell guard provider 链（rail Build → deny 模式 → shell permission 判定）、
// remote 往返。真实 git 仓库用 t.TempDir() + git init 构造；messager 边界用
// 回环 fake（mock 只允许进程/网络边界）。
//
// 跨子系统：agent_teams/worktree（lifecycle/所有权管理）→ tools/database
// （成员行 worktree 元数据）→ harness/tools/worktree（git 执行与锁）→
// harness/manifest（sys_operation rail provider 链）→ core/sys_operation/cwd。
//
// 对应 Python 代码：openjiuwen/agent_teams/worktree/（集成行为面）
package worktree
