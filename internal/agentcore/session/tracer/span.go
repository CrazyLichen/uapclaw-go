package tracer

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Span 追踪跨度基础结构体，对应 Python Span。
type Span struct {
	// TraceID 追踪标识
	TraceID string `json:"traceId"`
	// StartTime 开始时间
	StartTime *time.Time `json:"startTime,omitempty"`
	// EndTime 结束时间
	EndTime *time.Time `json:"endTime,omitempty"`
	// Inputs 输入数据
	Inputs any `json:"inputs,omitempty"`
	// Outputs 输出数据
	Outputs any `json:"outputs,omitempty"`
	// Error 错误信息
	Error map[string]any `json:"error,omitempty"`
	// InvokeID 调用标识
	InvokeID string `json:"invokeId,omitempty"`
	// ParentInvokeID 父调用标识
	ParentInvokeID string `json:"parentInvokeId,omitempty"`
	// ChildInvokesID 子调用标识列表
	ChildInvokesID []string `json:"childInvokes,omitempty"`
	// Status 模块状态
	Status string `json:"status,omitempty"`
	// OnInvokeData 执行期间的中间过程信息
	OnInvokeData []map[string]any `json:"onInvokeData,omitempty"`
}

// TraceAgentSpan Agent 追踪跨度，嵌入 Span 并扩展 Agent 相关字段，对应 Python TraceAgentSpan。
type TraceAgentSpan struct {
	Span
	// InvokeType 调用类型
	InvokeType string `json:"invokeType,omitempty"`
	// Name 名称
	Name string `json:"name,omitempty"`
	// ElapsedTime 耗时
	ElapsedTime string `json:"elapsedTime,omitempty"`
	// MetaData 元数据，包含 LLM 函数工具和 token 信息
	MetaData map[string]any `json:"metaData,omitempty"`
}

// TraceWorkflowSpan 工作流追踪跨度，嵌入 Span 并扩展工作流相关字段，对应 Python TraceWorkflowSpan。
type TraceWorkflowSpan struct {
	Span
	// ExecutionID 执行标识
	ExecutionID string `json:"executionId,omitempty"`
	// SourceIDs 来源标识列表
	SourceIDs []string `json:"sourceIds,omitempty"`
	// WorkflowID 工作流标识
	WorkflowID string `json:"workflowId,omitempty"`
	// WorkflowVersion 工作流版本
	WorkflowVersion string `json:"workflowVersion,omitempty"`
	// WorkflowName 工作流名称
	WorkflowName string `json:"workflowName,omitempty"`
	// ComponentID 组件标识
	ComponentID string `json:"componentId,omitempty"`
	// ComponentName 组件名称
	ComponentName string `json:"componentName,omitempty"`
	// ComponentType 组件类型
	ComponentType string `json:"componentType,omitempty"`
	// LoopNodeID 循环节点标识
	LoopNodeID string `json:"loopNodeId,omitempty"`
	// LoopIndex 循环索引
	LoopIndex *int `json:"loopIndex,omitempty"`
	// LLMInvokeData LLM 调用数据，不序列化到 JSON（与 Python exclude=True 对齐）
	LLMInvokeData map[string]map[string]any `json:"-"`
	// ParentNodeID 父节点标识（子工作流场景）
	ParentNodeID string `json:"parentNodeId,omitempty"`
	// StreamInputs 流式输入列表
	StreamInputs []any `json:"streamInputs,omitempty"`
	// StreamOutputs 流式输出列表
	StreamOutputs []any `json:"streamOutputs,omitempty"`
	// InteractiveInputs 交互式输入
	InteractiveInputs any `json:"interactiveInputs,omitempty"`
	// InnerError 重试内部错误
	InnerError map[string]any `json:"innerError,omitempty"`
}

