package team_runtime

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// RuntimeBindable 可绑定运行时接口。
// Agent 实现此接口后，TeamRuntime.RegisterAgent() 会在 Agent 创建时
// 自动调用 BindRuntime 注入 TeamRuntime 引用和 agentID。
//
// 对应 Python: CommunicableAgent.bind_runtime() 方法签名。
type RuntimeBindable interface {
	// BindRuntime 绑定团队运行时，注入运行时引用和 Agent 标识。
	BindRuntime(runtime *TeamRuntime, agentID string)
}
