package lifecycle

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxParseIDs 单次批量操作的 ID 上限
	// Python: lifecycle.py parse_ids 的 1..100
	maxParseIDs = 100
	// defaultPageLimit 默认分页大小
	defaultPageLimit = 20
	// projectionOperationKeys 投影里保留的操作字段
	projectionOperationKeys = 9
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// operationProjectionKeys 投影保留的操作字段顺序。
	// Python: lifecycle.py projection 的 keys 元组
	operationProjectionKeys = []string{
		"operation_id", "resource_type", "resource_id", "kind",
		"status", "phase", "retryable", "updated_at", "stop_pending",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseIDs 解析批量请求里的会话标识列表。
// Python: lifecycle.py parse_ids
//
// 规则与上游一致：
//   - 给了 session_ids 就必须是 1..100 的列表（否则 BAD_REQUEST）
//   - delete 模式允许额外带单条 session_id（置于最前）
//   - 去重后仍不得超过 100
func ParseIDs(params map[string]any, deleteMode bool) ([]string, error) {
	rawIDs := make([]any, 0)
	if raw, exists := params["session_ids"]; exists {
		list, ok := raw.([]any)
		if !ok || len(list) < 1 || len(list) > maxParseIDs {
			return nil, &LifecycleError{
				Code: "BAD_REQUEST", Message: "session_ids must contain 1 to 100 IDs"}
		}
		rawIDs = append(rawIDs, list...)
	}
	if deleteMode {
		if single, exists := params["session_id"]; exists {
			rawIDs = append([]any{single}, rawIDs...)
		}
	}
	if len(rawIDs) == 0 {
		return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "session_ids is required"}
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(rawIDs))
	for _, item := range rawIDs {
		text, ok := item.(string)
		if !ok {
			return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "invalid resource ID"}
		}
		validated, err := ValidateID(text)
		if err != nil {
			return nil, err
		}
		if _, duplicated := seen[validated]; duplicated {
			continue
		}
		seen[validated] = struct{}{}
		result = append(result, validated)
	}
	if len(result) > maxParseIDs {
		return nil, &LifecycleError{
			Code: "BAD_REQUEST", Message: "at most 100 distinct IDs are allowed"}
	}
	return result, nil
}

