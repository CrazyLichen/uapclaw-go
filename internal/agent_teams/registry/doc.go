// Package registry 提供团队运行时池的访问接口，打破 agent ↔ runtime 循环依赖。
//
// 本包定义 PoolAccessor 和 PoolReader 最小接口：
//   - PoolAccessor(PoolReader() PoolReader)：runner 存储此类型，agent 通过 runner 获取
//   - PoolReader(GetSessionIDForTeam/RemoveTeam)：agent 只需这两个操作
//
// 依赖关系：
//
//	runner ──→ registry    （teamRuntimeManager 字段类型为 PoolAccessor）
//	runtime ──→ registry   （实现 PoolAccessor + PoolReader）
//	agent  ──→ runner      （通过 GetTeamRuntimeManager() 获取 PoolAccessor，零断言）
//
// 原来的 PoolEntry/PoolTeamEntry 接口和 PoolAny() any 已删除，因为：
//   - 应用层 TeamManager 替代了间接访问
//   - PoolAccessor.PoolReader() 返回 PoolReader 接口，无需 any 中转
//   - agent 包零类型断言直接调用
//
// 文件目录：
//
//	registry/
//	├── doc.go              # 包文档
//	└── interfaces.go       # PoolAccessor + PoolReader 接口
//
// 对应 Python 代码：无独立对应，接口从 runtime/manager.py + runtime/pool.py 提取
package registry
