// Package metadata 提供团队命名空间（Team Namespace）的持久化读写函数。
//
// 在 session state 的 "teams" 顶层 key 下，按 team_name 分桶存储每个团队的
// spec、context、allocator_state、db_state、lifecycle 等数据。
// 所有函数通过 SessionFacade.UpdateState / GetState 操作分桶数据，
// 遵循 read → mutate → write back 模式（对齐 Python session.update_state 浅合并）。
//
// 核心数据结构：
//
//	state["teams"][team_name] = {
//	    "spec": ...,                      // TeamAgentSpec.model_dump()
//	    "context": ...,                   // TeamRuntimeContext.model_dump()
//	    "model_allocator_state": ...,     // 可选
//	    "lifecycle": "running"|"paused",  // 可选，由 pause 路径写入
//	    "db_state": "pending_create"|"created"|"cleaned",
//	}
//
// 文件目录：
//
//	metadata/
//	├── doc.go           # 包文档
//	├── metadata.go      # Team Namespace 读写函数
//	└── metadata_test.go # 单元测试
//
// 对应 Python 代码：openjiuwen/agent_teams/runtime/metadata.py
package metadata
