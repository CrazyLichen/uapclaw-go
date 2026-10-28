package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────── 测试辅助 ────────────────────

// newAcpTestAdapter 构造带 instance 的最小 DeepAdapter（ACP 刷新测试用）。
func newAcpTestAdapter(t *testing.T) *DeepAdapter {
	t.Helper()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-acp-1"),
		agentschema.WithAgentName("acp-test-agent"),
	)
	d := NewDeepAdapter()
	d.instance = harness.NewDeepAgent(card)
	return d
}

// abilityNamesOf 收集 ability 名集合。
func abilityNamesOf(d *DeepAdapter) map[string]bool {
	out := map[string]bool{}
	if d.instance == nil {
		return out
	}
	for _, ab := range d.instance.AbilityManager().List() {
		out[ab.AbilityName()] = true
	}
	return out
}

// addFakeToolCard 向 AbilityManager 注入假工具卡（模拟默认文件/命令工具）。
func addFakeToolCard(d *DeepAdapter, name string) {
	card := tool.NewToolCard(name, "测试工具 "+name, nil, nil)
	d.instance.AbilityManager().Add(card)
}

// ──────────────────── acpRuntimeToolsEnabled 测试 ────────────────────

// TestAcpRuntimeToolsEnabled_无metadata 无 metadata 时 fs/terminal 均关。
func TestAcpRuntimeToolsEnabled_无metadata(t *testing.T) {
	fs, term := acpRuntimeToolsEnabled(nil)
	if fs || term {
		t.Errorf("nil metadata: fs=%v term=%v，应均 false", fs, term)
	}
	fs, term = acpRuntimeToolsEnabled(map[string]any{})
	if fs || term {
		t.Errorf("空 metadata: fs=%v term=%v，应均 false", fs, term)
	}
	// 无 acp_client_capabilities 键
	fs, term = acpRuntimeToolsEnabled(map[string]any{"other": "x"})
	if fs || term {
		t.Errorf("无 caps 键: fs=%v term=%v，应均 false", fs, term)
	}
}

// TestAcpRuntimeToolsEnabled_fs布尔 fs 为 True 布尔时开启。
func TestAcpRuntimeToolsEnabled_fs布尔(t *testing.T) {
	fs, _ := acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{"fs": true},
	})
	if !fs {
		t.Error("fs=true 应开启")
	}
	fs, _ = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{"fs": false},
	})
	if fs {
		t.Error("fs=false 应关闭")
	}
}

// TestAcpRuntimeToolsEnabled_fs字典 fs 为 dict 时按 readTextFile/writeTextFile 判定。
func TestAcpRuntimeToolsEnabled_fs字典(t *testing.T) {
	fs, _ := acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{
			"fs": map[string]any{"readTextFile": true},
		},
	})
	if !fs {
		t.Error("fs.readTextFile=true 应开启")
	}
	fs, _ = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{
			"fs": map[string]any{"writeTextFile": true},
		},
	})
	if !fs {
		t.Error("fs.writeTextFile=true 应开启")
	}
	fs, _ = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
	})
	if fs {
		t.Error("fs dict 全 false 应关闭")
	}
	// 非布尔非 dict（如字符串）→ 关闭
	fs, _ = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{"fs": "yes"},
	})
	if fs {
		t.Error("fs 非布尔非 dict 应关闭")
	}
}

// TestAcpRuntimeToolsEnabled_terminal terminal 能力四键任一真值开启。
func TestAcpRuntimeToolsEnabled_terminal(t *testing.T) {
	for _, key := range []string{"create", "output", "waitForExit", "release"} {
		_, term := acpRuntimeToolsEnabled(map[string]any{
			"acp_client_capabilities": map[string]any{
				"terminal": map[string]any{key: true},
			},
		})
		if !term {
			t.Errorf("terminal.%s=true 应开启", key)
		}
	}
	_, term := acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{
			"terminal": map[string]any{
				"create": false, "output": false, "waitForExit": false, "release": false,
			},
		},
	})
	if term {
		t.Error("terminal dict 全 false 应关闭")
	}
	_, term = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{"terminal": true},
	})
	if !term {
		t.Error("terminal=true 应开启")
	}
	// 非布尔非 dict → 关闭
	_, term = acpRuntimeToolsEnabled(map[string]any{
		"acp_client_capabilities": map[string]any{"terminal": 42},
	})
	if term {
		t.Error("terminal 非布尔非 dict 应关闭")
	}
}

// ──────────────────── shouldRegisterAcpRuntimeTools 测试 ────────────────────

// TestShouldRegisterAcpRuntimeTools_条件链 四条件逐一验证。
func TestShouldRegisterAcpRuntimeTools_条件链(t *testing.T) {
	// 非 acp channel → false
	if shouldRegisterAcpRuntimeTools("web", "r", "s", true) {
		t.Error("非 acp channel 应 false")
	}
	// 空 request_id → false
	if shouldRegisterAcpRuntimeTools("acp", "", "s", true) {
		t.Error("空 request_id 应 false")
	}
	// 空 session_id → false
	if shouldRegisterAcpRuntimeTools("acp", "r", "", true) {
		t.Error("空 session_id 应 false")
	}
	// 无能力 → false
	if shouldRegisterAcpRuntimeTools("acp", "r", "s", false) {
		t.Error("无运行时能力应 false")
	}
	// 全满足 → true
	if !shouldRegisterAcpRuntimeTools("acp", "r", "s", true) {
		t.Error("全满足应 true")
	}
}

