package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewPermissionInterruptRail 测试创建 PermissionInterruptRail
func TestNewPermissionInterruptRail(t *testing.T) {
	r := NewPermissionInterruptRail(
		map[string]any{"enabled": true},
		nil,
		[]string{"bash", "read"},
		nil,
	)
	assert.NotNil(t, r)
	assert.NotNil(t, r.Engine())
	assert.True(t, r.Engine().Enabled())
}

// TestNewPermissionInterruptRail_带Engine 测试传入已有引擎
func TestNewPermissionInterruptRail_带Engine(t *testing.T) {
	engine := harnesssecurity.NewPermissionEngine(
		map[string]any{"enabled": false},
		"/workspace",
	)
	r := NewPermissionInterruptRail(
		map[string]any{"enabled": false},
		engine,
		nil,
		nil,
	)
	assert.NotNil(t, r)
	assert.False(t, r.Engine().Enabled())
}

// TestNewPermissionInterruptRail_带Host 测试传入 ToolPermissionHost
func TestNewPermissionInterruptRail_带Host(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/test/workspace" },
		ToolPermissionChecksActive: func() bool { return true },
	}
	r := NewPermissionInterruptRail(
		map[string]any{"enabled": true},
		nil,
		nil,
		host,
	)
	assert.NotNil(t, r)
}

// TestNormalizeToolName 测试工具名归一化
func TestNormalizeToolName(t *testing.T) {
	r := NewPermissionInterruptRail(nil, nil, nil, nil)
	tests := []struct {
		input    string
		expected string
	}{
		{"exec_command", "mcp_exec_command"},
		{"free_search", "mcp_free_search"},
		{"paid_search", "mcp_paid_search"},
		{"fetch_webpage", "mcp_fetch_webpage"},
		{"bash", "bash"},
		{"read", "read"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, r.normalizeToolName(tt.input), "input=%s", tt.input)
	}
}

// TestBuildShellAutoConfirmKey 测试 Shell AST auto-confirm key 构建
func TestBuildShellAutoConfirmKey(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		cmd      string
		expected string
	}{
		{"简单命令", "bash", "ls -la", "bash:ls -la"},
		{"空命令", "bash", "", ""},
		{"纯空格", "bash", "   ", ""},
		{"管道命令-多子命令", "bash", "echo hello | grep h", ""}, // len(subcommands)!=1
		{"mcp_exec_command", "mcp_exec_command", "git status", "mcp_exec_command:git status"},
		{"create_terminal", "create_terminal", "pwd", "create_terminal:pwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildShellAutoConfirmKey(tt.tool, tt.cmd)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestShouldStoreAutoConfirm 测试是否应存储 auto-confirm key
func TestShouldStoreAutoConfirm(t *testing.T) {
	tests := []struct {
		name           string
		autoConfirm    bool
		session        any
		autoConfirmKey string
		persisted      bool
		expected       bool
	}{
		{"全满足", true, "session", "bash:ls", false, true},
		{"autoConfirm为false", false, "session", "bash:ls", false, false},
		{"session为nil", true, nil, "bash:ls", false, false},
		{"key为空", true, "session", "", false, false},
		{"已持久化", true, "session", "bash:ls", true, false},
		{"key非空且全满足", true, "session", "bash", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldStoreAutoConfirm(tt.autoConfirm, tt.session, tt.autoConfirmKey, tt.persisted))
		})
	}
}

// TestParseToolArgs 测试从 ToolCall 提取参数
func TestParseToolArgs(t *testing.T) {
	t.Run("nil_ToolCall", func(t *testing.T) {
		result := parseToolArgs(nil)
		assert.Equal(t, map[string]any{}, result)
	})

	t.Run("空参数", func(t *testing.T) {
		tc := &llmschema.ToolCall{Arguments: ""}
		result := parseToolArgs(tc)
		assert.Equal(t, map[string]any{}, result)
	})

	t.Run("JSON参数", func(t *testing.T) {
		tc := &llmschema.ToolCall{Arguments: `{"command": "ls -la"}`}
		result := parseToolArgs(tc)
		assert.Equal(t, "ls -la", result["command"])
	})

	t.Run("无效JSON", func(t *testing.T) {
		tc := &llmschema.ToolCall{Arguments: "not json"}
		result := parseToolArgs(tc)
		assert.Equal(t, map[string]any{}, result)
	})
}

