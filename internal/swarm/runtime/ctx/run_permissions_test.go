package ctx

import (
	"context"
	"reflect"
	"testing"
)

// TestOverlayRunPermissions_无覆盖返回原config 验证空 levels 时原样返回（不深拷贝）
func TestOverlayRunPermissions_无覆盖返回原config(t *testing.T) {
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}
	got, err := OverlayRunPermissions(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}

	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("got %v, want %v", got, cfg)
	}
	// 无覆盖时应返回同一个对象（Python 直接 return config，不 deepcopy）
	got["new_key"] = 1
	if _, exists := cfg["new_key"]; !exists {
		t.Error("should return the original map when no levels")
	}
}

// TestOverlayRunPermissions_空levels也返回原config 验证 `if not levels`
func TestOverlayRunPermissions_空levels也返回原config(t *testing.T) {
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}
	ctx := WithRunPermissions(context.Background(), map[string]string{})

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("got %v, want %v", got, cfg)
	}
}

// TestOverlayRunPermissions_覆盖为allow 验证请求级放行
func TestOverlayRunPermissions_覆盖为allow(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}

	tools := got["tools"].(map[string]any)
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", tools["bash"])
	}
	allowTools := got["allow_tools"].([]string)
	if len(allowTools) != 1 || allowTools[0] != "bash" {
		t.Errorf("allow_tools = %v", allowTools)
	}
}

// TestOverlayRunPermissions_deny不可被放宽 验证整工具 deny 优先（两条路径）
func TestOverlayRunPermissions_deny不可被放宽(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
	}{
		{"deny_tools 桶", map[string]any{
			"tools":      map[string]any{"rm": "deny"},
			"deny_tools": []string{"rm"},
		}},
		{"tools 字典为 deny", map[string]any{
			"tools": map[string]any{"rm": "deny"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := WithRunPermissions(context.Background(), map[string]string{"rm": "allow"})
			got, err := OverlayRunPermissions(ctx, c.cfg)
			if err != nil {
				t.Fatalf("OverlayRunPermissions failed: %v", err)
			}
			tools := got["tools"].(map[string]any)
			if tools["rm"] != "deny" {
				t.Errorf("rm = %v, want deny（deny 不可放宽）", tools["rm"])
			}
		})
	}
}

// TestOverlayRunPermissions_切换level清理旧桶 验证 allow→deny 时从 allow_tools 移除
func TestOverlayRunPermissions_切换level清理旧桶(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"edit": "deny"})
	cfg := map[string]any{
		"tools":       map[string]any{"edit": "allow"},
		"allow_tools": []string{"edit"},
	}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}

	if allowTools, _ := got["allow_tools"].([]string); len(allowTools) != 0 {
		t.Errorf("allow_tools should be empty, got %v", allowTools)
	}
	denyTools := got["deny_tools"].([]string)
	if len(denyTools) != 1 || denyTools[0] != "edit" {
		t.Errorf("deny_tools = %v, want [edit]", denyTools)
	}
}

// TestOverlayRunPermissions_三个桶输出排序 验证 Python sorted()
func TestOverlayRunPermissions_三个桶输出排序(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{
		"z_tool": "deny", "a_tool": "ask", "m_tool": "allow",
	})
	cfg := map[string]any{"tools": map[string]any{}}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}

	if got["deny_tools"].([]string)[0] != "z_tool" {
		t.Errorf("deny_tools = %v", got["deny_tools"])
	}
	if got["ask_tools"].([]string)[0] != "a_tool" {
		t.Errorf("ask_tools = %v", got["ask_tools"])
	}
	if got["allow_tools"].([]string)[0] != "m_tool" {
		t.Errorf("allow_tools = %v", got["allow_tools"])
	}
}

// TestOverlayRunPermissions_不修改原config 验证 deepcopy 隔离
func TestOverlayRunPermissions_不修改原config(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "deny"})
	cfg := map[string]any{
		"tools":       map[string]any{"bash": "ask"},
		"net_guard":   map[string]any{"enabled": true},
		"shell_guard": map[string]any{"block_on_deny": true},
	}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}

	tools := cfg["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("original config mutated: %v", tools["bash"])
	}
	// 所有已装安全护栏必须原样保留
	if !reflect.DeepEqual(got["net_guard"], cfg["net_guard"]) {
		t.Error("net_guard should be preserved")
	}
	if !reflect.DeepEqual(got["shell_guard"], cfg["shell_guard"]) {
		t.Error("shell_guard should be preserved")
	}
}

// TestOverlayRunPermissions_tools非dict时重建 验证 `dict(tools) if isinstance else {}`
func TestOverlayRunPermissions_tools非dict时重建(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	cfg := map[string]any{"tools": "invalid"}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}
	tools, ok := got["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools should be rebuilt as map, got %T", got["tools"])
	}
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v", tools["bash"])
	}
}

// ─── CURRENT_RUN_CONTEXT 兜底路径 ───

