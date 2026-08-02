# 10.3.25 Server Utils 完整实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整对齐 Python `jiuwenswarm/server/utils/` 三个模块（stream_utils/utils/diff_service）+ 写入端 history + interaction helpers

**Architecture:** 创建 `server/utils/` 独立包提取 ParseStreamChunk 及 10 个 helper；新增 GetChatID/IsTeamParams；新增 DiffService（含读取端）；新增 filesystem/history.go 写入端（appendOpHistory + buildHistoryPath + recordRmTargetsBeforeDeletion + detectAndRecordDeletions）；新增 interrupt/helpers.go（ConvertInteractionsToAskUserQuestion）；回填 DeepAdapter/write_file/edit_file/bash/handleCommandDiff

**Tech Stack:** Go stdlib + `github.com/sergi/go-diff/diffmatchpatch` + `sync.Mutex`/`sync.Once`

---

## File Structure

| # | File | Action | Responsibility |
|---|------|--------|---------------|
| 1 | `internal/swarm/server/utils/doc.go` | Create | 包文档 |
| 2 | `internal/swarm/server/utils/stream_utils.go` | Create | ParseStreamChunk + 10 helper 纯函数 |
| 3 | `internal/swarm/server/utils/stream_utils_test.go` | Create | stream_utils 测试 |
| 4 | `internal/swarm/server/utils/utils.go` | Create | GetChatID + IsTeamParams |
| 5 | `internal/swarm/server/utils/utils_test.go` | Create | utils 测试 |
| 6 | `internal/swarm/server/utils/diff_service.go` | Create | DiffService + 12 helper |
| 7 | `internal/swarm/server/utils/diff_service_test.go` | Create | diff_service 测试 |
| 8 | `internal/agentcore/harness/tools/filesystem/history.go` | Create | appendOpHistory + buildHistoryPath + recordRmTargets + detectDeletions |
| 9 | `internal/agentcore/harness/tools/filesystem/history_test.go` | Create | history 测试 |
| 10 | `internal/agentcore/harness/rails/interrupt/helpers.go` | Create | ConvertInteractionsToAskUserQuestion + ConvertActivateConfirm |
| 11 | `internal/agentcore/harness/rails/interrupt/helpers_test.go` | Create | helpers 测试 |
| 12 | `internal/swarm/server/adapter/deep_adapter_stream.go` | Modify | 删除 parseStreamChunk 方法 + usageAccumulator + helper 函数 |
| 13 | `internal/swarm/server/adapter/deep_adapter.go` | Modify | 调用改为 utils.ParseStreamChunk |
| 14 | `internal/agentcore/harness/tools/filesystem/write_file.go` | Modify | 回填 appendOpHistory |
| 15 | `internal/agentcore/harness/tools/filesystem/edit_file.go` | Modify | 回填 appendOpHistory |
| 16 | `internal/agentcore/harness/tools/shell/bash.go` | Modify | 回填 recordRmTargets + detectDeletions |
| 17 | `internal/swarm/server/handle_command.go` | Modify | handleCommandDiff 从 stub 改为调用 DiffService |
| 18 | `go.mod` | Modify | 新增 go-diff/diffmatchpatch 直接依赖 |

---

## Task 1: 新增 go-diff 依赖

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: 新增直接依赖**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go get github.com/sergi/go-diff/diffmatchpatch@v1.0.0
```

- [ ] **Step 2: 验证依赖**

```bash
grep "sergi/go-diff" go.mod
```

Expected: 出现 `github.com/sergi/go-diff/diffmatchpatch v1.0.0`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum && git commit -m "deps: 新增 go-diff/diffmatchpatch 直接依赖（DiffService diff 算法）"
```

---

## Task 2: 创建 server/utils/doc.go

**Files:**
- Create: `internal/swarm/server/utils/doc.go`

- [ ] **Step 1: 创建目录和文件**

```bash
mkdir -p internal/swarm/server/utils
```

```go
// Package utils 提供 AgentServer 工具函数。
//
// 包含流式 chunk 解析、请求参数提取、turn-based diff 查询等功能。
// 对齐 Python: jiuwenswarm/server/utils/
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── stream_utils.go     # ParseStreamChunk + 10 helper 纯函数
//	├── utils.go            # GetChatID + IsTeamParams
//	├── diff_service.go     # DiffService + GetTurnDiffs/GetFilesToRestore + 12 helper
//
// 对应 Python 代码：jiuwenswarm/server/utils/
package utils
```

