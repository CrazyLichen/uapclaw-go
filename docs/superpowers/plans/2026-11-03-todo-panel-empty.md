# 右侧任务栏「暂无任务」修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让模型调用 todo 工具后，前端右侧任务栏能收到非空的 `todos` 列表。

**Architecture:** 生产端 `formatTodosForFrontend` 保持返回 `[]map[string]any`（类型更精确）；消费端 `ParseStreamChunk` 的 `todo.updated` 分支改为同时接受 `[]any` 与 `[]map[string]any`，未知形态记 Warn 后按空列表下发。`parseTypedChunkMap` 中不可达的 `case "todo.updated"` 死分支删除，ACP 与前端零改动。

**Tech Stack:** Go 1.x + 标准 `testing`；集成测试用 MockLLM（`tests/integration/mockllm`）+ testify suite。

**设计文档:** `docs/superpowers/specs/2026-11-03-todo-panel-empty-design.md`

---

## 文件结构

| 文件 | 责任 | 本次动作 |
|---|---|---|
| `internal/swarm/server/utils/stream_utils.go` | 流式 chunk → 前端事件的翻译器；payload 字段读取助手同族（`Extract*FromPayload`） | 新增 `ExtractAnySliceFromPayload`；改造 `todo.updated` 分支 |
| `internal/swarm/server/utils/stream_utils_test.go` | 上述翻译器与助手的单测 | 新增助手用例 + `[]map` 形态用例 + 未知形态用例 |
| `internal/swarm/server/adapter/deep_adapter_team.go` | team 模式 chunk 解析（`parseTypedChunkMap`），带点号事件类型原样透传 | 删除不可达的 `case "todo.updated"`，补注释 |
| `internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go` | `chunkToMap` / `parseTypedChunkMap` 的单测 | 新增 1 例锁定 `todo.updated` 走 dotted 透传 |
| `tests/integration/swarm/server/adapter/todo_panel_test.go` | 全链路回归（MockLLM 真调 `todo_create`） | 已存在，保留并跑通 |

---

## Task 1: 清理上一轮工作区的反射版改动

上一轮排查时在工作区留下了一份基于 `reflect` 的修复（未提交），与本次方案冲突，先回滚干净。

**Files:**
- Delete: `internal/common/utils/slice.go`
- Delete: `internal/common/utils/slice_test.go`
- Revert: `internal/common/utils/doc.go`
- Revert: `internal/swarm/e2a/acp/acp_tool_updates.go`
- Revert: `internal/swarm/e2a/acp/acp_tool_updates_test.go`
- Revert: `internal/swarm/server/adapter/deep_adapter_team.go`
- Revert: `internal/swarm/server/utils/stream_utils.go`
- Revert: `internal/swarm/server/utils/stream_utils_test.go`
- Keep: `tests/integration/swarm/server/adapter/todo_panel_test.go`（未跟踪，本次要用）

- [ ] **Step 1: 回滚已跟踪文件的改动**

```bash
cd /home/opensource/uap-claw-go
git checkout -- \
  internal/common/utils/doc.go \
  internal/swarm/e2a/acp/acp_tool_updates.go \
  internal/swarm/e2a/acp/acp_tool_updates_test.go \
  internal/swarm/server/adapter/deep_adapter_team.go \
  internal/swarm/server/utils/stream_utils.go \
  internal/swarm/server/utils/stream_utils_test.go
```

- [ ] **Step 2: 删除未跟踪的反射助手**

```bash
rm internal/common/utils/slice.go internal/common/utils/slice_test.go
```

- [ ] **Step 3: 确认工作区只剩集成用例**

Run: `git status --short`
Expected: 只出现 `?? tests/integration/swarm/server/adapter/todo_panel_test.go`（以及 `.clawbench/` 下的无关产物），没有任何 ` M ` 开头的源码文件。

- [ ] **Step 4: 确认回滚后可编译**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./internal/swarm/...`
Expected: 无输出，退出码 0。

---

## Task 2: 新增 `ExtractAnySliceFromPayload` 读取助手

**Files:**
- Modify: `internal/swarm/server/utils/stream_utils.go`（插在 `ExtractFloatFromPayload` 之后，第 639 行附近）
- Test: `internal/swarm/server/utils/stream_utils_test.go`（追加到文件末尾）

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/server/utils/stream_utils_test.go` 末尾追加：

