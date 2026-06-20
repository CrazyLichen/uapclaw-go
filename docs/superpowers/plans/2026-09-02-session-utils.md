# 5.14 Session Utils 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `state/utils.go` 中的纯工具函数迁出到独立的 `session/utils` 包并导出，供 state、store、tracer、graph 等多个包共享使用，同时回填 state 包的调用点。

**Architecture:** 新建 `session/utils/` 子包，按职责拆分为 path.go / ref.go / dict.go / container.go / string.go / constants.go 六个文件。state 包保留依赖 `StateKey` 的函数（getBySchema 等），内部调用改为引用 utils 包的导出函数。依赖方向：`state → utils`（单向无环）。

**Tech Stack:** Go 1.22+，标准库，项目内 logger 包

**设计文档:** `docs/superpowers/specs/2026-09-02-session-utils-design.md`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/session/utils/doc.go` | 包文档 |
| `internal/agentcore/session/utils/constants.go` | 常量定义 |
| `internal/agentcore/session/utils/string.go` | 字符串辅助操作 |
| `internal/agentcore/session/utils/path.go` | 嵌套路径操作 |
| `internal/agentcore/session/utils/ref.go` | 引用路径操作 |
| `internal/agentcore/session/utils/dict.go` | 字典操作 |
| `internal/agentcore/session/utils/container.go` | 容器操作（深拷贝、安全扩展） |
| `internal/agentcore/session/utils/utils_test.go` | utils 包单元测试 |

### 修改文件

| 文件 | 改动内容 |
|------|---------|
| `internal/agentcore/session/state/utils.go` | 删除迁出函数，保留函数改为引用 utils 包 |
| `internal/agentcore/session/state/utils_test.go` | 迁出函数的测试删除，保留函数测试不动 |
| `internal/agentcore/session/state/inmemory_state.go` | `deepCopyValue` → `utils.DeepCopyValue` 等 |
| `internal/agentcore/session/state/inmemory_commit_state.go` | `deepCopyMap` → `utils.DeepCopyMap` 等 |
| `internal/agentcore/session/state/key.go` | `deepCopyMap` → `utils.DeepCopyMap` 等 |
| `internal/agentcore/session/state/workflow_commit_state.go` | `deepCopyUpdates` → `utils.DeepCopyUpdates` 等 |
| `internal/agentcore/session/state/doc.go` | 更新文件目录 |
| `internal/agentcore/session/doc.go` | 添加 utils 子包到文件目录 |
| `IMPLEMENTATION_PLAN.md` | 更新 5.14 状态 |

---

### Task 1: 创建 utils 包骨架（doc.go + constants.go）

**Files:**
- Create: `internal/agentcore/session/utils/doc.go`
- Create: `internal/agentcore/session/utils/constants.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package utils 提供会话系统的通用工具函数，包括嵌套路径操作、引用路径解析、
// 字典操作和容器深拷贝等。
//
// 本包从 state/utils.go 中迁出，作为独立子包供 state、store、tracer、graph 等
// 多个包共享使用，避免循环依赖。
//
// 文件目录：
//
//	utils/
//	├── doc.go           # 包文档
//	├── path.go          # 嵌套路径操作（SplitNestedPath, GetValueByNestedPath, RootToPath, RootToIndex）
//	├── ref.go           # 引用路径操作（IsRefPath, ExtractOriginKey）
//	├── dict.go          # 字典操作（UpdateDict, UpdateByKey, DeleteByKey, ExpandNestedStructure）
//	├── container.go     # 容器操作（SafeExtendContainer, DeepCopyMap/Slice/Value/Updates, ConvertUpdatesFromJSON）
//	├── string.go        # 字符串辅助（ContainsChar, ContainsSubstring, SplitString, ParseListIndexes）
//	└── constants.go     # 常量（RegexMaxLength, NestedPathSplit, NestedPathListSplit）
//
// 对应 Python 代码：openjiuwen/core/session/utils.py
//
// 未移植说明：
//   - create_wrapper_class：Python 中零调用，Go 使用手动委托模式替代（见 NodeSessionFacade/RouterSessionFacade）
//   - EndFrame/Frame：Go 使用 close(ch) 替代单源场景，多源场景 ⤵️ 延后到 8.x stream_actor 回填
package utils
```

- [ ] **Step 2: 创建 constants.go**

```go
package utils

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RegexMaxLength 正则匹配最大长度
	RegexMaxLength = 1000
	// NestedPathSplit 嵌套路径分隔符
	NestedPathSplit = "."
	// NestedPathListSplit 列表索引开始符
	NestedPathListSplit = "["
)
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/utils/
git commit -m "feat(session-utils): 创建 utils 包骨架 doc.go + constants.go"
```

---

### Task 2: 创建 string.go（字符串辅助函数）

**Files:**
- Create: `internal/agentcore/session/utils/string.go`

- [ ] **Step 1: 创建 string.go**

将 `state/utils.go` 中 `containsChar`、`containsSubstring`、`splitString`、`parseListIndexes` 四个函数迁出，改为导出，内部引用改为 `utils.NestedPathListSplit` 等常量。

```go
package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// ContainsChar 检查字符串是否包含指定字符/子串
func ContainsChar(s, substr string) bool {
	return len(s) >= len(substr) && ContainsSubstring(s, substr)
}

