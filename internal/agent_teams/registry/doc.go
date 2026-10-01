// Package registry 提供团队运行时池的访问接口。
//
// 本包的核心目的是打破 agent ↔ runtime 循环依赖：
// agent 和 runtime 包都需要访问 TeamRuntimeManager 的 Pool 功能，
// 但 agent 不能直接 import runtime（会形成 agent → runtime → agent 循环）。
// 通过在 registry 中定义 PoolAccessor/PoolEntry/PoolTeamEntry 接口，
// agent 和 runtime 都依赖 registry，互不 import。
//
// 文件目录：
//
//	registry/
//	├── doc.go              # 包文档
//	└── interfaces.go       # PoolAccessor + PoolEntry + PoolTeamEntry 接口
//
// 对应 Python 代码：无独立对应，接口从 runtime/manager.py + runtime/pool.py 提取
package registry
