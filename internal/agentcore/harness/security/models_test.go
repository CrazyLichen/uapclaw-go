package security

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPermissionLevel_默认值为Allow(t *testing.T) {
	var l PermissionLevel
	if l != PermissionLevelAllow {
		t.Errorf("PermissionLevel 零值应为 PermissionLevelAllow(0)，实际为 %d", l)
	}
}

func TestPermissionLevel_String(t *testing.T) {
	tests := []struct {
		level PermissionLevel
		want  string
	}{
		{PermissionLevelAllow, "allow"},
		{PermissionLevelAsk, "ask"},
		{PermissionLevelDeny, "deny"},
		{PermissionLevel(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("PermissionLevel(%d).String() = %q，期望 %q", tt.level, got, tt.want)
		}
	}
}

func TestPermissionLevel_MarshalJSON(t *testing.T) {
	tests := []struct {
		level PermissionLevel
		want  string
	}{
		{PermissionLevelAllow, `"allow"`},
		{PermissionLevelAsk, `"ask"`},
		{PermissionLevelDeny, `"deny"`},
	}
	for _, tt := range tests {
		data, err := json.Marshal(tt.level)
		if err != nil {
			t.Errorf("MarshalJSON(%d) 出错: %v", tt.level, err)
			continue
		}
		if string(data) != tt.want {
			t.Errorf("MarshalJSON(%d) = %s，期望 %s", tt.level, data, tt.want)
		}
	}
}

func TestPermissionLevel_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  PermissionLevel
	}{
		{`"allow"`, PermissionLevelAllow},
		{`"ask"`, PermissionLevelAsk},
		{`"deny"`, PermissionLevelDeny},
		{`"ALLOW"`, PermissionLevelAllow},
		{`"Ask"`, PermissionLevelAsk},
		{`"DENY"`, PermissionLevelDeny},
	}
	for _, tt := range tests {
		var l PermissionLevel
		if err := json.Unmarshal([]byte(tt.input), &l); err != nil {
			t.Errorf("UnmarshalJSON(%s) 出错: %v", tt.input, err)
			continue
		}
		if l != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d，期望 %d", tt.input, l, tt.want)
		}
	}
}

func TestPermissionLevel_UnmarshalJSON_无效值(t *testing.T) {
	var l PermissionLevel
	if err := json.Unmarshal([]byte(`"invalid"`), &l); err == nil {
		t.Error("UnmarshalJSON 无效值应返回错误")
	}
}

func TestPermissionLevel_UnmarshalJSON_非字符串(t *testing.T) {
	var l PermissionLevel
	if err := json.Unmarshal([]byte(`123`), &l); err == nil {
		t.Error("UnmarshalJSON 非字符串输入应返回错误")
	}
}

func TestPermissionLevel_JSON往返(t *testing.T) {
	levels := []PermissionLevel{PermissionLevelAllow, PermissionLevelAsk, PermissionLevelDeny}
	for _, original := range levels {
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("Marshal 出错: %v", err)
			continue
		}
		var decoded PermissionLevel
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("Unmarshal 出错: %v", err)
			continue
		}
		if decoded != original {
			t.Errorf("JSON 往返: 原值 %d，解码后 %d", original, decoded)
		}
	}
}

func TestParsePermissionLevel(t *testing.T) {
	tests := []struct {
		input string
		want  PermissionLevel
		err   bool
	}{
		{"allow", PermissionLevelAllow, false},
		{"ask", PermissionLevelAsk, false},
		{"deny", PermissionLevelDeny, false},
		{"ALLOW", PermissionLevelAllow, false},
		{"Ask", PermissionLevelAsk, false},
		{"DENY", PermissionLevelDeny, false},
		{"invalid", PermissionLevelAllow, true},
		{"", PermissionLevelAllow, true},
	}
	for _, tt := range tests {
		got, err := ParsePermissionLevel(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParsePermissionLevel(%q) 应返回错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParsePermissionLevel(%q) 出错: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParsePermissionLevel(%q) = %d，期望 %d", tt.input, got, tt.want)
			}
		}
	}
}