// TestParseConfirmPayload 测试解析确认载荷
func TestParseConfirmPayload(t *testing.T) {
	t.Run("PermissionConfirmResponse", func(t *testing.T) {
		input := &harnesssecurity.PermissionConfirmResponse{
			Approved:    true,
			Feedback:    "ok",
			AutoConfirm: true,
		}
		result := parseConfirmPayload(input)
		require.NotNil(t, result)
		assert.True(t, result.Approved)
		assert.Equal(t, "ok", result.Feedback)
		assert.True(t, result.AutoConfirm)
	})

	t.Run("ConfirmPayload", func(t *testing.T) {
		input := &interrupt.ConfirmPayload{
			Approved:    true,
			Feedback:    "test",
			AutoConfirm: false,
		}
		result := parseConfirmPayload(input)
		require.NotNil(t, result)
		assert.True(t, result.Approved)
		assert.Equal(t, "test", result.Feedback)
		assert.False(t, result.AutoConfirm)
	})

	t.Run("map", func(t *testing.T) {
		input := map[string]any{
			"approved":     true,
			"feedback":     "map feedback",
			"auto_confirm": true,
		}
		result := parseConfirmPayload(input)
		require.NotNil(t, result)
		assert.True(t, result.Approved)
		assert.Equal(t, "map feedback", result.Feedback)
		assert.True(t, result.AutoConfirm)
	})

	t.Run("map缺少approved", func(t *testing.T) {
		input := map[string]any{
			"feedback": "no approved",
		}
		result := parseConfirmPayload(input)
		assert.Nil(t, result)
	})

	t.Run("JSON字符串", func(t *testing.T) {
		input := `{"approved": true, "feedback": "json ok", "auto_confirm": false}`
		result := parseConfirmPayload(input)
		require.NotNil(t, result)
		assert.True(t, result.Approved)
		assert.Equal(t, "json ok", result.Feedback)
	})

	t.Run("无效类型", func(t *testing.T) {
		result := parseConfirmPayload(42)
		assert.Nil(t, result)
	})

	t.Run("无效JSON字符串", func(t *testing.T) {
		result := parseConfirmPayload("not json")
		assert.Nil(t, result)
	})
}

// TestIsPermissionAutoConfirmed 测试 auto-confirm 检查
func TestIsPermissionAutoConfirmed(t *testing.T) {
	t.Run("nil配置", func(t *testing.T) {
		assert.False(t, isPermissionAutoConfirmed(nil, "bash"))
	})

	t.Run("空key", func(t *testing.T) {
		assert.False(t, isPermissionAutoConfirmed(map[string]any{"bash": true}, ""))
	})

	t.Run("命中true", func(t *testing.T) {
		assert.True(t, isPermissionAutoConfirmed(map[string]any{"bash": true}, "bash"))
	})

	t.Run("命中false", func(t *testing.T) {
		assert.False(t, isPermissionAutoConfirmed(map[string]any{"bash": false}, "bash"))
	})

	t.Run("未命中", func(t *testing.T) {
		assert.False(t, isPermissionAutoConfirmed(map[string]any{"bash": true}, "read"))
	})
}

// TestFormatArgsPreview 测试参数预览格式化
func TestFormatArgsPreview(t *testing.T) {
	t.Run("nil参数", func(t *testing.T) {
		assert.Equal(t, "", formatArgsPreview(nil))
	})

	t.Run("正常参数", func(t *testing.T) {
		args := map[string]any{"command": "ls -la"}
		result := formatArgsPreview(args)
		assert.Contains(t, result, "command")
		assert.Contains(t, result, "ls -la")
	})

	t.Run("截断测试", func(t *testing.T) {
		longValue := strings.Repeat("a", 1500)
		args := map[string]any{"data": longValue}
		result := formatArgsPreview(args)
		assert.LessOrEqual(t, len(result), 1000)
	})
}

// TestConfirmPayloadSchemaForPermission 测试 ConfirmPayload Schema
func TestConfirmPayloadSchemaForPermission(t *testing.T) {
	schema := confirmPayloadSchemaForPermission()
	assert.Equal(t, "object", schema["type"])
	assert.Equal(t, "ConfirmPayload", schema["title"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "approved")
	assert.Contains(t, props, "feedback")
	assert.Contains(t, props, "auto_confirm")

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "approved")
}