// ContainsSubstring 检查字符串是否包含子串
func ContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// SplitString 按分隔符拆分字符串
func SplitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// ParseListIndexes 解析包含数组索引的部分
// 例: "c[1]" → ["c", 1], "[1]" → [1], "a[-1]" → ["a", -1]
func ParseListIndexes(part string) []any {
	var result []any
	bracketIdx := -1
	for i := 0; i < len(part); i++ {
		if part[i] == '[' {
			bracketIdx = i
			break
		}
	}
	if bracketIdx == -1 {
		return []any{part}
	}

	base := part[:bracketIdx]
	if base != "" {
		result = append(result, base)
	}

	remaining := part[bracketIdx:]
	for len(remaining) > 0 {
		if remaining[0] != '[' {
			break
		}
		end := -1
		for i := 1; i < len(remaining); i++ {
			if remaining[i] == ']' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		indexStr := remaining[1:end]
		var idx int
		isNeg := false
		parseStart := 0
		if len(indexStr) > 0 && indexStr[0] == '-' {
			isNeg = true
			parseStart = 1
		}
		isInt := true
		if parseStart >= len(indexStr) {
			isInt = false
		} else {
			parsed := 0
			for i := parseStart; i < len(indexStr); i++ {
				if indexStr[i] < '0' || indexStr[i] > '9' {
					isInt = false
					break
				}
				parsed = parsed*10 + int(indexStr[i]-'0')
			}
			if isInt {
				if isNeg {
					idx = -parsed
				} else {
					idx = parsed
				}
			}
		}
		if isInt {
			result = append(result, idx)
		} else {
			result = append(result, indexStr)
		}
		remaining = remaining[end+1:]
	}
	return result
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/utils/string.go
git commit -m "feat(session-utils): 迁出字符串辅助函数 ContainsChar/SplitString/ParseListIndexes"
```

---

### Task 3: 创建 path.go（嵌套路径操作）

**Files:**
- Create: `internal/agentcore/session/utils/path.go`

- [ ] **Step 1: 创建 path.go**

将 `splitNestedPath`、`getValueByNestedPath`、`rootToPath`、`rootToIndex` 四个函数迁出，改为导出。`parentEntry` 结构体和 `writeBackList` 辅助函数也迁入此文件（非导出），因为它们是 `rootToPath` 的内部实现。

```go
package utils

// ──────────────────────────── 结构体 ────────────────────────────

// parentEntry 父容器追踪条目，用于列表 append 后回写。
type parentEntry struct {
	// m 父 map（如果父容器是 map）
	m map[string]any
	// mKey 在父 map 中的键
	mKey string
	// l 父 list（如果父容器是 list）
	l []any
	// lIdx 在父 list 中的索引
	lIdx int
	// isMap 父容器是 map 还是 list
	isMap bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SplitNestedPath 拆分嵌套路径
// 例: "a_1.b.c[1].d" → ["a_1", "b", "c", 1, "d"]
func SplitNestedPath(nestedKey string) []any {
	if nestedKey == "" {
		return nil
	}
	if !ContainsChar(nestedKey, NestedPathSplit) &&
		!ContainsChar(nestedKey, NestedPathListSplit) &&
		!ContainsChar(nestedKey, "['") {
		return nil
	}

	var result []any
	parts := SplitString(nestedKey, NestedPathSplit)
	for _, part := range parts {
		if ContainsChar(part, NestedPathListSplit) {
			baseAndIndexes := ParseListIndexes(part)
			result = append(result, baseAndIndexes...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// GetValueByNestedPath 根据嵌套路径从 source 获取值
// 例: "a.b[0].c" → source["a"]["b"][0]["c"]
func GetValueByNestedPath(nestedKey string, source map[string]any) any {
	paths := SplitNestedPath(nestedKey)
	if len(paths) == 0 {
		return source[nestedKey]
	}

	var current any = source
	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			val, exists := m[p]
			if !exists {
				return nil
			}
			if isLast {
				return val
			}
			current = val
		case int:
			list, ok := current.([]any)
			if !ok {
				return nil
			}
			idx := p
			if idx < 0 {
				idx = len(list) + idx
				if idx < 0 {
					return nil
				}
			}
			if idx >= len(list) {
				return nil
			}
			if isLast {
				return list[idx]
			}
			current = list[idx]
		}
	}
	return nil
}

// RootToPath 沿嵌套路径导航到最终容器
// 返回 (最终key, 最终容器)
// 最终容器可能是 map[string]any 或 []any，对应最终 key 为 string 或 int
// createIfAbsent 为 true 时自动创建缺失的中间节点
func RootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, any) {
	create := len(createIfAbsent) > 0 && createIfAbsent[0]
	paths := SplitNestedPath(nestedPath)
	if len(paths) == 0 {
		return nestedPath, source
	}

	var current any = source
	// 父容器追踪栈，用于列表 append 后回写
	parents := make([]parentEntry, 0, len(paths))

	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return nil, nil
			}
			if _, exists := m[p]; !exists {
				if !create {
					return nil, nil
				}
				if !isLast && i+1 < len(paths) {
					if _, isInt := paths[i+1].(int); isInt {
						m[p] = []any{}
					} else {
						m[p] = map[string]any{}
					}
				} else {
					m[p] = map[string]any{}
				}
			}
			if isLast {
				return p, m
			}
			// 支持中间节点为 map 或 list
			switch next := m[p].(type) {
			case map[string]any:
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			case []any:
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			default:
				if !create {
					return nil, nil
				}
				next = map[string]any{}
				m[p] = next
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			}
		case int:
			list, ok := current.([]any)
			if !ok {
				return nil, nil
			}
			idx := p
			if idx < 0 {
				idx = len(list) + idx
				if idx < 0 {
					return nil, nil
				}
			}
			// 自动扩展列表
			if idx >= len(list) {
				if !create {
					return nil, nil
				}
				var ok2 bool
				list, ok2 = SafeExtendContainer(list, idx, isLast)
				if !ok2 {
					return nil, nil
				}
				// 回写到父容器（append 可能换了底层数组）
				writeBackList(parents, list)
			}
			if isLast {
				return idx, list
			}
			if idx >= len(list) {
				return nil, nil
			}
			parents = append(parents, parentEntry{l: list, lIdx: idx, isMap: false})
			current = list[idx]
		}
	}
	return nil, nil
}

// RootToIndex 通过纯索引路径导航嵌套列表结构。
// 对齐 Python root_to_index。
// 返回 (调整后的最终索引, 最终容器列表)。
// 嵌套深度上限 10，索引范围 [0,10000]，支持负索引自动调整。
func RootToIndex(indexes []int, source []any, createIfAbsent bool) (int, []any) {
	if source == nil || len(indexes) == 0 {
		return -1, nil
	}
	if len(indexes) > 10 {
		return -1, nil
	}

	current := source

	// 处理中间索引
	for i := 0; i < len(indexes)-1; i++ {
		idx := indexes[i]
		// 处理负索引
		if idx < 0 {
			idx = len(current) + idx
			if idx < 0 {
				return -1, nil
			}
		} else if idx > 10000 {
			return -1, nil
		}
		// 越界扩展
		if idx >= len(current) {
			if !createIfAbsent {
				return -1, nil
			}
			var ok bool
			current, ok = SafeExtendContainer(current, idx, false)
			if !ok {
				return -1, nil
			}
		}
		// 安全访问
		if idx >= len(current) {
			return -1, nil
		}
		next, ok := current[idx].([]any)
		if !ok {
			if current[idx] != nil {
				return -1, nil
			}
			// nil 位置自动创建列表
			if !createIfAbsent {
				return -1, nil
			}
			next = []any{}
			current[idx] = next
		}
		current = next
	}

	// 处理最终索引
	finalIdx := indexes[len(indexes)-1]
	if finalIdx < 0 {
		finalIdx = len(current) + finalIdx
		if finalIdx < 0 {
			return -1, nil
		}
	} else if finalIdx > 10000 {
		return -1, nil
	}
	if finalIdx >= len(current) {
		if !createIfAbsent {
			return -1, nil
		}
		var ok bool
		current, ok = SafeExtendContainer(current, finalIdx, true)
		if !ok {
			return -1, nil
		}
	}
	if finalIdx < 0 || finalIdx >= len(current) {
		return -1, nil
	}
	return finalIdx, current
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeBackList 将 append 后可能更换底层数组的 list 回写到父容器。
func writeBackList(parents []parentEntry, list []any) {
	if len(parents) == 0 {
		return
	}
	parent := parents[len(parents)-1]
	if parent.isMap {
		parent.m[parent.mKey] = list
	} else {
		parent.l[parent.lIdx] = list
	}
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/utils/path.go
git commit -m "feat(session-utils): 迁出嵌套路径操作 SplitNestedPath/GetValueByNestedPath/RootToPath/RootToIndex"
```

---

### Task 4: 创建 ref.go（引用路径操作）

**Files:**
- Create: `internal/agentcore/session/utils/ref.go`

- [ ] **Step 1: 创建 ref.go**

```go
package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// IsRefPath 判断是否为引用路径，如 "${start123.p2}"
func IsRefPath(path string) bool {
	return len(path) > 3 && len(path) <= RegexMaxLength &&
		path[:2] == "${" && path[len(path)-1] == '}'
}

// ExtractOriginKey 从引用路径中提取原始 key
// 例: "${start123.p2}" → "start123.p2"
func ExtractOriginKey(key string) string {
	if !ContainsChar(key, "$") {
		return key
	}
	start := -1
	for i := 0; i < len(key) && i < RegexMaxLength; i++ {
		if i+1 < len(key) && key[i] == '$' && key[i+1] == '{' {
			start = i + 2
			break
		}
	}
	if start == -1 {
		return key
	}
	for i := start; i < len(key); i++ {
		if key[i] == '}' {
			return key[start:i]
		}
	}
	return key
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/utils/ref.go
git commit -m "feat(session-utils): 迁出引用路径操作 IsRefPath/ExtractOriginKey"
```

---

### Task 5: 创建 dict.go（字典操作）

**Files:**
- Create: `internal/agentcore/session/utils/dict.go`

- [ ] **Step 1: 创建 dict.go**

将 `updateDict`、`updateByKey`、`deleteByKey`、`expandNestedStructure` 迁出。

```go
package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// UpdateDict 用 update 字典更新 source 字典
// source 是扁平结构，update 的 key 支持嵌套路径
// 如果 value 为 nil 则删除对应 key
func UpdateDict(update map[string]any, source map[string]any) {
	type removal struct {
		key       any
		container any // map[string]any 或 []any
	}
	var removed []removal

	for key, value := range update {
		currentKey, currentContainer := RootToPath(key, source, true)
		if value == nil {
			removed = append(removed, removal{key: currentKey, container: currentContainer})
		} else {
			UpdateByKey(currentKey, value, currentContainer)
		}
	}
	for _, r := range removed {
		DeleteByKey(r.key, r.container)
	}
}

// UpdateByKey 在 source 中按 key 更新值
// source 可以是 map[string]any 或 []any，key 对应为 string 或 int
func UpdateByKey(key any, newValue any, source any) {
	switch k := key.(type) {
	case string:
		m, ok := source.(map[string]any)
		if !ok {
			return
		}
		if _, exists := m[k]; !exists {
			m[k] = ExpandNestedStructure(newValue)
			return
		}
		if existing, ok := m[k].(map[string]any); ok {
			if newMap, ok := newValue.(map[string]any); ok {
				UpdateDict(newMap, existing)
				return
			}
		}
		m[k] = ExpandNestedStructure(newValue)
	case int:
		list, ok := source.([]any)
		if !ok {
			return
		}
		if k >= 0 && k < len(list) {
			list[k] = ExpandNestedStructure(newValue)
		}
	}
}

// DeleteByKey 在 source 中按 key 删除
// source 可以是 map[string]any 或 []any
func DeleteByKey(key any, source any) {
	switch k := key.(type) {
	case string:
		if m, ok := source.(map[string]any); ok {
			delete(m, k)
		}
	case int:
		if list, ok := source.([]any); ok {
			if k >= 0 && k < len(list) {
				list[k] = nil
			}
		}
	}
}

// ExpandNestedStructure 将嵌套 key 的字典展开为嵌套结构
// 例: {"a.b": 1} → {"a": {"b": 1}}
func ExpandNestedStructure(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, value := range v {
			currentKey, currentContainer := RootToPath(key, result, true)
			if currentKey == nil {
				continue
			}
			switch kk := currentKey.(type) {
			case string:
				if m, ok := currentContainer.(map[string]any); ok {
					m[kk] = ExpandNestedStructure(value)
				}
			case int:
				if list, ok := currentContainer.([]any); ok && kk < len(list) {
					list[kk] = ExpandNestedStructure(value)
				}
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = ExpandNestedStructure(item)
		}
		return result
	default:
		return data
	}
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/utils/dict.go
git commit -m "feat(session-utils): 迁出字典操作 UpdateDict/UpdateByKey/DeleteByKey/ExpandNestedStructure"
```

---

### Task 6: 创建 container.go（容器操作）

**Files:**
- Create: `internal/agentcore/session/utils/container.go`

- [ ] **Step 1: 创建 container.go**

将 `safeExtendContainer`、`deepCopyMap`、`deepCopySlice`、`deepCopyValue`、`deepCopyUpdates`、`convertUpdatesFromJSON` 迁出。

```go
package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// SafeExtendContainer 安全地扩展列表容器到 targetIndex 位置。
// 对齐 Python _safe_extend_container。
// 中间位置用 nil 填充，目标位置放空字典（isFinal=true）或空列表（isFinal=false）。
// 有上限保护（索引 [0,10000]、扩展量 ≤ 10000）。
func SafeExtendContainer(container []any, targetIndex int, isFinal bool) ([]any, bool) {
	if targetIndex < 0 || targetIndex > 10000 {
		return container, false
	}
	currentLen := len(container)
	if targetIndex < currentLen {
		return container, true
	}
	expansionNeeded := targetIndex - currentLen + 1
	if expansionNeeded > 10000 {
		return container, false
	}
	// 填充中间位置
	for i := currentLen; i < targetIndex; i++ {
		container = append(container, nil)
	}
	// 目标位置
	if isFinal {
		container = append(container, map[string]any{})
	} else {
		container = append(container, []any{})
	}
	return container, true
}

// DeepCopyMap 深拷贝 map[string]any
func DeepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = DeepCopyValue(v)
	}
	return dst
}

// DeepCopySlice 深拷贝 []any
func DeepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = DeepCopyValue(v)
	}
	return dst
}

// DeepCopyValue 深拷贝任意值（map/slice/原始值）
func DeepCopyValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return DeepCopyMap(v)
	case []any:
		return DeepCopySlice(v)
	default:
		return v // string/int/float/bool/nil 等原始值直接返回
	}
}