// ──────────────────── selectRegisteredRuntimeToolNames 测试 ────────────────────

// TestSelectRegisteredRuntimeToolNames_按序筛选 验证候选序保留与筛选。
func TestSelectRegisteredRuntimeToolNames_按序筛选(t *testing.T) {
	candidates := []string{"read_text_file", "write_text_file", "create_terminal"}
	abilityNames := []string{"create_terminal", "read_text_file", "other_tool"}
	got := selectRegisteredRuntimeToolNames(candidates, abilityNames)
	if len(got) != 2 || got[0] != "read_text_file" || got[1] != "create_terminal" {
		t.Errorf("got = %v", got)
	}
	// 空交集
	got = selectRegisteredRuntimeToolNames(candidates, []string{"nothing"})
	if len(got) != 0 {
		t.Errorf("空交集应空, got %v", got)
	}
}

// ──────────────────── isAcpRuntimeToolName 测试 ────────────────────

// TestIsAcpRuntimeToolName 判定六工具名。
func TestIsAcpRuntimeToolName(t *testing.T) {
	for _, n := range acpRuntimeToolNames {
		if !isAcpRuntimeToolName(n) {
			t.Errorf("%s 应为 ACP 工具名", n)
		}
	}
	for _, n := range []string{"read_file", "bash", "", "read_text"} {
		if isAcpRuntimeToolName(n) {
			t.Errorf("%s 不应为 ACP 工具名", n)
		}
	}
}

// ──────────────────── truthyAcpCap 测试 ────────────────────

// TestTruthyAcpCap Python truthiness 等价判定。
func TestTruthyAcpCap(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{nil, false},
		{false, false},
		{true, true},
		{"", false},
		{"x", true},
		{float64(0), false},
		{float64(1), true},
		{0, false},
		{1, true},
	}
	for _, c := range cases {
		if got := truthyAcpCap(c.v); got != c.want {
			t.Errorf("truthyAcpCap(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

// ──────────────────── refreshAcpRuntimeTools 测试 ────────────────────

// TestRefreshAcpRuntimeTools_instance未初始化 nil instance 不 panic。
func TestRefreshAcpRuntimeTools_instance未初始化(t *testing.T) {
	d := NewDeepAdapter()
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "s", RequestID: "r",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
}

// TestRefreshAcpRuntimeTools_nil配置 nil config 直接返回。
func TestRefreshAcpRuntimeTools_nil配置(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(nil)
}

// TestRefreshAcpRuntimeTools_acp通道移除默认工具 验证 channel=acp 时
// 移除 read_file/write_file/edit_file/bash/code 五默认工具。
func TestRefreshAcpRuntimeTools_acp通道移除默认工具(t *testing.T) {
	d := newAcpTestAdapter(t)
	for _, n := range []string{"read_file", "write_file", "edit_file", "bash", "code"} {
		addFakeToolCard(d, n)
	}
	// 非 ACP 工具不受影响
	addFakeToolCard(d, "wiki_query")

	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "s", RequestID: "r",
	})
	names := abilityNamesOf(d)
	for _, n := range []string{"read_file", "write_file", "edit_file", "bash", "code"} {
		if names[n] {
			t.Errorf("%s 应被移除", n)
		}
	}
	if !names["wiki_query"] {
		t.Error("wiki_query 不应被移除")
	}
}

// TestRefreshAcpRuntimeTools_非acp通道不移除默认工具 验证 channel≠acp 时保留。
func TestRefreshAcpRuntimeTools_非acp通道不移除默认工具(t *testing.T) {
	d := newAcpTestAdapter(t)
	addFakeToolCard(d, "read_file")
	addFakeToolCard(d, "bash")

	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "web", SessionID: "s", RequestID: "r",
	})
	names := abilityNamesOf(d)
	if !names["read_file"] || !names["bash"] {
		t.Error("非 acp 通道默认工具应保留")
	}
}

// TestRefreshAcpRuntimeTools_仅fs能力 验证 fs=true 时只注册 2 个 fs 工具。
func TestRefreshAcpRuntimeTools_仅fs能力(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
	names := abilityNamesOf(d)
	if !names["read_text_file"] || !names["write_text_file"] {
		t.Error("fs=true 应注册 read_text_file/write_text_file")
	}
	for _, n := range []string{"create_terminal", "read_terminal_output", "wait_for_terminal_exit", "release_terminal"} {
		if names[n] {
			t.Errorf("terminal 关闭时 %s 不应注册", n)
		}
	}
}