- [ ] **Step 2: Commit**

```bash
git add internal/swarm/server/utils/doc.go && git commit -m "feat(server/utils): 包文档"
```

---

## Task 3: 创建 server/utils/utils.go — GetChatID + IsTeamParams

**Files:**
- Create: `internal/swarm/server/utils/utils.go`
- Create: `internal/swarm/server/utils/utils_test.go`

- [ ] **Step 1: 写测试**

```go
//go:build test

package utils

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGetChatID_顶层字段(t *testing.T) {
	chatID := "chat-123"
	req := &schema.AgentRequest{ChatID: &chatID}
	result := GetChatID(req)
	if result != "chat-123" {
		t.Errorf("期望 chat-123, 实际 %s", result)
	}
}

func TestGetChatID_顶层字段为空(t *testing.T) {
	chatID := ""
	req := &schema.AgentRequest{ChatID: &chatID}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("期望空, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_feishu(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"feishu_chat_id": "fs-456"},
	}
	result := GetChatID(req)
	if result != "fs-456" {
		t.Errorf("期望 fs-456, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_wecom(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"wecom_chat_id": "wc-789"},
	}
	result := GetChatID(req)
	if result != "wc-789" {
		t.Errorf("期望 wc-789, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_dingtalk(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"dingtalk_chat_id": "dt-012"},
	}
	result := GetChatID(req)
	if result != "dt-012" {
		t.Errorf("期望 dt-012, 实际 %s", result)
	}
}

func TestGetChatID_Metadata回退_xiaoyi(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"xiaoyi_session_id": "xy-345"},
	}
	result := GetChatID(req)
	if result != "xy-345" {
		t.Errorf("期望 xy-345, 实际 %s", result)
	}
}

func TestGetChatID_全部为空(t *testing.T) {
	req := &schema.AgentRequest{}
	result := GetChatID(req)
	if result != "" {
		t.Errorf("期望空, 实际 %s", result)
	}
}

func TestGetChatID_顶层优先于Metadata(t *testing.T) {
	chatID := "top-level"
	req := &schema.AgentRequest{
		ChatID:   &chatID,
		Metadata: map[string]any{"feishu_chat_id": "fs-456"},
	}
	result := GetChatID(req)
	if result != "top-level" {
		t.Errorf("期望 top-level 优先, 实际 %s", result)
	}
}

func TestIsTeamParams_nil(t *testing.T) {
	result := IsTeamParams(nil)
	if result {
		t.Error("nil 应返回 false")
	}
}

func TestIsTeamParams_team键为true(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": true})
	if !result {
		t.Error("team=true 应返回 true")
	}
}

func TestIsTeamParams_team键为字符串(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": "yes"})
	if !result {
		t.Error("team='yes' 应返回 true")
	}
}

func TestIsTeamParams_team键为false(t *testing.T) {
	result := IsTeamParams(map[string]any{"team": false})
	if result {
		t.Error("team=false 应返回 false")
	}
}

func TestIsTeamParams_mode为team(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "team"})
	if !result {
		t.Error("mode=team 应返回 true")
	}
}

func TestIsTeamParams_mode为teamPlan(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "team.plan"})
	if !result {
		t.Error("mode=team.plan 应返回 true")
	}
}

func TestIsTeamParams_mode为codeTeam(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "code.team"})
	if !result {
		t.Error("mode=code.team 应返回 true")
	}
}

func TestIsTeamParams_mode为其他(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "code"})
	if result {
		t.Error("mode=code 应返回 false")
	}
}

func TestIsTeamParams_mode大小写不敏感(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": "TEAM"})
	if !result {
		t.Error("mode=TEAM 应返回 true（大小写不敏感）")
	}
}

func TestIsTeamParams_mode前后空格(t *testing.T) {
	result := IsTeamParams(map[string]any{"mode": " team.plan "})
	if !result {
		t.Error("mode=' team.plan ' 应返回 true（trim）")
	}
}
```

- [ ] **Step 2: 写实现**

