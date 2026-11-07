package adapter

import (
	"reflect"
	"testing"

	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
)

// TestResolveSessionMemoryConfig_三态 覆盖 Python _resolve_session_memory_config 的取值语义。
func TestResolveSessionMemoryConfig_三态(t *testing.T) {
	cases := []struct {
		name        string
		ctxCfg      map[string]any
		wantEnabled bool
		wantMode    string
	}{
		{
			name:        "缺省关闭",
			ctxCfg:      map[string]any{},
			wantEnabled: false,
		},
		{
			name:        "nil 配置关闭",
			ctxCfg:      nil,
			wantEnabled: false,
		},
		{
			name:        "true 用默认配置",
			ctxCfg:      map[string]any{"session_memory_config": true},
			wantEnabled: true,
			wantMode:    "agent_edit",
		},
		{
			name:        "false 关闭",
			ctxCfg:      map[string]any{"session_memory_config": false},
			wantEnabled: false,
		},
		{
			name:        "dict 覆盖",
			ctxCfg:      map[string]any{"session_memory_config": map[string]any{"update_mode": "direct_replace", "trigger_tokens": 20000}},
			wantEnabled: true,
			wantMode:    "direct_replace",
		},
		{
			name: "空 dict 回退到 session_memory",
			ctxCfg: map[string]any{
				"session_memory_config": map[string]any{},
				"session_memory":        map[string]any{"trigger_tokens": 30000},
			},
			wantEnabled: true,
			wantMode:    "agent_edit",
		},
		{
			name:        "非法类型关闭",
			ctxCfg:      map[string]any{"session_memory_config": "yes"},
			wantEnabled: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, ok := resolveSessionMemoryConfig(tc.ctxCfg)
			if ok != tc.wantEnabled {
				t.Fatalf("enabled = %v, want %v", ok, tc.wantEnabled)
			}
			if !ok {
				if cfg != nil {
					t.Errorf("关闭时不应返回配置，实际 %+v", cfg)
				}
				return
			}
			if tc.wantMode != "" && cfg.UpdateMode != tc.wantMode {
				t.Errorf("UpdateMode = %q, want %q", cfg.UpdateMode, tc.wantMode)
			}
		})
	}
}

// TestBuildContextProcessorRail_SessionMemory开关透传 验证 rail 构造时读取 session memory 配置。
func TestBuildContextProcessorRail_SessionMemory开关透传(t *testing.T) {
	d := NewDeepAdapter()

	reactCfg := map[string]any{
		"context_engine_config": map[string]any{
			"message_summary_offloader_config": map[string]any{"messages_threshold": nil},
			"full_compact_processor_config":    map[string]any{"trigger_total_tokens": 180000},
		},
	}
	rail, ok := d.buildContextProcessorRail(reactCfg).(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatal("buildContextProcessorRail 未返回 *ContextProcessorRail")
	}
	if rail.SessionMemoryEnabled() {
		t.Error("模板未配置 session_memory 时不应启用")
	}
	if got := rail.UserProcessorTypes(); len(got) != 2 {
		t.Errorf("用户处理器数量 = %d, want 2（%v）", len(got), got)
	}

	reactCfg["context_engine_config"].(map[string]any)["session_memory_config"] = true
	rail2, ok := d.buildContextProcessorRail(reactCfg).(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatal("buildContextProcessorRail 未返回 *ContextProcessorRail")
	}
	if !rail2.SessionMemoryEnabled() {
		t.Error("session_memory_config=true 时应启用 session memory")
	}
}

// TestFirstTruthyValue_复刻PythonOr语义 覆盖各类型的真假判定。
func TestFirstTruthyValue_复刻PythonOr语义(t *testing.T) {
	cases := []struct {
		name   string
		values []any
		want   any
	}{
		{"全空返回 nil", []any{nil, false, "", map[string]any{}}, nil},
		{"字符串", []any{"x"}, "x"},
		{"map", []any{map[string]any{"a": 1}}, map[string]any{"a": 1}},
		{"slice", []any{[]any{1}}, []any{1}},
		{"int 非零", []any{2}, 2},
		{"int64 非零", []any{int64(3)}, int64(3)},
		{"int 零跳过", []any{0, "fallback"}, "fallback"},
		{"int64 零跳过", []any{int64(0), "fallback"}, "fallback"},
		{"float64 非零", []any{1.5}, 1.5},
		{"float64 零跳过", []any{float64(0), "fallback"}, "fallback"},
		{"其它类型原样返回", []any{struct{}{}}, struct{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstTruthyValue(tc.values...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("firstTruthyValue = %v (%T), want %v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

// TestResolveSessionMemoryConfig_非法dict降级 非法字段导致解析失败 → 关闭并告警。
func TestResolveSessionMemoryConfig_非法dict降级(t *testing.T) {
	cfg, ok := resolveSessionMemoryConfig(map[string]any{
		"session_memory_config": map[string]any{"update_mode": "unknown-mode"},
	})
	if ok || cfg != nil {
		t.Fatalf("非法配置应降级为关闭，实际 ok=%v cfg=%+v", ok, cfg)
	}
}

// TestGetSkillCreateEnabled_对齐Python 环境变量优先 → react.evolution.skill_create → 默认 false。
func TestGetSkillCreateEnabled_对齐Python(t *testing.T) {
	d := NewDeepAdapter()

	t.Setenv("SKILL_CREATE", "")
	if d.getSkillCreateEnabled() {
		t.Error("缺省应关闭（Python 默认 false）")
	}

	d.setConfigSnapshots(map[string]any{"evolution": map[string]any{"skill_create": true}}, nil)
	if !d.getSkillCreateEnabled() {
		t.Error("react.evolution.skill_create=true 应启用")
	}

	t.Setenv("SKILL_CREATE", "false")
	if d.getSkillCreateEnabled() {
		t.Error("环境变量 SKILL_CREATE=false 应否决配置")
	}

	t.Setenv("SKILL_CREATE", "yes")
	if !d.getSkillCreateEnabled() {
		t.Error("环境变量 SKILL_CREATE=yes 应启用")
	}
}

// TestGetLanguage_配置读取 覆盖配置命中与缺省值。
// 注：min_team_members_for_create 随单 Agent SkillCreateRail 移植一并移除
// （Python 的 SkillCreateRail 无此参数），相关断言同步删除。
func TestGetLanguage_配置读取(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.getLanguage(); got != "cn" {
		t.Errorf("缺省语言 = %q, want cn", got)
	}

	d.setConfigSnapshots(map[string]any{"language": "en"}, nil)
	if got := d.getLanguage(); got != "en" {
		t.Errorf("language = %q, want en", got)
	}
}
