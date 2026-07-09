// Package skill 提供 AgentServer 侧的技能管理器（SkillManager），
// 负责技能的加载、安装、卸载、marketplace 操作、SkillNet/ClawHub/TeamSkillsHub 交互，
// 以及技能状态文件 skills_state.json 的读写与查询。
//
// SkillManager 对应 Python jiuwenswarm/server/runtime/skill/skill_manager.py，
// 为独立实现（不内嵌 agentcore SkillManager），通过 handler 方法
// 处理 skills.* 和 plugins.* 请求。
//
// state_utils 提供纯函数式的状态查询能力，可被 skill_manager 内部使用，
// 也可被其他模块直接引用，避免循环依赖。
//
// skill_routes 定义 skills.* 和 plugins.* 的 ReqMethod → handler 方法名路由映射，
// 以及 NeedsRebuild 判断逻辑。
//
// 文件目录：
//
//	skill/
//	├── doc.go              # 包文档
//	├── state_utils.go      # 纯函数式状态查询（GetStateFile/NormalizeSkillConfigs 等）
//	├── skill_manager.go    # SkillManager 核心结构体与 handler 方法
//	├── skill_routes.go     # ReqMethod → handler 路由映射 + NeedsRebuild
//	├── state_utils_test.go # state_utils 测试
//	├── skill_manager_test.go # SkillManager 测试
//	└── skill_routes_test.go  # 路由映射测试
//
// 对应 Python 代码：
//   - jiuwenswarm/server/runtime/skill/skilldev/state_utils.py → state_utils.go
//   - jiuwenswarm/server/runtime/skill/skill_manager.py → skill_manager.go
//   - jiuwenswarm/server/runtime/agent_adapter/interface.py (_SKILL_ROUTES/_PLUGIN_ROUTES) → skill_routes.go
package skill