```go
package utils

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// metadataChatIDKeys ChatID 回退查找的 metadata 键列表。
// 对齐 Python: get_chat_id 中的 feishu/wecom/dingtalk/xiaoyi 优先级
var metadataChatIDKeys = []string{
	"feishu_chat_id",
	"wecom_chat_id",
	"dingtalk_chat_id",
	"xiaoyi_session_id",
}

// teamModeValues IsTeamParams 匹配的 mode 值集合。
// 对齐 Python: is_team_params 中的 {"team", "team.plan", "code.team"}
var teamModeValues = map[string]bool{
	"team":       true,
	"team.plan":  true,
	"code.team":  true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetChatID 获取请求的 Chat ID（平台聊天标识）。
// 优先使用顶层 ChatID 字段，回退到 Metadata 中的平台特定字段。
// 对齐 Python: get_chat_id(request) (utils.py L5-24)
func GetChatID(req *schema.AgentRequest) string {
	// 优先使用顶层字段
	if req.ChatID != nil && *req.ChatID != "" {
		return *req.ChatID
	}
	// 回退到 metadata 中的平台字段
	if req.Metadata != nil {
		for _, key := range metadataChatIDKeys {
			if v, ok := req.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// IsTeamParams 判断参数是否为团队模式。
// 检查 params["team"] truthy 或 params["mode"] 为已知团队模式字符串。
// 对齐 Python: is_team_params(params) (utils.py L26-42)
func IsTeamParams(params map[string]any) bool {
	if params == nil {
		return false
	}
	// 检查 team 键 truthy
	if team, ok := params["team"]; ok {
		if isTruthy(team) {
			return true
		}
	}
	// 检查 mode 键为已知团队模式
	mode, ok := params["mode"]
	if !ok {
		return false
	}
	modeStr, ok := mode.(string)
	if !ok {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(modeStr))
	return teamModeValues[normalized]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTruthy 判断值是否为 truthy（非 nil/非 false/非 ""/非 0）。
// 对齐 Python: truthy 判断逻辑
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != ""
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/swarm/server/utils/... -run "TestGetChatID|TestIsTeamParams"
```

Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/utils/utils.go internal/swarm/server/utils/utils_test.go && git commit -m "feat(server/utils): 实现 GetChatID + IsTeamParams"
```

---

## Task 4: 创建 server/utils/stream_utils.go — 提取 ParseStreamChunk

**Files:**
- Create: `internal/swarm/server/utils/stream_utils.go`
- Create: `internal/swarm/server/utils/stream_utils_test.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_stream.go`（删除旧方法）
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（改调用）

这是最大的任务。需先将 `parseStreamChunk` 方法和 `usageAccumulator` 从 `deep_adapter_stream.go` 提取到 `utils/stream_utils.go`，然后改 DeepAdapter 调用点。

- [ ] **Step 1: 写 stream_utils.go 实现**

创建 `internal/swarm/server/utils/stream_utils.go`，内容为从 `deep_adapter_stream.go` 提取的全部逻辑，改为包级函数。核心变更：
- `usageAccumulator` → `UsageAccumulator`（导出）
- `parseStreamChunk` → `ParseStreamChunk`（导出包级函数，增加 `converter InteractionConverterFunc` 参数）
- `accumulateUsage` → `AccumulateUsage`（导出包级函数）
- `extractStringFromPayload`/`extractIntFromPayload`/`extractFloatFromPayload` → 导出
- `__interaction__` 分支调用 `ParseInteractionPayload(payload, converter)` 代替直接返回

完整代码见设计文档第四节（ParseStreamChunk 签名、chunkType 分发表、所有 helper 函数）。代码约 250 行，直接从 `deep_adapter_stream.go` 复制核心 switch 逻辑并改为包级函数签名。

- [ ] **Step 2: 修改 deep_adapter_stream.go — 删除旧实现**

删除 `parseStreamChunk` 方法、`usageAccumulator` 结构体、`accumulateUsage` 方法、`extractStringFromPayload`/`extractIntFromPayload`/`extractFloatFromPayload` 函数。这些已迁移到 `utils` 包。

保留 `deep_adapter_stream.go` 文件本身（可能还有其他内容），或如果该文件只剩删除内容，则删除整个文件。

- [ ] **Step 3: 修改 deep_adapter.go — 改调用**

在 `ProcessMessageStreamImpl` 的 goroutine 中（约 line 881 和 942）：
- `usage := &usageAccumulator{}` → `usage := &utils.UsageAccumulator{}`
- `parsed := d.parseStreamChunk(output, usage, emittedAskUserIDs)` → `parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs, d.interactionConverter)`
- `d.accumulateUsage(usage, ...)` → `utils.AccumulateUsage(usage, ...)`（如有其他调用点）

同时需在 DeepAdapter 结构体或初始化逻辑中新增 `interactionConverter` 字段：
```go
interactionConverter utils.InteractionConverterFunc
```
初始化时赋值：
```go
d.interactionConverter = interrupt.ConvertInteractionsToAskUserQuestion
```

- [ ] **Step 4: 写 stream_utils_test.go**

核心测试场景：
- `TestParseStreamChunk_nil` → 返回 nil
- `TestParseStreamChunk_controllerOutput_taskCompletion` → nil
- `TestParseStreamChunk_controllerOutput_taskFailed` → chat.error
- `TestParseStreamChunk_contentChunk` → chat.delta
- `TestParseStreamChunk_answer` → chat.final
- `TestParseStreamChunk_toolCall` → chat.tool_call
- `TestParseStreamChunk_toolResult` → chat.tool_result
- `TestParseStreamChunk_error` → chat.error
- `TestParseStreamChunk_thinking` → chat.thinking
- `TestParseStreamChunk_todoUpdated` → todo.updated
- `TestParseStreamChunk_contextUsage` → chat.context_usage
- `TestParseStreamChunk_contextCompressionState` → chat.context_compression_state
- `TestParseStreamChunk_askUserQuestion_去重` → 第二次返回 nil
- `TestParseStreamChunk_interaction_有converter` → 调用 converter
- `TestParseStreamChunk_interaction_无converter` → 返回 chat.interaction
- `TestParseStreamChunk_defaultFallback` → chat.delta
- `TestAccumulateUsage` → 累加 token/cost
- `TestSerializeValue_time` → RFC3339 格式
- `TestFindInteractionPayloads_嵌套` → 找到 payload
- `TestFindInteractionPayloads_循环检测` → 不死循环

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/swarm/server/utils/... -run "TestParseStreamChunk|TestAccumulateUsage|TestSerializeValue|TestFindInteraction"
```

