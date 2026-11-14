package common

import (
	"encoding/json"
	"testing"
)

// 数据表机器核对：总数与三套餐分组数。
// Python: model_vendor_registry.py:112-521（2026-11-28 grep -c 'VendorPreset(' 核对：
// total 21 / token_plan 5 / coding_plan 5 / custom_api 11）。
func TestVendorRegistry_数据表机器核对(t *testing.T) {
	all := GetAllPresets()
	if len(all) != 21 {
		t.Fatalf("预设总数应为 21，实际 %d", len(all))
	}
	// 机器核对：5/5/11
	checks := []struct {
		plan PlanKind
		want int
	}{
		{PlanKindTokenPlan, 5},
		{PlanKindCodingPlan, 5},
		{PlanKindCustomAPI, 11},
	}
	for _, c := range checks {
		if got := len(GetPresetsByPlan(c.plan)); got != c.want {
			t.Errorf("plan %s 应有 %d 条，实际 %d", c.plan, c.want, got)
		}
	}
}

// GetPreset：命中与未命中；字段值抽查（xlsx 勘误域名）。
func TestVendorRegistry_GetPreset_命中与字段(t *testing.T) {
	p := GetPreset("alibaba", PlanKindCodingPlan)
	if p == nil {
		t.Fatal("alibaba/coding_plan 应存在")
	}
	// xlsx 写 aliyun.com 是笔误，注册的是验证过的 aliyuncs.com
	if p.APIBase != "https://coding.dashscope.aliyuncs.com/v1" {
		t.Errorf("Coding Plan 基址应为验证过的域名: %s", p.APIBase)
	}
	if p.EndpointProfile != "dashscope" || p.DefaultModel != "qwen3-coder-next" || p.IconKey != "qwen" {
		t.Errorf("字段抽查不符: %v", p)
	}
	if GetPreset("alibaba", "no_such_plan") != nil {
		t.Errorf("未知 plan 应返回 nil")
	}
	if GetPreset("no_such_vendor", PlanKindCustomAPI) != nil {
		t.Errorf("未知厂商应返回 nil")
	}
}

// GetPresetByVendorKey：每厂商首个预设（图标查询）。
func TestVendorRegistry_ByVendorKey_首个预设(t *testing.T) {
	p := GetPresetByVendorKey("alibaba")
	if p == nil || p.Plan != PlanKindTokenPlan {
		t.Errorf("alibaba 首个预设应为 Token Plan 条目: %v", p)
	}
	if GetPresetByVendorKey("vllm-omni") == nil {
		t.Errorf("vllm-omni 应存在")
	}
}

// 智谱 Coding Plan 不开放 Anthropic（1309 误路由注释）。
func TestVendorRegistry_ZhipuCodingAnthropic禁用(t *testing.T) {
	p := GetPreset("zhipu", PlanKindCodingPlan)
	if p.AnthropicBase != "" {
		t.Errorf("zhipu/coding_plan 的 anthropic_base 应为空: %q", p.AnthropicBase)
	}
}

// ToFrontendPayload：分组与卡片键名契约。
// 对齐 Python: model_vendor_registry.py:570-593。
func TestVendorRegistry_ToFrontendPayload_契约键名(t *testing.T) {
	payload := ToFrontendPayload()
	if _, ok := payload["reasoning"]; !ok {
		t.Errorf("载荷应含 reasoning 目录")
	}
	for _, plan := range []string{"token_plan", "coding_plan", "custom_api"} {
		cards, ok := payload[plan].([]map[string]any)
		if !ok || len(cards) == 0 {
			t.Fatalf("plan %s 应有卡片", plan)
		}
		card := cards[0]
		for _, key := range []string{
			"vendor_key", "display_name", "plan", "client_provider", "api_base",
			"endpoint_profile", "default_model", "model_options", "icon_key",
			"models_endpoint", "models_needs_key", "generation_only",
			"reasoning_capabilities", "reasoning_rules",
			"supports_anthropic", "anthropic_base", "anthropic_client_provider",
		} {
			if _, ok := card[key]; !ok {
				t.Errorf("卡片缺少契约键 %q", key)
			}
		}
	}
	// vllm-omni：generation_only 且 anthropic_client_provider 为 nil
	for _, card := range payload["custom_api"].([]map[string]any) {
		if card["vendor_key"] == "vllm-omni" {
			if card["generation_only"] != true {
				t.Errorf("vllm-omni 应 generation_only")
			}
		}
		if card["vendor_key"] == "zhipu" {
			if card["supports_anthropic"] != false || card["anthropic_client_provider"] != nil {
				t.Errorf("zhipu/custom_api 的 anthropic_base 为空 → 不支持切换")
			}
		}
	}
}

// JSON 序列化键名快照（前端消费的 card 键在 JSON 下不漂移）。
func TestVendorRegistry_卡片JSON快照(t *testing.T) {
	payload := ToFrontendPayload()
	raw, err := json.Marshal(payload["token_plan"].([]map[string]any)[0])
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	if asMap["vendor_key"] != "alibaba" {
		t.Errorf("vendor_key 键值应保留: %v", asMap["vendor_key"])
	}
}

// NormalizeArkAPIBase：合法区域基址、host 根、Coding Plan 拒绝。
// 对齐 Python: volcengine_endpoints.py:14-32。
func TestNormalizeArkAPIBase_归一与拒绝(t *testing.T) {
	got, err := NormalizeArkAPIBase("https://ark.cn-beijing.volces.com/api/v3")
	if err != nil || got != "https://ark.cn-beijing.volces.com/api/v3" {
		t.Errorf("标准基址应原样: %v %v", got, err)
	}
	got, err = NormalizeArkAPIBase("https://ark.cn-beijing.volces.com")
	if err != nil || got != "https://ark.cn-beijing.volces.com/api/v3" {
		t.Errorf("host 根应补 /api/v3: %v %v", got, err)
	}
	got, err = NormalizeArkAPIBase("https://ark.cn-beijing.volces.com/")
	if err != nil || got != "https://ark.cn-beijing.volces.com/api/v3" {
		t.Errorf("尾部斜杠应容忍: %v %v", got, err)
	}
	if _, err := NormalizeArkAPIBase(ArkCodingAPIBase); err == nil {
		t.Errorf("Coding Plan 应拒绝")
	}
	if _, err := NormalizeArkAPIBase("https://evil.example.com/api/v3"); err == nil {
		t.Errorf("非 ark 域名应拒绝")
	}
	if _, err := NormalizeArkAPIBase("http://ark.cn-beijing.volces.com/api/v3"); err == nil {
		t.Errorf("非 https 应拒绝")
	}
}
