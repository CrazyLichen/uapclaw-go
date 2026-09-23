package schema

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryInterface 记忆类型的通用接口。
// 所有 Memory 类型（ACEMemory/ReasoningBankMemory/ReMeMemory）均实现此接口。
//
// 注意：FromVectorNode 不在接口中，因为它是构造函数语义（Python classmethod），
// Go 中使用包级函数 NewXxxFromVectorNode 代替。
type MemoryInterface interface {
	// GetWorkspaceID 返回工作空间标识
	GetWorkspaceID() string
	// ToVectorNode 转换为 VectorNode 用于向量存储
	ToVectorNode() *coreschema.VectorNode
}

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemory 记忆类型的公共基类。
// 对齐 Python io_schema.BaseMemory(BaseModel)，各 Memory 类型嵌入此结构体。
//
// Python 中 BaseMemory 含 workspace_id 字段（默认 "default"），
// 以及 to_vector_node/from_vector_node 抽象方法。
// Go 中抽象方法由 MemoryInterface 接口定义，FromVectorNode 由包级函数实现。
type BaseMemory struct {
	// WorkspaceID 工作空间/用户标识，默认 "default"
	WorkspaceID string `json:"workspace_id"`
}

// ACEMemory ACE 算法的记忆类型。
// 对齐 Python ACEMemory(BaseMemory)。
//
// ACE 记忆以 Playbook 条目形式组织，每条记忆属于一个 section，
// 包含内容文本和 helpful/harmful/neutral 反馈计数。
type ACEMemory struct {
	BaseMemory // 嵌入基类
	// ID 记忆标识
	ID string `json:"id"`
	// Section 记忆分类
	Section string `json:"section"`
	// Content 记忆内容
	Content string `json:"content"`
	// Helpful 有帮助计数，默认 0
	Helpful int `json:"helpful"`
	// Harmful 有害计数，默认 0
	Harmful int `json:"harmful"`
	// Neutral 中性计数，默认 0
	Neutral int `json:"neutral"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// ReasoningBankMemoryItem ReasoningBank 记忆条目。
// 对齐 Python ReasoningBankMemoryItem(BaseModel)。
//
// 非独立 Memory 类型，是 ReasoningBankMemory.Memory 列表中的元素。
type ReasoningBankMemoryItem struct {
	// Title 核心策略标识
	Title string `json:"title"`
	// Description 一句话摘要
	Description string `json:"description"`
	// Content 详细内容
	Content string `json:"content"`
}

// ReasoningBankMemory ReasoningBank 算法的记忆类型。
// 对齐 Python ReasoningBankMemory(BaseMemory)。
//
// RB 记忆以 query 为索引，挂载多个 MemoryItem。
// 注意：没有顶层 content 字段，content 嵌套在 Memory 列表项中。
type ReasoningBankMemory struct {
	BaseMemory // 嵌入基类
	// Query 用作 embedding 索引的查询
	Query string `json:"query"`
	// Memory 记忆条目列表
	Memory []ReasoningBankMemoryItem `json:"memory"`
	// Label 记忆标签，可选
	Label *bool `json:"label,omitempty"`
}

// ReMeMemoryMetadata ReMe 记忆的元数据。
// 对齐 Python ReMeMemoryMetadata(BaseModel)。
type ReMeMemoryMetadata struct {
	// Tags 记忆标签，默认空列表
	Tags []string `json:"tags"`
	// StepType 步骤类型，可选
	StepType string `json:"step_type"`
	// ToolsUsed 使用的工具，默认空列表
	ToolsUsed []string `json:"tools_used"`
	// Confidence 置信度，可选
	Confidence float64 `json:"confidence"`
	// Freq 使用频率，默认 0
	Freq int `json:"freq"`
	// Utility 效用分数，可选
	Utility float64 `json:"utility"`
}

// ReMeMemory ReMe 算法的记忆类型。
// 对齐 Python ReMeMemory(BaseMemory)。
//
// ReMe 记忆包含使用条件（when_to_use）、内容、分数和结构化元数据。
type ReMeMemory struct {
	BaseMemory // 嵌入基类
	// WhenToUse 使用条件
	WhenToUse string `json:"when_to_use"`
	// Content 记忆内容
	Content string `json:"content"`
	// Score 记忆分数 (0-1)，默认 0.0
	Score float64 `json:"score"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
	// Metadata 元数据
	Metadata ReMeMemoryMetadata `json:"metadata"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewACEMemoryFromVectorNode 从 VectorNode 反序列化 ACEMemory。
// 对齐 Python ACEMemory.from_vector_node(node) classmethod。
func NewACEMemoryFromVectorNode(node *coreschema.VectorNode) *ACEMemory {
	metadata := node.Metadata
	createdAt := parseTimeField(metadata, "created_at")
	updatedAt := parseTimeField(metadata, "updated_at")

	return &ACEMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", "default"),
		},
		ID:        getStringField(metadata, "id", ""),
		Section:   getStringField(metadata, "section", ""),
		Content:   getStringField(metadata, "content", ""),
		Helpful:   getIntField(metadata, "helpful"),
		Harmful:   getIntField(metadata, "harmful"),
		Neutral:   getIntField(metadata, "neutral"),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

// NewReasoningBankMemoryFromVectorNode 从 VectorNode 反序列化 ReasoningBankMemory。
// 对齐 Python ReasoningBankMemory.from_vector_node(node) classmethod。
func NewReasoningBankMemoryFromVectorNode(node *coreschema.VectorNode) *ReasoningBankMemory {
	metadata := node.Metadata

	var memoryItems []ReasoningBankMemoryItem
	if memRaw, ok := metadata["memory"]; ok && memRaw != nil {
		memoryItems = parseMemoryItems(memRaw)
	}

	var label *bool
	if l, ok := metadata["label"]; ok && l != nil {
		switch v := l.(type) {
		case bool:
			label = &v
		case *bool:
			label = v
		}
	}

	return &ReasoningBankMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", "default"),
		},
		Query:  getStringField(metadata, "query", ""),
		Memory: memoryItems,
		Label:  label,
	}
}

// NewReMeMemoryFromVectorNode 从 VectorNode 反序列化 ReMeMemory。
// 对齐 Python ReMeMemory.from_vector_node(node) classmethod。
func NewReMeMemoryFromVectorNode(node *coreschema.VectorNode) *ReMeMemory {
	metadata := node.Metadata
	createdAt := parseTimeField(metadata, "created_at")
	updatedAt := parseTimeField(metadata, "updated_at")

	// 对齐 Python：如果 created_at 缺失，使用当前时间
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	// 解析嵌套的 metadata
	var memMetadata ReMeMemoryMetadata
	if metaRaw, ok := metadata["metadata"]; ok && metaRaw != nil {
		if metaMap, ok := metaRaw.(map[string]any); ok {
			memMetadata = parseReMeMemoryMetadata(metaMap)
		}
	}

	return &ReMeMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", ""),
		},
		WhenToUse: getStringField(metadata, "when_to_use", ""),
		Content:   getStringField(metadata, "content", ""),
		Score:     getFloatField(metadata, "score"),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Metadata:  memMetadata,
	}
}

// VectorNodeToMemory 按 metadata.type 分发，将 VectorNode 转换为对应的 Memory 类型。
// 对齐 Python vector_node_to_memory(node) 工厂函数（io_schema.py 中无此函数，
// 但 memory.py 中有，此处提供统一分发能力）。
func VectorNodeToMemory(node *coreschema.VectorNode) (MemoryInterface, error) {
	typeVal, ok := node.Metadata["type"]
	if !ok {
		return nil, fmt.Errorf("VectorNodeToMemory: 缺少 metadata.type 字段")
	}
	typeStr, ok := typeVal.(string)
	if !ok {
		return nil, fmt.Errorf("VectorNodeToMemory: metadata.type 不是字符串: %T", typeVal)
	}

	switch typeStr {
	case "ace_memory":
		return NewACEMemoryFromVectorNode(node), nil
	case "reasoning_bank_memory":
		return NewReasoningBankMemoryFromVectorNode(node), nil
	case "reme_memory":
		return NewReMeMemoryFromVectorNode(node), nil
	default:
		return nil, fmt.Errorf("VectorNodeToMemory: 未知的记忆类型: %q", typeStr)
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetWorkspaceID 实现 MemoryInterface 接口。
// 对齐 Python BaseMemory.workspace_id 属性访问。
// 使用值接收者：不修改接收者，且值类型 ACEMemory 需满足 MemoryInterface 约束（SummarizeResponse[T MemoryInterface]）。
func (b BaseMemory) GetWorkspaceID() string {
	return b.WorkspaceID
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ACEMemory.to_vector_node()。
// metadata.type = "ace_memory"，向量嵌入内容 = Content。
// 使用值接收者：不修改接收者，且值类型需满足 MemoryInterface 约束。
func (m ACEMemory) ToVectorNode() *coreschema.VectorNode {
	// 对齐 Python: node_id = f"ace_{self.workspace_id}_{self.id}"
	nodeID := fmt.Sprintf("ace_%s_%s", m.WorkspaceID, m.ID)

	// 对齐 Python: embedding_content = self.content
	embeddingContent := m.Content

	// 对齐 Python: 存储所有字段到 metadata
	metadata := map[string]any{
		"type":         "ace_memory",
		"id":           m.ID,
		"section":      m.Section,
		"content":      m.Content,
		"helpful":      m.Helpful,
		"harmful":      m.Harmful,
		"neutral":      m.Neutral,
		"created_at":   formatTimeField(m.CreatedAt),
		"updated_at":   formatTimeField(m.UpdatedAt),
		"workspace_id": m.WorkspaceID,
	}

	return coreschema.NewVectorNode(nodeID, embeddingContent, nil, metadata)
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ReasoningBankMemory.to_vector_node()。
// metadata.type = "reasoning_bank_memory"，向量嵌入内容 = Query。
// 使用值接收者：不修改接收者，且值类型需满足 MemoryInterface 约束。
func (m ReasoningBankMemory) ToVectorNode() *coreschema.VectorNode {
	// 对齐 Python: combined = f"{self.query}|{self.memory[0].title}" if self.memory else self.query
	combined := m.Query
	if len(m.Memory) > 0 {
		combined = fmt.Sprintf("%s|%s", m.Query, m.Memory[0].Title)
	}

	// 对齐 Python: content_hash = hashlib.md5(combined.encode()).hexdigest()
	contentHash := md5Hash(combined)
	nodeID := fmt.Sprintf("reasoning_bank_%s_%s", m.WorkspaceID, contentHash)

	// 对齐 Python: embedding_content = self.query
	embeddingContent := m.Query

	// 对齐 Python: metadata 中直接存储 self.memory（Pydantic 模型列表会自动序列化）
	// Go 中需要将 MemoryItem 列表转为 []map[string]any
	memoryData := make([]any, len(m.Memory))
	for i, item := range m.Memory {
		memoryData[i] = map[string]any{
			"title":       item.Title,
			"description": item.Description,
			"content":     item.Content,
		}
	}

	metadata := map[string]any{
		"type":         "reasoning_bank_memory",
		"query":        m.Query,
		"memory":       memoryData,
		"label":        m.Label,
		"workspace_id": m.WorkspaceID,
	}

	return coreschema.NewVectorNode(nodeID, embeddingContent, nil, metadata)
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ReMeMemory.to_vector_node()。
// metadata.type = "reme_memory"，向量嵌入内容 = WhenToUse。
// 使用值接收者：不修改接收者，且值类型需满足 MemoryInterface 约束。
func (m ReMeMemory) ToVectorNode() *coreschema.VectorNode {
	// 对齐 Python: node_id = f"reme_{self.workspace_id}_{hashlib.md5(self.when_to_use.encode()).hexdigest()[:12]}"
	contentHash := md5Hash(m.WhenToUse)[:12]
	nodeID := fmt.Sprintf("reme_%s_%s", m.WorkspaceID, contentHash)

	// 对齐 Python: embedding_content = self.when_to_use
	embeddingContent := m.WhenToUse

	// 对齐 Python: 存储所有字段到 metadata
	metadata := map[string]any{
		"type":         "reme_memory",
		"when_to_use":  m.WhenToUse,
		"content":      m.Content,
		"score":        m.Score,
		"created_at":   formatTimeField(m.CreatedAt),
		"updated_at":   formatTimeField(m.UpdatedAt),
		"workspace_id": m.WorkspaceID,
		"metadata": map[string]any{
			"tags":       m.Metadata.Tags,
			"step_type":  m.Metadata.StepType,
			"tools_used": m.Metadata.ToolsUsed,
			"confidence": m.Metadata.Confidence,
			"freq":       m.Metadata.Freq,
			"utility":    m.Metadata.Utility,
		},
	}

	return coreschema.NewVectorNode(nodeID, embeddingContent, nil, metadata)
}

// String 实现 Stringer 接口。
// 对齐 Python ReasoningBankMemory.__repr__()。
func (m *ReasoningBankMemory) String() string {
	return fmt.Sprintf(
		"ReasoningBankMemory(query='%s', memory='%v', label='%v', workspace_id='%s')",
		m.Query, m.Memory, m.Label, m.WorkspaceID,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// md5Hash 计算字符串的 MD5 哈希。
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// getStringField 从 metadata 中安全获取字符串字段。
func getStringField(metadata map[string]any, key, defaultVal string) string {
	if v, ok := metadata[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getIntField 从 metadata 中安全获取整数字段。
func getIntField(metadata map[string]any, key string) int {
	if v, ok := metadata[key]; ok && v != nil {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return 0
}

// getFloatField 从 metadata 中安全获取浮点数字段。
func getFloatField(metadata map[string]any, key string) float64 {
	if v, ok := metadata[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	}
	return 0
}

// formatTimeField 格式化时间字段为 ISO 8601 字符串。
// 对齐 Python: self.created_at.isoformat() if isinstance(self.created_at, datetime) else self.created_at
func formatTimeField(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// parseTimeField 从 metadata 中安全解析时间字段。
// 支持 string（RFC3339/ISO 8601 解析）和 time.Time（直接赋值）。
// 对齐 Python: metadata.get("created_at", datetime.now(timezone.utc).isoformat())
func parseTimeField(metadata map[string]any, key string) time.Time {
	if v, ok := metadata[key]; ok && v != nil {
		switch t := v.(type) {
		case time.Time:
			return t
		case string:
			if t == "" {
				return time.Time{}
			}
			parsed, err := time.Parse(time.RFC3339, t)
			if err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

// parseMemoryItems 解析 ReasoningBankMemory 的 memory 字段。
// 支持 []any（JSON 反序列化后）和 []ReasoningBankMemoryItem 两种格式。
func parseMemoryItems(raw any) []ReasoningBankMemoryItem {
	var items []ReasoningBankMemoryItem

	switch memList := raw.(type) {
	case []any:
		for _, item := range memList {
			if m, ok := item.(map[string]any); ok {
				items = append(items, ReasoningBankMemoryItem{
					Title:       getStringField(m, "title", ""),
					Description: getStringField(m, "description", ""),
					Content:     getStringField(m, "content", ""),
				})
			}
		}
	case []ReasoningBankMemoryItem:
		items = memList
	case []map[string]any:
		for _, m := range memList {
			items = append(items, ReasoningBankMemoryItem{
				Title:       getStringField(m, "title", ""),
				Description: getStringField(m, "description", ""),
				Content:     getStringField(m, "content", ""),
			})
		}
	}

	return items
}

// parseReMeMemoryMetadata 从 map 解析 ReMeMemoryMetadata。
// 对齐 Python ReMeMemoryMetadata(tags=..., step_type=..., ...) 构造。
func parseReMeMemoryMetadata(m map[string]any) ReMeMemoryMetadata {
	tags := parseStringSlice(m, "tags")
	toolsUsed := parseStringSlice(m, "tools_used")

	return ReMeMemoryMetadata{
		Tags:       tags,
		StepType:   getStringField(m, "step_type", ""),
		ToolsUsed:  toolsUsed,
		Confidence: getFloatField(m, "confidence"),
		Freq:       getIntField(m, "freq"),
		Utility:    getFloatField(m, "utility"),
	}
}

// parseStringSlice 从 metadata 中解析字符串切片。
// 支持 []string（原生 Go 类型）和 []any（JSON 反序列化后）两种格式。
func parseStringSlice(m map[string]any, key string) []string {
	if v, ok := m[key]; ok && v != nil {
		switch arr := v.(type) {
		case []string:
			return arr
		case []any:
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}
