package trajectory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryStore 轨迹持久化接口。
//
// 提供 save/load/query 三个核心操作，支持可选的 version 隔离。
//
// 对应 Python: TrajectoryStore(Protocol)
type TrajectoryStore interface {
	// Save 保存轨迹。version 用于实验隔离。
	Save(trajectory *Trajectory, version string)
	// Load 按 execution_id 加载轨迹。
	Load(executionID string, version string) *Trajectory
	// Query 按条件查询轨迹列表。
	Query(version string, filters map[string]any) []*Trajectory
}

// InMemoryTrajectoryStore 内存轨迹存储，用于测试和开发。
//
// 对应 Python: InMemoryTrajectoryStore
type InMemoryTrajectoryStore struct {
	// data 版本 → executionID → Trajectory
	data map[string]map[string]*Trajectory
}

// FileTrajectoryStore 基于 JSONL 文件的轨迹存储。
//
// 对应 Python: FileTrajectoryStore
type FileTrajectoryStore struct {
	// baseDir 存储目录
	baseDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultVersion 默认版本标识
	defaultVersion = "default"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryTrajectoryStore 创建内存轨迹存储。
//
// 对应 Python: InMemoryTrajectoryStore()
func NewInMemoryTrajectoryStore() *InMemoryTrajectoryStore {
	return &InMemoryTrajectoryStore{
		data: make(map[string]map[string]*Trajectory),
	}
}

// Save 保存轨迹到内存。
//
// 对应 Python: InMemoryTrajectoryStore.save()
func (s *InMemoryTrajectoryStore) Save(trajectory *Trajectory, version string) {
	ver := version
	if ver == "" {
		ver = defaultVersion
	}
	if _, ok := s.data[ver]; !ok {
		s.data[ver] = make(map[string]*Trajectory)
	}
	s.data[ver][trajectory.ExecutionID] = trajectory
}

// Load 从内存加载轨迹。
//
// 对应 Python: InMemoryTrajectoryStore.load()
func (s *InMemoryTrajectoryStore) Load(executionID string, version string) *Trajectory {
	ver := version
	if ver == "" {
		ver = defaultVersion
	}
	if m, ok := s.data[ver]; ok {
		return m[executionID]
	}
	return nil
}

// Query 从内存查询轨迹列表。
//
// 对应 Python: InMemoryTrajectoryStore.query()
func (s *InMemoryTrajectoryStore) Query(version string, filters map[string]any) []*Trajectory {
	ver := version
	if ver == "" {
		ver = defaultVersion
	}
	versionData, ok := s.data[ver]
	if !ok {
		return nil
	}
	trajectories := make([]*Trajectory, 0, len(versionData))
	for _, t := range versionData {
		trajectories = append(trajectories, t)
	}
	// 应用过滤器
	// 对齐 Python: for key, value in filters.items():
	//     trajectories = [t for t in trajectories if getattr(t, key, None) == value]
	for key, value := range filters {
		filtered := make([]*Trajectory, 0, len(trajectories))
		for _, t := range trajectories {
			if matchTrajectoryField(t, key, value) {
				filtered = append(filtered, t)
			}
		}
		trajectories = filtered
	}
	return trajectories
}

// NewFileTrajectoryStore 创建文件轨迹存储。
//
// 对应 Python: FileTrajectoryStore(base_dir)
func NewFileTrajectoryStore(baseDir string) *FileTrajectoryStore {
	// 对齐 Python: self._base_dir.mkdir(parents=True, exist_ok=True)
	_ = os.MkdirAll(baseDir, 0o755)
	return &FileTrajectoryStore{baseDir: baseDir}
}

// Save 追加轨迹到 JSONL 文件。
//
// 对应 Python: FileTrajectoryStore.save()
func (s *FileTrajectoryStore) Save(trajectory *Trajectory, version string) {
	filePath := s.getFilePath(version)
	data := trajectoryToDict(trajectory)
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	// 追加写入，对齐 Python: with open(file_path, "a", encoding="utf-8") as f
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(b)
	_, _ = f.Write([]byte("\n"))
}

// Load 按 execution_id 从 JSONL 文件加载轨迹。
//
// 对应 Python: FileTrajectoryStore.load()
func (s *FileTrajectoryStore) Load(executionID string, version string) *Trajectory {
	filePath := s.getFilePath(version)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil
	}
	lines, err := readLines(filePath)
	if err != nil {
		return nil
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		if id, _ := data["execution_id"].(string); id == executionID {
			return dictToTrajectory(data)
		}
	}
	return nil
}

