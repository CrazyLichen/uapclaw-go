package tools

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
)

// catalogInfo 构造内置工具投影（真实 ToolInfo，非替身）。
func catalogInfo(name, desc string) *schema.ToolInfo {
	return &schema.ToolInfo{Name: name, Description: desc}
}

// catalogMcpInfo 构造 MCP 投影（isinstance McpToolInfo 等价物）。
func catalogMcpInfo(name, desc, server string) *schema.McpToolInfo {
	return &schema.McpToolInfo{ToolInfo: schema.ToolInfo{Name: name, Description: desc}, ServerName: server}
}

// TestDiscoveryMetadata_命中内置表 验证表内名字返回元数据、未知名字返回 nil。
func TestDiscoveryMetadata_命中内置表(t *testing.T) {
	if m := DiscoveryMetadata("cron_create_job"); m == nil {
		t.Fatal("cron_create_job 应命中内置表")
	} else if len(m.AliasesZh) == 0 || m.DisplayEn == "" {
		t.Fatalf("元数据字段不完整: %+v", m)
	}
	if m := DiscoveryMetadata("not_a_builtin_tool"); m != nil {
		t.Fatalf("未知工具不应命中: %+v", m)
	}
	if m := DiscoveryMetadata(""); m != nil {
		t.Fatalf("空名不应命中: %+v", m)
	}
}

// TestBuiltinToolDiscovery_全表不裁剪 验证表条目数与 Python 源一致（93 条）。
func TestBuiltinToolDiscovery_全表不裁剪(t *testing.T) {
	if len(builtinToolDiscovery) != 93 {
		t.Fatalf("内置投影表应 93 条, got %d", len(builtinToolDiscovery))
	}
	// 抽查首尾条目存在
	for _, name := range []string{"coding_memory_edit", "xiaoyi_gui_agent", "cron", "heartbeat_create_job"} {
		if _, ok := builtinToolDiscovery[name]; !ok {
			t.Fatalf("表缺条目 %s", name)
		}
	}
}

// TestDiscoveryDescription_中文回落原文 对齐 Python discovery_description：
// zh（及一切非 en）一律返回原始描述。
func TestDiscoveryDescription_中文回落原文(t *testing.T) {
	info := catalogInfo("cron_create_job", "创建定时任务")
	if got := DiscoveryDescription(info, "zh"); got != "创建定时任务" {
		t.Fatalf("zh 应返回原文, got %q", got)
	}
	if got := DiscoveryDescription(info, "jp"); got != "创建定时任务" {
		t.Fatalf("非 en 一律原文, got %q", got)
	}
}

// TestDiscoveryDescription_英文取投影 对齐 Python：en 且命中内置表时返回 display_en。
func TestDiscoveryDescription_英文取投影(t *testing.T) {
	info := catalogInfo("cron_create_job", "创建定时任务")
	if got := DiscoveryDescription(info, "en"); got == "创建定时任务" || got == "" {
		t.Fatalf("en 应取 display_en 投影, got %q", got)
	}
	// 未知工具 en 回落原文
	info2 := catalogInfo("unknown_x", "原始描述")
	if got := DiscoveryDescription(info2, "en"); got != "原始描述" {
		t.Fatalf("未知工具 en 应回落原文, got %q", got)
	}
}

// TestDiscoveryDescription_Mcp恒原文 对齐 Python：McpToolInfo 一律返回原始描述。
func TestDiscoveryDescription_Mcp恒原文(t *testing.T) {
	mcp := catalogMcpInfo("cron_create_job", "MCP 原始描述", "github")
	if got := DiscoveryDescription(mcp, "en"); got != "MCP 原始描述" {
		t.Fatalf("MCP 投影 en 也应返回原文, got %q", got)
	}
}

// TestDiscoverySearchText_双语拼接 对齐 Python discovery_search_text：
// 内置工具 = display_en + aliases_zh 空格 join；MCP 恒空串；未知工具空串。
func TestDiscoverySearchText_双语拼接(t *testing.T) {
	info := catalogInfo("cron_create_job", "创建定时任务")
	got := DiscoverySearchText(info)
	for _, want := range []string{"Create", "定时任务", "提醒我", "闹钟"} {
		if !strings.Contains(got, want) {
			t.Fatalf("search_text 缺 %q: %q", want, got)
		}
	}
	if got := DiscoverySearchText(catalogInfo("unknown_x", "d")); got != "" {
		t.Fatalf("未知工具 search_text 应为空, got %q", got)
	}
	if got := DiscoverySearchText(catalogMcpInfo("cron_create_job", "d", "github")); got != "" {
		t.Fatalf("MCP 投影 search_text 应为空, got %q", got)
	}
}

// TestApplyDiscoveryMetadata_属性注入 对齐 Python apply_discovery_metadata：
// 内置 ToolCard 注入 progressive_discovery 属性；MCP 卡与未知工具原样返回。
func TestApplyDiscoveryMetadata_属性注入(t *testing.T) {
	card := &tool.ToolCard{BaseCard: schema.BaseCard{Name: "cron_create_job", Description: "创建定时任务"}}
	out := ApplyDiscoveryMetadata(card)
	props, ok := out.Properties[ProgressiveDiscoveryProperty].(map[string]any)
	if !ok {
		t.Fatalf("未注入 %s 属性: %+v", ProgressiveDiscoveryProperty, out.Properties)
	}
	display, _ := props["display"].(map[string]any)
	if display["en"] == "" {
		t.Fatalf("display.en 缺失: %+v", props)
	}
	if aliases, _ := props["aliases"].([]string); len(aliases) == 0 {
		t.Fatalf("aliases 缺失: %+v", props)
	}
	// 未知工具原样返回（同指针）
	unknown := &tool.ToolCard{BaseCard: schema.BaseCard{Name: "unknown_x", Description: "d"}}
	if ApplyDiscoveryMetadata(unknown) != unknown {
		t.Fatal("未知工具应原样返回")
	}
}
