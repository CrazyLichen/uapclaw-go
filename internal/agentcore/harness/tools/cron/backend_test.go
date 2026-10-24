package cron

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCronToolContext_ToolScope 测试正常 channel+session 组合
func TestCronToolContext_ToolScope(t *testing.T) {
	ctx := &CronToolContext{
		ChannelID: "wechat",
		SessionID: "sess_123",
	}
	scope := ctx.ToolScope()
	if scope != "wechat:sess_123" {
		t.Errorf("ToolScope() = %q, want %q", scope, "wechat:sess_123")
	}
}

// TestCronToolContext_ToolScope_空值兜底 测试空 channel/session 的兜底逻辑
// Python: (self.channel_id or "unknown").strip() or "unknown"
func TestCronToolContext_ToolScope_空值兜底(t *testing.T) {
	tests := []struct {
		name      string
		ctx       *CronToolContext
		wantScope string
	}{
		{
			name:      "空ChannelID",
			ctx:       &CronToolContext{ChannelID: "", SessionID: "sess_123"},
			wantScope: "unknown:sess_123",
		},
		{
			name:      "空SessionID",
			ctx:       &CronToolContext{ChannelID: "wechat", SessionID: ""},
			wantScope: "wechat:default",
		},
		{
			name:      "全空",
			ctx:       &CronToolContext{ChannelID: "", SessionID: ""},
			wantScope: "unknown:default",
		},
		{
			name:      "空白字符串",
			ctx:       &CronToolContext{ChannelID: "   ", SessionID: "   "},
			wantScope: "unknown:default",
		},
		{
			name:      "前后空格",
			ctx:       &CronToolContext{ChannelID: " wechat ", SessionID: " sess_123 "},
			wantScope: "wechat:sess_123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := tt.ctx.ToolScope()
			if scope != tt.wantScope {
				t.Errorf("ToolScope() = %q, want %q", scope, tt.wantScope)
			}
		})
	}
}

// TestToolScope_无上下文 测试 nil context 时返回 "cron_default"
// Python: _tool_scope(None) → "cron:default" → replace(":","_") → "cron_default"
func TestToolScope_无上下文(t *testing.T) {
	scope := toolScope(nil)
	if scope != "cron_default" {
		t.Errorf("toolScope(nil) = %q, want %q", scope, "cron_default")
	}
}

// TestToolScope_有上下文 测试有 context 时返回替换后的 scope
// Python: _tool_scope(context) → context.tool_scope.replace(":","_")
func TestToolScope_有上下文(t *testing.T) {
	ctx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_123"}
	scope := toolScope(ctx)
	if scope != "wechat_sess_123" {
		t.Errorf("toolScope(ctx) = %q, want %q", scope, "wechat_sess_123")
	}
}

// TestToolScope_覆盖值优先 测试 ToolScopeOverride 非空时优先于默认计算。
// Python: team_manager 传 SimpleNamespace(tool_scope=f"team_member_{agent_id or 'unknown'}")，
// _tool_scope 读 context.tool_scope 属性即覆盖 frozen dataclass 的默认 channel:session 计算。
func TestToolScope_覆盖值优先(t *testing.T) {
	ctx := &CronToolContext{
		ChannelID:         "web",
		SessionID:         "s1",
		ToolScopeOverride: " team_member_member-1 ",
	}
	scope := toolScope(ctx)
	if scope != "team_member_member-1" {
		t.Errorf("toolScope(ctx with override) = %q, want %q", scope, "team_member_member-1")
	}
	// 空白覆盖不生效，回退默认计算
	ctx.ToolScopeOverride = "   "
	if scope := toolScope(ctx); scope != "web_s1" {
		t.Errorf("toolScope(ctx blank override) = %q, want %q", scope, "web_s1")
	}
}
