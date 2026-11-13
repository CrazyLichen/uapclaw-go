package routing

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestResolvedRoute_缺省Enabled 校验 ResolvedRoute enabled 缺省 true
// （Python schema.py:41 enabled: bool = True 的语义在构造器中体现）。
func TestResolvedRoute_缺省Enabled(t *testing.T) {
	route := NewResolvedRoute()
	if !route.Enabled {
		t.Fatal("enabled 缺省应为 true")
	}
	if route.RequestOverrides == nil {
		t.Fatal("request_overrides 缺省应为空 map")
	}
}

// TestResolvedRoutingConfig_Extra保留 校验 extra="allow" 的多余键保留语义
// （Python schema.py:66 model_config = {"extra": "allow"}）。
func TestResolvedRoutingConfig_Extra保留(t *testing.T) {
	data := []byte(`{"strategy":"ordered-failover","custom_key":"v1","n":42}`)
	var cfg ResolvedRoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Strategy == nil || *cfg.Strategy != "ordered-failover" {
		t.Fatalf("strategy 解析错误：%v", cfg.Strategy)
	}
	if v, ok := cfg.Extra["custom_key"].(string); !ok || v != "v1" {
		t.Fatalf("extra 键丢失：%v", cfg.Extra)
	}
	if v, ok := cfg.Extra["n"].(float64); !ok || v != 42 {
		t.Fatalf("extra 数值键丢失：%v", cfg.Extra)
	}
}

// TestCompiledModelSelection_序列化Tag 校验 JSON 键名对齐 Python
// schema.py:78-84 CompiledModelSelection。
func TestCompiledModelSelection_序列化Tag(t *testing.T) {
	c := CompiledModelSelection{
		SelectedType: "model_group",
		SelectedID:   "g1",
	}
	b, _ := json.Marshal(c)
	for _, key := range []string{`"selected_type":"model_group"`, `"selected_id":"g1"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("缺少键 %s：%s", key, b)
		}
	}
}

// TestRoutingStrategyName_取值 校验枚举字符串值对齐 Python schema.py:48-53。
func TestRoutingStrategyName_取值(t *testing.T) {
	if string(RoutingStrategyOrderedFailover) != "ordered-failover" {
		t.Fatal("ordered-failover 值漂移")
	}
	if string(RoutingStrategyTagFiltered) != "tag-filtered" {
		t.Fatal("tag-filtered 值漂移")
	}
}
