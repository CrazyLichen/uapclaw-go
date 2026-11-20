package schema

import (
	"encoding/json"
	"testing"
)

// 本文件承载 PermissionSection / ApprovalOverrideEntry 的 JSON 往返测试。
//
// 类型已于 2026-11-27（15.5 依赖重构）从 security 包下沉到 schema 包，
// 测试随之迁移，保持「一包一测试」对应关系。

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
				"id":         "deny_read_env_files",
				"tools":      []string{"read_file"},
				"match_type": "path",
				"pattern":    "re:\\.env(\\.local)?$",
				"action":     "deny",
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