// TestOverlayRunPermissions_兜底读RunContext 验证 RUN_PERMISSIONS 未绑定时
// 从 CURRENT_RUN_CONTEXT.tool_permissions 读取并归一。
func TestOverlayRunPermissions_兜底读RunContext(t *testing.T) {
	rc := &RunContext{
		ToolPermissions: []ToolPermissionLevel{
			{Name: "exec_command", Level: "allow"}, // 别名 → mcp_exec_command
		},
	}
	ctx := WithRunContext(context.Background(), rc)
	cfg := map[string]any{"tools": map[string]any{}}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}
	tools := got["tools"].(map[string]any)
	if tools["mcp_exec_command"] != "allow" {
		t.Errorf("normalized mcp_exec_command = %v, want allow", tools["mcp_exec_command"])
	}
}

// TestOverlayRunPermissions_RunContext无toolPermissions 验证 None 时不生效
func TestOverlayRunPermissions_RunContext无toolPermissions(t *testing.T) {
	ctx := WithRunContext(context.Background(), &RunContext{RequestID: "x"})
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("got %v, want original", got)
	}
}

// TestOverlayRunPermissions_主通道优先于兜底 验证 RUN_PERMISSIONS 有值时不再读 RunContext
func TestOverlayRunPermissions_主通道优先于兜底(t *testing.T) {
	rc := &RunContext{
		ToolPermissions: []ToolPermissionLevel{{Name: "bash", Level: "deny"}},
	}
	ctx := WithRunContext(context.Background(), rc)
	ctx = WithRunPermissions(ctx, map[string]string{"bash": "allow"})
	cfg := map[string]any{"tools": map[string]any{}}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("OverlayRunPermissions failed: %v", err)
	}
	if got["tools"].(map[string]any)["bash"] != "allow" {
		t.Error("RUN_PERMISSIONS should take precedence over RunContext fallback")
	}
}

// TestOverlayRunPermissions_兜底歧义名字报错 验证归一后撞车抛 ValueError
func TestOverlayRunPermissions_兜底歧义名字报错(t *testing.T) {
	rc := &RunContext{
		ToolPermissions: []ToolPermissionLevel{
			{Name: "exec_command", Level: "allow"},    // 别名 → mcp_exec_command
			{Name: "mcp_exec_command", Level: "deny"}, // 本名 → mcp_exec_command（撞车）
		},
	}
	ctx := WithRunContext(context.Background(), rc)
	cfg := map[string]any{"tools": map[string]any{}}

	_, err := OverlayRunPermissions(ctx, cfg)
	if err == nil {
		t.Error("ambiguous names should raise ValueError (Go: error)")
	}
}

// TestOverlayRunPermissions_兜底无效名字报错 验证归一为空串也报错
func TestOverlayRunPermissions_兜底无效名字报错(t *testing.T) {
	rc := &RunContext{
		ToolPermissions: []ToolPermissionLevel{{Name: "", Level: "allow"}},
	}
	ctx := WithRunContext(context.Background(), rc)

	_, err := OverlayRunPermissions(ctx, map[string]any{"tools": map[string]any{}})
	if err == nil {
		t.Error("empty canonical name should raise ValueError (Go: error)")
	}
}

// TestWithRunPermissions_nil清空绑定 验证显式 nil 分支
func TestWithRunPermissions_nil清空绑定(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	ctx = WithRunPermissions(ctx, nil)
	if got := RunPermissionsFromCtx(ctx); got != nil {
		t.Errorf("explicit nil should clear binding, got %v", got)
	}
}

// TestWithRunPermissions_拷贝隔离 验证绑定后修改原 map 不影响 ctx
func TestWithRunPermissions_拷贝隔离(t *testing.T) {
	original := map[string]string{"bash": "allow"}
	ctx := WithRunPermissions(context.Background(), original)
	original["bash"] = "deny"

	got := RunPermissionsFromCtx(ctx)
	if got["bash"] != "allow" {
		t.Errorf("bound map should be a copy, got %q", got["bash"])
	}
}

// TestOverlayRunPermissions_deepCopy嵌套 验证 []any / []string 深拷贝
func TestOverlayRunPermissions_deepCopy嵌套(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	cfg := map[string]any{
		"tools":       map[string]any{"bash": "ask"},
		"net_guard":   map[string]any{"urls": []any{"a.com"}},
		"allow_tools": []string{"read"},
		"deny_tools":  []any{"rm"},
	}

	got, err := OverlayRunPermissions(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 嵌套 []any 独立
	urls := got["net_guard"].(map[string]any)["urls"].([]any)
	urls[0] = "mutated"
	if cfg["net_guard"].(map[string]any)["urls"].([]any)[0] != "a.com" {
		t.Error("nested []any should be deep-copied")
	}
	// []string 独立
	allowTools := got["allow_tools"].([]string)
	_ = allowTools
	if cfg["allow_tools"].([]string)[0] != "read" {
		t.Error("[]string should be deep-copied")
	}
	// []any 桶被读取
	if got["deny_tools"].([]string)[0] != "rm" {
		t.Errorf("deny_tools from []any = %v", got["deny_tools"])
	}
}

// TestOverlayRunPermissions_nilConfig 验证 config 为 nil 时不 panic
func TestOverlayRunPermissions_nilConfig(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	got, err := OverlayRunPermissions(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["tools"].(map[string]any)["bash"] != "allow" {
		t.Errorf("got %v", got)
	}
}
