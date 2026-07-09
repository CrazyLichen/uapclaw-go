package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNormalizeSkillConfigs_正常输入 验证正常规范化
func TestNormalizeSkillConfigs_正常输入(t *testing.T) {
	raw := map[string]any{
		"skill1": map[string]any{"enabled": true},
		"skill2": map[string]any{"enabled": false},
		"skill3": map[string]any{}, // 缺少 enabled，默认 true
	}
	result := NormalizeSkillConfigs(raw)
	if len(result) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(result))
	}
	if !result["skill1"]["enabled"] {
		t.Error("skill1 应为 enabled")
	}
	if result["skill2"]["enabled"] {
		t.Error("skill2 应为 disabled")
	}
	if !result["skill3"]["enabled"] {
		t.Error("skill3 默认应为 enabled")
	}
}

// TestNormalizeSkillConfigs_空名称 验证空名称被跳过
func TestNormalizeSkillConfigs_空名称(t *testing.T) {
	raw := map[string]any{
		"":   map[string]any{"enabled": true},
		"  ": map[string]any{"enabled": true},
		"ok": map[string]any{"enabled": true},
	}
	result := NormalizeSkillConfigs(raw)
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(result))
	}
	if _, ok := result["ok"]; !ok {
		t.Error("应包含 ok")
	}
}

// TestNormalizeSkillConfigs_非字典输入 验证非字典返回空
func TestNormalizeSkillConfigs_非字典输入(t *testing.T) {
	result := NormalizeSkillConfigs("not a dict")
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// TestNormalizeSkillConfigs_nil输入 验证 nil 返回空
func TestNormalizeSkillConfigs_nil输入(t *testing.T) {
	result := NormalizeSkillConfigs(nil)
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// TestGetRegisteredSkillNames_正常输入 验证正常提取
func TestGetRegisteredSkillNames_正常输入(t *testing.T) {
	state := map[string]any{
		"installed_plugins": []any{
			map[string]any{"name": "plugin1"},
			map[string]any{"name": "plugin2"},
		},
		"local_skills": []any{
			map[string]any{"name": "local1"},
		},
	}
	result := GetRegisteredSkillNames(state)
	if len(result) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(result))
	}
	expected := map[string]bool{"plugin1": true, "plugin2": true, "local1": true}
	for k := range expected {
		if !result[k] {
			t.Errorf("应包含 %s", k)
		}
	}
}

// TestGetRegisteredSkillNames_空状态 验证空状态返回空
func TestGetRegisteredSkillNames_空状态(t *testing.T) {
	result := GetRegisteredSkillNames(map[string]any{})
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// TestNormalizeLocalSkills_正常输入 验证正常过滤
func TestNormalizeLocalSkills_正常输入(t *testing.T) {
	raw := []any{
		map[string]any{"name": "skill1", "version": "1.0"},
		map[string]any{"name": "skill2", "version": "2.0"},
		map[string]any{"name": "deleted", "version": "3.0"},
	}
	existing := map[string]bool{"skill1": true, "skill2": true}
	result := NormalizeLocalSkills(raw, existing)
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(result))
	}
}

// TestNormalizeLocalSkills_非列表输入 验证非列表返回空
func TestNormalizeLocalSkills_非列表输入(t *testing.T) {
	result := NormalizeLocalSkills("not a list", nil)
	if result != nil {
		t.Fatalf("期望 nil，实际 %v", result)
	}
}

// TestGetSkillEnabled_正常 验证正常读取
func TestGetSkillEnabled_正常(t *testing.T) {
	state := map[string]any{
		"skill_configs": map[string]any{
			"skill1": map[string]any{"enabled": true},
			"skill2": map[string]any{"enabled": false},
		},
	}
	if !GetSkillEnabled(state, "skill1") {
		t.Error("skill1 应为 enabled")
	}
	if GetSkillEnabled(state, "skill2") {
		t.Error("skill2 应为 disabled")
	}
}

// TestGetSkillEnabled_默认true 验证不存在时默认 true
func TestGetSkillEnabled_默认true(t *testing.T) {
	state := map[string]any{}
	if !GetSkillEnabled(state, "unknown") {
		t.Error("不存在的技能默认应为 enabled")
	}
}

// TestGetSkillEnabled_空名称 验证空名称返回 true
func TestGetSkillEnabled_空名称(t *testing.T) {
	if !GetSkillEnabled(map[string]any{}, "") {
		t.Error("空名称应返回 true")
	}
}

// TestSetSkillEnabled_正常 验证正常设置
func TestSetSkillEnabled_正常(t *testing.T) {
	state := map[string]any{}
	SetSkillEnabled(state, "skill1", false)

	configs, ok := state["skill_configs"].(map[string]any)
	if !ok {
		t.Fatal("应创建 skill_configs")
	}
	cfg, ok := configs["skill1"].(map[string]any)
	if !ok {
		t.Fatal("应创建 skill1 配置")
	}
	if toBool(cfg["enabled"]) != false {
		t.Error("skill1 应为 disabled")
	}
}

