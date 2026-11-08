package tool_call

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewToolDescriptionMethod 验证构造函数保留配置与评估器引用。
func TestNewToolDescriptionMethod(t *testing.T) {
	cfg := map[string]any{"verbose": true}
	m := NewToolDescriptionMethod(cfg, nil, &SimpleEval{})
	if m == nil {
		t.Fatal("不应返回 nil")
	}
	if m.evalFn == nil {
		t.Fatal("evalFn 应被保留")
	}
	if m.config["verbose"] != true {
		t.Fatalf("config 应保留，实际 %v", m.config)
	}
}

// TestStep_迭代0返回原始描述 验证 it==0 时直接返回工具原始描述。
func TestStep_迭代0返回原始描述(t *testing.T) {
	m := NewToolDescriptionMethod(map[string]any{}, nil, &SimpleEval{})
	tool := map[string]any{
		"name":        "bash",
		"description": "run shell",
	}

	out, data, score, err := m.Step(context.Background(), tool, nil, nil, 0)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if out["description"] != "run shell" {
		t.Fatalf("description = %v, 期望原始描述", out["description"])
	}
	if out["iteration"] != 0 {
		t.Fatalf("iteration 应为 0，实际 %v", out["iteration"])
	}
	if len(data) != 1 || data[0] != "run shell" {
		t.Fatalf("data = %v, 期望含原始描述", data)
	}
	if score != 0 {
		t.Fatalf("无评估结果时分数应为 0，实际 %v", score)
	}
}

// TestGetExamples_无示例目录返回空 验证未配置 examples_dir 时返回空切片。
func TestGetExamples_无示例目录返回空(t *testing.T) {
	m := NewToolDescriptionMethod(map[string]any{}, nil, &SimpleEval{})
	got := m.GetExamples(context.Background(), map[string]any{"name": "bash"})
	if len(got) != 0 {
		t.Fatalf("应返回空示例，实际 %v", got)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