// TestDeepCopyMap 测试深拷贝 map
func TestDeepCopyMap(t *testing.T) {
	t.Run("nil输入", func(t *testing.T) {
		result := deepCopyMap(nil)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("正常拷贝", func(t *testing.T) {
		original := map[string]any{
			"enabled": true,
			"tools":   map[string]any{"bash": "allow"},
		}
		copied := deepCopyMap(original)

		// 修改拷贝不影响原始
		copied["enabled"] = false
		enabled, _ := original["enabled"].(bool)
		assert.True(t, enabled)
	})

	t.Run("嵌套修改独立", func(t *testing.T) {
		original := map[string]any{
			"tools": map[string]any{"bash": "allow"},
		}
		copied := deepCopyMap(original)

		tools := copied["tools"].(map[string]any)
		tools["bash"] = "deny"

		// 原始不受影响
		origTools := original["tools"].(map[string]any)
		assert.Equal(t, "allow", origTools["bash"])
	})
}

// TestBuildMessage 测试中断消息构建
func TestBuildMessage(t *testing.T) {
	r := NewPermissionInterruptRail(nil, nil, nil, nil)

	t.Run("基本消息", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
		result := &harnesssecurity.PermissionResult{
			Permission:  harnesssecurity.PermissionLevelAsk,
			MatchedRule: "defaults.bash",
			Reason:      "需要确认",
		}
		msg := r.buildMessage(tc, result)
		assert.Contains(t, msg, "bash")
		assert.Contains(t, msg, "需要授权")
		assert.Contains(t, msg, "defaults.bash")
	})

	t.Run("带外部路径", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "read", Arguments: `{"path": "/etc/passwd"}`}
		result := &harnesssecurity.PermissionResult{
			Permission:    harnesssecurity.PermissionLevelAsk,
			MatchedRule:   "external_directory",
			ExternalPaths: []string{"/etc/passwd"},
		}
		msg := r.buildMessage(tc, result)
		assert.Contains(t, msg, "/etc/passwd")
	})

	t.Run("nil_ToolCall", func(t *testing.T) {
		result := &harnesssecurity.PermissionResult{
			Permission:  harnesssecurity.PermissionLevelAsk,
			MatchedRule: "",
		}
		msg := r.buildMessage(nil, result)
		assert.Contains(t, msg, "N/A")
	})
}

// TestBuildAlwaysAllowHint 测试自动确认提示构建
func TestBuildAlwaysAllowHint(t *testing.T) {
	r := NewPermissionInterruptRail(nil, nil, nil, nil)

	t.Run("nil_ToolCall", func(t *testing.T) {
		assert.Equal(t, "", r.buildAlwaysAllowHint(nil))
	})

	t.Run("bash简单命令", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
		hint := r.buildAlwaysAllowHint(tc)
		assert.Contains(t, hint, "记住")
		assert.Contains(t, hint, "auto_confirm")
	})

	t.Run("bash空命令", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": ""}`}
		hint := r.buildAlwaysAllowHint(tc)
		// 空命令没有 shell key，getAutoConfirmKey 也返回空 → 无提示
		assert.Equal(t, "", hint)
	})

	t.Run("非shell工具", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "read", Arguments: `{"path": "/tmp"}`}
		hint := r.buildAlwaysAllowHint(tc)
		assert.Contains(t, hint, "read")
	})

	t.Run("空工具名", func(t *testing.T) {
		tc := &llmschema.ToolCall{Name: "", Arguments: `{}`}
		hint := r.buildAlwaysAllowHint(tc)
		assert.Equal(t, "", hint)
	})
}

// TestConfirmPathLabel 测试确认路径标签
func TestConfirmPathLabel(t *testing.T) {
	t.Run("无hosted", func(t *testing.T) {
		r := NewPermissionInterruptRail(nil, nil, nil, nil)
		assert.Equal(t, "interrupt", r.confirmPathLabel())
	})

	t.Run("有hosted", func(t *testing.T) {
		host := &harnesssecurity.ToolPermissionHost{
			RequestPermissionConfirmation: func(req harnesssecurity.PermissionConfirmationRequest) (*harnesssecurity.PermissionConfirmResponse, error) {
				return nil, nil
			},
		}
		r := NewPermissionInterruptRail(nil, nil, nil, host)
		assert.Equal(t, "hosted", r.confirmPathLabel())
	})
}

// TestUpdateConfig 测试配置热更新
func TestUpdateConfig(t *testing.T) {
	r := NewPermissionInterruptRail(
		map[string]any{"enabled": true},
		nil,
		[]string{"bash"},
		nil,
	)
	assert.True(t, r.Engine().Enabled())

	r.UpdateConfig(map[string]any{"enabled": false}, []string{"read"})
	assert.False(t, r.Engine().Enabled())
}

