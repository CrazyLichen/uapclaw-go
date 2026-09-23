package schema

import (
	"encoding/json"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────── ACE 系列 ────────────────

// ACESummarizeRequest ACE 摘要请求。
// 对齐 Python ACESummarizeRequest(BaseModel)。
type ACESummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential，默认 "none"
	Matts string `json:"matts"`
	// Query 摘要查询
	Query string `json:"query"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// GroundTruth 可选的参考答案
	GroundTruth *string `json:"ground_truth,omitempty"`
	// Feedback 可选的轨迹环境反馈
	Feedback []string `json:"feedback,omitempty"`
}

// ACESummarizeResponse ACE 摘要响应。
// 对齐 Python ACESummarizeResponse(BaseModel)。
type ACESummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ACEMemory `json:"memory"`
}

// ACERetrieveRequest ACE 检索请求。
// 对齐 Python ACERetrieveRequest(BaseModel)，检索所有记忆。
type ACERetrieveRequest struct {
	// UserID 可选的用户标识
	UserID *string `json:"user_id,omitempty"`
}

// ACERetrievedMemory ACE 检索到的记忆条目。
// 对齐 Python ACERetrievedMemory(BaseModel)。
type ACERetrievedMemory struct {
	// ID 记忆标识
	ID string `json:"id"`
	// Section 记忆分类
	Section string `json:"section"`
	// Content 记忆内容
	Content string `json:"content"`
	// Helpful 有帮助计数
	Helpful int `json:"helpful"`
	// Harmful 有害计数
	Harmful int `json:"harmful"`
	// Neutral 中性计数
	Neutral int `json:"neutral"`
}

// ACERetrieveResponse ACE 检索响应。
// 对齐 Python ACERetrieveResponse(BaseModel)。
type ACERetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ACERetrievedMemory `json:"retrieved_memory"`
}

// ──────────────── ReasoningBank 系列 ────────────────

// ReasoningBankSummarizeRequest ReasoningBank 摘要请求。
// 对齐 Python ReasoningBankSummarizeRequest(BaseModel)。
type ReasoningBankSummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential，默认 "none"
	Matts string `json:"matts"`
	// Query 摘要查询
	Query string `json:"query"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// Label 可选的轨迹标签列表
	Label []*bool `json:"label,omitempty"`
}

// ReasoningBankSummarizeResponse ReasoningBank 摘要响应。
// 对齐 Python ReasoningBankSummarizeResponse(BaseModel)。
type ReasoningBankSummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ReasoningBankMemory `json:"memory"`
}

// ReasoningBankRetrieveRequest ReasoningBank 检索请求。
// 对齐 Python ReasoningBankRetrieveRequest(BaseModel)。
type ReasoningBankRetrieveRequest struct {
	// Query 检索查询
	Query string `json:"query"`
	// TopK 检索返回数量，默认 5
	TopK int `json:"topk"`
}

// ReasoningBankRetrievedMemory ReasoningBank 检索到的记忆条目。
// 对齐 Python ReasoningBankRetrievedMemory(BaseModel)。
type ReasoningBankRetrievedMemory struct {
	// Title 记忆标题
	Title string `json:"title"`
	// Description 记忆描述
	Description string `json:"description"`
	// Content 记忆内容
	Content string `json:"content"`
}

// ReasoningBankRetrieveResponse ReasoningBank 检索响应。
// 对齐 Python ReasoningBankRetrieveResponse(BaseModel)。
type ReasoningBankRetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ReasoningBankRetrievedMemory `json:"retrieved_memory"`
}

// ──────────────── ReMe 系列 ────────────────