Expected: 全部 PASS

- [ ] **Step 6: 确保 adapter 包测试也通过**

```bash
go test -tags=test -v ./internal/swarm/server/adapter/... -run "TestDeepAdapter" 2>&1 | head -50
```

Expected: 原有 adapter 测试继续通过（调用改为 utils 包）

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/server/utils/stream_utils.go internal/swarm/server/utils/stream_utils_test.go internal/swarm/server/adapter/deep_adapter_stream.go internal/swarm/server/adapter/deep_adapter.go && git commit -m "feat(server/utils): 提取 ParseStreamChunk 到独立 utils 包 + 改 DeepAdapter 调用"
```

---

## Task 5: 创建 harness/rails/interrupt/helpers.go — ConvertInteractionsToAskUserQuestion

**Files:**
- Create: `internal/agentcore/harness/rails/interrupt/helpers.go`
- Create: `internal/agentcore/harness/rails/interrupt/helpers_test.go`
- Modify: `internal/agentcore/harness/rails/interrupt/doc.go`（添加 helpers.go 条目）

- [ ] **Step 1: 写测试**

```go
//go:build test

package interrupt

import "testing"

func TestConvertInteractionsToAskUserQuestion_activateConfirm(t *testing.T) {
	payload := map[string]any{
		"type":    "activate_confirm",
		"message": "确认执行？",
	}
	result := ConvertInteractionsToAskUserQuestion(payload)
	if result["event_type"] != "harness.activate_interaction" {
		t.Errorf("期望 harness.activate_interaction, 实际 %v", result["event_type"])
	}
}

func TestConvertInteractionsToAskUserQuestion_普通类型(t *testing.T) {
	payload := map[string]any{
		"type":       "ask_user",
		"questions":  []any{map[string]any{"question": "是否继续？"}},
		"request_id": "req-123",
	}
	result := ConvertInteractionsToAskUserQuestion(payload)
	if result["event_type"] != "chat.ask_user_question" {
		t.Errorf("期望 chat.ask_user_question, 实际 %v", result["event_type"])
	}
}

func TestConvertInteractionsToAskUserQuestion_nil(t *testing.T) {
	result := ConvertInteractionsToAskUserQuestion(nil)
	if result != nil {
		t.Errorf("nil payload 应返回 nil")
	}
}

