package schema

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	controllerschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Part 多态内容载体，表示文本、二进制、URL 或任意数据。
//
// Raw 字段使用自定义 JSON marshal：序列化时做 UTF-8 decode 转字符串（对齐 Python Pydantic v2
// 对 bytes 的默认序列化行为），非 UTF-8 数据将返回错误。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_result.py (Part)
type Part struct {
	// Text 文本内容
	Text *string `json:"text,omitempty"`
	// Raw 原始二进制数据（自定义 JSON marshal，UTF-8 decode 对齐 Python）
	Raw RawBytes `json:"raw,omitempty"`
	// URL 外部引用地址
	URL *string `json:"url,omitempty"`
	// Data 任意结构化数据
	Data any `json:"data,omitempty"`
	// Filename 文件名
	Filename *string `json:"filename,omitempty"`
	// MediaType 媒体类型
	MediaType *string `json:"media_type,omitempty"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
}

// Artifact 内容组，聚合多个 Part 并附带语义名称和元数据。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_result.py (Artifact)
type Artifact struct {
	// ArtifactID 产物标识（camelCase，对齐 A2A 协议）
	ArtifactID *string `json:"artifactId,omitempty"`
	// Name 语义名称，如 "summary"、"chart"
	Name *string `json:"name,omitempty"`
	// Description 描述
	Description *string `json:"description,omitempty"`
	// Parts 内容分片列表
	Parts []Part `json:"parts"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
}

// AgentResult Agent 执行结果，是 Agent.Invoke 的标准返回类型。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_result.py (AgentResult)
type AgentResult struct {
	// TaskID 任务标识（snake_case，对齐 Python）
	TaskID *string `json:"task_id,omitempty"`
	// SessionID 会话标识（camelCase，对齐 A2A 协议）
	SessionID *string `json:"sessionId,omitempty"`
	// Status 任务状态
	Status controllerschema.TaskStatus `json:"status"`
	// Artifacts 结果产物列表
	Artifacts []Artifact `json:"artifacts"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// RawBytes 自定义 []byte 类型，JSON 序列化时做 UTF-8 decode 对齐 Python Pydantic v2 行为。
//
// Python Pydantic v2 对 bytes 字段默认使用 UTF-8 decode 转字符串，
// 而非 Go 标准库的 base64 编码。此类型确保 Go 侧序列化输出与 Python 一致。
type RawBytes []byte

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTextPart 创建文本 Part 的便捷函数。
func NewTextPart(text string) Part {
	return Part{Text: &text}
}

// NewURLPart 创建 URL Part 的便捷函数。
func NewURLPart(url string) Part {
	return Part{URL: &url}
}

// NewDataPart 创建数据 Part 的便捷函数。
func NewDataPart(data any) Part {
	return Part{Data: data}
}

// NewRawPart 创建二进制 Part 的便捷函数。
func NewRawPart(raw []byte) Part {
	return Part{Raw: raw}
}

// NewArtifact 创建 Artifact 的便捷函数。
func NewArtifact(name string, parts ...Part) Artifact {
	return Artifact{
		Name:  &name,
		Parts: parts,
	}
}

// NewAgentResult 创建 AgentResult 的便捷函数。
func NewAgentResult(status controllerschema.TaskStatus) *AgentResult {
	return &AgentResult{
		Status: status,
	}
}

// IsTerminal 判断 AgentResult 状态是否为终态。
func (r *AgentResult) IsTerminal() bool {
	return r.Status.IsTerminal()
}

// MarshalJSON 实现 json.Marshaler 接口：将 []byte 做 UTF-8 decode 转 JSON 字符串。
// 非 UTF-8 数据返回错误，与 Python 的 UnicodeDecodeError 行为对齐。
func (r RawBytes) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	if !utf8.Valid(r) {
		return nil, fmt.Errorf("RawBytes 包含非 UTF-8 数据，无法序列化（对齐 Python UnicodeDecodeError）")
	}
	return json.Marshal(string(r))
}

// UnmarshalJSON 实现 json.Unmarshaler 接口：将 JSON 字符串 UTF-8 encode 回 []byte。
func (r *RawBytes) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*r = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("RawBytes 反序列化失败: %w", err)
	}
	*r = []byte(s)
	return nil
}
