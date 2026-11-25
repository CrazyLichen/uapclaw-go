package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// newToolRetrievalTestAdapter 构造带真实 instance 的最小 DeepAdapter。
func newToolRetrievalTestAdapter(t *testing.T) *DeepAdapter {
	t.Helper()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-tool-retrieval-1"),
		agentschema.WithAgentName("tool-retrieval-test-agent"),
	)
	d := NewDeepAdapter()
	d.instance = harness.NewDeepAgent(card)
	return d
}

// echoTool 最小可执行工具（真 tool.Tool 实现，仅回显）。
type echoTool struct{ card *tool.ToolCard }

func (s echoTool) Card() *tool.ToolCard { return s.card }
func (s echoTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"success": true, "echo": inputs}, nil
}
func (s echoTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
func (s echoTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// registerDeferredCards 注册 deferred/direct 测试卡。
func registerDeferredCards(d *DeepAdapter) {
	am := d.instance.AbilityManager()
	deferred := tool.NewToolCardWithID("cron_create_job", "cron_create_job", "创建定时任务", nil, nil)
	deferred.Exposure = tool.ToolExposureDeferred
	deferred.SetExposureDeclared(true)
	_ = am.AddAbility(deferred, echoTool{card: deferred})
	direct := tool.NewToolCardWithID("bash", "bash", "执行命令", nil, nil)
	direct.Exposure = tool.ToolExposureDirect
	direct.SetExposureDeclared(true)
	_ = am.AddAbility(direct, echoTool{card: direct})
}

// TestRegisterToolRetrievalPromptRail_单一所有者 对齐 Python：
// 开关开 → 丢弃内置 ProgressiveToolRail + 排队本层 rail；开关关 → 内置保留。
func TestRegisterToolRetrievalPromptRail_单一所有者(t *testing.T) {
	d := newToolRetrievalTestAdapter(t)
	registerDeferredCards(d)
	// 开：双开关 true
	enabled := map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": true}
	d.registerToolRetrievalPromptRail(enabled)
	if d.toolRetrievalPromptRail == nil {
		t.Fatal("开启态应挂载本层 rail")
	}
	if d.toolRetrievalSessionEnabled == nil || !*d.toolRetrievalSessionEnabled {
		t.Fatal("构建期冻结应为 true")
	}

	// 关：新 adapter（新会话 child），内置 rail 保留
	d2 := newToolRetrievalTestAdapter(t)
	registerDeferredCards(d2)
	d2.registerToolRetrievalPromptRail(map[string]any{})
	if d2.toolRetrievalPromptRail != nil {
		t.Fatal("关闭态不应挂载本层 rail")
	}
	if d2.toolRetrievalSessionEnabled == nil || *d2.toolRetrievalSessionEnabled {
		t.Fatal("关闭态冻结应为 false")
	}
}

// TestDropPendingProgressiveToolRail 对齐 Python _drop_pending_progressive_tool_rail
// （interface_deep.py:7289-7305，手术方法归属 adapter）：pending 中
// ProgressiveToolRail 被剔除其余保留；无内置时返回 false。
func TestDropPendingProgressiveToolRail(t *testing.T) {
	d := newToolRetrievalTestAdapter(t)
	if d.dropPendingProgressiveToolRail() {
		t.Fatal("无内置 rail 时应返回 false")
	}
	// 混入内置 rail + 其他 rail：剔除内置、保留其他（Python kept 列表重建）
	other := commrails.NewToolRetrievalPromptRail(d.getOrCreateToolRetrievalToolkit(map[string]any{}), map[string]any{})
	d.instance.QueuePendingRail(other)
	d.instance.QueuePendingRail(rails.NewProgressiveToolRail(nil))
	if !d.dropPendingProgressiveToolRail() {
		t.Fatal("存在内置 rail 时应返回 true")
	}
	for _, rail := range d.instance.PendingRails() {
		if _, isProgressive := rail.(*rails.ProgressiveToolRail); isProgressive {
			t.Fatal("剔除后 pending 中不应再有内置 rail")
		}
	}
}

// TestToolRetrievalEnableFlipPending_R5判据 对齐 Python :7498-7517：
// frozen 与 live 任一方向不一致 → true；未冻结 → false。
func TestToolRetrievalEnableFlipPending_R5判据(t *testing.T) {
	d := newToolRetrievalTestAdapter(t)
	// 未冻结
	if d.toolRetrievalEnableFlipPending(map[string]any{}) {
		t.Fatal("未冻结应恒 false")
	}
	// 冻结 on，live off → 翻转
	on := true
	d.toolRetrievalSessionEnabled = &on
	if !d.toolRetrievalEnableFlipPending(map[string]any{}) {
		t.Fatal("on→off 应翻转")
	}
	// 冻结 off，live off → 不翻转
	off := false
	d.toolRetrievalSessionEnabled = &off
	if d.toolRetrievalEnableFlipPending(map[string]any{}) {
		t.Fatal("off→off 不应翻转")
	}
	// live on（configBase 双开）
	liveOn := map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": true}
	if !d.toolRetrievalEnableFlipPending(liveOn) {
		t.Fatal("off→on 应翻转")
	}
	// 冻结 on，live on → 不翻转
	d2 := newToolRetrievalTestAdapter(t)
	on2 := true
	d2.toolRetrievalSessionEnabled = &on2
	if d2.toolRetrievalEnableFlipPending(liveOn) {
		t.Fatal("on→on 不应翻转")
	}
}

// TestToolRetrievalEnabled_配置开关 对齐 Python is_tool_retrieval_enabled 双开关。
func TestToolRetrievalEnabled_配置开关(t *testing.T) {
	if config.ToolRetrievalEnabled(nil) {
		t.Fatal("nil 配置应关闭")
	}
	if config.ToolRetrievalEnabled(map[string]any{"progressive_tool_enabled": true}) {
		t.Fatal("仅 enabled 应关闭")
	}
	if !config.ToolRetrievalEnabled(map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": true}) {
		t.Fatal("双开应启用")
	}
}

// TestMcpServerDescriptions_条目与注册 对齐 Python _mcp_server_descriptions：
// 配置条目 plain description 双语共用；只有名字的 server 也贡献名字。
func TestMcpServerDescriptions_条目与注册(t *testing.T) {
	d := newToolRetrievalTestAdapter(t)
	d.configBase = map[string]any{
		"mcp": map[string]any{
			"servers": []any{
				map[string]any{"name": "github", "description": "GitHub 服务器"},
				map[string]any{"name": "bare_server"},
			},
		},
	}
	descs := d.mcpServerDescriptions()
	if len(descs) != 2 {
		t.Fatalf("应有两个 server, got %v", descs)
	}
	if _, ok := descs["github"]; !ok {
		t.Fatal("github 条目缺失")
	}
	if _, ok := descs["bare_server"]; !ok {
		t.Fatal("只有名字的 server 也应贡献条目")
	}
}