func TestConvertActivateConfirm(t *testing.T) {
	payload := map[string]any{"message": "确认"}
	result := ConvertActivateConfirm(payload)
	if result["event_type"] != "harness.activate_interaction" {
		t.Errorf("期望 harness.activate_interaction, 实际 %v", result["event_type"])
	}
	if result["payload"] != payload {
		t.Errorf("期望 payload 保留原始数据")
	}
}
```

- [ ] **Step 2: 写实现**

```go
package interrupt

// ──────────────────────────── 导出函数 ────────────────────────────

// ConvertInteractionsToAskUserQuestion 将 interaction payload 转换为前端事件。
// 对齐 Python: convert_interactions_to_ask_user_question (interrupt_helpers.py)
func ConvertInteractionsToAskUserQuestion(payload any) map[string]any {
	if payload == nil {
		return nil
	}
	p, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	// 检查 type 字段
	typeVal, _ := p["type"].(string)
	if typeVal == "activate_confirm" {
		return ConvertActivateConfirm(p)
	}
	// 其他类型 → 构建 ask_user_question 事件
	questions, _ := p["questions"].([]any)
	if questions == nil {
		questions = []any{}
	}
	return map[string]any{
		"event_type":        "chat.ask_user_question",
		"ask_user_question": map[string]any{
			"questions":  questions,
			"request_id": p["request_id"],
		},
	}
}

// ConvertActivateConfirm 处理 activate_confirm 类型，构建 harness.activate_interaction 事件。
// 对齐 Python: _parse_interaction_payload 中的 activate_confirm 分支
func ConvertActivateConfirm(payload map[string]any) map[string]any {
	return map[string]any{
		"event_type": "harness.activate_interaction",
		"payload":    payload,
	}
}
```

- [ ] **Step 3: 更新 interrupt/doc.go**

在文件目录中添加 `helpers.go` 条目。

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/agentcore/harness/rails/interrupt/... -run "TestConvert"
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/interrupt/helpers.go internal/agentcore/harness/rails/interrupt/helpers_test.go internal/agentcore/harness/rails/interrupt/doc.go && git commit -m "feat(interrupt): 实现 ConvertInteractionsToAskUserQuestion"
```

---

## Task 6: 创建 filesystem/history.go — 写入端

**Files:**
- Create: `internal/agentcore/harness/tools/filesystem/history.go`
- Create: `internal/agentcore/harness/tools/filesystem/history_test.go`
- Modify: `internal/agentcore/harness/tools/filesystem/doc.go`（添加 history.go 条目）

- [ ] **Step 1: 写测试**

核心测试场景：
- `TestAppendOpHistory_新建文件` → 创建 .agent_history 目录 + JSON 文件
- `TestAppendOpHistory_追加到已有` → 读取 + append + trim
- `TestAppendOpHistory_trim超过上限` → entries > 100 时 trim
- `TestBuildHistoryPath_正常` → `<workspace>/.agent_history/file_ops_<agentID>_<sessionID>.json`
- `TestRecordRmTargetsBeforeDeletion_有文件` → 读取文件内容 + append delete
- `TestRecordRmTargetsBeforeDeletion_文件不存在` → skip
- `TestDetectAndRecordDeletions_文件消失` → append delete entry
- `TestDetectAndRecordDeletions_无变化` → 不写入

- [ ] **Step 2: 写实现**

核心 4 个函数（对齐 Python filesystem.py L73-239）：

