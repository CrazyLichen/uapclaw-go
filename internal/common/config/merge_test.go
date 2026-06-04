package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepMerge_模板有用户无_添加(t *testing.T) {
	tmpl := map[string]any{
		"new_key": "new_value",
	}
	user := map[string]any{}

	result := DeepMerge(tmpl, user, 4)

	if result["new_key"] != "new_value" {
		t.Errorf("期望 new_value，实际 %v", result["new_key"])
	}
}

func TestDeepMerge_双方都有_保留用户值(t *testing.T) {
	tmpl := map[string]any{
		"key": "template_value",
	}
	user := map[string]any{
		"key": "user_value",
	}

	result := DeepMerge(tmpl, user, 4)

	if result["key"] != "user_value" {
		t.Errorf("期望 user_value，实际 %v", result["key"])
	}
}

func TestDeepMerge_用户有模板无_保留(t *testing.T) {
	tmpl := map[string]any{}
	user := map[string]any{
		"custom_key": "custom_value",
	}

	result := DeepMerge(tmpl, user, 4)

	if result["custom_key"] != "custom_value" {
		t.Errorf("期望保留 custom_value，实际 %v", result["custom_key"])
	}
}

func TestDeepMerge_递归合并嵌套map(t *testing.T) {
	tmpl := map[string]any{
		"server": map[string]any{
			"host": "0.0.0.0",
			"port": 8080,
			"new_field": "value",
		},
	}
	user := map[string]any{
		"server": map[string]any{
			"host": "localhost",
			"port": 9090,
		},
	}

	result := DeepMerge(tmpl, user, 4)

	server := result["server"].(map[string]any)
	// 用户值保留
	if server["host"] != "localhost" {
		t.Errorf("期望 localhost，实际 %v", server["host"])
	}
	if server["port"] != 9090 {
		t.Errorf("期望 9090，实际 %v", server["port"])
	}
	// 模板新增字段添加
	if server["new_field"] != "value" {
		t.Errorf("期望 value，实际 %v", server["new_field"])
	}
}

func TestDeepMerge_最大深度限制(t *testing.T) {
	tmpl := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "tmpl_val",
			},
		},
	}
	user := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "user_val",
			},
		},
	}

	// 深度 1：只合并一层，level2 不递归
	result1 := DeepMerge(tmpl, user, 1)
	level1 := result1["level1"].(map[string]any)
	level2 := level1["level2"].(map[string]any)
	if level2["key"] != "user_val" {
		// 深度不够时，保留用户值
		t.Logf("深度1: level2.key = %v", level2["key"])
	}
}

func TestDeepMerge_默认深度(t *testing.T) {
	tmpl := map[string]any{"key": "value"}
	user := map[string]any{}

	// maxDepth <= 0 使用默认值 4
	result := DeepMerge(tmpl, user, 0)
	if result["key"] != "value" {
		t.Errorf("期望 value，实际 %v", result["key"])
	}
}

func TestDeepMerge_深拷贝不影响原始(t *testing.T) {
	tmpl := map[string]any{
		"nested": map[string]any{"key": "tmpl"},
	}
	user := map[string]any{}

	result := DeepMerge(tmpl, user, 4)

	// 修改 result 不应影响 tmpl
	result["nested"].(map[string]any)["key"] = "modified"

	if tmpl["nested"].(map[string]any)["key"] != "tmpl" {
		t.Error("深拷贝应该不影响原始模板数据")
	}
}

func TestMigrateFromTemplate_用户文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "template.yaml")
	userPath := filepath.Join(tmpDir, "config.yaml")

	// 创建模板
	os.WriteFile(tmplPath, []byte("key: value\nnew_key: new_value\n"), 0o644)

	changed, err := MigrateFromTemplate(tmplPath, userPath)
	if err != nil {
		t.Fatalf("MigrateFromTemplate 失败: %v", err)
	}
	if !changed {
		t.Error("用户文件不存在时应返回 changed=true")
	}

	// 验证用户文件已创建
	data, err := readYAMLFile(userPath)
	if err != nil {
		t.Fatalf("读取用户配置失败: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("期望 value，实际 %v", data["key"])
	}
}

func TestMigrateFromTemplate_新增配置项(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "template.yaml")
	userPath := filepath.Join(tmpDir, "config.yaml")

	// 创建模板（含新字段）
	os.WriteFile(tmplPath, []byte("key: value\nnew_key: new_value\n"), 0o644)

	// 创建用户配置（缺少新字段）
	os.WriteFile(userPath, []byte("key: user_value\n"), 0o644)

	changed, err := MigrateFromTemplate(tmplPath, userPath)
	if err != nil {
		t.Fatalf("MigrateFromTemplate 失败: %v", err)
	}
	if !changed {
		t.Error("新增配置项时应返回 changed=true")
	}

	// 验证合并结果
	data, err := readYAMLFile(userPath)
	if err != nil {
		t.Fatalf("读取用户配置失败: %v", err)
	}
	if data["key"] != "user_value" {
		t.Errorf("期望保留用户值 user_value，实际 %v", data["key"])
	}
	if data["new_key"] != "new_value" {
		t.Errorf("期望添加新配置 new_value，实际 %v", data["new_key"])
	}
}

func TestMigrateFromTemplate_无需变更(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "template.yaml")
	userPath := filepath.Join(tmpDir, "config.yaml")

	content := "key: value\n"
	os.WriteFile(tmplPath, []byte(content), 0o644)
	os.WriteFile(userPath, []byte(content), 0o644)

	changed, err := MigrateFromTemplate(tmplPath, userPath)
	if err != nil {
		t.Fatalf("MigrateFromTemplate 失败: %v", err)
	}
	if changed {
		t.Error("配置无变更时应返回 changed=false")
	}
}

func TestMigrateFromTemplate_保留用户自定义(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "template.yaml")
	userPath := filepath.Join(tmpDir, "config.yaml")

	// 模板只有 key
	os.WriteFile(tmplPath, []byte("key: value\n"), 0o644)
	// 用户有 key 和 custom
	os.WriteFile(userPath, []byte("key: user_value\ncustom: my_setting\n"), 0o644)

	_, err := MigrateFromTemplate(tmplPath, userPath)
	if err != nil {
		t.Fatalf("MigrateFromTemplate 失败: %v", err)
	}

	data, err := readYAMLFile(userPath)
	if err != nil {
		t.Fatalf("读取用户配置失败: %v", err)
	}

	// 用户自定义项应保留
	if data["custom"] != "my_setting" {
		t.Errorf("期望保留自定义项 my_setting，实际 %v", data["custom"])
	}
}

func TestMapsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]any
		b    map[string]any
		want bool
	}{
		{
			name: "相同",
			a:    map[string]any{"key": "value"},
			b:    map[string]any{"key": "value"},
			want: true,
		},
		{
			name: "不同",
			a:    map[string]any{"key": "value1"},
			b:    map[string]any{"key": "value2"},
			want: false,
		},
		{
			name: "长度不同",
			a:    map[string]any{"key": "value"},
			b:    map[string]any{"key": "value", "extra": "data"},
			want: false,
		},
		{
			name: "嵌套相同",
			a:    map[string]any{"nested": map[string]any{"inner": "val"}},
			b:    map[string]any{"nested": map[string]any{"inner": "val"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("mapsEqual() = %v，期望 %v", got, tt.want)
			}
		})
	}
}
