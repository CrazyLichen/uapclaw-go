package browser_move

import (
	"testing"
)

func TestExtractJSONObject_空输入(t *testing.T) {
	result := ExtractJSONObject(nil)
	if len(result) != 0 {
		t.Errorf("期望空 map, 得到 %v", result)
	}
}

func TestExtractJSONObject_空字符串(t *testing.T) {
	result := ExtractJSONObject("")
	if len(result) != 0 {
		t.Errorf("期望空 map, 得到 %v", result)
	}
}

func TestExtractJSONObject_dict输入(t *testing.T) {
	input := map[string]any{"ok": true, "status": "completed"}
	result := ExtractJSONObject(input)
	if result["ok"] != true || result["status"] != "completed" {
		t.Errorf("期望直接返回, 得到 %v", result)
	}
}

func TestExtractJSONObject_直接JSON(t *testing.T) {
	result := ExtractJSONObject(`{"ok": true, "final": "done"}`)
	if result["ok"] != true || result["final"] != "done" {
		t.Errorf("期望解析成功, 得到 %v", result)
	}
}

func TestExtractJSONObject_json代码块(t *testing.T) {
	input := "Here is the result:\n```json\n{\"ok\": true}\n```\nDone"
	result := ExtractJSONObject(input)
	if result["ok"] != true {
		t.Errorf("期望从代码块提取, 得到 %v", result)
	}
}

func TestExtractJSONObject_首尾花括号(t *testing.T) {
	input := "Some text before {\"ok\": false, \"error\": \"test\"} some text after"
	result := ExtractJSONObject(input)
	if result["ok"] != false || result["error"] != "test" {
		t.Errorf("期望从花括号提取, 得到 %v", result)
	}
}

func TestExtractJSONObject_Playwright标记(t *testing.T) {
	input := "### Result {\"ok\": true, \"page\": {\"url\": \"https://example.com\"}} ### Ran Playwright code"
	result := ExtractJSONObject(input)
	if result["ok"] != true {
		t.Errorf("期望从 Playwright 标记提取, 得到 %v", result)
	}
}

func TestExtractJSONObject_无法解析(t *testing.T) {
	result := ExtractJSONObject("no json here")
	if len(result) != 0 {
		t.Errorf("期望空 map, 得到 %v", result)
	}
}

func TestSanitizeJSONSchema_折叠anyOfnullable(t *testing.T) {
	input := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	}
	result := SanitizeJSONSchema(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("期望返回 map")
	}
	if resultMap["type"] != "string" {
		t.Errorf("期望 type=string, 得到 %v", resultMap["type"])
	}
	if _, exists := resultMap["anyOf"]; exists {
		t.Error("期望 anyOf 被折叠移除")
	}
}

func TestSanitizeJSONSchema_移除不支持关键字(t *testing.T) {
	input := map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"$id":         "test-id",
		"type":        "object",
		"properties":  map[string]any{},
	}
	result := SanitizeJSONSchema(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("期望返回 map")
	}
	if _, exists := resultMap["$schema"]; exists {
		t.Error("期望 $schema 被移除")
	}
	if _, exists := resultMap["$id"]; exists {
		t.Error("期望 $id 被移除")
	}
	if resultMap["type"] != "object" {
		t.Errorf("期望 type=object, 得到 %v", resultMap["type"])
	}
}

func TestSanitizeJSONSchema_nullType转object(t *testing.T) {
	input := map[string]any{
		"type": nil,
	}
	result := SanitizeJSONSchema(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("期望返回 map")
	}
	if resultMap["type"] != "object" {
		t.Errorf("期望 null type → object, 得到 %v", resultMap["type"])
	}
}

func TestSanitizeJSONSchema_递归properties(t *testing.T) {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
				"$schema": "should-be-removed",
			},
		},
	}
	result := SanitizeJSONSchema(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("期望返回 map")
	}
	props, ok := resultMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("期望 properties 为 map")
	}
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("期望 name 为 map")
	}
	if _, exists := nameProp["$schema"]; exists {
		t.Error("期望嵌套 $schema 被递归移除")
	}
	if nameProp["type"] != "string" {
		t.Errorf("期望 name.type=string, 得到 %v", nameProp["type"])
	}
}

func TestSanitizeJSONSchema_非map输入(t *testing.T) {
	result := SanitizeJSONSchema("just a string")
	if result != "just a string" {
		t.Errorf("期望原样返回, 得到 %v", result)
	}
}