```go
package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxHistoryPerFile 每个文件最大历史条目数。
// 对齐 Python: MAX_HISTORY_PER_FILE = 100
const maxHistoryPerFile = 100

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// historyMu history 文件写入互斥锁。
// 对齐 Python: _HISTORY_LOCK (asyncio.Lock)
var historyMu sync.Mutex

// ──────────────────────────── 非导出函数 ────────────────────────────

// appendOpHistory 追加一条文件操作记录到 history JSON 文件。
// 对齐 Python: _append_op_history (filesystem.py L73-103)
func appendOpHistory(historyPath string, filePath string, action string, oldContent *string, newContent *string) error {
	entry := map[string]any{
		"action":      action,
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"old_content": oldContent,
		"new_content": newContent,
	}
	historyMu.Lock()
	defer historyMu.Unlock()

	history := map[string][]any{}
	data, err := os.ReadFile(historyPath)
	if err == nil && len(data) > 0 {
		if jsonErr := json.Unmarshal(data, &history); jsonErr != nil {
			logger.Warn(logComponent).Str("history_path", historyPath).Err(jsonErr).Msg("解析 history JSON 失败，使用空历史")
		}
	}
	entries := history[filePath]
	entries = append(entries, entry)
	if len(entries) > maxHistoryPerFile {
		entries = entries[len(entries)-maxHistoryPerFile:]
	}
	history[filePath] = entries

	dir := filepath.Dir(historyPath)
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		return mkErr
	}
	jsonData, marshalErr := json.Marshal(history)
	if marshalErr != nil {
		return marshalErr
	}
	tmpPath := historyPath + ".tmp"
	if writeErr := os.WriteFile(tmpPath, jsonData, 0644); writeErr != nil {
		return writeErr
	}
	return os.Replace(tmpPath, historyPath)
}

// buildHistoryPath 构建 history 文件路径。
// 对齐 Python: _build_history_path (4份合并: filesystem.py L840-850/L1157-1167 + bash/_tool.py L146-157 + powershell/_tool.py L122-128)
// 路径格式: <workspace>/.agent_history/file_ops_<agentID>_<sessionID>.json
func buildHistoryPath(session stream.StreamableSession) string {
	baseDir := session.GetWorkspace()
	agentID := "default"
	if id := session.GetAgentID(); id != "" {
		agentID = id
	}
	sessionID := session.GetSessionID()
	return filepath.Join(baseDir, ".agent_history", "file_ops_"+agentID+"_"+sessionID+".json")
}

// recordRmTargetsBeforeDeletion 在 rm 命令执行前，读取并记录将被删除的文件内容。
// 对齐 Python: _record_rm_targets_before_deletion (filesystem.py L180-202)
func recordRmTargetsBeforeDeletion(historyPath string, rmTargets []string, fsReader func(string) (string, error)) {
	for _, rawPath := range rmTargets {
		absPath := rawPath
		if !filepath.IsAbs(rawPath) {
			absPath = filepath.Join(filepath.Dir(historyPath), "..", rawPath) // 相对路径转绝对路径
		}
		absPath = filepath.Clean(absPath)
		content, err := fsReader(absPath)
		if err != nil || content == "" {
			continue // 文件不存在或读取失败，跳过
		}
		_ = appendOpHistory(historyPath, absPath, "delete", &content, nil)
	}
}

// detectAndRecordDeletions 在 bash 执行后，扫描 history 检测消失的文件并记录。
// 对齐 Python: _detect_and_record_deletions (filesystem.py L205-239)
func detectAndRecordDeletions(historyPath string) {
	historyMu.Lock()
	defer historyMu.Unlock()

	data, err := os.ReadFile(historyPath)
	if err != nil {
		return // 文件不存在，无需检测
	}
	history := map[string][]any{}
	if jsonErr := json.Unmarshal(data, &history); jsonErr != nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	deletionsAdded := false
	for filePath, entries := range history {
		if len(entries) == 0 {
			continue
		}
		last, ok := entries[len(entries)-1].(map[string]any)
		if !ok {
			continue
		}
		if last["action"] == "delete" {
			continue // 已经标记删除
		}
		if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
			// 文件消失了
			oldContent := last["new_content"]
			entries = append(entries, map[string]any{
				"action":      "delete",
				"timestamp":   now,
				"old_content": oldContent,
				"new_content": nil,
			})
			if len(entries) > maxHistoryPerFile {
				entries = entries[len(entries)-maxHistoryPerFile:]
			}
			history[filePath] = entries
			deletionsAdded = true
		}
	}
	if !deletionsAdded {
		return
	}
	dir := filepath.Dir(historyPath)
	_ = os.MkdirAll(dir, 0755)
	jsonData, _ := json.Marshal(history)
	tmpPath := historyPath + ".tmp"
	_ = os.WriteFile(tmpPath, jsonData, 0644)
	_ = os.Replace(tmpPath, historyPath)
}
```

注意：`buildHistoryPath` 的 `session` 参数需要 `stream.StreamableSession` 接口提供 `GetWorkspace()/GetAgentID()/GetSessionID()`。如果该接口不存在，需定义或使用现有 session 接口。检查代码后确认具体类型。

- [ ] **Step 3: 更新 filesystem/doc.go**

