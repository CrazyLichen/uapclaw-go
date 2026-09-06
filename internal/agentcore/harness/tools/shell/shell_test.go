package shell

import "testing"

// TestNewPermissionConfig 测试创建权限配置
func TestNewPermissionConfig(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeAuto, []string{"rm -rf"}, []string{"ls"})
	if cfg.Mode != PermissionModeAuto {
		t.Errorf("Mode = %v, want %v", cfg.Mode, PermissionModeAuto)
	}
	if len(cfg.DenyPatterns) != 1 {
		t.Errorf("DenyPatterns 长度 = %d, want 1", len(cfg.DenyPatterns))
	}
	if len(cfg.AllowPatterns) != 1 {
		t.Errorf("AllowPatterns 长度 = %d, want 1", len(cfg.AllowPatterns))
	}
}

// TestNewPermissionConfig_空模式 测试空模式配置
func TestNewPermissionConfig_空模式(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeBypass, nil, nil)
	if cfg.Mode != PermissionModeBypass {
		t.Errorf("Mode = %v, want %v", cfg.Mode, PermissionModeBypass)
	}
	if len(cfg.DenyPatterns) != 0 {
		t.Errorf("DenyPatterns 长度应为 0")
	}
	if len(cfg.AllowPatterns) != 0 {
		t.Errorf("AllowPatterns 长度应为 0")
	}
}

// TestCheckPermission_Bypass 测试 BYPASS 模式
func TestCheckPermission_Bypass(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeBypass, nil, nil)
	ok, reason := CheckPermission("rm -rf /", cfg, false)
	if !ok {
		t.Errorf("BYPASS 模式应允许所有命令，reason: %s", reason)
	}
}

// TestCheckPermission_DenyPattern 测试拒绝模式
func TestCheckPermission_DenyPattern(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeAuto, []string{"rm -rf"}, nil)
	ok, _ := CheckPermission("rm -rf /", cfg, false)
	if ok {
		t.Error("匹配 deny pattern 的命令应被拒绝")
	}
}

// TestCheckPermission_AllowPattern 测试允许模式
func TestCheckPermission_AllowPattern(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeAcceptEdits, nil, []string{"ls"})
	ok, _ := CheckPermission("ls -la", cfg, false)
	if !ok {
		t.Error("匹配 allow pattern 的命令应被允许")
	}
}
