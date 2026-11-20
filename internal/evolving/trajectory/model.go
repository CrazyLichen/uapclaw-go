package trajectory

import (
	"encoding/json"
	"fmt"
)

// model.go 规范化不可变轨迹值对象。
// 对齐 Python: agent_evolving/trajectory/model.py（全文，2026-12-04 快照）。
// 持久表示就是普通 OTLP JSON；Trajectory 持有 payload 的私有深拷贝，
// 只暴露脱离式投影——输入映射与返回给调用方的值都无法改动快照。
// Go 侧不可变以「导出字段仅 payload 且全部方法返回拷贝」约定实现
//（偏差 D-25.2b-8：Go 无 __setattr__ 封印，构造后不得写 payload 字段）。

// ──────────────────────────── 结构体 ────────────────────────────

// Trajectory 规范化 OTLP 轨迹载荷的不可变值对象。
// Python: model.py class Trajectory（__slots__=("_payload","_sealed")）。
type Trajectory struct {
	// payload 规范化 OTLP JSON（resourceSpans 树）。构造后禁止修改。
	// Python: self._payload。
	payload map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// copyJSON 返回脱离式拷贝并归一化映射实现。
// Python: model.py _copy_json。
func copyJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[fmt.Sprint(key)] = copyJSON(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = copyJSON(item)
		}
		return result
	default:
		return deepCopyJSON(value)
	}
}

// deepCopyJSON 经 JSON 往返实现深拷贝（对齐 Python deepcopy 的 JSON 值语义）。
func deepCopyJSON(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return value
	}
	return decoded
}

// decodeOTLPValue 解码 OTLP AnyValue 的 JSON 表示（或返回脱离的普通值）。
// 少量导出器直接给已解码值，接受普通标量保持访问器宽容、不改规范输出。
// Python: model.py _decode_otlp_value。
func decodeOTLPValue(value any) any {
	m, ok := value.(map[string]any)
	if !ok {
		return toJsonCompatible(value)
	}
	if v, exists := m["stringValue"]; exists {
		return toJsonCompatible(v)
	}
	if v, exists := m["boolValue"]; exists {
		b, _ := v.(bool)
		return b
	}
	if v, exists := m["intValue"]; exists {
		// Python intValue 是字符串化整数；容错解析。
		switch n := v.(type) {
		case string:
			var parsed int
			if _, err := fmt.Sscanf(n, "%d", &parsed); err == nil {
				return parsed
			}
			return toJsonCompatible(v)
		case float64:
			return int(n)
		case int:
			return n
		default:
			return toJsonCompatible(v)
		}
	}
	if v, exists := m["doubleValue"]; exists {
		if f, ok := v.(float64); ok {
			return f
		}
		return toJsonCompatible(v)
	}
	if v, exists := m["bytesValue"]; exists {
		return toJsonCompatible(v)
	}
	if v, exists := m["arrayValue"]; exists {
		array, _ := v.(map[string]any)
		values, _ := array["values"].([]any)
		result := make([]any, 0, len(values))
		for _, item := range values {
			result = append(result, decodeOTLPValue(item))
		}
		return result
	}
	if v, exists := m["kvlistValue"]; exists {
		kvlist, _ := v.(map[string]any)
		values, _ := kvlist["values"].([]any)
		result := make(map[string]any, len(values))
		for _, item := range values {
			entry, ok := item.(map[string]any)
			if !ok || entry["key"] == nil {
				continue
			}
			result[fmt.Sprint(entry["key"])] = decodeOTLPValue(entry["value"])
		}
		return result
	}
	// 测试与部分导出器用的属性映射已是普通字典；递归脱离而非返回输入。
	result := make(map[string]any, len(m))
	for key, item := range m {
		result[key] = decodeOTLPValue(item)
	}
	return result
}

// toOTLPValue 把简单资源元数据编码为 OTLP AnyValue。
// Python: model.py _to_otlp_value。
func toOTLPValue(value any) map[string]any {
	v := copyJSON(value)
	if v == nil {
		return map[string]any{}
	}
	switch typed := v.(type) {
	case bool:
		return map[string]any{"boolValue": typed}
	case int:
		return map[string]any{"intValue": fmt.Sprint(typed)}
	case int64:
		return map[string]any{"intValue": fmt.Sprint(typed)}
	case float64:
		return map[string]any{"doubleValue": typed}
	case string:
		return map[string]any{"stringValue": typed}
	case []any:
		values := make([]any, len(typed))
		for i, item := range typed {
			values[i] = toOTLPValue(item)
		}
		return map[string]any{"arrayValue": map[string]any{"values": values}}
	case map[string]any:
		values := make([]any, 0, len(typed))
		for key, item := range typed {
			if item == nil {
				continue
			}
			values = append(values, map[string]any{"key": key, "value": toOTLPValue(item)})
		}
		return map[string]any{"kvlistValue": map[string]any{"values": values}}
	default:
		return map[string]any{"stringValue": fmt.Sprint(typed)}
	}
}

// resourceAttributes 读规范 OTLP 载荷的第一组资源属性。
// Python: model.py _resource_attributes。
func resourceAttributes(payload map[string]any) map[string]any {
	resourceSpans, _ := payload["resourceSpans"].([]any)
	for _, item := range resourceSpans {
		resourceSpan, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resource, _ := resourceSpan["resource"].(map[string]any)
		if resource == nil {
			resource = map[string]any{}
		}
		attributes, _ := resource["attributes"].([]any)
		result := map[string]any{}
		for _, attrItem := range attributes {
			entry, ok := attrItem.(map[string]any)
			if !ok || entry["key"] == nil {
				continue
			}
			result[fmt.Sprint(entry["key"])] = decodeOTLPValue(entry["value"])
		}
		if len(result) > 0 {
			return result
		}
	}
	return map[string]any{}
}