// Query 从 JSONL 文件查询匹配的轨迹列表。
//
// 对应 Python: FileTrajectoryStore.query()
func (s *FileTrajectoryStore) Query(version string, filters map[string]any) []*Trajectory {
	filePath := s.getFilePath(version)
	results := make([]*Trajectory, 0)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return results
	}
	lines, err := readLines(filePath)
	if err != nil {
		return results
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		// 应用过滤器
		match := true
		for key, value := range filters {
			if data[key] != value {
				match = false
				break
			}
		}
		if match {
			traj := dictToTrajectory(data)
			if traj != nil {
				results = append(results, traj)
			}
		}
	}
	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getFilePath 获取版本对应的 JSONL 文件路径。
//
// 对应 Python: FileTrajectoryStore._get_file_path()
func (s *FileTrajectoryStore) getFilePath(version string) string {
	ver := version
	if ver == "" {
		ver = defaultVersion
	}
	filename := fmt.Sprintf("trajectories_%s.jsonl", ver)
	return filepath.Join(s.baseDir, filename)
}

// trajectoryToDict 将 Trajectory 转换为可 JSON 序列化的字典。
//
// 对应 Python: FileTrajectoryStore._trajectory_to_dict()
func trajectoryToDict(trajectory *Trajectory) map[string]any {
	result := toJSONCompatible(trajectory)
	if m, ok := result.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// toJSONCompatible 递归转换值到 JSON 兼容数据。
//
// 对齐 Python:
//
//	if hasattr(obj, "model_dump") and callable(obj.model_dump):
//	    return _to_json_compatible(obj.model_dump())
//	if hasattr(obj, "__dataclass_fields__"):
//	    return _to_json_compatible(asdict(obj))
//	if isinstance(obj, (list, tuple)):
//	    return [_to_json_compatible(item) for item in obj]
//	if isinstance(obj, dict):
//	    return {str(key): _to_json_compatible(value) for key, value in obj.items()}
//	if isinstance(obj, (str, int, float, bool)) or obj is None:
//	    return obj
//	return str(obj)
//
// 对应 Python: FileTrajectoryStore._to_json_compatible()
func toJSONCompatible(obj any) any {
	if obj == nil {
		return nil
	}
	// 优先使用 JSON 序列化（等价于 Python model_dump / asdict）
	b, err := json.Marshal(obj)
	if err != nil {
		return fmt.Sprint(obj)
	}
	var unmarshalled any
	if err := json.Unmarshal(b, &unmarshalled); err != nil {
		return fmt.Sprint(obj)
	}
	return jsonSafeRecursive(unmarshalled)
}

// jsonSafeRecursive 递归处理 JSON 反序列化后的值，确保所有键为字符串。
// 与 JSONSafe 类似，但处理已反序列化的 any 类型。
func jsonSafeRecursive(v any) any {
	switch val := v.(type) {
	case nil, string, int, float64, bool:
		return val
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = jsonSafeRecursive(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(val))
		for key, item := range val {
			result[key] = jsonSafeRecursive(item)
		}
		return result
	default:
		return fmt.Sprint(val)
	}
}

// dictToTrajectory 将字典转换为 Trajectory。
//
// 对齐 Python:
//
//	steps_data = data.get("steps", [])
//	steps = []
//	for step_data in steps_data:
//	    detail_data = step_data.pop("detail", None)
//	    detail = None
//	    if detail_data:
//	        if "messages" in detail_data:
//	            detail = LLMCallDetail(**detail_data)
//	        elif "tool_name" in detail_data:
//	            detail = ToolCallDetail(**detail_data)
//	    step_data["detail"] = detail
//	    steps.append(TrajectoryStep(**step_data))
//	data["steps"] = steps
//	return Trajectory(**data)
//
// 对应 Python: FileTrajectoryStore._dict_to_trajectory()
func dictToTrajectory(data map[string]any) *Trajectory {
	defer func() {
		// 对齐 Python: except (KeyError, TypeError, ValueError): return None
		_ = recover()
	}()

	// 转换 steps
	stepsRaw, _ := data["steps"].([]any)
	steps := make([]*TrajectoryStep, 0, len(stepsRaw))
	for _, stepRaw := range stepsRaw {
		stepData, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		// 提取并转换 detail
		var detail StepDetail
		if detailData, ok := stepData["detail"].(map[string]any); ok && len(detailData) > 0 {
			if _, hasMessages := detailData["messages"]; hasMessages {
				detail = mapToLLMCallDetail(detailData)
			} else if _, hasToolName := detailData["tool_name"]; hasToolName {
				detail = mapToToolCallDetail(detailData)
			}
		}
		stepData["detail"] = detail
		steps = append(steps, mapToTrajectoryStep(stepData))
	}

	return &Trajectory{
		ExecutionID: toString(data["execution_id"]),
		Steps:       steps,
		Source:      toString(data["source"]),
		CaseID:      toString(data["case_id"]),
		SessionID:   toString(data["session_id"]),
		Cost:        toCostInfo(data["cost"]),
		Meta:        toMapAny(data["meta"]),
	}
}

// mapToLLMCallDetail 从字典构造 LLMCallDetail。
func mapToLLMCallDetail(data map[string]any) *LLMCallDetail {
	return &LLMCallDetail{
		Model:    toString(data["model"]),
		Messages: toSliceOfMapAny(data["messages"]),
		Response: toMapAny(data["response"]),
		Tools:    toSliceOfMapAny(data["tools"]),
		Usage:    toMapAny(data["usage"]),
		Meta:     toMapAny(data["meta"]),
	}
}

// mapToToolCallDetail 从字典构造 ToolCallDetail。
func mapToToolCallDetail(data map[string]any) *ToolCallDetail {
	return &ToolCallDetail{
		ToolName:        toString(data["tool_name"]),
		CallArgs:        toMapAny(data["call_args"]),
		CallResult:      toMapAny(data["call_result"]),
		ToolDescription: toString(data["tool_description"]),
		ToolSchema:      toMapAny(data["tool_schema"]),
		ToolCallID:      toString(data["tool_call_id"]),
	}
}

// mapToTrajectoryStep 从字典构造 TrajectoryStep。
func mapToTrajectoryStep(data map[string]any) *TrajectoryStep {
	detail, _ := data["detail"].(StepDetail)
	return &TrajectoryStep{
		Kind:               StepKind(toString(data["kind"])),
		Error:              toMapAny(data["error"]),
		StartTimeMs:        toInt(data["start_time_ms"]),
		EndTimeMs:          toInt(data["end_time_ms"]),
		Detail:             detail,
		Reward:             toFloat64(data["reward"]),
		PromptTokenIDs:     toIntSlice(data["prompt_token_ids"]),
		CompletionTokenIDs: toIntSlice(data["completion_token_ids"]),
		Logprobs:           data["logprobs"],
		Meta:               toMapAny(data["meta"]),
	}
}

// toString 安全转换为字符串。
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// toInt 安全转换为 int。
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		return 0
	case string:
		var i int
		fmt.Sscanf(n, "%d", &i)
		return i
	default:
		return 0
	}
}

