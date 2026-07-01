package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DataFrame 数据帧接口，Controller 输出载荷的数据单元。
//
// 对应 Python: openjiuwen/core/controller/schema/dataframe.py (BaseDataFrame)
type DataFrame interface {
	// DataType 返回数据类型标识："text" | "json" | "file"
	DataType() string
}

// TextDataFrame 文本数据帧。
//
// 对应 Python: openjiuwen/core/controller/schema/dataframe.py (TextDataFrame)
type TextDataFrame struct {
	// Text 文本内容
	Text string `json:"text"`
}

// JsonDataFrame JSON 数据帧。
//
// 对应 Python: openjiuwen/core/controller/schema/dataframe.py (JsonDataFrame)
type JsonDataFrame struct {
	// Data JSON 数据
	Data map[string]any `json:"data"`
}

// FileDataFrame 文件数据帧。
//
// 对应 Python: openjiuwen/core/controller/schema/dataframe.py (FileDataFrame)
type FileDataFrame struct {
	// Name 文件名
	Name string `json:"name"`
	// MIMEType MIME 类型
	MIMEType string `json:"mime_type"`
	// Bytes 文件字节内容
	Bytes []byte `json:"bytes,omitempty"`
	// URI 文件 URI
	URI string `json:"uri,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// dataFrameSlice DataFrame 切片的类型别名，用于实现多态 JSON 序列化/反序列化。
type dataFrameSlice []DataFrame

// ──────────────────────────── 导出函数 ────────────────────────────

// DataType 实现 DataFrame 接口，返回 "text"。
func (d *TextDataFrame) DataType() string { return "text" }

// DataType 实现 DataFrame 接口，返回 "json"。
func (d *JsonDataFrame) DataType() string { return "json" }

// DataType 实现 DataFrame 接口，返回 "file"。
func (d *FileDataFrame) DataType() string { return "file" }

// MarshalDataFrames 将 DataFrame 切片序列化为 JSON（多态序列化）。
func MarshalDataFrames(dfs []DataFrame) ([]byte, error) {
	return json.Marshal(dataFrameSlice(dfs))
}

// UnmarshalDataFrames 从 JSON 反序列化为 DataFrame 切片（多态反序列化）。
func UnmarshalDataFrames(data []byte) ([]DataFrame, error) {
	var ds dataFrameSlice
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, err
	}
	return []DataFrame(ds), nil
}

// MarshalJSON 实现 json.Marshaler，遍历每个 DataFrame 按具体类型序列化。
func (ds dataFrameSlice) MarshalJSON() ([]byte, error) {
	items := make([]json.RawMessage, len(ds))
	for i, df := range ds {
		data, err := json.Marshal(df)
		if err != nil {
			return nil, fmt.Errorf("序列化数据帧 [%d] 失败: %w", i, err)
		}
		items[i] = data
	}
	return json.Marshal(items)
}

// UnmarshalJSON 实现 json.Unmarshaler，按字段特征分发到具体类型反序列化。
func (ds *dataFrameSlice) UnmarshalJSON(data []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return err
	}

	result := make([]DataFrame, len(raws))
	for i, raw := range raws {
		// 探测数据帧类型：根据 JSON 中的特征字段判断
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return fmt.Errorf("反序列化数据帧 [%d] 失败: %w", i, err)
		}

		var df DataFrame
		var err error
		switch {
		case hasKey(probe, "text"):
			var d TextDataFrame
			err = json.Unmarshal(raw, &d)
			df = &d
		case hasKey(probe, "data"):
			var d JsonDataFrame
			err = json.Unmarshal(raw, &d)
			df = &d
		case hasKey(probe, "name"):
			var d FileDataFrame
			err = json.Unmarshal(raw, &d)
			df = &d
		default:
			return fmt.Errorf("反序列化数据帧 [%d] 失败: 无法识别的数据帧格式", i)
		}
		if err != nil {
			return fmt.Errorf("反序列化数据帧 [%d] 失败: %w", i, err)
		}
		result[i] = df
	}
	*ds = result
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasKey 检查 JSON 探测结果中是否包含指定字段。
func hasKey(probe map[string]json.RawMessage, key string) bool {
	_, ok := probe[key]
	return ok
}
