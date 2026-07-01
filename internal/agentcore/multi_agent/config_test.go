package multi_agent

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamConfig_默认值 验证默认值 MaxAgents=10, MaxConcurrentMessages=100, MessageTimeout=30.0。
//
// 对应 Python: TeamConfig() 默认值
func TestNewTeamConfig_默认值(t *testing.T) {
	cfg := schema.NewTeamConfig()
	if cfg.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents=10，实际 %d", cfg.MaxAgents)
	}
	if cfg.MaxConcurrentMessages != 100 {
		t.Errorf("期望 MaxConcurrentMessages=100，实际 %d", cfg.MaxConcurrentMessages)
	}
	if cfg.MessageTimeout != 30.0 {
		t.Errorf("期望 MessageTimeout=30.0，实际 %f", cfg.MessageTimeout)
	}
}

// TestTeamConfig_链式配置 验证 ConfigureMaxAgents/ConfigureTimeout/ConfigureConcurrency 链式调用。
//
// 对应 Python: TeamConfig().configure_max_agents(5).configure_timeout(60.0).configure_concurrency(200)
func TestTeamConfig_链式配置(t *testing.T) {
	cfg := schema.NewTeamConfig().
		ConfigureMaxAgents(5).
		ConfigureTimeout(60.0).
		ConfigureConcurrency(200)

	if cfg.MaxAgents != 5 {
		t.Errorf("期望 MaxAgents=5，实际 %d", cfg.MaxAgents)
	}
	if cfg.MessageTimeout != 60.0 {
		t.Errorf("期望 MessageTimeout=60.0，实际 %f", cfg.MessageTimeout)
	}
	if cfg.MaxConcurrentMessages != 200 {
		t.Errorf("期望 MaxConcurrentMessages=200，实际 %d", cfg.MaxConcurrentMessages)
	}
}

// TestTeamConfig_链式配置_返回自身 验证链式方法返回 *TeamConfig 指针。
//
// 对应 Python: configure_xxx() -> self 的链式语义
func TestTeamConfig_链式配置_返回自身(t *testing.T) {
	cfg := schema.NewTeamConfig()
	ptr1 := cfg.ConfigureMaxAgents(3)
	if ptr1 != cfg {
		t.Error("ConfigureMaxAgents 应返回自身指针")
	}
	ptr2 := cfg.ConfigureTimeout(10.0)
	if ptr2 != cfg {
		t.Error("ConfigureTimeout 应返回自身指针")
	}
	ptr3 := cfg.ConfigureConcurrency(50)
	if ptr3 != cfg {
		t.Error("ConfigureConcurrency 应返回自身指针")
	}
}

// TestTeamConfig_Extra 验证 SetExtra/GetExtra 读写。
//
// 对应 Python: model_config={"extra": "allow"} 允许动态额外字段
func TestTeamConfig_Extra(t *testing.T) {
	cfg := schema.NewTeamConfig()

	// 不存在的 key
	val, ok := cfg.GetExtra("not_exist")
	if ok {
		t.Error("不存在的 key 不应返回 ok=true")
	}
	if val != nil {
		t.Errorf("不存在的 key 应返回 nil，实际 %v", val)
	}

	// 设置后读取
	cfg.SetExtra("custom_key", "custom_value")
	val, ok = cfg.GetExtra("custom_key")
	if !ok {
		t.Error("已设置的 key 应返回 ok=true")
	}
	if val != "custom_value" {
		t.Errorf("期望 'custom_value'，实际 %v", val)
	}

	// 覆盖
	cfg.SetExtra("custom_key", 42)
	val, ok = cfg.GetExtra("custom_key")
	if !ok {
		t.Error("覆盖后应返回 ok=true")
	}
	if val != 42 {
		t.Errorf("期望 42，实际 %v", val)
	}
}

// TestTeamConfig_JSON序列化 验证 JSON marshal/unmarshal（Extra 不序列化）。
func TestTeamConfig_JSON序列化(t *testing.T) {
	cfg := schema.NewTeamConfig()
	cfg.SetExtra("secret", "value")

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded schema.TeamConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents=10，实际 %d", decoded.MaxAgents)
	}
	// Extra 不参与 JSON 序列化（json:"-"）
	if decoded.Extra != nil {
		t.Errorf("Extra 应为 nil（不参与 JSON 反序列化），实际 %v", decoded.Extra)
	}
}

// TestTeamConfig_JSON序列化_omitempty 验证非零值字段出现在 JSON 中。
func TestTeamConfig_JSON序列化_omitempty(t *testing.T) {
	cfg := schema.NewTeamConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["max_agents"]; !ok {
		t.Error("max_agents 应出现在 JSON 中（非零值）")
	}
}
