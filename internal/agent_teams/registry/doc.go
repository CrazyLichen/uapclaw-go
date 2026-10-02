// Package registry 提供团队运行时池的访问接口，打破 agent ↔ runtime 循环依赖。
//
// 本包定义 PoolAccessor 和 PoolReader 最小接口：
// agent 包通过这些接口访问 TeamRuntimeManager 的 Pool 功能，
// 无需直接 import runtime 包（避免 agent → runtime → agent 循环）。
//
// 原来的 PoolEntry 接口（含 GetEntry/RemoveEntry/HasActive/ListTeamNames/TeamsForSession）
// 和 PoolTeamEntry 接口已删除，因为：
// - 应用层 TeamManager（swarm/agents/harness/team）替代了间接访问
// - adapter 直接通过 runtime.GetTeamRuntimeManager().Pool() 访问池
// - agent 包只需要 GetSessionIDForTeam + RemoveTeam 两个操作
//
// 文件目录：
//
//	registry/
//	├── doc.go              # 包文档
//	└── interfaces.go       # PoolAccessor + PoolReader 接口
//
// 对应 Python 代码：无独立对应，接口从 runtime/manager.py + runtime/pool.py 提取
package registry