// firstValue 返回第一个存在且非 nil 的键值。
// Python: model.py _first_value。
func firstValue(attributes map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, exists := attributes[key]; exists && v != nil {
			return v
		}
	}
	return nil
}

// asOptionalString 可选字符串化。
// Python: model.py _as_optional_string。
func asOptionalString(value any) *string {
	if value == nil {
		return nil
	}
	s := fmt.Sprint(value)
	return &s
}

// NewTrajectory 从规范 OTLP 载荷构造（校验 + 深拷贝封存）。
// Python: model.py Trajectory.__init__。
// 校验：payload 必须含非空 resourceSpans；资源属性必须含非空 trajectory_id；
// 含 team_id/member_id 时必须同时有 session_id。
func NewTrajectory(payload map[string]any) (*Trajectory, error) {
	if payload == nil {
		return nil, fmt.Errorf("trajectory payload must be a mapping")
	}
	resourceSpans, ok := payload["resourceSpans"].([]any)
	if !ok || len(resourceSpans) == 0 {
		return nil, fmt.Errorf("trajectory payload must contain non-empty resourceSpans")
	}
	for _, item := range resourceSpans {
		if _, ok := item.(map[string]any); !ok {
			return nil, fmt.Errorf("resourceSpans entries must be mappings")
		}
	}
	attributes := resourceAttributes(payload)
	if attrs, ok := attributes[TrajectoryID].(string); !ok || attrs == "" {
		if attributes[TrajectoryID] == nil {
			return nil, fmt.Errorf("trajectory payload requires a non-empty trajectory_id")
		}
	}
	_, hasTeam := attributes[TeamID]
	_, hasMember := attributes[MemberID]
	if (hasTeam || hasMember) && attributes[SessionID] == nil {
		return nil, fmt.Errorf("team_id/member_id requires session_id")
	}
	return &Trajectory{payload: copyJSON(payload).(map[string]any)}, nil
}

// FromOTLP 创建轨迹并接管脱离载荷的所有权。
// Python: model.py Trajectory.from_otlp。
func FromOTLP(payload map[string]any) (*Trajectory, error) {
	return NewTrajectory(payload)
}

// ToOTLP 返回规范载荷的独立 JSON 拷贝。
// Python: model.py Trajectory.to_otlp。
func (t *Trajectory) ToOTLP() map[string]any {
	return copyJSON(t.payload).(map[string]any)
}

// ResourceAttributes 返回解码后的资源属性（脱离映射）。
// Python: model.py Trajectory.resource_attributes。
func (t *Trajectory) ResourceAttributes() map[string]any {
	return resourceAttributes(t.payload)
}

// TrajectoryID 返回轨迹标识。
// Python: model.py Trajectory.trajectory_id。
func (t *Trajectory) TrajectoryID() string {
	return fmt.Sprint(t.ResourceAttributes()[TrajectoryID])
}

// SessionID 返回会话 ID（可选）。
// Python: model.py Trajectory.session_id。
func (t *Trajectory) SessionID() *string {
	return asOptionalString(firstValue(t.ResourceAttributes(), SessionID))
}

// TeamID 返回团队 ID（可选）。
// Python: model.py Trajectory.team_id。
func (t *Trajectory) TeamID() *string {
	return asOptionalString(firstValue(t.ResourceAttributes(), TeamID))
}

// MemberID 返回成员 ID（可选）。
// Python: model.py Trajectory.member_id。
func (t *Trajectory) MemberID() *string {
	return asOptionalString(firstValue(t.ResourceAttributes(), MemberID))
}

// Metadata 资源元数据的别名，供 Store 查询代码使用。
// Python: model.py Trajectory.metadata。
func (t *Trajectory) Metadata() map[string]any {
	return t.ResourceAttributes()
}

// WithResourceAttributes 返回合并资源属性后的新轨迹（原对象不变）。
// Python: model.py Trajectory.with_resource_attributes。
func (t *Trajectory) WithResourceAttributes(attributes map[string]any) (*Trajectory, error) {
	if attributes == nil {
		return nil, fmt.Errorf("attributes must be a mapping")
	}
	payload := t.ToOTLP()
	resourceSpans, _ := payload["resourceSpans"].([]any)
	if len(resourceSpans) == 0 {
		return nil, fmt.Errorf("trajectory payload must contain non-empty resourceSpans")
	}
	resourceSpan, _ := resourceSpans[0].(map[string]any)
	if resourceSpan == nil {
		resourceSpan = map[string]any{}
	}
	resource, _ := resourceSpan["resource"].(map[string]any)
	if resource == nil {
		resource = map[string]any{}
	}
	encodedAttributes, _ := resource["attributes"].([]any)
	// 规范映射可能以 {"value": AnyValue} 条目形态出现，先归一为列表。
	if encodedAttributes == nil {
		encodedAttributes = []any{}
	}
	byKey := map[string]map[string]any{}
	for _, item := range encodedAttributes {
		entry, ok := item.(map[string]any)
		if !ok || entry["key"] == nil {
			continue
		}
		byKey[fmt.Sprint(entry["key"])] = entry
	}
	for key, value := range attributes {
		if value == nil {
			continue
		}
		item := map[string]any{"key": key, "value": toOTLPValue(value)}
		if existing, exists := byKey[key]; exists {
			for k, v := range item {
				existing[k] = v
			}
		} else {
			encodedAttributes = append(encodedAttributes, item)
			byKey[key] = item
		}
	}
	resource["attributes"] = encodedAttributes
	resourceSpan["resource"] = resource
	resourceSpans[0] = resourceSpan
	payload["resourceSpans"] = resourceSpans
	return FromOTLP(payload)
}
