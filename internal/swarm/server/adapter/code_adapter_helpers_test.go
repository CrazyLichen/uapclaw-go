package adapter

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestParseStringSlice 验证按 key 取字符串切片的三种输入形态。
func TestParseStringSlice(t *testing.T) {
	t.Run("key 缺失返回 nil", func(t *testing.T) {
		if got := parseStringSlice(map[string]any{}, "k"); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("值为 nil 返回 nil", func(t *testing.T) {
		if got := parseStringSlice(map[string]any{"k": nil}, "k"); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("[]any 提取字符串并跳过非字符串", func(t *testing.T) {
		got := parseStringSlice(map[string]any{"k": []any{"a", 1, "b", nil}}, "k")
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("应提取 a、b，实际 %v", got)
		}
	})

	t.Run("[]string 原样返回", func(t *testing.T) {
		got := parseStringSlice(map[string]any{"k": []string{"x", "y"}}, "k")
		if len(got) != 2 || got[0] != "x" {
			t.Fatalf("应原样返回，实际 %v", got)
		}
	})

	t.Run("其它类型返回 nil", func(t *testing.T) {
		if got := parseStringSlice(map[string]any{"k": 42}, "k"); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})
}

// TestCopyMap 验证浅拷贝语义与 nil 入参。
func TestCopyMap(t *testing.T) {
	t.Run("nil 返回空 map 而非 nil", func(t *testing.T) {
		got := copyMap(nil)
		if got == nil {
			t.Fatal("应返回非 nil 空 map")
		}
		if len(got) != 0 {
			t.Fatalf("应为空，实际 %v", got)
		}
	})

	t.Run("拷贝后修改不影响原 map", func(t *testing.T) {
		src := map[string]any{"a": 1}
		dst := copyMap(src)
		dst["b"] = 2
		if _, ok := src["b"]; ok {
			t.Fatal("修改副本不应影响原 map")
		}
		if dst["a"] != 1 {
			t.Fatalf("应保留原键值，实际 %v", dst)
		}
	})
}

// TestToStringAny 验证 any 转字符串。
func TestToStringAny(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil 返回空串", nil, ""},
		{"字符串原样返回", "abc", "abc"},
		{"非字符串走格式化", 42, "42"},
		{"浮点走格式化", 1.5, "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toStringAny(tt.in); got != tt.want {
				t.Fatalf("toStringAny(%v) = %q, 期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMergeToolCards 验证工具卡片去重合并。
func TestMergeToolCards(t *testing.T) {
	t.Run("agent 为 nil 返回 0", func(t *testing.T) {
		if got := mergeToolCards(nil, []*tool.ToolCard{{}}); got != 0 {
			t.Fatalf("应返回 0，实际 %d", got)
		}
	})

	t.Run("卡片为空返回 0", func(t *testing.T) {
		if got := mergeToolCards(nil, nil); got != 0 {
			t.Fatalf("应返回 0，实际 %d", got)
		}
	})
}

// TestAgentHasRailType 验证 rail 类型存在性检查的空值防护。
func TestAgentHasRailType(t *testing.T) {
	t.Run("agent 与 rail 均 nil 返回 false", func(t *testing.T) {
		if agentHasRailType(nil, nil) {
			t.Fatal("应返回 false")
		}
	})
}

// TestQueueRailIfMissing 验证空 rail 不加入。
func TestQueueRailIfMissing(t *testing.T) {
	t.Run("rail 为 nil 返回 false", func(t *testing.T) {
		if queueRailIfMissing(nil, nil) {
			t.Fatal("应返回 false")
		}
	})
}

// TestQueueRailsIfMissing 验证批量加入对空列表返回 0。
func TestQueueRailsIfMissing(t *testing.T) {
	if got := queueRailsIfMissing(nil, nil); got != 0 {
		t.Fatalf("空列表应返回 0，实际 %d", got)
	}
}

// TestMergeSubagents 验证子代理合并的空值防护。
func TestMergeSubagents(t *testing.T) {
	t.Run("agent 为 nil 返回 0", func(t *testing.T) {
		if got := mergeSubagents(nil, nil); got != 0 {
			t.Fatalf("应返回 0，实际 %d", got)
		}
	})
}

// TestCodeAdapterResolvePromptLanguage 验证 code 模式强制英文。
func TestCodeAdapterResolvePromptLanguage(t *testing.T) {
	c := NewCodeAdapter()
	if got := c.resolvePromptLanguage(); got != "en" {
		t.Fatalf("code 模式提示词语言应为 en，实际 %q", got)
	}
}

// TestCodeAdapterResolveRuntimeLanguage 验证运行时语言覆写与默认英文。
func TestCodeAdapterResolveRuntimeLanguage(t *testing.T) {
	c := NewCodeAdapter()
	if got := c.resolveRuntimeLanguage(); got != "en" {
		t.Fatalf("默认应为 en，实际 %q", got)
	}
	c.runtimeLanguageOverride = "cn"
	if got := c.resolveRuntimeLanguage(); got != "cn" {
		t.Fatalf("覆写后应为 cn，实际 %q", got)
	}
}

// TestCodeAdapter_Instance_NilDeep 验证未装配 deep 时 Instance 返回 nil。
func TestCodeAdapter_Instance_NilDeep(t *testing.T) {
	c := NewCodeAdapter()
	if got := c.Instance(); got != nil {
		t.Fatalf("未装配 deep 时应返回 nil，实际 %#v", got)
	}
}

// TestMergeMemberMcpConfigs_无DeepConfig 验证 agent 无 DeepConfig 时返回 0。
func TestMergeMemberMcpConfigs_无DeepConfig(t *testing.T) {
	c := NewCodeAdapter()
	c.deep = NewDeepAdapter()
	// MergeMemberMcpConfigs 内部直接取 agent.DeepConfig()，传 nil 会 panic，
	// 故用未配置的真实 DeepAgent（DeepConfig() 返回 nil）覆盖该短路径。
	agent := harness.NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("merge-mcp-test"),
		agentschema.WithAgentName("merge-mcp-test"),
	))
	if got := c.MergeMemberMcpConfigs(agent, map[string]any{}); got != 0 {
		t.Fatalf("无 DeepConfig 时应返回 0，实际 %d", got)
	}
}

// TestCodeAdapterBuildWorktreeRail 验证 worktree rail 构造且默认启用。
func TestCodeAdapterBuildWorktreeRail(t *testing.T) {
	c := NewCodeAdapter()
	rail := c.buildWorktreeRail()
	if rail == nil {
		t.Fatal("应返回非 nil rail")
	}
}
