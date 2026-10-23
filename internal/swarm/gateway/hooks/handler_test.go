package hooks

import (
	"context"
	"testing"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// TestOnSessionStart_activeSessions去重（重写：二次调用不触发第二次 setdefault）
func TestOnSessionStart_activeSessions去重(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventSessionStart: {{
				Matcher: "*",
				Hooks:   hooks,
			}},
		},
	})
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Fatalf("首次应补 timeout=10，实际 %v", hooks[0]["timeout"])
	}
	// 复位 timeout 验证第二次不再进入 setdefault（去重提前 return）
	delete(hooks[0], "timeout")
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("同 session 二次 SessionStart 应去重（不应再次 setdefault）")
	}
}

// TestOnSessionEnd_移除activeSessions后可再次start
func TestOnSessionEnd_移除activeSessions后可再次start(t *testing.T) {
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{},
	})
	h.mu.Lock()
	h.activeSessions["sess-1"] = true
	h.mu.Unlock()

	h.OnSessionEnd(context.Background(), "sess-1", "clear")

	h.mu.Lock()
	_, still := h.activeSessions["sess-1"]
	h.mu.Unlock()
	if still {
		t.Error("SessionEnd 应从 activeSessions 移除")
	}
}

// TestOnSessionEnd_matchQuery差异 reason 匹配 matcher 才执行
func TestOnSessionEnd_matchQuery差异(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventSessionEnd: {{
				Matcher: "clear",
				Hooks:   hooks,
			}},
		},
	})
	h.OnSessionEnd(context.Background(), "sess-1", "timeout")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("reason=timeout 不匹配 matcher=clear，不应触发 setdefault")
	}
	h.OnSessionEnd(context.Background(), "sess-1", "clear")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("匹配时应补 timeout=10，实际 %v", hooks[0]["timeout"])
	}
}

// TestOnSessionStart_cwd字段 hook input 含 cwd（os.Getwd 成功路径）
func TestOnSessionStart_cwd字段(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventSessionStart: {{
				Matcher: "*",
				Hooks:   hooks,
			}},
		},
	})
	h.OnSessionStart(context.Background(), "sess-cwd", "startup")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("首次应补 timeout=10，实际 %v", hooks[0]["timeout"])
	}
}

// TestOnUserPromptSubmit_timeoutSetdefault 无 timeout 的 hook 就地补默认值 10，已有则不覆盖
func TestOnUserPromptSubmit_timeoutSetdefault(t *testing.T) {
	hooks := []map[string]any{
		{"type": "command", "command": "echo ok"},               // 无 timeout → 补 10
		{"type": "command", "command": "echo ok", "timeout": 5}, // 有 → 不覆盖
	}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventUserPromptSubmit: {{Matcher: "*", Hooks: hooks}},
		},
	})
	h.OnUserPromptSubmit(context.Background(), "sess-1", "hello")

	// setdefault 就地修改共享 map（Python 语义）
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("无 timeout 应补 10，实际 %v", hooks[0]["timeout"])
	}
	if v, _ := hooks[1]["timeout"].(int); v != 5 {
		t.Errorf("已有 timeout=5 不应覆盖，实际 %v", hooks[1]["timeout"])
	}
}

// TestOnNotification_matchQuery差异（重写：matcher 不匹配 → 配置未被 setdefault）
func TestOnNotification_matchQuery差异(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventNotification: {{
				Matcher: "permission",
				Hooks:   hooks,
			}},
		},
	})
	h.OnNotification(context.Background(), "cron", "job done", "sess-1")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("notification_type=cron 不匹配 matcher=permission，不应触发 setdefault")
	}
	h.OnNotification(context.Background(), "permission", "need approve", "sess-1")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("匹配时应补 timeout=10，实际 %v", hooks[0]["timeout"])
	}
}

// TestDisableAllHooks_短路
func TestDisableAllHooks_短路(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		DisableAllHooks: true,
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventUserPromptSubmit: {{
				Matcher: "*",
				Hooks:   hooks,
			}},
		},
	})
	h.OnUserPromptSubmit(context.Background(), "sess-1", "hello")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("DisableAllHooks 应短路（不触发 setdefault）")
	}
}