// toFloat64 安全转换为 float64。
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// toMapAny 安全转换为 map[string]any。
func toMapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// toSliceOfMapAny 安全转换为 []map[string]any。
func toSliceOfMapAny(v any) []map[string]any {
	if slice, ok := v.([]any); ok {
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

// toIntSlice 安全转换为 []int。
func toIntSlice(v any) []int {
	if slice, ok := v.([]any); ok {
		result := make([]int, 0, len(slice))
		for _, item := range slice {
			result = append(result, toInt(item))
		}
		return result
	}
	return nil
}

// toCostInfo 安全转换为 CostInfo。
func toCostInfo(v any) CostInfo {
	if m, ok := v.(map[string]any); ok {
		result := make(CostInfo, len(m))
		for key, val := range m {
			result[key] = toInt(val)
		}
		return result
	}
	return nil
}

// readLines 读取文件所有行。
func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n"), nil
}

// matchTrajectoryField 按 key 匹配 Trajectory 字段值。
//
// 对齐 Python: getattr(t, key, None) == value
func matchTrajectoryField(t *Trajectory, key string, value any) bool {
	switch key {
	case "execution_id":
		return t.ExecutionID == fmt.Sprint(value)
	case "source":
		return t.Source == fmt.Sprint(value)
	case "case_id":
		return t.CaseID == fmt.Sprint(value)
	case "session_id":
		return t.SessionID == fmt.Sprint(value)
	default:
		return false
	}
}
