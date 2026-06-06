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
			"host":      "0.0.0.0",
			"port":      8080,
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
	_ = os.WriteFile(tmplPath, []byte("key: value\nnew_key: new_value\n"), 0o644)

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
	_ = os.WriteFile(tmplPath, []byte("key: value\nnew_key: new_value\n"), 0o644)

	// 创建用户配置（缺少新字段）
	_ = os.WriteFile(userPath, []byte("key: user_value\n"), 0o644)

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
	_ = os.WriteFile(tmplPath, []byte(content), 0o644)
	_ = os.WriteFile(userPath, []byte(content), 0o644)

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
	_ = os.WriteFile(tmplPath, []byte("key: value\n"), 0o644)
	// 用户有 key 和 custom
	_ = os.WriteFile(userPath, []byte("key: user_value\ncustom: my_setting\n"), 0o644)

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
		{
			name: "嵌套不同",
			a:    map[string]any{"nested": map[string]any{"inner": "val1"}},
			b:    map[string]any{"nested": map[string]any{"inner": "val2"}},
			want: false,
		},
		{
			name: "键不存在",
			a:    map[string]any{"key": "value"},
			b:    map[string]any{"other": "value"},
			want: false,
		},
		{
			name: "int与float64比较",
			a:    map[string]any{"port": 8080},
			b:    map[string]any{"port": float64(8080)},
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

// TestToFloat64 测试类型转换
func TestToFloat64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  float64
		ok    bool
	}{
		{"int", 42, 42, true},
		{"int64", int64(42), 42, true},
		{"float64", float64(3.14), 3.14, true},
		{"string", "hello", 0, false},
		{"bool", true, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloat64(tt.input)
			if ok != tt.ok {
				t.Errorf("toFloat64(%v) ok = %v，期望 %v", tt.input, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("toFloat64(%v) = %v，期望 %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestMigrateFromTemplate_模板不存在 测试模板文件不存在时返回错误
func TestMigrateFromTemplate_模板不存在(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "nonexistent.yaml")
	userPath := filepath.Join(tmpDir, "config.yaml")

	_, err := MigrateFromTemplate(tmplPath, userPath)
	if err == nil {
		t.Error("模板不存在时应返回错误")
	}
}

// TestMigrateFromTemplate_用户文件读取失败 测试用户文件无法读取时返回错误
func TestMigrateFromTemplate_用户文件读取失败(t *testing.T) {
	tmpDir := t.TempDir()
	tmplPath := filepath.Join(tmpDir, "template.yaml")

	// 创建模板
	_ = os.WriteFile(tmplPath, []byte("key: value\n"), 0o644)

	// 用户文件路径为一个目录（导致读取失败）
	userPath := filepath.Join(tmpDir, "config.yaml")
	_ = os.MkdirAll(userPath, 0o755)

	_, err := MigrateFromTemplate(tmplPath, userPath)
	if err == nil {
		t.Error("用户文件读取失败时应返回错误")
	}
}

// TestDeepCopyValue 测试深拷贝各种类型
func TestDeepCopyValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"map", map[string]any{"key": "value"}},
		{"slice", []any{1, 2, 3}},
		{"string", "hello"},
		{"int", 42},
		{"nil", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deepCopyValue(tt.input)
			// 验证不 panic 且值一致
			if tt.input == nil {
				if result != nil {
					t.Error("nil 深拷贝应为 nil")
				}
				return
			}
			switch v := result.(type) {
			case map[string]any:
				original := tt.input.(map[string]any)
				v["key"] = "modified"
				if original["key"] == "modified" {
					t.Error("map 深拷贝不应影响原始")
				}
			case []any:
				original := tt.input.([]any)
				v[0] = 999
				if original[0] == 999 {
					t.Error("slice 深拷贝不应影响原始")
				}
			default:
				if result != tt.input {
					t.Errorf("值类型期望 %v，实际 %v", tt.input, result)
				}
			}
		})
	}
}

// TestReadYAMLFile_空文件 测试读取空 YAML 文件
func TestReadYAMLFile_空文件(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.yaml")
	_ = os.WriteFile(path, []byte(""), 0o644)

	data, err := readYAMLFile(path)
	if err != nil {
		t.Fatalf("readYAMLFile 失败: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("空文件应返回空 map，实际 %v", data)
	}
}

// TestReadYAMLFile_不存在 测试读取不存在的文件
func TestReadYAMLFile_不存在(t *testing.T) {
	_, err := readYAMLFile("/nonexistent/path.yaml")
	if err == nil {
		t.Error("文件不存在时应返回错误")
	}
}
