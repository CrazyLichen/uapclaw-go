// Package hooks（server/hooks）提供 UserHookRail，对齐 Python jiuwenswarm/server/hooks/user_hook_rail.py。
//
// HookExecutor 已迁至 common/hooks（11.13，供 Gateway/AgentServer 共享）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go            # 包文档
//	└── user_hook_rail.go # UserHookRail(embed DeepAgentRail) 4 个钩子方法
//
// 对应 Python 代码：jiuwenswarm/server/hooks/user_hook_rail.py
package hooks