// ReMeSummarizeRequest ReMe 摘要请求。
// 对齐 Python ReMeSummarizeRequest(BaseModel)。
type ReMeSummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential，默认 "none"
	Matts string `json:"matts"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// Score 可选的轨迹分数列表 (0-1)
	Score []float64 `json:"score,omitempty"`
}

// ReMeSummarizeResponse ReMe 摘要响应。
// 对齐 Python ReMeSummarizeResponse(BaseModel)。
type ReMeSummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ReMeMemory `json:"memory"`
}

// ReMeRetrieveRequest ReMe 检索请求。
// 对齐 Python ReMeRetrieveRequest(BaseModel)。
type ReMeRetrieveRequest struct {
	// Query 检索查询
	Query string `json:"query"`
	// TopKRetrieval 初始检索数量，默认 10
	TopKRetrieval int `json:"topk_retrieval"`
	// TopKRerank 重排序后保留数量，默认 5
	TopKRerank int `json:"topk_rerank"`
}

// ReMeRetrievedMemory ReMe 检索到的记忆条目。
// 对齐 Python ReMeRetrievedMemory(BaseModel)。
type ReMeRetrievedMemory struct {
	// WhenToUse 使用条件
	WhenToUse string `json:"when_to_use"`
	// Content 记忆内容
	Content string `json:"content"`
}

// ReMeRetrieveResponse ReMe 检索响应。
// 对齐 Python ReMeRetrieveResponse(BaseModel)。
type ReMeRetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ReMeRetrievedMemory `json:"retrieved_memory"`
}

// ──────────────── 泛型 Response ────────────────

// SummarizeResponse 通用摘要响应，支持任意算法的 Memory 类型。
// 对齐 Python SummarizeResponse(BaseModel) 的 Union 字段，Go 用泛型实现。
//
// 泛型约束 T MemoryInterface：摘要响应的 Memory 必须是可向量化的完整记忆类型
// （ACEMemory/ReasoningBankMemory/ReMeMemory），与 RetrieveResponse 中的
// RetrievedMemory 快照类型区分（后者不实现 MemoryInterface，不需要向量化）。
//
// 使用示例：
//
//	SummarizeResponse[ACEMemory]      // ACE 算法
//	SummarizeResponse[ReasoningBankMemory] // ReasoningBank 算法
//	SummarizeResponse[ReMeMemory]     // ReMe 算法
type SummarizeResponse[T MemoryInterface] struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表（算法特定类型）
	Memory []T `json:"memory"`
}

// RetrieveResponse 通用检索响应，支持任意算法的 RetrievedMemory 类型。
// 对齐 Python RetrieveResponse(BaseModel) 的 Union 字段，Go 用泛型实现。
//
// 使用示例：
//
//	RetrieveResponse[ACERetrievedMemory]           // ACE 算法
//	RetrieveResponse[ReasoningBankRetrievedMemory]  // ReasoningBank 算法
//	RetrieveResponse[ReMeRetrievedMemory]          // ReMe 算法
type RetrieveResponse[T any] struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表（算法特定类型）
	RetrievedMemory []T `json:"retrieved_memory"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default="none"，JSON 缺少 matts 字段时使用 "none" 默认值。
func (r *ACESummarizeRequest) UnmarshalJSON(data []byte) error {
	type alias ACESummarizeRequest
	var a alias
	a.Matts = "none" // 对齐 Python default
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = ACESummarizeRequest(a)
	return nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default="none"，JSON 缺少 matts 字段时使用 "none" 默认值。
func (r *ReasoningBankSummarizeRequest) UnmarshalJSON(data []byte) error {
	type alias ReasoningBankSummarizeRequest
	var a alias
	a.Matts = "none" // 对齐 Python default
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = ReasoningBankSummarizeRequest(a)
	return nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default=5，JSON 缺少 topk 字段时使用 5 默认值。
func (r *ReasoningBankRetrieveRequest) UnmarshalJSON(data []byte) error {
	type alias ReasoningBankRetrieveRequest
	var a alias
	a.TopK = 5 // 对齐 Python default
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = ReasoningBankRetrieveRequest(a)
	return nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default="none"，JSON 缺少 matts 字段时使用 "none" 默认值。
func (r *ReMeSummarizeRequest) UnmarshalJSON(data []byte) error {
	type alias ReMeSummarizeRequest
	var a alias
	a.Matts = "none" // 对齐 Python default
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = ReMeSummarizeRequest(a)
	return nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default=topk_retrieval=10/topk_rerank=5，
// JSON 缺少字段时使用对应默认值。
func (r *ReMeRetrieveRequest) UnmarshalJSON(data []byte) error {
	type alias ReMeRetrieveRequest
	var a alias
	a.TopKRetrieval = 10 // 对齐 Python default
	a.TopKRerank = 5     // 对齐 Python default
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = ReMeRetrieveRequest(a)
	return nil
}