// DeepCopyUpdates 深拷贝暂存更新数据
func DeepCopyUpdates(updates map[string][]map[string]any) map[string][]map[string]any {
	if updates == nil {
		return nil
	}
	result := make(map[string][]map[string]any, len(updates))
	for key, list := range updates {
		copied := make([]map[string]any, len(list))
		for i, u := range list {
			copied[i] = DeepCopyMap(u)
		}
		result[key] = copied
	}
	return result
}

// ConvertUpdatesFromJSON 将 JSON 反序列化后的 updates 数据转换为 map[string][]map[string]any。
//
// JSON 反序列化会将 []map[string]any 变为 []any（每个元素是 map[string]any），
// 导致类型断言 gs.(map[string][]map[string]any) 失败。此函数递归处理，
// 将 map[string]any 中值为 []any 的字段转换为 []map[string]any。
func ConvertUpdatesFromJSON(raw any) (map[string][]map[string]any, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	result := make(map[string][]map[string]any, len(m))
	for key, val := range m {
		slice, ok := val.([]any)
		if !ok {
			return nil, false
		}
		maps := make([]map[string]any, len(slice))
		for i, item := range slice {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			maps[i] = itemMap
		}
		result[key] = maps
	}
	return result, true
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/utils/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/utils/container.go
git commit -m "feat(session-utils): 迁出容器操作 SafeExtendContainer/DeepCopy*/ConvertUpdatesFromJSON"
```

---

### Task 7: 重构 state/utils.go — 删除迁出函数，保留函数改用 utils 包

**Files:**
- Modify: `internal/agentcore/session/state/utils.go`

这是核心步骤。从 `state/utils.go` 中删除所有已迁出到 `utils` 包的函数，保留依赖 `StateKey` 的函数，内部调用改为 `utils.Xxx`。

- [ ] **Step 1: 重写 state/utils.go**

删除以下函数/常量/结构体：
- 常量: `regexMaxLength`, `nestedPathSplit`, `nestedPathListSplit`
- 结构体: `parentEntry`
- 函数: `deepCopyMap`, `deepCopySlice`, `deepCopyValue`, `splitNestedPath`, `isRefPath`, `extractOriginKey`, `updateDict`, `getValueByNestedPath`, `safeExtendContainer`, `rootToIndex`, `rootToPath`, `writeBackList`, `expandNestedStructure`, `updateByKey`, `deleteByKey`, `containsChar`, `containsSubstring`, `splitString`, `parseListIndexes`, `deepCopyUpdates`, `convertUpdatesFromJSON`

保留以下函数，内部调用改为 `utils.Xxx`：
- `getBySchema`（依赖 `StateKey`）
- `getBySchemaMap`（依赖 `StateKey`）
- `getBySchemaList`（依赖 `StateKey`）
- `getValueByNestedPathMap`（被 `getBySchema` 专用）

文件内容变为：

```go
package state

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// getBySchema 根据 schema 从 data 中获取值
// schema 可以是 string（路径）、map[string]any（批量映射）、[]any（列表映射）
// isRoot 表示是否为根层调用：根层时字符串 schema 视为数据路径，非根层时非引用路径的字符串视为默认值
func getBySchema(schema StateKey, data map[string]any, isRootOrNestedPath ...any) any {
	isRoot := true
	var nestedPath string
	for _, arg := range isRootOrNestedPath {
		switch v := arg.(type) {
		case string:
			nestedPath = v
		case bool:
			isRoot = v
		}
	}

	if nestedPath != "" {
		data = getValueByNestedPathMap(nestedPath, data)
	}

	if data == nil {
		return nil
	}

	switch schema.Type() {
	case StateKeyString:
		originKey := utils.ExtractOriginKey(schema.String())
		// 非根层 + 非引用路径 → 字符串本身就是值，不从 data 中查找
		if originKey == schema.String() && !isRoot {
			return schema.String()
		}
		return utils.GetValueByNestedPath(originKey, data)
	case StateKeyMap:
		return getBySchemaMap(schema.Map(), data)
	case StateKeyList:
		return getBySchemaList(schema.List(), data)
	default:
		return nil
	}
}

// getValueByNestedPathMap 与 GetValueByNestedPath 类似，但返回 map[string]any
// 用于 getBySchema 中根据前缀定位
func getValueByNestedPathMap(nestedKey string, source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := utils.GetValueByNestedPath(nestedKey, source)
	if m, ok := result.(map[string]any); ok {
		return m
	}
	return nil
}

// getBySchemaMap 处理 map schema 的递归读取
// 对应 Python: get_by_schema 中 dict 分支
// 只有引用路径（${...}）才从 data 取值，普通字符串保留为默认值
func getBySchemaMap(schema map[string]any, data map[string]any) map[string]any {
	result := map[string]any{}
	for targetKey, targetSchema := range schema {
		switch s := targetSchema.(type) {
		case []any:
			result[targetKey] = getBySchema(ListKey(s), data, false)
		case map[string]any:
			result[targetKey] = getBySchema(SchemaKey(s), data, false)
		case string:
			if utils.IsRefPath(s) {
				// 引用路径 → 从 data 取值
				result[targetKey] = getBySchema(StringKey(s), data, false)
			} else {
				// 普通字符串 → 保留为默认值
				result[targetKey] = s
			}
		default:
			result[targetKey] = targetSchema
		}
	}
	return result
}

// getBySchemaList 处理 list schema 的递归读取
// 对应 Python: get_by_schema 中 list 分支
func getBySchemaList(schema []any, data map[string]any) []any {
	result := make([]any, len(schema))
	for i, item := range schema {
		switch s := item.(type) {
		case string:
			if utils.IsRefPath(s) {
				result[i] = getBySchema(StringKey(s), data, false)
			} else {
				result[i] = s
			}
		case map[string]any:
			result[i] = getBySchema(SchemaKey(s), data, false)
		case []any:
			result[i] = getBySchema(ListKey(s), data, false)
		default:
			result[i] = item
		}
	}
	return result
}
```

- [ ] **Step 2: 验证编译（state 包单独）**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/state/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/utils.go
git commit -m "refactor(state): 删除已迁出函数，保留函数改用 utils 包"
```

---

### Task 8: 回填 state 包其他文件的调用

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_state.go`
- Modify: `internal/agentcore/session/state/inmemory_commit_state.go`
- Modify: `internal/agentcore/session/state/key.go`
- Modify: `internal/agentcore/session/state/workflow_commit_state.go`

- [ ] **Step 1: 修改 inmemory_state.go**

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"`，然后替换：

- 第 35 行: `deepCopyValue(getBySchema(...))` → `utils.DeepCopyValue(getBySchema(...))`
- 第 42 行: `deepCopyValue(getBySchema(...))` → `utils.DeepCopyValue(getBySchema(...))`
- 第 56 行: `updateDict(deepCopyMap(data), s.state)` → `utils.UpdateDict(utils.DeepCopyMap(data), s.state)`
- 第 64 行: `deepCopyMap(s.state)` → `utils.DeepCopyMap(s.state)`

- [ ] **Step 2: 修改 inmemory_commit_state.go**

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"`，然后替换：

- 第 89 行: `deepCopyMap(data)` → `utils.DeepCopyMap(data)`
- 第 148 行: `deepCopyMap(u)` → `utils.DeepCopyMap(u)`
- 第 162 行: `deepCopyUpdates(updates)` → `utils.DeepCopyUpdates(updates)`

- [ ] **Step 3: 修改 key.go**

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"`，然后替换：

- 第 57 行: `deepCopyMap(schema)` → `utils.DeepCopyMap(schema)`
- 第 63 行: `deepCopySlice(keys)` → `utils.DeepCopySlice(keys)`
- 第 93 行: `deepCopyMap(k.value.(map[string]any))` → `utils.DeepCopyMap(k.value.(map[string]any))`
- 第 101 行: `deepCopySlice(k.value.([]any))` → `utils.DeepCopySlice(k.value.([]any))`

- [ ] **Step 4: 修改 workflow_commit_state.go**

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"`，然后替换：

- 第 197 行: `deepCopyUpdates(s.ioState.GetUpdates())` → `utils.DeepCopyUpdates(s.ioState.GetUpdates())`
- 第 198 行: `deepCopyUpdates(s.compState.GetUpdates())` → `utils.DeepCopyUpdates(s.compState.GetUpdates())`
- 第 199 行: `deepCopyUpdates(s.workflowState.GetUpdates())` → `utils.DeepCopyUpdates(s.workflowState.GetUpdates())`
- 第 202 行: `deepCopyUpdates(s.globalState.GetUpdates())` → `utils.DeepCopyUpdates(s.globalState.GetUpdates())`
- 第 227 行: `convertUpdatesFromJSON(gs)` → `utils.ConvertUpdatesFromJSON(gs)`
- 第 234 行: `convertUpdatesFromJSON(io)` → `utils.ConvertUpdatesFromJSON(io)`
- 第 241 行: `convertUpdatesFromJSON(comp)` → `utils.ConvertUpdatesFromJSON(comp)`
- 第 248 行: `convertUpdatesFromJSON(wf)` → `utils.ConvertUpdatesFromJSON(wf)`

- [ ] **Step 5: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 6: 运行 state 包现有测试**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/agentcore/session/state/... -v -count=1`
Expected: 非 utils_test.go 的测试全部通过（utils_test.go 的测试因函数已删除会失败，Task 9 处理）

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/session/state/inmemory_state.go internal/agentcore/session/state/inmemory_commit_state.go internal/agentcore/session/state/key.go internal/agentcore/session/state/workflow_commit_state.go
git commit -m "refactor(state): 回填 state 包其他文件，改用 utils 包导出函数"
```

---

### Task 9: 迁移测试文件

**Files:**
- Modify: `internal/agentcore/session/state/utils_test.go`（删除迁出函数的测试）
- Create: `internal/agentcore/session/utils/utils_test.go`（新建 utils 包测试）

- [ ] **Step 1: 从 state/utils_test.go 中删除迁出函数的测试**

删除以下测试函数/测试区块：
- `deepCopyMap` 相关测试
- `deepCopySlice` 相关测试
- `splitNestedPath` 相关测试
- `isRefPath` / `extractOriginKey` 相关测试
- `expandNestedStructure` 相关测试
- `updateDict` 相关测试
- `getValueByNestedPath` 相关测试
- `rootToPath` 相关测试
- `updateByKey` / `deleteByKey` 相关测试
- `safeExtendContainer` 相关测试
- `rootToIndex` 相关测试
- `convertUpdatesFromJSON` 相关测试
- `deepCopyUpdates` 相关测试
- `splitString` 相关测试
- `parseListIndexes` 相关测试

保留的测试（如果有的话）：
- `getBySchema` 相关测试
- `getBySchemaMap` 相关测试
- `getBySchemaList` 相关测试
- `getValueByNestedPathMap` 相关测试

注意：由于 `state/utils_test.go` 是同包测试（`package state`），保留的测试中调用 `getBySchema` 等非导出函数不需要改动。但需要添加 utils import 如果测试中引用了 `utils.Xxx`。

- [ ] **Step 2: 创建 utils/utils_test.go**

将删除的测试迁到 utils 包，函数名改为测试导出函数。由于是 `package utils` 的测试，调用 `utils.SplitNestedPath` 直接写 `SplitNestedPath`。

测试需覆盖：
- `SplitNestedPath` — 基本拆分、空字符串、简单字符串、负索引、引号路径
- `GetValueByNestedPath` — 简单路径、嵌套路径、列表索引、不存在的路径、负索引
- `RootToPath` — 简单路径、嵌套路径、createIfAbsent、列表索引
- `RootToIndex` — 基本索引、嵌套索引、负索引、空输入、越界
- `IsRefPath` — 正确格式、非引用、边界（${}、${x}、超长）
- `ExtractOriginKey` — 引用路径、普通字符串、未闭合
- `UpdateDict` — 简单更新、删除、嵌套更新
- `UpdateByKey` — map 更新、list 更新、不存在 key
- `DeleteByKey` — map 删除、list 删除
- `ExpandNestedStructure` — 嵌套 key 展开、slice 展开、原始值
- `SafeExtendContainer` — 正常扩展、边界检查、上限保护
- `DeepCopyMap` / `DeepCopySlice` / `DeepCopyValue` — 深拷贝验证
- `DeepCopyUpdates` — nil 输入、正常拷贝
- `ConvertUpdatesFromJSON` — 正常转换、类型不匹配
- `ContainsChar` / `ContainsSubstring` / `SplitString` — 基本功能
- `ParseListIndexes` — 各种索引格式

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/agentcore/session/utils/... -v -count=1`
Expected: 全部通过

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/agentcore/session/state/... -v -count=1`
Expected: 全部通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/state/utils_test.go internal/agentcore/session/utils/utils_test.go
git commit -m "test(session-utils): 迁移测试到 utils 包，state 包保留 getBySchema 测试"
```

---

### Task 10: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/session/state/doc.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 更新 state/doc.go**

在文件目录中 `utils.go` 的描述改为"StateKey 依赖函数（getBySchema 等）"：

```
//	├── utils.go            # getBySchema 等 StateKey 依赖函数
```

- [ ] **Step 2: 更新 session/doc.go**

在文件目录中添加 `utils/` 子包条目：

```
//	├── utils/              # 通用工具函数（嵌套路径/引用路径/字典/容器操作）
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/doc.go internal/agentcore/session/doc.go
git commit -m "docs(session): 更新 doc.go 文件目录，添加 utils 子包"
```

---

### Task 11: 全量编译和测试验证

**Files:**
- 无新增/修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go build -tags=test ./...`
Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/agentcore/session/... -v -count=1`
Expected: 全部通过

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && GOPROXY=https://goproxy.cn,direct go test -tags=test -cover ./internal/agentcore/session/utils/...`
Expected: 覆盖率 ≥ 85%

---

### Task 12: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 5.14 状态从 ☐ 改为 ✅**

找到 5.14 行：
```
| 5.14 | ☐ | Session Utils | 会话工具函数 | `openjiuwen/core/session/utils.py` |
```
改为：
```
| 5.14 | ✅ | Session Utils | 会话工具函数 | `openjiuwen/core/session/utils.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.14 Session Utils 状态为 ✅"
```
