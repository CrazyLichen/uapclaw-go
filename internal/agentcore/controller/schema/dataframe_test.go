package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTextDataFrame_DataType 测试 TextDataFrame.DataType 返回值对齐 Python。
func TestTextDataFrame_DataType(t *testing.T) {
	d := &TextDataFrame{Text: "hello"}
	assert.Equal(t, "text", d.DataType())
}

// TestJsonDataFrame_DataType 测试 JsonDataFrame.DataType 返回值对齐 Python。
func TestJsonDataFrame_DataType(t *testing.T) {
	d := &JsonDataFrame{Data: map[string]any{"key": "value"}}
	assert.Equal(t, "json", d.DataType())
}

// TestFileDataFrame_DataType 测试 FileDataFrame.DataType 返回值对齐 Python。
func TestFileDataFrame_DataType(t *testing.T) {
	d := &FileDataFrame{Name: "test.txt", MIMEType: "text/plain"}
	assert.Equal(t, "file", d.DataType())
}

// TestDataFrame_接口断言 确保 DataFrame 接口实现。
func TestDataFrame_接口断言(t *testing.T) {
	var _ DataFrame = (*TextDataFrame)(nil)
	var _ DataFrame = (*JsonDataFrame)(nil)
	var _ DataFrame = (*FileDataFrame)(nil)
}

// TestDataFrame_MarshalDataFrames 测试 DataFrame 切片的序列化
func TestDataFrame_MarshalDataFrames(t *testing.T) {
	dfs := []DataFrame{
		&TextDataFrame{Text: "hello"},
		&JsonDataFrame{Data: map[string]any{"key": "value"}},
		&FileDataFrame{Name: "test.txt", MIMEType: "text/plain"},
	}

	data, err := MarshalDataFrames(dfs)
	if err != nil {
		t.Fatalf("MarshalDataFrames 失败: %v", err)
	}
	if len(data) == 0 {
		t.Error("MarshalDataFrames 返回空数据")
	}

	// 反序列化验证 round-trip
	got, err := UnmarshalDataFrames(data)
	if err != nil {
		t.Fatalf("UnmarshalDataFrames 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("反序列化后长度 = %d, want 3", len(got))
	}
	textDf, ok := got[0].(*TextDataFrame)
	if !ok || textDf.Text != "hello" {
		t.Errorf("got[0] 不是 TextDataFrame 或 Text 不等于 hello")
	}
	jsonDf, ok := got[1].(*JsonDataFrame)
	if !ok || jsonDf.Data["key"] != "value" {
		t.Errorf("got[1] 不是 JsonDataFrame 或 Data[key] 不等于 value")
	}
	fileDf, ok := got[2].(*FileDataFrame)
	if !ok || fileDf.Name != "test.txt" {
		t.Errorf("got[2] 不是 FileDataFrame 或 Name 不等于 test.txt")
	}
}

// TestDataFrame_UnmarshalDataFrames 测试反序列化
func TestDataFrame_UnmarshalDataFrames(t *testing.T) {
	// 正常 JSON 数组
	data := []byte(`[{"text":"hi"},{"data":{"k":"v"}},{"name":"f.txt","mime_type":"text/plain"}]`)
	dfs, err := UnmarshalDataFrames(data)
	if err != nil {
		t.Fatalf("UnmarshalDataFrames 失败: %v", err)
	}
	if len(dfs) != 3 {
		t.Fatalf("长度 = %d, want 3", len(dfs))
	}

	// 无效 JSON
	_, err = UnmarshalDataFrames([]byte(`invalid`))
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
}

// TestDataFrame_UnmarshalJSON_未知类型 测试未知 DataType 反序列化报错
func TestDataFrame_UnmarshalJSON_未知类型(t *testing.T) {
	// 不包含 text/data/name 字段，无法识别类型
	data := []byte(`[{"unknown_field":123}]`)
	_, err := UnmarshalDataFrames(data)
	if err == nil {
		t.Error("未知 DataFrame 格式应返回错误")
	}
}
