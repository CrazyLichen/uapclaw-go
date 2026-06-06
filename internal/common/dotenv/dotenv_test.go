package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestParse_EmptyAndComments 测试空行和注释的解析。
func TestParse_EmptyAndComments(t *testing.T) {
	content := `# 这是注释
KEY1=value1

# 另一个注释
KEY2=value2
`
	result := Parse(content)

	if len(result) != 2 {
		t.Fatalf("期望 2 个键，实际 %d 个", len(result))
	}
	if result["KEY1"] != "value1" {
		t.Errorf("KEY1 期望 value1，实际 %s", result["KEY1"])
	}
	if result["KEY2"] != "value2" {
		t.Errorf("KEY2 期望 value2，实际 %s", result["KEY2"])
	}
}

// TestParse_QuotedValues 测试带引号的值。
func TestParse_QuotedValues(t *testing.T) {
	content := `KEY1="hello world"
KEY2='single quoted'
KEY3=no_quotes
KEY4="value with # inside"
KEY5=value # 行尾注释
`
	result := Parse(content)

	if result["KEY1"] != "hello world" {
		t.Errorf("KEY1 期望 'hello world'，实际 '%s'", result["KEY1"])
	}
	if result["KEY2"] != "single quoted" {
		t.Errorf("KEY2 期望 'single quoted'，实际 '%s'", result["KEY2"])
	}
	if result["KEY3"] != "no_quotes" {
		t.Errorf("KEY3 期望 'no_quotes'，实际 '%s'", result["KEY3"])
	}
	if result["KEY4"] != "value with # inside" {
		t.Errorf("KEY4 期望 'value with # inside'，实际 '%s'", result["KEY4"])
	}
	if result["KEY5"] != "value" {
		t.Errorf("KEY5 期望 'value'，实际 '%s'", result["KEY5"])
	}
}

// TestParse_UnclosedQuote 测试未闭合引号返回引号后的全部内容。
func TestParse_UnclosedQuote(t *testing.T) {
	content := `KEY1="unclosed value
KEY2='another unclosed
`
	result := Parse(content)

	if result["KEY1"] != "unclosed value" {
		t.Errorf("KEY1 期望 'unclosed value'，实际 '%s'", result["KEY1"])
	}
	if result["KEY2"] != "another unclosed" {
		t.Errorf("KEY2 期望 'another unclosed'，实际 '%s'", result["KEY2"])
	}
}

// TestParse_ExportPrefix 测试 export 前缀。
func TestParse_ExportPrefix(t *testing.T) {
	content := `export KEY1=value1
export KEY2="quoted"
`
	result := Parse(content)

	if result["KEY1"] != "value1" {
		t.Errorf("KEY1 期望 value1，实际 %s", result["KEY1"])
	}
	if result["KEY2"] != "quoted" {
		t.Errorf("KEY2 期望 quoted，实际 %s", result["KEY2"])
	}
}

// TestParse_EmptyValue 测试空值。
func TestParse_EmptyValue(t *testing.T) {
	content := `KEY1=
KEY2=""
KEY3=''
`
	result := Parse(content)

	if result["KEY1"] != "" {
		t.Errorf("KEY1 期望空字符串，实际 '%s'", result["KEY1"])
	}
	if result["KEY2"] != "" {
		t.Errorf("KEY2 期望空字符串，实际 '%s'", result["KEY2"])
	}
	if result["KEY3"] != "" {
		t.Errorf("KEY3 期望空字符串，实际 '%s'", result["KEY3"])
	}
}

// TestParse_InvalidLines 测试无效行被忽略。
func TestParse_InvalidLines(t *testing.T) {
	content := `KEY1=value1
=NO_KEY
NO_EQUALS
KEY2=value2
`
	result := Parse(content)

	if len(result) != 2 {
		t.Fatalf("期望 2 个键，实际 %d 个", len(result))
	}
	if result["KEY1"] != "value1" {
		t.Errorf("KEY1 期望 value1，实际 %s", result["KEY1"])
	}
	if result["KEY2"] != "value2" {
		t.Errorf("KEY2 期望 value2，实际 %s", result["KEY2"])
	}
}

