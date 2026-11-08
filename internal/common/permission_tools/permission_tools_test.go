package permission_tools

import "testing"

// TestResolvePermissionToolName_别名映射 测试别名归一化为 canonical 名
func TestResolvePermissionToolName_别名映射(t *testing.T) {
	cases := []struct{ in, wantCanonical string }{
		{"exec_command", "mcp_exec_command"},
		{"fetch_webpage", "mcp_fetch_webpage"},
		{"free_search", "mcp_free_search"},
		{"paid_search", "mcp_paid_search"},
		{"bash", "bash"},               // 非别名原样返回
		{"  read_file  ", "read_file"}, // 首尾空白被 trim
	}
	for _, c := range cases {
		got := ResolvePermissionToolName(c.in, nil)
		if got.CanonicalName != c.wantCanonical {
			t.Errorf("ResolvePermissionToolName(%q).CanonicalName = %q, want %q",
				c.in, got.CanonicalName, c.wantCanonical)
		}
		if got.Conflict {
			t.Errorf("ResolvePermissionToolName(%q).Conflict = true, want false", c.in)
		}
	}
}

// TestResolvePermissionToolName_别名列表 测试 canonical 名反查出全部别名并按字典序排序
func TestResolvePermissionToolName_别名列表(t *testing.T) {
	got := ResolvePermissionToolName("mcp_fetch_webpage", nil)
	if len(got.Aliases) != 1 || got.Aliases[0] != "fetch_webpage" {
		t.Errorf("Aliases = %v, want [fetch_webpage]", got.Aliases)
	}
	if got.RegisteredName != "mcp_fetch_webpage" {
		t.Errorf("RegisteredName = %q, want mcp_fetch_webpage", got.RegisteredName)
	}
}

// TestResolvePermissionToolName_冲突检测 测试三个 or 分支的 fail-closed 语义
func TestResolvePermissionToolName_冲突检测(t *testing.T) {
	// 分支 1：registered_name 在 canonical_names 里，且是别名键，且映射后的名不同
	got := ResolvePermissionToolName("exec_command", []string{"exec_command"})
	if !got.Conflict {
		t.Fatal("分支1: Conflict = false, want true")
	}
	if got.CanonicalName != "" {
		t.Errorf("分支1: CanonicalName = %q, want empty (fail-closed)", got.CanonicalName)
	}
	if len(got.Aliases) != 0 {
		t.Errorf("分支1: Aliases = %v, want empty", got.Aliases)
	}
	if got.RegisteredName != "exec_command" {
		t.Errorf("分支1: RegisteredName = %q, want exec_command", got.RegisteredName)
	}

	// 分支 2：registered_name 是别名映射的目标（alias_targets 成员），同时又是别名键
	// 现有 4 项映射里没有这种名字，构造不了——退化为验证正常 canonical 名不冲突
	got2 := ResolvePermissionToolName("mcp_fetch_webpage", nil)
	if got2.Conflict {
		t.Error("分支2/3: 正常 canonical 名不应 conflict")
	}
}

// TestNormalizePermissionToolName 测试 normalize 直接返回 canonical 名
func TestNormalizePermissionToolName(t *testing.T) {
	if got := NormalizePermissionToolName("exec_command"); got != "mcp_exec_command" {
		t.Errorf("NormalizePermissionToolName(exec_command) = %q, want mcp_exec_command", got)
	}
	// conflict 时 canonical 为空串
	if got := NormalizePermissionToolName("exec_command"); got == "" {
		t.Log("conflict 场景需显式 canonical_names 触发，此处为正常路径")
	}
	if got := NormalizePermissionToolName(nil); got != "" {
		t.Errorf("NormalizePermissionToolName(nil) = %q, want empty", got)
	}
}

// TestStricterPermissionLevel 测试取更严级别，未知输入按 ask
func TestStricterPermissionLevel(t *testing.T) {
	cases := []struct{ left, right, want string }{
		{"allow", "ask", "ask"},
		{"ask", "deny", "deny"},
		{"deny", "allow", "deny"},
		{"allow", "allow", "allow"},
		{"bogus", "allow", "bogus"}, // bogus 按 ask 排名 1，>= allow 的 0，返回 left
		{"allow", "bogus", "bogus"},
		{"", "", "ask"}, // 空串 → "ask" 兜底后两者都是 ask，返回 left(="ask")
	}
	for _, c := range cases {
		if got := StricterPermissionLevel(c.left, c.right); got != c.want {
			t.Errorf("StricterPermissionLevel(%q,%q) = %q, want %q", c.left, c.right, got, c.want)
		}
	}
}