添加 `history.go` 条目。

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/agentcore/harness/tools/filesystem/... -run "TestAppendOpHistory|TestBuildHistoryPath|TestRecordRm|TestDetect"
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/tools/filesystem/history.go internal/agentcore/harness/tools/filesystem/history_test.go internal/agentcore/harness/tools/filesystem/doc.go && git commit -m "feat(filesystem): 实现文件操作历史记录（appendOpHistory + buildHistoryPath + recordRmTargets + detectDeletions）"
```

---

## Task 7: 回填 write_file.go — 调用 appendOpHistory

**Files:**
- Modify: `internal/agentcore/harness/tools/filesystem/write_file.go`

- [ ] **Step 1: 在 WriteFileTool invoke 成功后插入 appendOpHistory 调用**

在 `write_file.go` 中，文件写入成功 + read state 更新后（约 line 186-188 之间），插入：

```go
// 记录文件操作历史
historyPath := buildHistoryPath(session)
if historyPath != "" {
    var oldContentPtr *string
    if operationType == "update" {
        oldContentPtr = &oldContent
    }
    if appendErr := appendOpHistory(historyPath, path, "write", oldContentPtr, &content); appendErr != nil {
        logger.Warn(logComponent).Str("history_path", historyPath).Str("file_path", path).Err(appendErr).Msg("记录文件操作历史失败")
    }
}
```

注意：需确认 `session` 变量（传入 `buildHistoryPath`）的可用性。WriteFileTool 的闭包签名中可能需要调整，确保 session 对象可获取 agentID/sessionID/workspace。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/agentcore/harness/tools/filesystem/... -run "TestWriteFile"
```

Expected: 原有测试继续通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/filesystem/write_file.go && git commit -m "feat(filesystem): 回填 WriteFileTool appendOpHistory 调用"
```

---

## Task 8: 回填 edit_file.go — 调用 appendOpHistory

**Files:**
- Modify: `internal/agentcore/harness/tools/filesystem/edit_file.go`

- [ ] **Step 1: 在 EditFileTool invoke 成功后插入 appendOpHistory 调用**

两个写入分支（新文件创建 + 编辑替换）各插入 appendOpHistory 调用。逻辑类似 write_file.go。

- [ ] **Step 2: 运行测试**

```bash
go test -tags=test -v ./internal/agentcore/harness/tools/filesystem/... -run "TestEditFile"
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/filesystem/edit_file.go && git commit -m "feat(filesystem): 回填 EditFileTool appendOpHistory 调用"
```

---

## Task 9: 回填 bash.go — 调用 recordRmTargets + detectDeletions

**Files:**
- Modify: `internal/agentcore/harness/tools/shell/bash.go`

- [ ] **Step 1: 在 BashTool 前景执行路径中插入 rm 跟踪**

在前景执行成功后（约 line 193-195），插入：

```go
// rm 目标检测：执行前记录待删文件内容
historyPath := buildHistoryPath(session)
if historyPath != "" {
    rmTargets := ParseRmTargets(command)
    if len(rmTargets) > 0 {
        recordRmTargetsBeforeDeletion(historyPath, rmTargets, func(p string) (string, error) {
            data, err := os.ReadFile(p)
            return string(data), err
        })
    }
    // 执行后检测消失文件
    detectAndRecordDeletions(historyPath)
}
```

注意：`buildHistoryPath` 在 filesystem 包中定义，shell 包需要 import filesystem 包。需检查是否会循环依赖。由于 shell 包当前不 import filesystem，这不会循环。

- [ ] **Step 2: 运行测试**

```bash
go test -tags=test -v ./internal/agentcore/harness/tools/shell/... -run "TestBash"
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/shell/bash.go && git commit -m "feat(shell): 回填 BashTool rm 跟踪（recordRmTargetsBeforeDeletion + detectAndRecordDeletions）"
```

---

## Task 10: 创建 server/utils/diff_service.go — DiffService

**Files:**
- Create: `internal/swarm/server/utils/diff_service.go`
- Create: `internal/swarm/server/utils/diff_service_test.go`

这是最复杂的任务，对齐 Python diff_service.py（476行）。核心逻辑包括：
- 读取 history.json → 建立 turn 边界
- 读取 .agent_history/file_ops_*.json → 按时间映射到 turn
- computeHunks 用 diffmatchpatch 计算 diff
- finalizeTurn 统计

- [ ] **Step 1: 写 diff_service.go 实现**

约 400 行 Go 代码。包含：
- `DiffService` 结构体 + `NewDiffService` + `GetDiffService`（sync.Once 单例）
- `TurnDiff/FileDiff/Hunk/DiffStats/RestoreFileAction/FileEdit` 导出类型
- `GetTurnDiffs` + `GetFilesToRestore` 导出方法
- 12 个非导出 helper 方法（computeTurnDiffs/isTurnEnd/findNextUserTime/readHistory/getProjectDirFromMetadata/isValidFileOpsFile/readAgentHistory/findFileEditsByTimeRange/isoToTimestamp/timestampToISO/computeHunks/finalizeTurn）
- dedupTimeToleranceSec = 2.0 常量

- [ ] **Step 2: 写 diff_service_test.go**

使用 `t.TempDir()` 创建测试数据文件，核心场景：
- `TestGetTurnDiffs_空历史` → 返回空
- `TestGetTurnDiffs_单turn单文件` → 1 TurnDiff + 1 FileDiff + hunks
- `TestGetTurnDiffs_多turn` → 按时间倒序
- `TestGetFilesToRestore_turn0` → 返回所有文件的 old_content
- `TestGetFilesToRestore_新文件标记删除` → RestoreContent=nil, Action="delete"
- `TestComputeHunks_创建文件` → oldContent=nil
- `TestComputeHunks_删除文件` → newContent=nil
- `TestComputeHunks_正常diff` → hunks 带 +/- 行
- `TestReadAgentHistory_多文件合并` → 全局 + session-specific
- `TestReadAgentHistory_dedup时间容差` → 2秒内视为同一操作
- `TestIsoToTimestamp` + `TestTimestampToISO` → 双向转换

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -v ./internal/swarm/server/utils/... -run "TestGetTurnDiffs|TestGetFilesToRestore|TestComputeHunks|TestReadAgentHistory|TestIso|TestTimestamp"
```

Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/utils/diff_service.go internal/swarm/server/utils/diff_service_test.go && git commit -m "feat(server/utils): 实现 DiffService（GetTurnDiffs + GetFilesToRestore + 12 helper）"
```

---

## Task 11: 回填 handleCommandDiff — 从 stub 改为调用 DiffService

**Files:**
- Modify: `internal/swarm/server/handle_command.go`

- [ ] **Step 1: 修改 handleCommandDiff**

将 stub 实现替换为实际调用 DiffService：

```go
func (s *AgentServer) handleCommandDiff(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := request.SessionID
	if sessionID == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"diffs": []any{},
				"error": "session_id is required",
			}),
		), nil
	}

	diffs := utils.GetDiffService().GetTurnDiffs(sessionID)
	// 序列化 diffs 为 []map[string]any
	diffMaps := make([]map[string]any, len(diffs))
	for i, d := range diffs {
		diffMaps[i] = turnDiffToMap(d)
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"diffs": diffMaps,
		}),
	), nil
}
```

需新增 `turnDiffToMap` 辅助函数将 `TurnDiff` 结构体序列化为前端消费的 dict。

- [ ] **Step 2: 运行测试**

```bash
go test -tags=test -v ./internal/swarm/server/... -run "TestHandleCommandDiff"
```

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/handle_command.go && git commit -m "feat(server): handleCommandDiff 从 stub 改为调用 DiffService"
```