// TestResolvePermissionInterrupt_Allow 测试 ALLOW 决策路径
func TestResolvePermissionInterrupt_Allow(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"read": "allow",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "read", Arguments: `{"path": "/tmp"}`}
	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil, // userInput = nil → 首次检查
		nil, // autoConfirmConfig
	)

	approve, ok := decision.(*interrupt.ApproveResult)
	require.True(t, ok, "期望 ApproveResult，实际 %T", decision)
	assert.NotNil(t, approve)
}

// TestResolvePermissionInterrupt_Deny 测试 DENY 决策路径
func TestResolvePermissionInterrupt_Deny(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "deny",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "rm -rf /"}`}
	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil,
		nil,
	)

	reject, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok, "期望 RejectResult，实际 %T", decision)
	assert.Contains(t, fmt.Sprintf("%v", reject.ToolResult), "PERMISSION_DENIED")
}

// TestResolvePermissionInterrupt_AskInterrupt 测试 ASK → Interrupt 路径
func TestResolvePermissionInterrupt_AskInterrupt(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil,
		nil,
	)

	interruptResult, ok := decision.(*interrupt.InterruptResult)
	require.True(t, ok, "期望 InterruptResult，实际 %T", decision)
	assert.NotNil(t, interruptResult.Request)
}

// TestResolvePermissionInterrupt_AutoConfirm 测试 auto-confirm 命中路径
func TestResolvePermissionInterrupt_AutoConfirm(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"read": "ask",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "read", Arguments: `{"path": "/tmp"}`}
	// read 工具的 auto-confirm key 为 "read"
	autoConfirmConfig := map[string]any{"read": true}

	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil, // userInput = nil
		autoConfirmConfig,
	)

	approve, ok := decision.(*interrupt.ApproveResult)
	require.True(t, ok, "期望 ApproveResult（auto-confirm 命中），实际 %T", decision)
	assert.NotNil(t, approve)
}

// TestResolvePermissionInterrupt_UserApproved 测试用户批准恢复路径
func TestResolvePermissionInterrupt_UserApproved(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
	userInput := map[string]any{
		"approved":     true,
		"auto_confirm": false,
	}

	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		userInput,
		nil,
	)

	approve, ok := decision.(*interrupt.ApproveResult)
	require.True(t, ok, "期望 ApproveResult（用户批准），实际 %T", decision)
	assert.NotNil(t, approve)
}

// TestResolvePermissionInterrupt_UserRejected 测试用户拒绝恢复路径
func TestResolvePermissionInterrupt_UserRejected(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
	userInput := map[string]any{
		"approved": false,
		"feedback": "不批准",
	}

	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		userInput,
		nil,
	)

	reject, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok, "期望 RejectResult（用户拒绝），实际 %T", decision)
	assert.Equal(t, "不批准", reject.ToolResult)
}

// TestResolvePermissionInterrupt_InvalidPayload 测试无效载荷恢复路径
func TestResolvePermissionInterrupt_InvalidPayload(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, nil)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}

	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		42, // 无效类型
		nil,
	)

	// 无效载荷 → 重新中断
	_, ok := decision.(*interrupt.InterruptResult)
	assert.True(t, ok, "期望 InterruptResult（无效载荷），实际 %T", decision)
}

// TestResolvePermissionInterrupt_SceneHookApprove 测试 SceneHook approve 短路
func TestResolvePermissionInterrupt_SceneHookApprove(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "deny",
		},
	}
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			return []string{"approve"}, nil
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, host)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil,
		nil,
	)

	approve, ok := decision.(*interrupt.ApproveResult)
	require.True(t, ok, "期望 ApproveResult（SceneHook approve），实际 %T", decision)
	assert.NotNil(t, approve)
}

// TestResolvePermissionInterrupt_SceneHookReject 测试 SceneHook reject 短路
func TestResolvePermissionInterrupt_SceneHookReject(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "allow",
		},
	}
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			return []string{"reject", "custom deny reason"}, nil
		},
	}
	r := NewPermissionInterruptRail(config, nil, nil, host)

	toolCall := &llmschema.ToolCall{Name: "bash", Arguments: `{"command": "ls"}`}
	decision := r.resolvePermissionInterrupt(
		nil,  // ctx
		nil,  // cbc
		toolCall,
		nil,
		nil,
	)

	reject, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok, "期望 RejectResult（SceneHook reject），实际 %T", decision)
	assert.Equal(t, "custom deny reason", reject.ToolResult)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