// Page 对列表做过滤 + 排序 + 分页。
// Python: lifecycle.py page
//
// 过滤：work_mode（work/code）、keyword（title 或 name 模糊）、project_id。
// 排序：archived_at 倒序，同刻按 ID 升序——保证分页稳定。
// offset 语义（非游标）与上游一致。
func Page(items []map[string]any, params map[string]any, key string, maximum int) (map[string]any, error) {
	limit, ok := asPageInt(params["limit"], defaultPageLimit)
	if !ok || limit < 1 || limit > maximum {
		return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "invalid pagination"}
	}
	offset, ok := asPageInt(params["offset"], 0)
	if !ok || offset < 0 {
		return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "invalid pagination"}
	}
	workMode, hasWorkMode := params["work_mode"]
	if hasWorkMode {
		text, isString := workMode.(string)
		if !isString || (text != "work" && text != "code") {
			return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "invalid filter"}
		}
	}
	keyword := ""
	if raw, exists := params["keyword"]; exists {
		text, isString := raw.(string)
		if !isString {
			return nil, &LifecycleError{Code: "BAD_REQUEST", Message: "invalid filter"}
		}
		keyword = strings.ToLower(text)
	}
	var projectFilter string
	if raw, exists := params["project_id"]; exists {
		text, _ := raw.(string)
		validated, err := ValidateID(text)
		if err != nil {
			return nil, err
		}
		projectFilter = validated
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	titleKey := "title"
	if key != "sessions" {
		titleKey = "name"
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if hasWorkMode {
			stored, _ := item["work_mode"].(string)
			if stored != workMode {
				continue
			}
		}
		if keyword != "" {
			title, _ := item[titleKey].(string)
			if !strings.Contains(strings.ToLower(title), keyword) {
				continue
			}
		}
		if projectFilter != "" {
			stored, _ := item["project_id"].(string)
			if stored != projectFilter {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	idKey := "session_id"
	if key != "sessions" {
		idKey = "project_id"
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		leftArchived := asFloat(filtered[i]["archived_at"])
		rightArchived := asFloat(filtered[j]["archived_at"])
		if leftArchived != rightArchived {
			return leftArchived > rightArchived
		}
		leftID, _ := filtered[i][idKey].(string)
		rightID, _ := filtered[j][idKey].(string)
		return leftID < rightID
	})
	total := len(filtered)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	selected := filtered[start:end]
	return map[string]any{
		key:        selected,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(selected) < total,
	}, nil
}

// Projection 生成单个资源的生命周期投影。
// Python: lifecycle.py projection
//
// 调用方若已经读过状态文件（如 event_snapshots），通过 value / projectValue
// 传入，避免同一文件被每个会话条目重复读一次。
func Projection(
	kind, resourceID string, projectID string,
	value map[string]any, projectValue map[string]any, archived *bool,
) map[string]any {
	if value == nil {
		value = State(kind, resourceID)
	}
	operation, _ := value["operation"].(map[string]any)
	if operation != nil {
		if status, _ := operation["status"].(string); status == OperationStatusCompleted {
			operation = nil
		}
	}
	blocked, _ := value["blocked"].(bool)
	if kind == "session" {
		if archived == nil {
			_, archivedPath, err := SessionPaths(resourceID, "")
			blocked = blocked || (err == nil && pathExists(archivedPath))
		} else {
			blocked = blocked || *archived
		}
		if projectID != "" {
			parent := Projection("project", projectID, "", projectValue, nil, nil)
			if parentBlocked, ok := parent["execution_blocked"].(bool); ok && parentBlocked {
				blocked = true
			}
			if operation == nil {
				if parentOperation, ok := parent["lifecycle_operation"].(map[string]any); ok {
					operation = parentOperation
				}
			}
		}
	}
	// 注意：不能直接写 projectOperation(operation)——它返回 map[string]any(nil)，
	// 装进 any 后不等于 nil（Go 的 typed-nil 陷阱），调用方断言 nil 会失败。
	var lifecycleOperation any
	if operation != nil {
		lifecycleOperation = projectOperation(operation)
	}
	return map[string]any{
		"lifecycle_operation": lifecycleOperation,
		"execution_blocked":   blocked,
		"stop_pending":        operation != nil && readBool(operation, "stop_pending"),
	}
}

// ProjectIDFor 由元数据推断项目标识。
// Python: lifecycle.py project_id_for
//
// 缺 project_id 但有 project_dir 时按目录反查（兼容历史数据），
// 反查失败则落到 default / default_code。
func ProjectIDFor(meta map[string]any, projectLookup *ProjectLookup) string {
	if meta == nil {
		return "default"
	}
	if stored, _ := meta["project_id"].(string); strings.TrimSpace(stored) != "" {
		return stored
	}
	if projectDir, _ := meta["project_dir"].(string); strings.TrimSpace(projectDir) != "" {
		lookup := projectLookup
		if lookup == nil {
			built, err := BuildProjectLookup()
			if err == nil {
				lookup = built
			}
		}
		if lookup != nil {
			normalized := NormalizePathForMatch(projectDir)
			if candidates, ok := lookup.ByDirectory[normalized]; ok && len(candidates) > 0 {
				return candidates[0].ProjectID
			}
		}
	}
	if workMode, _ := meta["work_mode"].(string); workMode == "code" {
		return "default_code"
	}
	return "default"
}

// BuildProjectLookup 构建项目目录 → 项目列表的反查表（批量操作只建一次）。
// Python: lifecycle.py build_project_lookup
//
// 会话清单里可能有很多没有 project_id 的老元数据；每会话重建一次映射会
// 把兼容回退变成 projects.json 的 N+1 读取。
//
// ⚠️ 项目存储（Python project_store.py，1213 行）尚未迁移到 Go 侧，
// 此处通过 ProjectSource 窄接口注入；未注入时返回空表，ProjectIDFor 退化为
// default / default_code（与上游无项目数据时的行为一致）。
func BuildProjectLookup() (*ProjectLookup, error) {
	projects := ListProjects()
	lookup := &ProjectLookup{
		ByDirectory: map[string][]ProjectCandidate{},
		WorkModes:   map[string]string{},
	}
	for _, project := range projects {
		if strings.TrimSpace(project.ProjectDir) != "" {
			normalized := NormalizePathForMatch(project.ProjectDir)
			lookup.ByDirectory[normalized] = append(lookup.ByDirectory[normalized],
				ProjectCandidate{ProjectID: project.ProjectID, WorkMode: project.WorkMode})
		}
		lookup.WorkModes[project.ProjectID] = project.WorkMode
	}
	return lookup, nil
}

// EventSnapshots 生成面向 Gateway 的事件刷新快照。
// Python: lifecycle.py event_snapshots
//
// 资源 revision 跨重启保留，Gateway 据此判断是否需要刷新。
// 同一轮里项目状态按 project_id 缓存一次，避免每个会话条目重复读同一文件。
func EventSnapshots() ([]map[string]any, error) {
	directory := filepath.Join(GetAgentRootDir(), "lifecycle", "resources")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, nil
	}
	projectStates := map[string]map[string]any{}
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		value, readErr := ReadJSON(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			continue
		}
		operation, _ := value["operation"].(map[string]any)
		if operation == nil {
			continue
		}
		kind, _ := operation["resource_type"].(string)
		resourceID, _ := operation["resource_id"].(string)
		if kind == "project" {
			if operationKind, _ := operation["kind"].(string); operationKind != OperationKindDelete {
				continue
			}
		}
		projectID := "default"
		if stored, ok := operation["project_id"].(string); ok && stored != "" {
			projectID = stored
		} else if kind == "project" {
			projectID = resourceID
		}
		var projectValue map[string]any
		if kind == "session" {
			cached, ok := projectStates[projectID]
			if !ok {
				cached = State("project", projectID)
				projectStates[projectID] = cached
			}
			projectValue = cached
		}
		projectionProjectID := ""
		if kind == "session" {
			projectionProjectID = projectID
		}
		payload := map[string]any{
			"resource_id":  resourceID,
			"operation_id": operation["operation_id"],
			"revision":     value["revision"],
			"project_id":   projectID,
		}
		for key, item := range Projection(kind, resourceID, projectionProjectID,
			value, projectValue, nil) {
			payload[key] = item
		}
		if kind == "session" {
			payload["session_id"] = resourceID
		}
		operationKind, _ := operation["kind"].(string)
		operationResult, _ := operation["result"].(map[string]any)
		if operationResult == nil {
			operationResult = map[string]any{}
		}
		status, _ := operation["status"].(string)
		result = append(result, map[string]any{
			"event":     kind + ".lifecycle.updated",
			"payload":   payload,
			"completed": status == OperationStatusCompleted,
			"kind":      operationKind,
			"result":    operationResult,
		})
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// projectOperation 抽取投影需要的操作字段；操作为 nil 时返回 nil。
func projectOperation(operation map[string]any) map[string]any {
	if operation == nil {
		return nil
	}
	out := make(map[string]any, projectionOperationKeys)
	for _, key := range operationProjectionKeys {
		out[key] = operation[key]
	}
	return out
}

// readBool 读布尔字段。
func readBool(value map[string]any, key string) bool {
	boolean, _ := value[key].(bool)
	return boolean
}

// asPageInt 把分页参数转整数。
// Python: lifecycle.py _as_page_int
//
// HTTP GET 查询串常把数字变成字符串，WS/JSON 则已是数字，两种都要接受。
// 第二个返回值为 false 表示无法解析。
func asPageInt(value any, fallback int) (int, bool) {
	if value == nil {
		return fallback, true
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), true
		}
		return 0, false
	case string:
		text := strings.TrimSpace(typed)
		parsed, err := strconv.Atoi(text)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