---

## Task 12: 更新 doc.go 文件 + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/swarm/server/doc.go`（添加 utils/ 条目）
- Modify: `internal/swarm/server/adapter/doc.go`（标注 stream_utils 迁移）
- Modify: `IMPLEMENTATION_PLAN.md`（10.3.25 ☐ → ✅）

- [ ] **Step 1: 更新所有 doc.go**

在各 doc.go 的文件目录中添加新文件条目。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 `10.3.25 | ☐ | Utils | 延后` 改为 `10.3.25 | ✅ | Utils | stream_utils(提取ParseStreamChunk+10helper)/utils(GetChatID+IsTeamParams)/diff_service(DiffService+写入端history回填)`

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/doc.go internal/swarm/server/adapter/doc.go internal/agentcore/harness/tools/filesystem/doc.go internal/agentcore/harness/rails/interrupt/doc.go IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN 10.3.25 ✅"
```

---

## Task 13: 最终验证 — 全量测试

- [ ] **Step 1: 运行全量测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags=test -cover ./internal/swarm/server/utils/... ./internal/agentcore/harness/tools/filesystem/... ./internal/agentcore/harness/rails/interrupt/... ./internal/swarm/server/adapter/... ./internal/swarm/server/...
```

Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 确认编译无错误**

```bash
go build ./...
```

Expected: 无编译错误

- [ ] **Step 3: Final commit（如有遗漏修复）**

如有必要，提交最终修复。
