// scheduling.go 波次调度——依赖拓扑排序与角色子波分组（纯内存，无持久化）。
// Python: action_groups/scheduling.py（全量对齐，127 行）。

package actiongroups

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchedAction 调度输入的动作载体（Python MemberOptimizationAction 的调度字段面）。
type SchedAction struct {
	// ActionID 动作唯一标识
	ActionID string
	// DependsOn 依赖动作 ID 清单
	DependsOn []string
	// TargetPath 目标路径
	TargetPath string
	// DeclaredWritePaths 声明写路径
	DeclaredWritePaths []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildActionWaves 拓扑排序构建依赖波次。
// Python: scheduling.py:15-34 build_action_waves。
func BuildActionWaves(actions []SchedAction) ([][]string, error) {
	// Python: remaining = {action_id: set(depends_on)}
	remaining := map[string]map[string]bool{}
	for _, action := range actions {
		remaining[action.ActionID] = toSet(action.DependsOn)
	}
	// Python: waves = []; completed = set()
	waves := [][]string{}
	completed := map[string]bool{}
	// Python: while remaining: ready = sorted(...); if not ready: raise ValueError(cycle)
	for len(remaining) > 0 {
		var ready []string
		for actionID, deps := range remaining {
			if depsSubset(deps, completed) {
				ready = append(ready, actionID)
			}
		}
		sort.Strings(ready)
		if len(ready) == 0 {
			// Python: raise ValueError(f"dependency cycle detected. Remaining: {deps_left}")
			depsLeft := map[string][]string{}
			for actionID, deps := range remaining {
				depsLeft[actionID] = setToSortedList(deps)
			}
			return nil, fmt.Errorf("dependency cycle detected. Remaining: %v", depsLeft)
		}
		waves = append(waves, ready)
		for _, actionID := range ready {
			completed[actionID] = true
			delete(remaining, actionID)
		}
	}
	return waves, nil
}

// BuildActionWavesFromData 从原始动作字典构建波次。
// Python: scheduling.py:37-53 build_action_waves_from_data。
func BuildActionWavesFromData(actions []map[string]any) ([][]string, error) {
	// Python: remaining = {action["action_id"]: set(action.get("depends_on", []))}
	remaining := map[string]map[string]bool{}
	for _, action := range actions {
		actionID := fmt.Sprintf("%v", action["action_id"])
		dependsOn := []string{}
		if deps, ok := action["depends_on"].([]any); ok {
			for _, dep := range deps {
				dependsOn = append(dependsOn, fmt.Sprintf("%v", dep))
			}
		}
		remaining[actionID] = toSet(dependsOn)
	}
	// Python: 同 build_action_waves 主循环
	waves := [][]string{}
	completed := map[string]bool{}
	for len(remaining) > 0 {
		var ready []string
		for actionID, deps := range remaining {
			if depsSubset(deps, completed) {
				ready = append(ready, actionID)
			}
		}
		sort.Strings(ready)
		if len(ready) == 0 {
			depsLeft := map[string][]string{}
			for actionID, deps := range remaining {
				depsLeft[actionID] = setToSortedList(deps)
			}
			return nil, fmt.Errorf("dependency cycle detected in raw actions. Remaining: %v", depsLeft)
		}
		waves = append(waves, ready)
		for _, actionID := range ready {
			completed[actionID] = true
			delete(remaining, actionID)
		}
	}
	return waves, nil
}

// BuildWavesFromDepsOnly 回退波次重建（无视任何提供的 action_waves 字段）。
// Python: scheduling.py:56-58 build_waves_from_deps_only。
func BuildWavesFromDepsOnly(actions []map[string]any) ([][]string, error) {
	return BuildActionWavesFromData(actions)
}

// ResolveDeclaredPaths 解析声明写路径（缺省回落 [target_path]）。
// Python: scheduling.py:99-103 resolve_declared_paths。
func ResolveDeclaredPaths(action SchedAction) []string {
	// Python: if action.declared_write_paths: return list(...)
	if len(action.DeclaredWritePaths) > 0 {
		return append([]string{}, action.DeclaredWritePaths...)
	}
	// Python: return [action.target_path] if action.target_path else []
	if action.TargetPath != "" {
		return []string{action.TargetPath}
	}
	return []string{}
}

// BuildRoleSubwaves 角色内子波分组（声明写路径重叠的后置）。
// Python: scheduling.py:61-96 build_role_subwaves。
func BuildRoleSubwaves(actions []SchedAction) [][]SchedAction {
	// Python: if not actions: return []
	if len(actions) == 0 {
		return [][]SchedAction{}
	}
	// Python: remaining = list(actions); subwaves = []
	remaining := append([]SchedAction{}, actions...)
	subwaves := [][]SchedAction{}
	// Python: while remaining: first = remaining.pop(0); current_subwave = [first]
	for len(remaining) > 0 {
		first := remaining[0]
		remaining = remaining[1:]
		currentSubwave := []SchedAction{first}
		// Python: later = []; for action in remaining: ...
		var later []SchedAction
		for _, action := range remaining {
			overlaps := false
			for _, existing := range currentSubwave {
				if pathOverlaps(ResolveDeclaredPaths(existing), ResolveDeclaredPaths(action)) {
					overlaps = true
					break
				}
			}
			if overlaps {
				later = append(later, action)
			} else {
				currentSubwave = append(currentSubwave, action)
			}
		}
		// Python: current_subwave.sort(key=lambda action: action.action_id)
		sort.Slice(currentSubwave, func(i, j int) bool {
			return currentSubwave[i].ActionID < currentSubwave[j].ActionID
		})
		subwaves = append(subwaves, currentSubwave)
		remaining = later
	}
	return subwaves
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toSet 字符串集合。
func toSet(list []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range list {
		out[item] = true
	}
	return out
}

// depsSubset 依赖集是否全在 completed 中。
func depsSubset(deps, completed map[string]bool) bool {
	for dep := range deps {
		if !completed[dep] {
			return false
		}
	}
	return true
}

// setToSortedList 集合转排序列表。
func setToSortedList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

// pathOverlaps 两组路径是否相交。
// Python: scheduling.py:106-110 _path_overlaps。
func pathOverlaps(pathsA, pathsB []string) bool {
	// Python: normalized_a/b = {normalized(path) for path in ... if path}
	normalizedA := map[string]bool{}
	for _, p := range pathsA {
		if p != "" {
			normalizedA[normalizePath(p)] = true
		}
	}
	normalizedB := map[string]bool{}
	for _, p := range pathsB {
		if p != "" {
			normalizedB[normalizePath(p)] = true
		}
	}
	for pathA := range normalizedA {
		for pathB := range normalizedB {
			if singlePathOverlaps(pathA, pathB) {
				return true
			}
		}
	}
	return false
}

// normalizePath 路径规范化（posix、去首尾斜杠）。
// Python: scheduling.py:113-114 _normalize_path。
func normalizePath(pathStr string) string {
	return strings.Trim(path.Clean(strings.ReplaceAll(pathStr, "\\", "/")), "/")
}

// singlePathOverlaps 单路径重叠判定（相等或目录前缀包含）。
// Python: scheduling.py:117-119 _single_path_overlaps。
func singlePathOverlaps(pathA, pathB string) bool {
	return pathA == pathB ||
		strings.HasPrefix(pathA, pathB+"/") ||
		strings.HasPrefix(pathB, pathA+"/")
}