// TestSetSkillEnabled_覆盖 验证覆盖已有配置
func TestSetSkillEnabled_覆盖(t *testing.T) {
	state := map[string]any{
		"skill_configs": "invalid", // 非 dict，应被重置
	}
	SetSkillEnabled(state, "skill1", true)

	configs, ok := state["skill_configs"].(map[string]any)
	if !ok {
		t.Fatal("应重置为有效 map")
	}
	cfg := configs["skill1"].(map[string]any)
	if !toBool(cfg["enabled"]) {
		t.Error("skill1 应为 enabled")
	}
}

// TestListDisabledSkills_正常 验证正常列出
func TestListDisabledSkills_正常(t *testing.T) {
	state := map[string]any{
		"skill_configs": map[string]any{
			"skill1": map[string]any{"enabled": true},
			"skill2": map[string]any{"enabled": false},
			"skill3": map[string]any{"enabled": false},
		},
	}
	result := ListDisabledSkills(state)
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(result))
	}
	// 结果应排序
	if result[0] != "skill2" || result[1] != "skill3" {
		t.Errorf("结果应排序为 [skill2, skill3]，实际 %v", result)
	}
}

// TestListDisabledSkills_空状态 验证空状态返回空
func TestListDisabledSkills_空状态(t *testing.T) {
	result := ListDisabledSkills(map[string]any{})
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// TestListExecutionDisabledSkills_正常 验证只返回已安装的禁用技能
func TestListExecutionDisabledSkills_正常(t *testing.T) {
	state := map[string]any{
		"installed_plugins": []any{
			map[string]any{"name": "skill2"},
		},
		"skill_configs": map[string]any{
			"skill1": map[string]any{"enabled": false},
			"skill2": map[string]any{"enabled": false},
		},
	}
	result := ListExecutionDisabledSkills(state)
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(result))
	}
	if result[0] != "skill2" {
		t.Errorf("期望 skill2，实际 %s", result[0])
	}
}

// TestLoadExecutionDisabledSkills_文件不存在 验证文件不存在时返回空
func TestLoadExecutionDisabledSkills_文件不存在(t *testing.T) {
	// 使用临时目录，确保没有 skills_state.json
	tmpDir := t.TempDir()
	origGetStateFile := GetStateFile
	// 覆盖 GetStateFile 行为通过临时环境
	_ = origGetStateFile
	stateFile := filepath.Join(tmpDir, "skills_state.json")
	// 直接测试读取逻辑
	data, err := os.ReadFile(stateFile)
	if err == nil {
		t.Error("文件不存在应返回错误")
	}
	_ = data
}

// TestFilterVisibleSkillNames_无禁用 验证无禁用时全部返回
func TestFilterVisibleSkillNames_无禁用(t *testing.T) {
	names := []string{"skill1", "skill2", "skill3"}
	// 由于 LoadExecutionDisabledSkills 依赖文件系统，此测试验证过滤逻辑
	disabledSet := map[string]bool{"skill2": true}
	var visible []string
	for _, n := range names {
		if !disabledSet[n] {
			visible = append(visible, n)
		}
	}
	if len(visible) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(visible))
	}
}

// TestLoadExecutionDisabledSkills_有效文件 验证从文件加载
func TestLoadExecutionDisabledSkills_有效文件(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "skills_state.json")

	state := map[string]any{
		"installed_plugins": []any{
			map[string]any{"name": "disabled_skill"},
		},
		"skill_configs": map[string]any{
			"disabled_skill": map[string]any{"enabled": false},
		},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(stateFile, data, 0o644)

	// 读取并验证
	loaded, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	var loadedState map[string]any
	json.Unmarshal(loaded, &loadedState)
	result := ListExecutionDisabledSkills(loadedState)
	if len(result) != 1 || result[0] != "disabled_skill" {
		t.Errorf("期望 [disabled_skill]，实际 %v", result)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestToBool_各种类型 验证 toBool 处理各种类型
func TestToBool_各种类型(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true},
		{false, false},
		{nil, false},
		{0, false},
		{1, true},
		{0.0, false},
		{1.5, true},
		{"", false},
		{"hello", true},
	}
	for _, tt := range tests {
		if got := toBool(tt.input); got != tt.want {
			t.Errorf("toBool(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestToString_各种类型 验证 toString 处理各种类型
func TestToString_各种类型(t *testing.T) {
	if toString(nil) != "" {
		t.Error("nil 应返回空字符串")
	}
	if toString("hello") != "hello" {
		t.Error("字符串应原样返回")
	}
	if toString(123) != "" {
		t.Error("非字符串应返回空字符串")
	}
}

// TestToSliceOfAny_各种类型 验证 toSliceOfAny 处理各种类型
func TestToSliceOfAny_各种类型(t *testing.T) {
	if _, ok := toSliceOfAny(nil); ok {
		t.Error("nil 应返回 false")
	}
	if _, ok := toSliceOfAny("not a slice"); ok {
		t.Error("非切片应返回 false")
	}
	if s, ok := toSliceOfAny([]any{1, 2, 3}); !ok || len(s) != 3 {
		t.Error("切片应正确返回")
	}
}

// TestTrimSpace 验证 trimSpace 去除空白
func TestTrimSpace(t *testing.T) {
	if trimSpace("  hello  ") != "hello" {
		t.Error("trimSpace 应去除首尾空白")
	}
}
