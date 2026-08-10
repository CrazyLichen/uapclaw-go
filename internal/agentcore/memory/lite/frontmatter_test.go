package lite

import "testing"

// TestParseFrontmatter_正常内容 测试正常 frontmatter 解析
func TestParseFrontmatter_正常内容(t *testing.T) {
	content := "---\nname: test\ndescription: 测试\ntype: user\n---\n正文内容"
	fm := ParseFrontmatter(content)
	if fm == nil {
		t.Fatal("Expected non-nil frontmatter")
	}
	if fm["name"] != "test" {
		t.Errorf("Expected name=test, got %s", fm["name"])
	}
	if fm["description"] != "测试" {
		t.Errorf("Expected description=测试, got %s", fm["description"])
	}
	if fm["type"] != "user" {
		t.Errorf("Expected type=user, got %s", fm["type"])
	}
}

// TestParseFrontmatter_无Frontmatter 测试无 frontmatter
func TestParseFrontmatter_无Frontmatter(t *testing.T) {
	fm := ParseFrontmatter("纯正文内容")
	if fm != nil {
		t.Errorf("Expected nil for content without frontmatter, got %v", fm)
	}
}

// TestParseFrontmatter_无结束标记 测试无结束标记
func TestParseFrontmatter_无结束标记(t *testing.T) {
	fm := ParseFrontmatter("---\nname: test")
	if fm != nil {
		t.Errorf("Expected nil for unclosed frontmatter, got %v", fm)
	}
}

// TestParseFrontmatter_空Frontmatter 测试空 frontmatter
func TestParseFrontmatter_空Frontmatter(t *testing.T) {
	fm := ParseFrontmatter("---\n---\n正文")
	if fm != nil {
		t.Errorf("Expected nil for empty frontmatter, got %v", fm)
	}
}

// TestValidateFrontmatter_合法 测试合法 frontmatter
func TestValidateFrontmatter_合法(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d", "type": "user"}
	ok, err := ValidateFrontmatter(fm)
	if !ok {
		t.Errorf("Expected valid, got error: %s", err)
	}
}

// TestValidateFrontmatter_缺字段 测试缺少必填字段
func TestValidateFrontmatter_缺字段(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d"}
	ok, err := ValidateFrontmatter(fm)
	if ok {
		t.Error("Expected invalid for missing type")
	}
	if err == "" {
		t.Error("Expected error message")
	}
}

// TestValidateFrontmatter_非法类型 测试非法 type
func TestValidateFrontmatter_非法类型(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d", "type": "invalid"}
	ok, err := ValidateFrontmatter(fm)
	if ok {
		t.Error("Expected invalid for bad type")
	}
	if err == "" {
		t.Error("Expected error message")
	}
}

// TestEnrichFrontmatter_创建 测试创建时填充时间戳
func TestEnrichFrontmatter_创建(t *testing.T) {
	fm := map[string]string{"name": "n"}
	result := EnrichFrontmatter(fm, false)
	if _, ok := result["created_at"]; !ok {
		t.Error("Expected created_at to be set")
	}
	if _, ok := result["updated_at"]; !ok {
		t.Error("Expected updated_at to be set")
	}
}

// TestEnrichFrontmatter_编辑 测试编辑时不覆盖 created_at
func TestEnrichFrontmatter_编辑(t *testing.T) {
	fm := map[string]string{"name": "n", "created_at": "2025-01-01"}
	result := EnrichFrontmatter(fm, true)
	if result["created_at"] != "2025-01-01" {
		t.Errorf("Expected created_at preserved, got %s", result["created_at"])
	}
	if _, ok := result["updated_at"]; !ok {
		t.Error("Expected updated_at to be set")
	}
}

// TestRebuildContentWithFrontmatter 测试重建内容
func TestRebuildContentWithFrontmatter(t *testing.T) {
	content := "---\nname: old\n---\n正文内容"
	fm := map[string]string{"name": "new", "type": "user", "description": "d"}
	result := RebuildContentWithFrontmatter(content, fm)
	if result == "" {
		t.Fatal("Expected non-empty result")
	}
	if !stringsContains(result, "正文内容") {
		t.Errorf("Expected body preserved in result: %s", result)
	}
	if !stringsContains(result, "name: new") {
		t.Errorf("Expected new frontmatter in result: %s", result)
	}
}

// TestExtractBody 测试提取 body
func TestExtractBody(t *testing.T) {
	content := "---\nname: test\n---\n正文内容"
	body := ExtractBody(content)
	if body != "正文内容" {
		t.Errorf("Expected '正文内容', got '%s'", body)
	}
}

// TestExtractBody_无Frontmatter 测试无 frontmatter 时提取 body
func TestExtractBody_无Frontmatter(t *testing.T) {
	content := "纯正文内容"
	body := ExtractBody(content)
	if body != "纯正文内容" {
		t.Errorf("Expected '纯正文内容', got '%s'", body)
	}
}

// TestExtractBody_空Body 测试空 body
func TestExtractBody_空Body(t *testing.T) {
	content := "---\nname: test\n---\n"
	body := ExtractBody(content)
	if body != "" {
		t.Errorf("Expected empty body, got '%s'", body)
	}
}

// stringsContains 字符串包含辅助函数
func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || searchSubstring(s, sub))
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