func TestPermissionResult_IsAllowed(t *testing.T) {
	tests := []struct {
		result PermissionResult
		want   bool
	}{
		{PermissionResult{Permission: PermissionLevelAllow}, true},
		{PermissionResult{Permission: PermissionLevelAsk}, false},
		{PermissionResult{Permission: PermissionLevelDeny}, false},
	}
	for _, tt := range tests {
		if got := tt.result.IsAllowed(); got != tt.want {
			t.Errorf("IsAllowed() = %v，期望 %v (permission=%d)", got, tt.want, tt.result.Permission)
		}
	}
}

func TestPermissionResult_IsDenied(t *testing.T) {
	tests := []struct {
		result PermissionResult
		want   bool
	}{
		{PermissionResult{Permission: PermissionLevelAllow}, false},
		{PermissionResult{Permission: PermissionLevelAsk}, false},
		{PermissionResult{Permission: PermissionLevelDeny}, true},
	}
	for _, tt := range tests {
		if got := tt.result.IsDenied(); got != tt.want {
			t.Errorf("IsDenied() = %v，期望 %v (permission=%d)", got, tt.want, tt.result.Permission)
		}
	}
}

func TestPermissionResult_NeedsApproval(t *testing.T) {
	tests := []struct {
		result PermissionResult
		want   bool
	}{
		{PermissionResult{Permission: PermissionLevelAllow}, false},
		{PermissionResult{Permission: PermissionLevelAsk}, true},
		{PermissionResult{Permission: PermissionLevelDeny}, false},
	}
	for _, tt := range tests {
		if got := tt.result.NeedsApproval(); got != tt.want {
			t.Errorf("NeedsApproval() = %v，期望 %v (permission=%d)", got, tt.want, tt.result.Permission)
		}
	}
}

func TestPermissionResult_序列化(t *testing.T) {
	r := PermissionResult{
		Permission:   PermissionLevelAsk,
		MatchedRule:  "deny_read_env",
		Reason:       "安全策略禁止读取 .env 文件",
		ExternalPaths: []string{"/home/user/.env"},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal PermissionResult 出错: %v", err)
	}
	var decoded PermissionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal PermissionResult 出错: %v", err)
	}
	if decoded.Permission != r.Permission {
		t.Errorf("Permission = %d，期望 %d", decoded.Permission, r.Permission)
	}
	if decoded.MatchedRule != r.MatchedRule {
		t.Errorf("MatchedRule = %q，期望 %q", decoded.MatchedRule, r.MatchedRule)
	}
	if decoded.Reason != r.Reason {
		t.Errorf("Reason = %q，期望 %q", decoded.Reason, r.Reason)
	}
	if len(decoded.ExternalPaths) != 1 || decoded.ExternalPaths[0] != r.ExternalPaths[0] {
		t.Errorf("ExternalPaths = %v，期望 %v", decoded.ExternalPaths, r.ExternalPaths)
	}
}

func TestPermissionResult_省略字段(t *testing.T) {
	r := PermissionResult{Permission: PermissionLevelAllow}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	// 允许的权限不应包含 omitempty 字段
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map 出错: %v", err)
	}
	if _, ok := m["matched_rule"]; ok {
		t.Error("matched_rule 应被省略")
	}
	if _, ok := m["reason"]; ok {
		t.Error("reason 应被省略")
	}
	if _, ok := m["external_paths"]; ok {
		t.Error("external_paths 应被省略")
	}
}

func TestPermissionConfirmResponse_序列化(t *testing.T) {
	resp := PermissionConfirmResponse{
		Approved:    true,
		Feedback:    "用户确认执行",
		AutoConfirm: true,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	var decoded PermissionConfirmResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 出错: %v", err)
	}
	if decoded.Approved != resp.Approved {
		t.Errorf("Approved = %v，期望 %v", decoded.Approved, resp.Approved)
	}
	if decoded.AutoConfirm != resp.AutoConfirm {
		t.Errorf("AutoConfirm = %v，期望 %v", decoded.AutoConfirm, resp.AutoConfirm)
	}
}