// TestParse_URLWithHash 测试 URL 中包含 # 不被误切。
func TestParse_URLWithHash(t *testing.T) {
	content := `URL=http://example.com#anchor
URL2=http://example.com/path # 这是注释
`
	result := Parse(content)

	if result["URL"] != "http://example.com#anchor" {
		t.Errorf("URL 期望 'http://example.com#anchor'，实际 '%s'", result["URL"])
	}
	if result["URL2"] != "http://example.com/path" {
		t.Errorf("URL2 期望 'http://example.com/path'，实际 '%s'", result["URL2"])
	}
}

// TestParse_EqualsInValue 测试值中包含等号。
func TestParse_EqualsInValue(t *testing.T) {
	content := `KEY1=value=with=equals
KEY2="a=b=c"
`
	result := Parse(content)

	if result["KEY1"] != "value=with=equals" {
		t.Errorf("KEY1 期望 'value=with=equals'，实际 '%s'", result["KEY1"])
	}
	if result["KEY2"] != "a=b=c" {
		t.Errorf("KEY2 期望 'a=b=c'，实际 '%s'", result["KEY2"])
	}
}

// TestParse_Whitespace 测试键值两侧空格处理。
func TestParse_Whitespace(t *testing.T) {
	content := `  KEY1  =  value1
	KEY2	=	value2
`
	result := Parse(content)

	if result["KEY1"] != "value1" {
		t.Errorf("KEY1 期望 'value1'，实际 '%s'", result["KEY1"])
	}
	if result["KEY2"] != "value2" {
		t.Errorf("KEY2 期望 'value2'，实际 '%s'", result["KEY2"])
	}
}

// TestLoad 文件加载与环境变量注入测试。
func TestLoad(t *testing.T) {
	// 创建临时 .env 文件
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	content := `UAPCLAW_DATA_DIR=/tmp/test_workspace
UAPCLAW_INSTANCE=test_instance
AGENT_SERVER_PORT=19092
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}

	// 保存原有环境变量
	origDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	origInstance := os.Getenv("UAPCLAW_INSTANCE")
	origPort := os.Getenv("AGENT_SERVER_PORT")
	t.Cleanup(func() {
		_ = os.Setenv("UAPCLAW_DATA_DIR", origDataDir)
		_ = os.Setenv("UAPCLAW_INSTANCE", origInstance)
		_ = os.Setenv("AGENT_SERVER_PORT", origPort)
	})

	// 执行加载
	if err := Load(envPath); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	// 验证环境变量
	if v := os.Getenv("UAPCLAW_DATA_DIR"); v != "/tmp/test_workspace" {
		t.Errorf("UAPCLAW_DATA_DIR 期望 '/tmp/test_workspace'，实际 '%s'", v)
	}
	if v := os.Getenv("UAPCLAW_INSTANCE"); v != "test_instance" {
		t.Errorf("UAPCLAW_INSTANCE 期望 'test_instance'，实际 '%s'", v)
	}
	if v := os.Getenv("AGENT_SERVER_PORT"); v != "19092" {
		t.Errorf("AGENT_SERVER_PORT 期望 '19092'，实际 '%s'", v)
	}
}

// TestLoad_FileNotFound 测试文件不存在时的错误。
func TestLoad_FileNotFound(t *testing.T) {
	err := Load("/nonexistent/path/.env")
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestLoad_Override 测试 override 行为：.env 值覆盖已有环境变量。
func TestLoad_Override(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// 设置已有环境变量
	origVal := os.Getenv("TEST_DOTENV_OVERRIDE")
	_ = os.Setenv("TEST_DOTENV_OVERRIDE", "old_value")
	t.Cleanup(func() {
		_ = os.Setenv("TEST_DOTENV_OVERRIDE", origVal)
	})

	// .env 文件中的值覆盖
	content := "TEST_DOTENV_OVERRIDE=new_value\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}

	if err := Load(envPath); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	if v := os.Getenv("TEST_DOTENV_OVERRIDE"); v != "new_value" {
		t.Errorf("期望 override 为 'new_value'，实际 '%s'", v)
	}
}