// TestRefreshAcpRuntimeTools_仅terminal能力 验证 terminal=true 时只注册 4 个终端工具。
func TestRefreshAcpRuntimeTools_仅terminal能力(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"terminal": true},
		},
	})
	names := abilityNamesOf(d)
	for _, n := range []string{"create_terminal", "read_terminal_output", "wait_for_terminal_exit", "release_terminal"} {
		if !names[n] {
			t.Errorf("terminal=true 应注册 %s", n)
		}
	}
	if names["read_text_file"] || names["write_text_file"] {
		t.Error("fs 关闭时 fs 工具不应注册")
	}
}

// TestRefreshAcpRuntimeTools_无能力不注册 验证无 caps 时不注册。
func TestRefreshAcpRuntimeTools_无能力不注册(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
	})
	if names := abilityNamesOf(d); len(names) != 0 {
		t.Errorf("无能力时不应注册任何工具, got %v", names)
	}
}

// TestRefreshAcpRuntimeTools_缺会话标识不注册 验证空 request/session 时不注册。
func TestRefreshAcpRuntimeTools_缺会话标识不注册(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
	if names := abilityNamesOf(d); len(names) != 0 {
		t.Errorf("空 session_id 不应注册, got %v", names)
	}
}

// TestRefreshAcpRuntimeTools_重注册刷新 验证二次调用先移除旧 ACP 工具再注册（幂等）。
func TestRefreshAcpRuntimeTools_重注册刷新(t *testing.T) {
	d := newAcpTestAdapter(t)
	cfg := &runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	}
	d.refreshAcpRuntimeTools(cfg)
	d.refreshAcpRuntimeTools(cfg)
	names := abilityNamesOf(d)
	// 幂等：不重复
	if !names["read_text_file"] || !names["write_text_file"] {
		t.Error("二次调用后 fs 工具应仍在")
	}
}

// TestRefreshAcpRuntimeTools_能力降级移除 验证能力从全开降为 fs 后 terminal 工具被移除。
func TestRefreshAcpRuntimeTools_能力降级移除(t *testing.T) {
	d := newAcpTestAdapter(t)
	// 全开
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true, "terminal": true},
		},
	})
	if names := abilityNamesOf(d); len(names) != 6 {
		t.Fatalf("全开应 6 工具, got %v", names)
	}
	// 降级为仅 fs
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
	names := abilityNamesOf(d)
	if len(names) != 2 {
		t.Errorf("降级后应仅 2 工具, got %v", names)
	}
	if !names["read_text_file"] || !names["write_text_file"] {
		t.Error("fs 工具应保留")
	}
}

// ──────────────────── collectRegisteredAbilityNames 测试 ────────────────────

// TestCollectRegisteredAbilityNames_收集与trim 验证收集与 trim 语义。
func TestCollectRegisteredAbilityNames_收集与trim(t *testing.T) {
	d := newAcpTestAdapter(t)
	addFakeToolCard(d, "read_file")
	got := collectRegisteredAbilityNames(d)
	if len(got) != 1 || got[0] != "read_file" {
		t.Errorf("got = %v", got)
	}
	// nil instance → 空
	if got := collectRegisteredAbilityNames(NewDeepAdapter()); len(got) != 0 {
		t.Errorf("nil instance 应空, got %v", got)
	}
}

// ──────────────────── updateRuntimeConfig 接线测试 ────────────────────

// TestUpdateRuntimeConfig_接线acp刷新 验证 deep/code 两调用点均触发 ACP 刷新。
func TestUpdateRuntimeConfig_接线acp刷新(t *testing.T) {
	// deep 侧
	d := newAcpTestAdapter(t)
	d.updateRuntimeConfig(context.Background(), &runtimeConfig{
		Mode: "agent.plan", ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
	names := abilityNamesOf(d)
	if !names["read_text_file"] {
		t.Error("deep updateRuntimeConfig 应触发 ACP fs 工具注册")
	}

	// code 侧
	c := &CodeAdapter{deep: d}
	// 清空已注册（重新走 code 链）
	for _, ab := range d.instance.AbilityManager().List() {
		d.instance.AbilityManager().Remove(ab.AbilityName())
	}
	c.updateRuntimeConfig(context.Background(), &runtimeConfig{
		Mode: "code", ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{"fs": true},
		},
	})
	names = abilityNamesOf(d)
	if !names["read_text_file"] {
		t.Error("code updateRuntimeConfig 应触发 ACP fs 工具注册")
	}
}

// TestRefreshAcpRuntimeTools_快照日志不panic 验证 acp 通道快照日志路径无 panic。
func TestRefreshAcpRuntimeTools_快照日志不panic(t *testing.T) {
	d := newAcpTestAdapter(t)
	d.refreshAcpRuntimeTools(&runtimeConfig{
		ChannelID: "acp", SessionID: "sess-1", RequestID: "req-1",
		RequestMetadata: map[string]any{
			"acp_client_capabilities": map[string]any{
				"fs": map[string]any{"readTextFile": true},
				"terminal": map[string]any{
					"create": true, "output": true, "waitForExit": true, "release": true,
				},
			},
		},
	})
	names := abilityNamesOf(d)
	if len(names) != 6 {
		t.Errorf("全能力应 6 工具, got %v", names)
	}
	// 排序字段无空名混入
	for n := range names {
		if strings.TrimSpace(n) == "" {
			t.Error("不应有空名能力")
		}
	}
}