func TestApprovalOverrideEntry_序列化(t *testing.T) {
	entry := ApprovalOverrideEntry{
		ID:        "allow_git_status",
		Tools:     []string{"bash", "mcp_exec_command"},
		MatchType: "command",
		Pattern:   "re:^git\\s+status\\s*$",
		Action:    "allow",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	var decoded ApprovalOverrideEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 出错: %v", err)
	}
	if decoded.ID != entry.ID {
		t.Errorf("ID = %q，期望 %q", decoded.ID, entry.ID)
	}
	if len(decoded.Tools) != 2 {
		t.Errorf("Tools 长度 = %d，期望 2", len(decoded.Tools))
	}
	if decoded.MatchType != entry.MatchType {
		t.Errorf("MatchType = %q，期望 %q", decoded.MatchType, entry.MatchType)
	}
}

func TestPermissionsSection_默认值(t *testing.T) {
	var ps PermissionsSection
	if ps.Enabled {
		t.Error("PermissionsSection 零值 Enabled 应为 false")
	}
	if ps.Schema != "" {
		t.Error("PermissionsSection 零值 Schema 应为空字符串")
	}
	if ps.Defaults != nil {
		t.Error("PermissionsSection 零值 Defaults 应为 nil")
	}
	if ps.Tools != nil {
		t.Error("PermissionsSection 零值 Tools 应为 nil")
	}
	if ps.Rules != nil {
		t.Error("PermissionsSection 零值 Rules 应为 nil")
	}
	if ps.ApprovalOverrides != nil {
		t.Error("PermissionsSection 零值 ApprovalOverrides 应为 nil")
	}
	if ps.ExternalDirectory != nil {
		t.Error("PermissionsSection 零值 ExternalDirectory 应为 nil")
	}
}

func TestPermissionsSection_序列化(t *testing.T) {
	ps := PermissionsSection{
		Enabled:  true,
		Schema:   "tiered_policy",
		Defaults: map[string]any{"*": "allow"},
		Tools:    map[string]any{"read_file": "ask", "write_file": "deny"},
		Rules: []map[string]any{
			{
				"id":        "deny_read_env_files",
				"tools":     []string{"read_file"},
				"match_type": "path",
				"pattern":   "re:\\.env(\\.local)?$",
				"action":    "deny",
			},
		},
		ApprovalOverrides: []ApprovalOverrideEntry{
			{
				ID:        "allow_git_status",
				Tools:     []string{"bash"},
				MatchType: "command",
				Pattern:   "re:^git\\s+status\\s*$",
				Action:    "allow",
			},
		},
		ExternalDirectory: map[string]string{"*": "ask"},
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	var decoded PermissionsSection
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 出错: %v", err)
	}
	if decoded.Enabled != ps.Enabled {
		t.Errorf("Enabled = %v，期望 %v", decoded.Enabled, ps.Enabled)
	}
	if decoded.Schema != ps.Schema {
		t.Errorf("Schema = %q，期望 %q", decoded.Schema, ps.Schema)
	}
	if len(decoded.ApprovalOverrides) != 1 {
		t.Errorf("ApprovalOverrides 长度 = %d，期望 1", len(decoded.ApprovalOverrides))
	}
	if decoded.ApprovalOverrides[0].ID != "allow_git_status" {
		t.Errorf("ApprovalOverrides[0].ID = %q，期望 %q", decoded.ApprovalOverrides[0].ID, "allow_git_status")
	}
	if decoded.ExternalDirectory["*"] != "ask" {
		t.Errorf("ExternalDirectory[\"*\"] = %q，期望 %q", decoded.ExternalDirectory["*"], "ask")
	}
}

func TestPermissionsSection_省略可选字段(t *testing.T) {
	ps := PermissionsSection{Enabled: true}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map 出错: %v", err)
	}
	if _, ok := m["schema"]; ok {
		t.Error("schema 应被省略")
	}
	if _, ok := m["defaults"]; ok {
		t.Error("defaults 应被省略")
	}
	if _, ok := m["tools"]; ok {
		t.Error("tools 应被省略")
	}
	if _, ok := m["rules"]; ok {
		t.Error("rules 应被省略")
	}
	if _, ok := m["approval_overrides"]; ok {
		t.Error("approval_overrides 应被省略")
	}
	if _, ok := m["external_directory"]; ok {
		t.Error("external_directory 应被省略")
	}
}
