// Package hooks（server/hooks）提供 Hook 执行引擎和 UserHookRail，对齐 Python jiuwenswarm/server/hooks/。
//
// 本包包含：
//   - HookExecutor：统一调度 command/prompt 两类 hook，返回 HookResult
//   - UserHookRail：以 Rail 形态拦截工具调用和 Agent 生命周期
//
// 配置模型定义在上层 common/hooks 包（HookType/HookEvent/HooksConfig/LoadHooksConfig）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go            # 包文档
//	├── executor.go       # HookOutcome/HookResult/LLMConfig + HookExecutor + ParseCommandOutput + ExtractJSONFromResponse
//	└── user_hook_rail.go # UserHookRail(embed DeepAgentRail) 4 个钩子方法
//
// 对应 Python 代码：jiuwenswarm/server/hooks/executor.py + user_hook_rail.py
package hooks
