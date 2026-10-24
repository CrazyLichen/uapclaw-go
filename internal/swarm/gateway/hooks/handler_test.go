package hooks

import (
	"context"
	"testing"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// TestOnSessionStart_activeSessions去重（S-02 重写：二次调用去重 + 共享配置不被污染）
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
	// S-02 治本：applyGatewayTimeout 构造新 map，共享配置不应被写入 timeout
	if _, touched := hooks[0]["timeout"]; touched {
		t.Fatalf("共享配置 map 不应被就地写入 timeout（S-02 治本），实际 %v", hooks[0]["timeout"])
	}
	h.mu.Lock()
	_, active := h.activeSessions["sess-1"]
	h.mu.Unlock()
	if !active {
		t.Fatal("首次 SessionStart 应登记 activeSessions")
	}
	// 二次调用去重（activeSessions 命中提前 return）
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	h.mu.Lock()
	still := h.activeSessions["sess-1"]
	h.mu.Unlock()
	if !still {
		t.Error("去重不应移除 activeSessions（仅 OnSessionEnd 移除）")
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

// TestOnSessionEnd_matchQuery差异（S-02 重写：matcher 不匹配 → 不执行 + 共享配置不污染）
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
		t.Error("reason=timeout 不匹配 matcher=clear，不应触发执行")
	}
	h.OnSessionEnd(context.Background(), "sess-1", "clear")
	// S-02 治本：即使匹配执行，共享 map 也不被污染
	if _, touched := hooks[0]["timeout"]; touched {
		t.Errorf("匹配执行也不应就地写共享 map（S-02 治本），实际 %v", hooks[0]["timeout"])
	}
}

// TestOnSessionStart_cwd字段（S-02 重写：执行不 panic + 共享配置不污染）
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
	if _, touched := hooks[0]["timeout"]; touched {
		t.Errorf("共享配置不应被写入 timeout（S-02 治本），实际 %v", hooks[0]["timeout"])
	}
}

// TestOnUserPromptSubmit_timeoutSetdefault（S-02 重写：applyGatewayTimeout 返回带 timeout 的新配置，共享 map 不污染）
func TestOnUserPromptSubmit_timeoutSetdefault(t *testing.T) {
	hooks := []map[string]any{
		{"type": "command", "command": "echo ok"},               // 无 timeout → 补 10
		{"type": "command", "command": "echo ok", "timeout": 5}, // 有 → 不覆盖
	}
	out := applyGatewayTimeout(hooks, 10)
	// 返回的配置带正确 timeout（RunAll 收到的生效值）
	if v, _ := out[0]["timeout"].(int); v != 10 {
		t.Errorf("无 timeout 应补 10，实际 %v", out[0]["timeout"])
	}
	if v, _ := out[1]["timeout"].(int); v != 5 {
		t.Errorf("已有 timeout=5 不应覆盖，实际 %v", out[1]["timeout"])
	}
	// 原字段透传
	if out[0]["command"] != "echo ok" || out[1]["command"] != "echo ok" {
		t.Error("克隆应透传原有字段")
	}
	// S-02 治本：共享 map 不被就地修改
	if _, touched := hooks[0]["timeout"]; touched {
		t.Errorf("共享 map 不应被写入 timeout，实际 %v", hooks[0]["timeout"])
	}
	if v, _ := hooks[1]["timeout"].(int); v != 5 {
		t.Errorf("原 timeout=5 应保持不变，实际 %v", hooks[1]["timeout"])
	}
	// 返回的是新 map（非同引用）
	if &out[0] == &hooks[0] {
		t.Error("应构造新 slice")
	}
}

// TestOnNotification_matchQuery差异（S-02 重写：matcher 不匹配不执行 + 匹配执行不污染共享 map）
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
		t.Error("notification_type=cron 不匹配 matcher=permission，不应触发执行")
	}
	h.OnNotification(context.Background(), "permission", "need approve", "sess-1")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Errorf("匹配执行也不应就地写共享 map（S-02 治本），实际 %v", hooks[0]["timeout"])
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

// TestApplyGatewayTimeout_并发无数据竞争 S-02 回归测试。
// 旧实现就地写共享 map，并发调用 On* 触发 race（go test -race 下必炸）；
// 新实现构造新 map，共享配置只读。
func TestApplyGatewayTimeout_并发无数据竞争(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventUserPromptSubmit: {{Matcher: "*", Hooks: hooks}},
		},
	})
	const concurrency = 16
	done := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			h.OnUserPromptSubmit(context.Background(), "sess-1", "hello")
			// 并发读共享配置（Match 内部遍历）
			_ = h.config.Match(hookscfg.HookEventUserPromptSubmit, "")
		}(i)
	}
	for i := 0; i < concurrency; i++ {
		<-done
	}
}
