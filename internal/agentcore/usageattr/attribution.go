package usageattr

// Attribution 一次请求的用量归属（owner 与调用血缘）。
// 键集合与 Python 的 _attribution_fields 一致：product_session_id /
// execution_session_id / context_owner_id / agent_id / team_id / member_name /
// invocation_id / parent_invocation_id / delegation_id / agent_path / depth /
// cache_identity / parent_cache_identity / cache_mode / cache_scope。
// Python: dict[str, Any]（react_agent.py L1326-1418）
type Attribution map[string]any
