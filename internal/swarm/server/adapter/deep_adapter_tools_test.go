package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSyncTool 用于测试的轻量工具
type fakeSyncTool struct{ name string }

func (f *fakeSyncTool) Card() *tool.ToolCard {
	return tool.NewToolCard(f.name, f.name, nil, nil)
}
func (f *fakeSyncTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeSyncTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestSyncToolGroup_四象限 验证 Python 同签名 syncToolGroup 的四象限行为。
// Python: _sync_tool_group (interface_deep.py:1319-1350)
func TestSyncToolGroup_四象限(t *testing.T) {
	d := NewDeepAdapter()
	fake := &fakeSyncTool{name: "test_tool_x"}

	// 象限1: disabled+notRegistered → 原样（nil, false）
	tools, reg := d.syncToolGroup(nil, false, false, func() []tool.Tool { return nil }, "x")
	if reg || tools != nil {
		t.Error("disabled+unregistered 应返回 nil,false")
	}

	// 象限2: disabled+registered → 移除并注销
	_, reg = d.syncToolGroup([]tool.Tool{fake}, true, false, nil, "x")
	if reg {
		t.Error("disabled+registered 应注销")
	}

	// 象限3: enabled+notRegistered → 注册
	tools, reg = d.syncToolGroup(nil, false, true, func() []tool.Tool { return []tool.Tool{fake} }, "x")
	if !reg || len(tools) != 1 {
		t.Errorf("enabled+unregistered 应注册，got reg=%v len=%d", reg, len(tools))
	}

	// 象限4: enabled+registered → 原样返回
	same, reg2 := d.syncToolGroup(tools, true, true, nil, "x")
	if !reg2 || len(same) != 1 {
		t.Errorf("enabled+registered 应原样返回，got reg=%v len=%d", reg2, len(same))
	}
}

// TestSyncToolGroup_panic恢复 验证 createFn panic 时 safeCreateTools 兜底返回 nil。
// Python: interface_deep.py:1347-1349（except Exception → warning + return [], False）
func TestSyncToolGroup_panic恢复(t *testing.T) {
	d := NewDeepAdapter()
	tools, reg := d.syncToolGroup(nil, false, true, func() []tool.Tool {
		panic("create failed")
	}, "x")
	if reg || tools != nil {
		t.Errorf("panic 应返回 nil,false，got %v,%v", tools, reg)
	}
}

// TestBuildImageGenModelConfig_无key返回false 验证 image_gen 配置门控。
// Python: _build_image_gen_model_config (interface_deep.py:1261-1269)
func TestBuildImageGenModelConfig_无key返回false(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "")
	d := NewDeepAdapter()
	if d.buildImageGenModelConfig(map[string]any{}) {
		t.Error("无 IMAGE_GEN_API_KEY 应返回 false")
	}
	// YAML 注入 key 后应返回 true
	t.Setenv("IMAGE_GEN_API_KEY", "k")
	if !d.buildImageGenModelConfig(map[string]any{}) {
		t.Error("有 IMAGE_GEN_API_KEY 应返回 true")
	}
}

// TestGetToolCards_步骤1_wiki三工具 验证 getToolCards 步骤 1 注册 wiki 3 工具。
// Python: _get_tool_cards 步骤 1 (interface_deep.py:2360-2366)
func TestGetToolCards_步骤1_wiki三工具(t *testing.T) {
	d := NewDeepAdapter()
	d.configBase = map[string]any{}
	cards := d.getToolCards("agent-1")
	names := map[string]bool{}
	for _, c := range cards {
		names[c.Name] = true
	}
	for _, want := range []string{"wiki_ingest", "wiki_query", "wiki_lint"} {
		if !names[want] {
			t.Errorf("缺少 wiki 工具卡: %s", want)
		}
	}
}
