package exception

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestResolveCategory_ManualOverrides(t *testing.T) {
	// 测试手动覆盖表中的特例
	category := ResolveCategory(StatusCommonEncryptionError)
	if category != ErrorCategoryFramework {
		t.Errorf("COMMON_ENCRYPTION_ERROR 应映射到 Framework，实际 %v", category)
	}

	category = ResolveCategory(StatusCommonDecryptionError)
	if category != ErrorCategoryFramework {
		t.Errorf("COMMON_DECRYPTION_ERROR 应映射到 Framework，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Invalid(t *testing.T) {
	// INVALID → Validation
	category := ResolveCategory(NewStatusCode("WORKFLOW_INPUT_INVALID", 100000, ""))
	if category != ErrorCategoryValidation {
		t.Errorf("含 INVALID 关键字应映射到 Validation，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Param(t *testing.T) {
	// PARAM → Validation
	category := ResolveCategory(NewStatusCode("AGENT_PROMPT_PARAM_ERROR", 120004, ""))
	if category != ErrorCategoryValidation {
		t.Errorf("含 PARAM 关键字应映射到 Validation，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Init(t *testing.T) {
	// INIT → Framework
	category := ResolveCategory(NewStatusCode("SERVICE_INIT_FAILED", 100000, ""))
	if category != ErrorCategoryFramework {
		t.Errorf("含 INIT 关键字应映射到 Framework，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Execution(t *testing.T) {
	// EXECUTION → Execution
	category := ResolveCategory(NewStatusCode("WORKFLOW_EXECUTION_ERROR", 100102, ""))
	if category != ErrorCategoryExecution {
		t.Errorf("含 EXECUTION 关键字应映射到 Execution，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Config(t *testing.T) {
	// CONFIG → Validation
	category := ResolveCategory(NewStatusCode("MODEL_CONFIG_ERROR", 181003, ""))
	if category != ErrorCategoryValidation {
		t.Errorf("含 CONFIG 关键字应映射到 Validation，实际 %v", category)
	}
}

func TestResolveCategory_KeywordRules_Schema(t *testing.T) {
	// SCHEMA → Validation
	category := ResolveCategory(NewStatusCode("SCHEMA_VALIDATE_INVALID", 189001, ""))
	if category != ErrorCategoryValidation {
		t.Errorf("含 SCHEMA 关键字应映射到 Validation，实际 %v", category)
	}
}

func TestResolveCategory_RangeRules(t *testing.T) {
	// 100000-119999 → Execution (Workflow 域)
	category := ResolveCategory(NewStatusCode("UNKNOWN_WORKFLOW", 100500, ""))
	if category != ErrorCategoryExecution {
		t.Errorf("100500 应映射到 Execution（Workflow 域），实际 %v", category)
	}

	// 180000-189999 → Framework (Foundation 域)
	category = ResolveCategory(NewStatusCode("UNKNOWN_FOUNDATION", 185000, ""))
	if category != ErrorCategoryFramework {
		t.Errorf("185000 应映射到 Framework（Foundation 域），实际 %v", category)
	}
}

func TestResolveCategory_Fallback(t *testing.T) {
	// 不匹配任何规则，兜底 Execution
	category := ResolveCategory(NewStatusCode("COMPLETELY_UNKNOWN", 999999, ""))
	if category != ErrorCategoryExecution {
		t.Errorf("未知 code 应兜底到 Execution，实际 %v", category)
	}
}

func TestResolveCategory_Success(t *testing.T) {
	// SUCCESS: code=0 不在任何区间，name 不匹配关键字和手动覆盖，兜底 Execution
	category := ResolveCategory(StatusSuccess)
	if category != ErrorCategoryExecution {
		t.Errorf("SUCCESS 应兜底到 Execution，实际 %v", category)
	}
}

func TestResolveCategory_Error(t *testing.T) {
	// ERROR: 命中关键字 "ERROR" → Execution
	category := ResolveCategory(StatusError)
	if category != ErrorCategoryExecution {
		t.Errorf("ERROR 命中 EXECUTION 关键字应映射到 Execution，实际 %v", category)
	}
}

func TestResolveCategory_Priority(t *testing.T) {
	// 手动覆盖优先级高于关键字和区间
	// COMMON_ENCRYPTION_ERROR 在手动覆盖表中指定为 Framework
	// 虽然名字包含 "ERROR" 但手动覆盖优先
	category := ResolveCategory(StatusCommonEncryptionError)
	if category != ErrorCategoryFramework {
		t.Errorf("手动覆盖应优先，COMMON_ENCRYPTION_ERROR 应映射到 Framework，实际 %v", category)
	}
}

func TestResolveCategory_StoreGraphOverrides(t *testing.T) {
	// STORE_GRAPH_BACKEND_ALREADY_EXISTS → Validation（手动覆盖）
	category := ResolveCategory(StatusStoreGraphBackendAlreadyExists)
	if category != ErrorCategoryValidation {
		t.Errorf("STORE_GRAPH_BACKEND_ALREADY_EXISTS 应映射到 Validation，实际 %v", category)
	}

	// STORE_GRAPH_BACKEND_NOT_FOUND → Validation（手动覆盖）
	category = ResolveCategory(StatusStoreGraphBackendNotFound)
	if category != ErrorCategoryValidation {
		t.Errorf("STORE_GRAPH_BACKEND_NOT_FOUND 应映射到 Validation，实际 %v", category)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestMatchKeyword(t *testing.T) {
	tests := []struct {
		name     string
		expected ErrorCategory
	}{
		{"WORKFLOW_INPUT_INVALID", ErrorCategoryValidation},
		{"MODEL_INIT_FAILED", ErrorCategoryFramework},
		{"WORKFLOW_EXECUTION_ERROR", ErrorCategoryExecution},
		{"COMPLETELY_UNKNOWN", ErrorCategory(-1)},
	}
	for _, tt := range tests {
		if got := matchKeyword(tt.name); got != tt.expected {
			t.Errorf("matchKeyword(%q) = %v，期望 %v", tt.name, got, tt.expected)
		}
	}
}

func TestMatchRange(t *testing.T) {
	tests := []struct {
		code     int
		expected ErrorCategory
	}{
		{100000, ErrorCategoryExecution},
		{180000, ErrorCategoryFramework},
		{999999, ErrorCategory(-1)},
		{0, ErrorCategory(-1)},
	}
	for _, tt := range tests {
		if got := matchRange(tt.code); got != tt.expected {
			t.Errorf("matchRange(%d) = %v，期望 %v", tt.code, got, tt.expected)
		}
	}
}