// SpanManager 追踪跨度管理器，管理会话期间的 Span 生命周期
type SpanManager struct {
	traceID      string
	parentID     string
	mu           sync.RWMutex
	order        []string
	sessionSpans map[string]*Span
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpanManager 创建追踪跨度管理器
func NewSpanManager(traceID string, parentID ...string) *SpanManager {
	pid := ""
	if len(parentID) > 0 {
		pid = parentID[0]
	}
	return &SpanManager{
		traceID:      traceID,
		parentID:     pid,
		order:        make([]string, 0),
		sessionSpans: make(map[string]*Span),
	}
}

// GetSpan 根据调用标识获取 Span
func (m *SpanManager) GetSpan(invokeID string) *Span {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, id := range m.order {
		if id == invokeID {
			return m.sessionSpans[invokeID]
		}
	}
	return nil
}

// PopSpan 移除指定调用标识的 Span
func (m *SpanManager) PopSpan(invokeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range m.order {
		if id == invokeID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			delete(m.sessionSpans, invokeID)
			return
		}
	}
}

// CreateAgentSpan 创建 Agent 追踪跨度，自动生成 UUID 作为 invokeID
func (m *SpanManager) CreateAgentSpan(parentSpan ...*TraceAgentSpan) *TraceAgentSpan {
	m.mu.Lock()
	defer m.mu.Unlock()

	invokeID := uuid.New().String()
	span := &TraceAgentSpan{
		Span: Span{
			TraceID:  m.traceID,
			InvokeID: invokeID,
			Status:   "",
		},
	}

	if len(parentSpan) > 0 && parentSpan[0] != nil {
		span.ParentInvokeID = parentSpan[0].InvokeID
		parentSpan[0].AppendChildInvokeID(invokeID)
		m.refreshSpanRecordLocked(parentSpan[0].InvokeID, parentSpan[0])
	}

	m.refreshSpanRecordLocked(invokeID, &span.Span)
	return span
}

// CreateWorkflowSpan 创建工作流追踪跨度
func (m *SpanManager) CreateWorkflowSpan(invokeID string, parentSpan ...*TraceWorkflowSpan) *TraceWorkflowSpan {
	m.mu.Lock()
	defer m.mu.Unlock()

	span := &TraceWorkflowSpan{
		Span: Span{
			TraceID:  m.traceID,
			InvokeID: invokeID,
			Status:   "",
		},
		ExecutionID:  m.traceID,
		ParentNodeID: m.parentID,
	}

	if len(parentSpan) > 0 && parentSpan[0] != nil {
		span.ParentInvokeID = parentSpan[0].InvokeID
		parentSpan[0].AppendChildInvokeID(invokeID)
		m.refreshSpanRecordLocked(parentSpan[0].InvokeID, parentSpan[0])
	}

	m.refreshSpanRecordLocked(invokeID, &span.Span)
	return span
}

// UpdateSpan 更新 Span 字段值
func (m *SpanManager) UpdateSpan(span *Span, data map[string]any) {
	span.Update(data)
	m.mu.Lock()
	m.refreshSpanRecordLocked(span.InvokeID, span)
	m.mu.Unlock()
}

// LastSpan 获取最后一个 Span
func (m *SpanManager) LastSpan() *Span {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.order) == 0 {
		return nil
	}
	lastID := m.order[len(m.order)-1]
	return m.sessionSpans[lastID]
}

// Update 更新 Span 字段值
func (s *Span) Update(data map[string]any) {
	val := reflect.ValueOf(s).Elem()
	typ := val.Type()

	for key, value := range data {
		field, found := typ.FieldByName(key)
		if !found {
			continue
		}
		fieldVal := val.FieldByName(key)
		if !fieldVal.CanSet() {
			continue
		}
		if err := setFieldValue(fieldVal, field.Type, value); err != nil {
			logger.Warn(logComponent).
				Str("field", key).
				Err(err).
				Msg("Span.Update 设置字段失败")
		}
	}
}

// AppendChildInvokeID 追加子调用标识
func (s *Span) AppendChildInvokeID(invokeID string) {
	s.ChildInvokesID = append(s.ChildInvokesID, invokeID)
}

// AppendStreamOutput 追加流式输出块
func (s *TraceWorkflowSpan) AppendStreamOutput(chunk any) {
	s.StreamOutputs = append(s.StreamOutputs, chunk)
}

