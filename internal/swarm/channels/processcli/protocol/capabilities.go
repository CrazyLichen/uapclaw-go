package protocol

// ──────────────────────────── 全局变量 ────────────────────────────

// capabilityFeatures 实现的功能集（全 true）。
// Python: capabilities.py:19-40 features = dict.fromkeys((17 项), True)
var capabilityFeatures = []string{
	"custom_agent",
	"run_model",
	"run_skills",
	"run_mcp",
	"run_permissions",
	"structured_output",
	"max_turns",
	"max_budget_usd",
	"host_tools",
	"interaction_callback",
	"unattended_permission_rejection",
	"cancel",
	"session_resume",
	"stable_event_fields",
	"field_errors",
	"empty_tools",
	"protocol_schema",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ProtocolCapabilities 声明已实现的功能；模型/工具可用性需另行查询。
// Python: capabilities.py:18-65 protocol_capabilities
func ProtocolCapabilities() map[string]any {
	schema := ProtocolSchema()

	// Python: features = dict.fromkeys((...), True)
	features := make(map[string]any, len(capabilityFeatures))
	for _, name := range capabilityFeatures {
		features[name] = true
	}

	// Python: :43 "run_fields": list(schema["$defs"]["run"]["properties"])
	runFields := schemaProperties(schema, "run")
	// Python: :44 "query_operations": schema["$defs"]["query"]["properties"]["operation"]["enum"]
	queryOperations := schemaQueryOperations(schema)

	return map[string]any{
		"schema_version":           CurrentSchemaVersion,
		"supported_schema_versions": stringSliceAny(SupportedSchemaVersions),
		"protocol_revision":        ProtocolRevision,
		"features":                 features,
		"run_fields":               runFields,
		"query_operations":         queryOperations,
		// Python: :45-47
		"control_types":       stringSliceAny([]string{"answer", "tool_result", "cancel"}),
		"stable_event_types":  stringSliceAny(StableEventTypes),
		// Python: :48-51
		"input_encoding":  "utf-8",
		"output_encoding": "utf-8",
		"output_format":   "jsonl",
		"max_input_bytes": MaxRunInputBytes,
		// Python: :52-63
		"compatibility": map[string]any{
			"unknown_input_fields":  "reject",
			"unknown_output_fields": "ignore",
			"unknown_event_types":   "ignore",
			"feature_detection":     "query protocol.capabilities before using optional features",
			"breaking_changes":      "require a new schema_version",
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// schemaProperties 取 $defs[<name>].properties 的键列表。
// Python: list(schema["$defs"][name]["properties"])
func schemaProperties(schema map[string]any, definition string) []any {
	defs, _ := schema["$defs"].(map[string]any)
	if defs == nil {
		return []any{}
	}
	target, _ := defs[definition].(map[string]any)
	if target == nil {
		return []any{}
	}
	props, _ := target["properties"].(map[string]any)
	out := make([]any, 0, len(props))
	for key := range props {
		out = append(out, key)
	}
	return out
}

// schemaQueryOperations 取 $defs[query].properties.operation.enum。
// Python: schema["$defs"]["query"]["properties"]["operation"]["enum"]
func schemaQueryOperations(schema map[string]any) []any {
	defs, _ := schema["$defs"].(map[string]any)
	if defs == nil {
		return []any{}
	}
	query, _ := defs["query"].(map[string]any)
	if query == nil {
		return []any{}
	}
	props, _ := query["properties"].(map[string]any)
	if props == nil {
		return []any{}
	}
	operation, _ := props["operation"].(map[string]any)
	if operation == nil {
		return []any{}
	}
	enum, ok := operation["enum"].([]any)
	if !ok {
		return []any{}
	}
	return enum
}
