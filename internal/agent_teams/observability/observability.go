package observability

// ──────────────────────────── 导出函数 ────────────────────────────

// 公共 API 重导出（对齐 Python __init__.py）。
//
// Python:
//
//	from openjiuwen.agent_teams.observability import (
//	    ObservabilityConfig,
//	    ObservabilityRail,
//	    attach_to_team_agent,
//	    init_observability,
//	    shutdown_observability,
//	)
//
// 使用方式：
//
//	cfg := observability.DefaultObservabilityConfig()
//	observability.InitObservability(cfg)
//	rail := observability.NewObservabilityRail(nil)
//	// 注册 rail 到 DeepAgent
//	observability.ShutdownObservability()