// AppendStreamInputs 追加流式输入块
func (s *TraceWorkflowSpan) AppendStreamInputs(chunk any) {
	s.StreamInputs = append(s.StreamInputs, chunk)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshSpanRecordLocked 刷新 Span 记录（调用方已持锁）
func (m *SpanManager) refreshSpanRecordLocked(invokeID string, baseSpan interface{}) {
	span := extractBaseSpan(baseSpan)
	if span == nil {
		return
	}
	found := false
	for _, id := range m.order {
		if id == invokeID {
			found = true
			break
		}
	}
	if !found {
		m.order = append(m.order, invokeID)
	}
	m.sessionSpans[invokeID] = span
}

// extractBaseSpan 从 TraceAgentSpan/TraceWorkflowSpan 提取 *Span 指针
func extractBaseSpan(v interface{}) *Span {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	// 尝试直接断言 *Span
	if s, ok := v.(*Span); ok {
		return s
	}
	// 通过反射获取嵌入的 Span 字段
	sf := rv.FieldByName("Span")
	if sf.IsValid() && sf.Kind() == reflect.Pointer {
		if s, ok := sf.Interface().(*Span); ok {
			return s
		}
	}
	// Span 是值类型嵌入时，需要取地址
	if sf.IsValid() && sf.Kind() == reflect.Struct {
		return sf.Addr().Interface().(*Span)
	}
	return nil
}

// setFieldValue 通过反射设置字段值，处理类型转换
func setFieldValue(fieldVal reflect.Value, fieldType reflect.Type, value any) error {
	if value == nil {
		return nil
	}

	vVal := reflect.ValueOf(value)

	// 直接赋值：类型匹配
	if vVal.Type().AssignableTo(fieldType) {
		fieldVal.Set(vVal)
		return nil
	}

	// 指针类型字段
	if fieldType.Kind() == reflect.Pointer {
		// value 为 nil 时不设置
		if vVal.IsZero() {
			return nil
		}
		elemType := fieldType.Elem()
		// 值转指针
		if vVal.Type().AssignableTo(elemType) {
			fieldVal.Set(reflect.New(elemType))
			fieldVal.Elem().Set(vVal)
			return nil
		}
		// 尝试转换
		if vVal.Type().ConvertibleTo(elemType) {
			fieldVal.Set(reflect.New(elemType))
			fieldVal.Elem().Set(vVal.Convert(elemType))
			return nil
		}
		return fmt.Errorf("无法将 %v 转换为 *%v", vVal.Type(), elemType)
	}

	// 尝试类型转换
	if vVal.Type().ConvertibleTo(fieldType) {
		fieldVal.Set(vVal.Convert(fieldType))
		return nil
	}

	// 切片类型
	if fieldType.Kind() == reflect.Slice {
		if vVal.Kind() == reflect.Slice {
			slice := reflect.MakeSlice(fieldType, vVal.Len(), vVal.Len())
			for i := 0; i < vVal.Len(); i++ {
				elem := vVal.Index(i)
				if elem.Type().ConvertibleTo(fieldType.Elem()) {
					slice.Index(i).Set(elem.Convert(fieldType.Elem()))
				} else if elem.Type().AssignableTo(fieldType.Elem()) {
					slice.Index(i).Set(elem)
				} else {
					return fmt.Errorf("切片元素类型不匹配: 索引 %d, 源 %v, 目标 %v", i, elem.Type(), fieldType.Elem())
				}
			}
			fieldVal.Set(slice)
			return nil
		}
	}

	// map 类型
	if fieldType.Kind() == reflect.Map && vVal.Kind() == reflect.Map {
		newMap := reflect.MakeMap(fieldType)
		iter := vVal.MapRange()
		for iter.Next() {
			k := iter.Key()
			v := iter.Value()
			if k.Type().ConvertibleTo(fieldType.Key()) && v.Type().ConvertibleTo(fieldType.Elem()) {
				newMap.SetMapIndex(k.Convert(fieldType.Key()), v.Convert(fieldType.Elem()))
			}
		}
		fieldVal.Set(newMap)
		return nil
	}

	return fmt.Errorf("无法将 %v 转换为 %v", vVal.Type(), fieldType)
}