```go
// TestExtractAnySliceFromPayload 覆盖 payload 列表字段的两种真实形态与非法形态。
//
// payload 里的 list 在进程内有两种类型：JSON 反序列化得到 []any，
// 业务代码直接构造得到 []map[string]any（如 todo.updated 的 todos）。
// 只断言其中一种都会让另一种整块丢失。
func TestExtractAnySliceFromPayload(t *testing.T) {
	anyItems := []any{map[string]any{"id": "1"}}
	mapItems := []map[string]any{{"id": "1"}, {"id": "2"}}

	tests := []struct {
		name    string
		payload map[string]any
		wantLen int
		wantOK  bool
		wantID  string
	}{
		{name: "键缺失", payload: map[string]any{}, wantLen: 0, wantOK: true},
		{name: "值为nil", payload: map[string]any{"todos": nil}, wantLen: 0, wantOK: true},
		{name: "any切片", payload: map[string]any{"todos": anyItems}, wantLen: 1, wantOK: true, wantID: "1"},
		{name: "结构体切片", payload: map[string]any{"todos": mapItems}, wantLen: 2, wantOK: true, wantID: "1"},
		{name: "非法形态", payload: map[string]any{"todos": "not-a-list"}, wantLen: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractAnySliceFromPayload(tt.payload, "todos")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantID == "" {
				return
			}
			first, isMap := got[0].(map[string]any)
			if !isMap {
				t.Fatalf("got[0] 类型 = %T, want map[string]any", got[0])
			}
			if first["id"] != tt.wantID {
				t.Errorf("got[0][id] = %v, want %v", first["id"], tt.wantID)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ -run TestExtractAnySliceFromPayload -count=1`
Expected: 编译失败，`undefined: ExtractAnySliceFromPayload`。

- [ ] **Step 3: 实现助手**

在 `internal/swarm/server/utils/stream_utils.go` 的 `ExtractFloatFromPayload` 函数之后（`SerializeValue` 之前）插入：

```go
// ExtractAnySliceFromPayload 从 payload 提取列表字段，收敛为 []any。
//
// payload 里的 list 在进程内有两种形态：
//   - []any：JSON 反序列化产物（跨进程 chunk、落盘重放、测试夹具、ACP 出站事件）；
//   - []map[string]any：业务代码直接构造（todo.updated 的 todos 由
//     stream_event_emit.formatTodosForFrontend 产出）。
//
// 只断言 []any 会把后者整块吞成空列表（前端右侧任务栏「永远暂无任务」）；
// 只断言 []map[string]any 又会让前者塌陷。两条路径都真实存在，故两者都接受。
//
// 返回 ok=false 表示键存在、值非 nil，但不是上述两种形态——调用方据此记
// Warn，避免再次静默降级为空列表。
func ExtractAnySliceFromPayload(payload map[string]any, key string) ([]any, bool) {
	v, ok := payload[key]
	if !ok || v == nil {
		return []any{}, true
	}
	switch items := v.(type) {
	case []any:
		return items, true
	case []map[string]any:
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out, true
	default:
		return nil, false
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ -run TestExtractAnySliceFromPayload -count=1 -v`
Expected: 5 个子用例全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/utils/stream_utils.go internal/swarm/server/utils/stream_utils_test.go
git commit -m "feat(stream): 新增 ExtractAnySliceFromPayload 统一 payload 列表读取

payload 里的 list 在进程内可能是 []any（JSON 反序列化）或
[]map[string]any（业务代码构造），只断言一种会让另一种整块丢失。"
```

---

## Task 3: 改造 `todo.updated` 消费分支

**Files:**
- Modify: `internal/swarm/server/utils/stream_utils.go:298-311`（`case "todo.updated"`）
- Test: `internal/swarm/server/utils/stream_utils_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/server/utils/stream_utils_test.go` 的 `TestParseStreamChunk_todoUpdated_无todos` 之后追加：

```go
// TestParseStreamChunk_todoUpdated_结构体切片todos 覆盖生产端真实 payload 形态。
//
// stream_event_emit.formatTodosForFrontend 产出 []map[string]any，
// 只断言 []any 时该 payload 会被吞成空列表，前端任务栏表现为「永远暂无任务」。
func TestParseStreamChunk_todoUpdated_结构体切片todos(t *testing.T) {
	output := makeOutput("todo.updated", map[string]any{
		"todos": []map[string]any{
			{"id": "1", "content": "任务1", "activeForm": "执行任务1", "status": "in_progress"},
			{"id": "2", "content": "任务2", "activeForm": "执行任务2", "status": "pending"},
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil, false)
	if result["event_type"] != "todo.updated" {
		t.Fatalf("期望 todo.updated, 实际 %v", result["event_type"])
	}
	todos, ok := result["todos"].([]any)
	if !ok {
		t.Fatalf("todos 类型 = %T, want []any", result["todos"])
	}
	if len(todos) != 2 {
		t.Fatalf("期望 todos 列表长度 2, 实际 %v", result["todos"])
	}
	first, isMap := todos[0].(map[string]any)
	if !isMap || first["id"] != "1" || first["status"] != "in_progress" {
		t.Errorf("todos[0] = %v, want map[id:1 status:in_progress]", todos[0])
	}
}

// TestParseStreamChunk_todoUpdated_非法形态 未知形态不得 panic，按空列表下发（并打 Warn）。
func TestParseStreamChunk_todoUpdated_非法形态(t *testing.T) {
	output := makeOutput("todo.updated", map[string]any{"todos": "not-a-list"})
	result := ParseStreamChunk(output, nil, nil, nil, false)
	if result["event_type"] != "todo.updated" {
		t.Fatalf("期望 todo.updated, 实际 %v", result["event_type"])
	}
	todos, ok := result["todos"].([]any)
	if !ok {
		t.Fatalf("todos 类型 = %T, want []any", result["todos"])
	}
	if len(todos) != 0 {
		t.Errorf("非法形态应下发空列表, 实际 %v", todos)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ -run TestParseStreamChunk_todoUpdated -count=1 -v`
Expected: `结构体切片todos` FAIL（`期望 todos 列表长度 2, 实际 []`）；`非法形态` PASS（当前已兜空列表，属回归守卫）。

- [ ] **Step 3: 改造分支**

把 `internal/swarm/server/utils/stream_utils.go` 中现有的：

```go
	case "todo.updated":
		// Python: L5133-5135: 提取 todos 列表
		todos, _ := payload["todos"].([]any)
		if todos == nil {
			todos = []any{}
		}
		return map[string]any{
			"event_type": "todo.updated",
			"todos":      todos,
		}
```

替换为：

```go
	case "todo.updated":
		// Python: L5133-5135: 提取 todos 列表。
		//
		// Python 的 list 不携带元素类型，payload["todos"] 直接可用；Go 里同一字段
		// 可能是 []any（JSON 反序列化）或 []map[string]any（formatTodosForFrontend
		// 直接构造），只断言 []any 会让后者被兜成空列表——前端任务栏因此永远
		// 显示「暂无任务」（2026-11-03 修复）。
		todos, ok := ExtractAnySliceFromPayload(payload, "todos")
		if !ok {
			logger.Warn(logComponent).
				Str("event_type", "todo.updated").
				Str("key", "todos").
				Str("actual_type", fmt.Sprintf("%T", payload["todos"])).
				Msg("todo.updated todos type assertion failed, using empty list")
			todos = []any{}
		}
		return map[string]any{
			"event_type": "todo.updated",
			"todos":      todos,
		}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ -run TestParseStreamChunk_todoUpdated -count=1 -v`
Expected: 4 个用例（`todoUpdated`、`结构体切片todos`、`无todos`、`非法形态`）全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/utils/stream_utils.go internal/swarm/server/utils/stream_utils_test.go
git commit -m "fix(stream): todo.updated 兼容 []map 形态，修右侧任务栏恒为空

生产端 formatTodosForFrontend 产出 []map[string]any，消费端只断言
[]any 导致断言失败后兜成空列表，前端每轮收到 todos=[] 故永不显示。
未知形态打 Warn 暴露真实类型，不再静默降级。"
```

---

## Task 4: 删除 team 侧不可达分支并锁定透传行为

`parseTypedChunkMap` 开头 `strings.Contains(chunkType, ".")` 已对 `todo.updated` 提前 return，其 `case "todo.updated"` 永不可达。先用测试锁住"透传"这一真实行为，再删死分支。

**Files:**
- Test: `internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`（`case "todo.updated"` 约 1694-1702；dotted 分支约 1507-1519）

- [ ] **Step 1: 写行为锁定测试**

在 `internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go` 末尾追加：

```go
// TestParseTypedChunkMap_TodoUpdated_点号前缀透传 锁定 todo.updated 的实际分派路径。
//
// chunkType 含点号 → 走 dotted 前缀分支原样透传（不归一化、不做类型断言），
// 删除其下不可达的 case "todo.updated" 后行为必须保持不变。
func TestParseTypedChunkMap_TodoUpdated_点号前缀透传(t *testing.T) {
	chunk := map[string]any{
		"type": "todo.updated",
		"payload": map[string]any{
			"todos": []map[string]any{
				{"id": "1", "content": "任务1", "status": "in_progress"},
				{"id": "2", "content": "任务2", "status": "pending"},
			},
		},
	}

	got := parseTypedChunkMap(chunk)
	if got == nil {
		t.Fatal("todo.updated 不应返回 nil")
	}
	if got["event_type"] != "todo.updated" {
		t.Fatalf("event_type = %v, want todo.updated", got["event_type"])
	}
	todos, ok := got["todos"].([]map[string]any)
	if !ok {
		t.Fatalf("todos 类型 = %T, want []map[string]any（dotted 分支原样透传）", got["todos"])
	}
	if len(todos) != 2 {
		t.Fatalf("len(todos) = %d, want 2", len(todos))
	}
	if todos[0]["id"] != "1" {
		t.Errorf("todos[0][id] = %v, want 1", todos[0]["id"])
	}
}
```

- [ ] **Step 2: 运行测试确认基线通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestParseTypedChunkMap_TodoUpdated_点号前缀透传 -count=1 -v`
Expected: PASS（删除死分支前的基线）。

- [ ] **Step 3: 删除死分支并补注释**

在 `internal/swarm/server/adapter/deep_adapter_team.go` 中删除：

```go
	case "todo.updated":
		var todos any = []any{}
		if td, ok := payload["todos"].([]any); ok {
			todos = td
		}
		return map[string]any{"event_type": "todo.updated", "todos": todos}
```

并在 `if strings.Contains(chunkType, ".") {` 之前补充注释：

```go
	// 带点号的事件类型（含 todo.updated）统一在此原样透传：值不做归一化，
	// 下游直接 JSON 序列化给前端。注意下方 switch 因此不可能命中带点号的
	// 类型（历史上曾有一个不可达的 case "todo.updated"，2026-11-03 删除）。
```

- [ ] **Step 4: 运行测试确认行为不变**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run "TestParseTypedChunkMap|TestChunkToMap" -count=1`
Expected: PASS，行为与删除前一致。

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_team.go internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go
git commit -m "refactor(adapter): 删除 parseTypedChunkMap 中不可达的 todo.updated 分支

chunkType 含点号时函数开头已提前 return 原样透传，该 case 永不命中。
补测试锁定 dotted 路径透传行为，避免后人误以为此处会归一化 todos。"
```

---

## Task 5: 集成用例与本地全量门禁

**Files:**
- Verify: `tests/integration/swarm/server/adapter/todo_panel_test.go`

- [ ] **Step 1: 跑全链路集成用例**

Run:
```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" \
  ./tests/integration/swarm/server/adapter/ -run TestTodoPanelSuite -count=1 -v 2>&1 | tail -30
```
Expected: `PASS`，且日志中出现 `event=todo.updated payload=...`；断言 `todo_create 之后必须收到非空 todos` 通过。

若失败且报 `todos` 为空，说明 Task 3 未生效；若报事件序列里没有 `todo.updated`，说明 Rail 未注册或工具名不在白名单，回头查 `stream_event_helpers.go:31`。

- [ ] **Step 2: 跑两个改动包的单测 + 覆盖率**

Run:
```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" \
  ./internal/swarm/server/utils/ ./internal/swarm/server/adapter/ -count=1 -cover
```
Expected: 两个包均 `ok`。再核对改动行覆盖：

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ \
  -count=1 -coverprofile=/tmp/utils.out && go tool cover -func=/tmp/utils.out | grep -E "ExtractAnySliceFromPayload|total"
```
Expected: `ExtractAnySliceFromPayload` 100.0%，包 total ≥85%（项目覆盖率门禁）。

- [ ] **Step 3: 跑 race**

Run:
```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race \
  ./internal/swarm/server/utils/ ./internal/swarm/server/adapter/ -count=1
```
Expected: `ok`，无 DATA RACE。

- [ ] **Step 4: 格式与静态检查**

Run:
```bash
gofmt -l internal/swarm/server/utils/stream_utils.go \
          internal/swarm/server/utils/stream_utils_test.go \
          internal/swarm/server/adapter/deep_adapter_team.go \
          internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go
go vet -tags "sqlite_fts5 test" ./internal/swarm/server/utils/ ./internal/swarm/server/adapter/
```
Expected: `gofmt -l` 无输出；`go vet` 无告警。

- [ ] **Step 5: 集成审计（改动集成用例时必跑）**

Run: `go run ./cmd/integration-audit -verify`
Expected: 无新增不合规目录（`tests/integration/swarm/server/adapter` 已有其他用例，跨度满足要求）。

---

## Task 6: 推送并确认远端 CI 四绿

规则 6：集成测试相关改动的完成标准是远端 Build / Lint / Unit Test / Integration Test 四 job 全绿。

- [ ] **Step 1: 推送**

```bash
git push origin fix/review-2026-09-23
```

- [ ] **Step 2: 找到本次触发的 run**

```bash
gh run list --limit 3 --branch fix/review-2026-09-23
```

- [ ] **Step 3: 跟踪直到结束**

```bash
gh run watch <run-id> --exit-status
```
Expected: 四个 job 全部 SUCCESS。

- [ ] **Step 4: 失败则定位修复并重新推送**

```bash
gh run view <run-id> --log-failed | tail -80
```
常见失败：改动行覆盖率门禁（补用例）、`staticcheck`（`fmt` 未使用等）、集成审计灰区（补真实跨子系统调用）。

- [ ] **Step 5: 全绿后汇报**

汇总：改动文件清单、单测/集成/race 结果、CI run id 与四 job 状态、以及"未做"的边界（生产端类型不变、ACP 不动、未审计其他 list 字段）。

- [ ] **Step 6: 交付界面验证清单（由用户执行）**

自动化无法点 UI，把以下清单交给用户在本机验证：

1. 启动服务并进入 agent 模式，发一句"把这次需求拆成 3 个待办"。
2. 期望：右侧任务栏出现 1 个「进行中」+ 2 个「待处理」，不再显示「暂无任务」。
3. 让模型把第 1 项标记完成 → 期望任务项实时移动到「已完成」分组。
4. 切换到另一个会话 → 期望任务栏清空；切换回来 → 期望历史会话不串台。
5. 全程后端日志不应出现 `todo.updated todos type assertion failed`。

---

## 预期最终 diff 规模

| 文件 | 变化 |
|---|---|
| `internal/swarm/server/utils/stream_utils.go` | +35 / -5（助手 25 行 + 分支改造 10 行） |
| `internal/swarm/server/utils/stream_utils_test.go` | +75（助手 5 例 + todo 2 例） |
| `internal/swarm/server/adapter/deep_adapter_team.go` | +3 / -8（删死分支 + 注释） |
| `internal/swarm/server/adapter/deep_adapter_team_chunktomap_test.go` | +30（1 例） |
