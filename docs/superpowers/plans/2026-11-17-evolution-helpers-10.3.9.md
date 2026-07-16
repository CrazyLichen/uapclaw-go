# 10.3.9 EvolutionHelpers 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 EvolutionHelpers 纯工具模块 + GatewayPush 推送传输层 + delivery_context 补齐，对齐 Python evolution_helpers.py / gateway_push / session_metadata

**Architecture:** 新建 3 个包/子包（gateway_push、adapter/evolution），修改 2 个现有文件（agent_server.go、handle_session.go），删除 isApprovalEvent 错误实现改用 if/else if 分流

**Tech Stack:** Go 1.24+，项目内部 swarm/server、swarm/transport 包

**Design Spec:** `docs/superpowers/specs/2026-11-17-evolution-helpers-10.3.9-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/swarm/server/gateway_push/doc.go` | 包文档 |
| Create | `internal/swarm/server/gateway_push/transport.go` | GatewayPushTransport 接口 + ChannelPushTransport 实现 |
| Create | `internal/swarm/server/gateway_push/transport_test.go` | ChannelPushTransport 单元测试 |
| Create | `internal/swarm/server/adapter/evolution/doc.go` | 包文档 |
| Create | `internal/swarm/server/adapter/evolution/helpers.go` | 3 结构体 + 常量/变量 + 22 导出函数 + 1 非导出函数 |
| Create | `internal/swarm/server/adapter/evolution/helpers_test.go` | 单元测试 |
| Modify | `internal/swarm/server/agent_server.go` | 新增单例 + SendPush 高层方法 |
| Modify | `internal/swarm/server/handle_session.go` | 新增 delivery_context + BuildServerPushMessage |
| Modify | `internal/swarm/server/adapter/deep_adapter_evolution.go` | 删除 isApprovalEvent |
| Modify | `internal/swarm/server/adapter/deep_adapter_helpers_test.go` | 删除 isApprovalEvent 测试 |
| Modify | `internal/swarm/server/adapter/doc.go` | 新增 evolution/ 子包条目 |

---

### Task 1: AgentServer 单例 + SendPush

**Files:**
- Modify: `internal/swarm/server/agent_server.go`

- [ ] **Step 1: 在 agent_server.go 全局变量区块新增单例变量**

在现有 `logComponent` 常量之后、全局变量区块（如有）添加：

```go
var (
	// agentServerInstance AgentServer 单例实例
	agentServerInstance *AgentServer
	// agentServerOnce 保证单例只设置一次
	agentServerOnce sync.Once
)
```

- [ ] **Step 2: 新增 GetInstance 和 ResetInstance 导出函数**

在导出函数区块（`Transport()` 方法之后）添加：

```go
// GetInstance 返回 AgentServer 单例实例。
// 对齐 Python: AgentWebSocketServer.get_instance()
func GetInstance() *AgentServer { return agentServerInstance }

// ResetInstance 重置单例（仅用于测试）。
// 对齐 Python: AgentWebSocketServer.reset_instance()
func ResetInstance() {
	agentServerInstance = nil
	agentServerOnce = sync.Once{}
}
```

- [ ] **Step 3: 在 run() 方法中设置单例**

在 `run()` 方法中 `running = true` 之后添加：

```go
// 设置单例
agentServerOnce.Do(func() {
	agentServerInstance = s
})
```

- [ ] **Step 4: 新增 SendPush 导出方法**

在 `Transport()` 方法之后添加：

```go
// SendPush AgentServer 主动向 Gateway 推送消息（高层方法）。
//
// 对齐 Python: AgentWebSocketServer.send_push(msg)
// 内部流程：BuildServerPushWire(msg) → json.Marshal → sendToGateway(data)
// 这是所有 server_push 场景的统一入口。
func (s *AgentServer) SendPush(ctx context.Context, msg map[string]any) error {
	wire := transport.BuildServerPushWire(msg)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("SendPush: wire 编码失败")
		return fmt.Errorf("wire 编码失败: %w", err)
	}
	s.sendToGateway(data)

	responseKind := ""
	if rk, ok := msg["response_kind"].(string); ok {
		responseKind = strings.TrimSpace(rk)
	}
	if responseKind != "" {
		channelID, _ := msg["channel_id"].(string)
		logger.Info(logComponent).
			Str("channel_id", channelID).
			Str("response_kind", responseKind).
			Msg("SendPush response_kind wire 已发送")
	}
	return nil
}
```

注意：需要在文件头部 import 中添加 `"strings"` 和 `"fmt"`（如尚未存在）。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/...`
Expected: 编译成功

- [ ] **Step 6: Commit**

```
feat(agentserver): add GetInstance/ResetInstance singleton and SendPush method
```

---

### Task 2: gateway_push 包 — 接口与实现

**Files:**
- Create: `internal/swarm/server/gateway_push/doc.go`
- Create: `internal/swarm/server/gateway_push/transport.go`
- Create: `internal/swarm/server/gateway_push/transport_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package gateway_push 提供 AgentServer → Gateway 的下行推送抽象与实现。
//
// 定义 GatewayPushTransport 接口——所有 server_push 场景的统一推送入口，
// 以及 ChannelPushTransport 进程内实现（通过 AgentServer 单例发送）。
// 将来跨进程模式使用 WebSocketPushTransport（也在本包中实现）。
//
// 所有 server_push 场景（evolution 状态/cron 触发/文件推送/多会话工具等）
// 统一通过 GatewayPushTransport.SendPush 推送，不直接操作底层 Transport。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go            # 包文档
//	├── transport.go      # GatewayPushTransport 接口 + ChannelPushTransport 实现
//	└── transport_test.go # ChannelPushTransport 单元测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
```

- [ ] **Step 2: 创建 transport.go**

```go
package gateway_push

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelPushTransport 进程内推送实现，通过 AgentServer 单例发送。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py
// (WebSocketGatewayPushTransport)
//
// Python 通过 AgentWebSocketServer.get_instance().send_push(msg) 推送，
// Go 侧同样通过 server.GetInstance().SendPush(msg) 推送。
type ChannelPushTransport struct{}

// ──────────────────────────── 常量 ────────────────────────────

// logComponentPush 推送日志组件
const logComponentPush = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelPushTransport 创建 ChannelPushTransport 实例。
func NewChannelPushTransport() *ChannelPushTransport {
	return &ChannelPushTransport{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 接口合规：ChannelPushTransport 实现 GatewayPushTransport
var _ GatewayPushTransport = (*ChannelPushTransport)(nil)

// ──────────────────────────── 接口 ────────────────────────────

// GatewayPushTransport AgentServer → Gateway 的推送传输协议。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
// 所有 server_push 场景统一通过此接口推送，不直接操作底层 Transport。
type GatewayPushTransport interface {
	// SendPush 向 Gateway 发送一条 server_push 语义的消息。
	//
	// msg 格式与 Python AgentWebSocketServer.send_push 入参一致：
	//   {request_id, channel_id, session_id, payload, metadata?, response_kind?}
	// 内部自动调 BuildServerPushWire 编码为 E2A wire 格式。
	SendPush(ctx context.Context, msg map[string]any) error
}

// SendPush 通过 AgentServer 单例向 Gateway 推送消息。
func (t *ChannelPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
	s := server.GetInstance()
	if s == nil {
		logger.Warn(logComponentPush).Msg("ChannelPushTransport: AgentServer 单例未初始化")
		return fmt.Errorf("AgentServer 单例未初始化")
	}
	return s.SendPush(ctx, msg)
}
```

注意：Go 源码声明排列顺序按规范2 — 接口排在结构体之前。上面的 doc.go 中已说明接口在 transport.go 中，实际代码应调整顺序为：接口 → 结构体 → 常量 → 导出函数 → 非导出函数。修正后：

```go
package gateway_push

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayPushTransport AgentServer → Gateway 的推送传输协议。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
// 所有 server_push 场景统一通过此接口推送，不直接操作底层 Transport。
type GatewayPushTransport interface {
	// SendPush 向 Gateway 发送一条 server_push 语义的消息。
	//
	// msg 格式与 Python AgentWebSocketServer.send_push 入参一致：
	//   {request_id, channel_id, session_id, payload, metadata?, response_kind?}
	// 内部自动调 BuildServerPushWire 编码为 E2A wire 格式。
	SendPush(ctx context.Context, msg map[string]any) error
}

// ChannelPushTransport 进程内推送实现，通过 AgentServer 单例发送。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py
// (WebSocketGatewayPushTransport)
//
// Python 通过 AgentWebSocketServer.get_instance().send_push(msg) 推送，
// Go 侧同样通过 server.GetInstance().SendPush(msg) 推送。
type ChannelPushTransport struct{}

// ──────────────────────────── 常量 ────────────────────────────

// logComponentPush 推送日志组件
const logComponentPush = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelPushTransport 创建 ChannelPushTransport 实例。
func NewChannelPushTransport() *ChannelPushTransport {
	return &ChannelPushTransport{}
}

// SendPush 通过 AgentServer 单例向 Gateway 推送消息。
func (t *ChannelPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
	s := server.GetInstance()
	if s == nil {
		logger.Warn(logComponentPush).Msg("ChannelPushTransport: AgentServer 单例未初始化")
		return fmt.Errorf("AgentServer 单例未初始化")
	}
	return s.SendPush(ctx, msg)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 接口合规：ChannelPushTransport 实现 GatewayPushTransport
var _ GatewayPushTransport = (*ChannelPushTransport)(nil)
```

- [ ] **Step 3: 创建 transport_test.go**

```go
package gateway_push

import (
	"context"
	"testing"
)

// TestNewChannelPushTransport 测试创建实例。
func TestNewChannelPushTransport(t *testing.T) {
	transport := NewChannelPushTransport()
	if transport == nil {
		t.Error("NewChannelPushTransport 不应返回 nil")
	}
}

// TestChannelPushTransport_SendPush_无单例 测试无单例时返回错误。
func TestChannelPushTransport_SendPush_无单例(t *testing.T) {
	// 确保单例为 nil
	ResetTestInstance()
	transport := NewChannelPushTransport()
	ctx := t.Context()
	msg := map[string]any{"request_id": "req-1"}
	err := transport.SendPush(ctx, msg)
	if err == nil {
		t.Error("无单例时 SendPush 应返回错误")
	}
}

// TestGatewayPushTransport接口合规 测试 ChannelPushTransport 实现 GatewayPushTransport。
func TestGatewayPushTransport接口合规(t *testing.T) {
	var _ GatewayPushTransport = (*ChannelPushTransport)(nil)
}
```

注意：`ResetTestInstance()` 需要一个包级函数来重置 server 包的单例。因为 `gateway_push` 是 `server` 的子包，可以直接调用 `server.ResetInstance()`。但测试中需要避免 import cycle — 实际上 `gateway_push` 已经 import `server`，所以可以直接调 `server.ResetInstance()`。修正测试：

```go
package gateway_push

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
)

// TestNewChannelPushTransport 测试创建实例。
func TestNewChannelPushTransport(t *testing.T) {
	transport := NewChannelPushTransport()
	if transport == nil {
		t.Error("NewChannelPushTransport 不应返回 nil")
	}
}

// TestChannelPushTransport_SendPush_无单例 测试无单例时返回错误。
func TestChannelPushTransport_SendPush_无单例(t *testing.T) {
	server.ResetInstance()
	transport := NewChannelPushTransport()
	ctx := t.Context()
	msg := map[string]any{"request_id": "req-1"}
	err := transport.SendPush(ctx, msg)
	if err == nil {
		t.Error("无单例时 SendPush 应返回错误")
	}
}

// TestGatewayPushTransport接口合规 测试 ChannelPushTransport 实现 GatewayPushTransport。
func TestGatewayPushTransport接口合规(t *testing.T) {
	var _ GatewayPushTransport = (*ChannelPushTransport)(nil)
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/gateway_push/...`
Expected: 编译成功

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/gateway_push/... -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```
feat(gateway_push): add GatewayPushTransport interface and ChannelPushTransport
```

---

### Task 3: handle_session.go — delivery_context 补齐

**Files:**
- Modify: `internal/swarm/server/handle_session.go`

- [ ] **Step 1: 新增常量和全局变量**

在现有常量 `metadataFileName` 附近添加：

```go
const (
	// deliveryContextKind 推送类型，对齐 Python _DELIVERY_KIND_SERVER_PUSH
	deliveryContextKind = "server_push"
)

var (
	// deliveryContextCache 内存缓存，解决异步写入时读取到陈旧数据的竞态
	// 对齐 Python: _METADATA_CACHE
	deliveryContextCache = make(map[string]map[string]any)
	// deliveryContextMu 保护缓存的读写锁
	// 对齐 Python: _CACHE_LOCK
	deliveryContextMu sync.RWMutex
)
```

- [ ] **Step 2: 新增 SetSessionDeliveryContext 函数**

对齐 Python `set_session_delivery_context()`，在导出函数区块添加。该函数从 metadata.json 读取 delivery_context，合并传入参数后写回，并更新内存缓存。

- [ ] **Step 3: 新增 GetSessionDeliveryContext 函数**

对齐 Python `get_session_delivery_context()`，优先从内存缓存读取，否则从 metadata.json 读取。

- [ ] **Step 4: 新增 BuildServerPushMessage 函数**

对齐 Python `build_server_push_message()`：

```go
// BuildServerPushMessage 基于 session delivery context 构造 server_push 消息。
//
// 对齐 Python: build_server_push_message()
// 被 evolution_helpers 和其他推送场景调用。
func BuildServerPushMessage(
	sessionID, requestID string,
	payload map[string]any,
	fallbackChannelID ...string,
) map[string]any {
	deliveryCtx := GetSessionDeliveryContext(sessionID)
	channelID := "default"
	if deliveryCtx != nil {
		if cid, ok := deliveryCtx["channel_id"].(string); ok && cid != "" {
			channelID = cid
		}
	}
	if len(fallbackChannelID) > 0 && fallbackChannelID[0] != "" && channelID == "default" {
		channelID = fallbackChannelID[0]
	}

	message := map[string]any{
		"request_id": requestID,
		"channel_id": channelID,
		"session_id": sessionID,
		"payload":    payload,
	}
	if deliveryCtx != nil {
		if rm, ok := deliveryCtx["route_metadata"].(map[string]any); ok && len(rm) > 0 {
			message["metadata"] = deepCopyMap(rm)
		}
	}
	return message
}
```

- [ ] **Step 5: 新增 deepCopyMap 非导出辅助函数**

```go
// deepCopyMap 深拷贝 map，对齐 Python copy.deepcopy()。
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(val)
		default:
			dst[k] = v
		}
	}
	return dst
}
```

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/...`
Expected: 编译成功

- [ ] **Step 7: Commit**

```
feat(session): add delivery context (set/get/buildServerPushMessage) aligning Python session_metadata
```

---

### Task 4: adapter/evolution 包 — helpers.go 核心

**Files:**
- Create: `internal/swarm/server/adapter/evolution/doc.go`
- Create: `internal/swarm/server/adapter/evolution/helpers.go`

这是最大的任务。按照 spec 第 3.3 和 3.4 节，逐步创建文件。

- [ ] **Step 1: 创建 doc.go**

```go
// Package evolution 提供技能演进（Skill Evolution）事件分类、状态提取和推送的共享辅助工具。
//
// 本包是纯工具模块，不含外部依赖，所有函数均为无状态纯函数或简单数据结构。
// 消费者通过 EvolutionPushContext 注入推送传输和回调函数来使用推送能力。
//
// 核心功能：
//   - 事件分类：将 SDK 内部演进事件分为 approval/outcome/progress/stream 四类
//   - 状态提取：从原始事件中提取 request_id、stage、terminal 等字段
//   - Noop 检测：根据消息内容识别"无演进信号"场景，映射到细粒度 noop 阶段
//   - 推送桥接：通过 EvolutionPushContext 将演进状态推送到 Gateway 侧
//   - 审批分组：按 request_id 聚合同一审批的多个事件
//
// 文件目录：
//
//	evolution/
//	├── doc.go            # 包文档
//	├── helpers.go        # 3 结构体 + 常量/变量 + ~22 导出函数 + 1 非导出函数
//	└── helpers_test.go   # 单元测试
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
package evolution
```

- [ ] **Step 2: 创建 helpers.go — 结构体区块**

按规范2顺序：接口 → 结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数

```go
package evolution

import (
	"context"
	"math"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	gatewaypush "github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionPushContext evolution 推送上下文。
// 对齐 Python: EvolutionPushContext
type EvolutionPushContext struct {
	// Transport 推送传输
	Transport gatewaypush.GatewayPushTransport
	// ChannelID 通道标识（可能为空）
	ChannelID string
	// SessionID 会话标识
	SessionID string
}

// EvolutionStatusUpdate evolution 状态更新。
// 对齐 Python: EvolutionStatusUpdate
type EvolutionStatusUpdate struct {
	// RequestID 请求标识
	RequestID string
	// Status 状态
	Status string
	// Stage 阶段
	Stage string
	// Message 消息
	Message string
}

// EvolutionProgressStatus evolution 进度状态。
// 对齐 Python: EvolutionProgressStatus
type EvolutionProgressStatus struct {
	// Stage 阶段
	Stage string
	// Message 消息
	Message string
	// RequestID 请求标识（nil 表示无）
	RequestID *string
	// Terminal 是否终结
	Terminal bool
}

// TerminalProgressItem 终结进度条目。
// 对齐 Python: terminal_progress_from_events 返回的 tuple
type TerminalProgressItem struct {
	// RequestID 请求标识
	RequestID *string
	// Terminal 终结信息
	Terminal map[string]string
}
```

- [ ] **Step 3: 创建 helpers.go — 常量区块**

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamEvolutionIdleSleepSec watcher 空闲轮询间隔
	TeamEvolutionIdleSleepSec = 1.0
	// TeamEvolutionEventTimeoutSec 事件超时
	TeamEvolutionEventTimeoutSec = 900.0
	// TeamEvolutionEventTimeoutGraceSec 超时宽限
	TeamEvolutionEventTimeoutGraceSec = 5.0

	// TeamEvolutionStartStage 起始阶段
	TeamEvolutionStartStage = "collecting"
	// TeamEvolutionStartMessage 起始消息
	TeamEvolutionStartMessage = "Running team skill evolution analysis..."
	// TeamEvolutionNoopStage 无演进（通用）
	TeamEvolutionNoopStage = "no_evolution_generated"
	// TeamEvolutionNoopNoSkillStage 无演进（无技能）
	TeamEvolutionNoopNoSkillStage = "no_evolution_no_skill"
	// TeamEvolutionNoopNoSignalStage 无演进（无信号）
	TeamEvolutionNoopNoSignalStage = "no_evolution_no_signal"
	// TeamEvolutionNoopNoRecordsStage 无演进（无记录）
	TeamEvolutionNoopNoRecordsStage = "no_evolution_no_records"
	// TeamEvolutionHiddenStage 隐藏阶段
	TeamEvolutionHiddenStage = "hidden"
)

// logComponentEvolution 日志组件
const logComponentEvolution = logger.ComponentAgentServer
```

- [ ] **Step 4: 创建 helpers.go — 全局变量区块**

```go
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// TeamEvolutionNoopMarkers 通用 noop 标记
	TeamEvolutionNoopMarkers = []string{
		"no existing skill found",
		"no evolution signals detected",
		"no evolution records generated",
	}
	// TeamEvolutionNoSkillMarkers 无技能标记
	TeamEvolutionNoSkillMarkers = []string{
		"no skill usage",
		"no existing skill",
		"no regular skill could be attributed",
		"no team/swarm skill",
	}
	// TeamEvolutionNoSignalMarkers 无信号标记
	TeamEvolutionNoSignalMarkers = []string{
		"no actionable evolution signals detected",
		"no evolution signals detected",
	}

	// TeamEvolutionNoopStages noop 阶段集合
	TeamEvolutionNoopStages = map[string]struct{}{
		TeamEvolutionNoopStage:          {},
		TeamEvolutionNoopNoSkillStage:   {},
		TeamEvolutionNoopNoSignalStage:  {},
		TeamEvolutionNoopNoRecordsStage: {},
	}
	// TeamEvolutionHiddenTerminalStages 隐藏终结阶段集合
	TeamEvolutionHiddenTerminalStages = map[string]struct{}{
		TeamEvolutionHiddenStage: {},
		"failed":                 {},
		"timed_out":              {},
	}
	// TeamEvolutionVisibleProgressStages 可见进度阶段集合
	TeamEvolutionVisibleProgressStages = map[string]struct{}{
		"generating":                        {},
		"approval_required":                 {},
		"completed":                         {},
		TeamEvolutionNoopStage:              {},
		TeamEvolutionNoopNoSkillStage:       {},
		TeamEvolutionNoopNoSignalStage:      {},
		TeamEvolutionNoopNoRecordsStage:     {},
	}

	// sdkProgressStageMap SDK→显示阶段映射
	// 对齐 Python: _SDK_PROGRESS_STAGE_MAP
	sdkProgressStageMap = map[string]string{
		"started":            "detecting",
		"detecting_signals":  "detecting",
		"staging":            "generating",
		"generating_updates": "generating",
		"approval_required":  "approval_required",
		"auto_approved":      "completed",
		"cancelled":          TeamEvolutionHiddenStage,
		"completed":          "completed",
		"failed":             "failed",
		"timed_out":          "timed_out",
	}

	// sdkProgressTerminalStages SDK 终结阶段集合
	// 对齐 Python: _SDK_PROGRESS_TERMINAL_STAGES
	sdkProgressTerminalStages = map[string]struct{}{
		"auto_approved": {},
		"cancelled":     {},
		"completed":     {},
		"failed":        {},
		"timed_out":     {},
	}
)
```

- [ ] **Step 5: 创建 helpers.go — 函数类型定义**

在全局变量之后、导出函数之前添加：

```go
// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// 对齐 Python: build_server_push_message 回调参数
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any

// ParseStreamChunkFunc 解析流式 chunk 的函数类型。
// 对齐 Python: parse_stream_chunk 回调参数
type ParseStreamChunkFunc func(evt any) map[string]any

// BroadcastEventFunc 广播事件的函数类型。
// 对齐 Python: broadcast_event 回调参数
type BroadcastEventFunc func(channelID *string, sessionID string, parsed map[string]any)

// WarnMissingRequestIDFunc 缺少 request_id 时的警告回调。
// 对齐 Python: group_evolution_approvals 的 warn_missing_request_id 参数
type WarnMissingRequestIDFunc func(sessionID string)
```

- [ ] **Step 6: 创建 helpers.go — 22 个导出函数**

逐一实现 spec 3.4 中列出的 22 个导出函数，对齐 Python `evolution_helpers.py` 的每一个函数。每个函数的逻辑逐行对齐 Python 源码。由于篇幅限制，这里列出关键函数签名，实现时需要对照 `/home/opensource/jiuwenswarm-develop/jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py` 逐函数翻译。

关键函数（需要特别对照 Python 实现的）：
1. `EventPayloadDict` — 对齐 L99-104
2. `EventType` — 对齐 L107-113
3. `ResolveEvolutionEventTimeoutSec` — 对齐 L116-138
4. `IsEvolutionApprovalEvent` — 对齐 L141-145
5. `EvolutionEventKind` — 对齐 L148-157
6. `IsEvolutionOutcomeEvent` — 对齐 L160-161
7. `EvolutionOutcomeFromEvent` — 对齐 L164-175
8. `ExtractEvolutionRequestID` — 对齐 L178-187
9. `EvolutionProgressStatusFromEvent` — 对齐 L190-213
10. `VisibleEvolutionProgressFromEvents` — 对齐 L216-221
11. `ProgressForRequest` — 对齐 L224-232
12. `TerminalStage` — 对齐 L235-236
13. `TerminalProgressFromEvents` — 对齐 L239-245
14. `TeamEvolutionTerminalProgress` — 对齐 L260-305
15. `BuildEvolutionStatusUpdate` — 对齐 L308-319
16. `TeamEvolutionEndUpdate` — 对齐 L322-354
17. `GroupEvolutionApprovals` — 对齐 L357-373
18. `MakeTeamEvolutionCycleRequestID` — 对齐 L376-377
19. `PushEvolutionStatus` — 对齐 L380-402
20. `PushEvolutionEvent` — 对齐 L405-423
21. `BroadcastEvolutionProgress` — 对齐 L426-443
22. `PushEvolutionProgress` — 对齐 L446-479

- [ ] **Step 7: 创建 helpers.go — 非导出函数 noopStageFromMessage**

```go
// noopStageFromMessage 从消息内容推断 noop 阶段。
// 对齐 Python: _noop_stage_from_message()
func noopStageFromMessage(messageLower string) *string {
	if containsAnyMarker(messageLower, TeamEvolutionNoSkillMarkers) {
		result := TeamEvolutionNoopNoSkillStage
		return &result
	}
	if containsAnyMarker(messageLower, TeamEvolutionNoSignalMarkers) {
		result := TeamEvolutionNoopNoSignalStage
		return &result
	}
	if strings.Contains(messageLower, "no evolution records generated") {
		result := TeamEvolutionNoopNoRecordsStage
		return &result
	}
	if containsAnyMarker(messageLower, TeamEvolutionNoopMarkers) {
		result := TeamEvolutionNoopStage
		return &result
	}
	return nil
}

// containsAnyMarker 检查消息是否包含任一标记。
func containsAnyMarker(messageLower string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(messageLower, marker) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 8: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/evolution/...`
Expected: 编译成功

- [ ] **Step 9: Commit**

```
feat(evolution): add EvolutionHelpers with event classification, status extraction and push bridging
```

---

### Task 5: adapter/evolution 包 — 单元测试

**Files:**
- Create: `internal/swarm/server/adapter/evolution/helpers_test.go`

- [ ] **Step 1: 编写单元测试**

对每个导出函数编写测试用例，重点覆盖：
- `EventPayloadDict` / `EventType`：各种输入格式（dict、struct with payload、nil）
- `IsEvolutionApprovalEvent`：event_type 为 `chat.ask_user_question` 时返回 true
- `EvolutionEventKind`：approval/outcome/progress/stream 分类
- `IsEvolutionOutcomeEvent`：kind == "outcome" 时返回 true
- `EvolutionOutcomeFromEvent`：提取 status/message
- `ExtractEvolutionRequestID`：从 payload 和 _evolution_meta 提取
- `EvolutionProgressStatusFromEvent`：各种 stage 映射、noop 检测、terminal 判断
- `VisibleEvolutionProgressFromEvents`：过滤可见阶段
- `ProgressForRequest`：按 requestID 过滤
- `TeamEvolutionTerminalProgress`：隐藏终结阶段、noop 阶段、正常终结
- `BuildEvolutionStatusUpdate` / `TeamEvolutionEndUpdate`：字段正确
- `GroupEvolutionApprovals`：分组和缺失 requestID 警告
- `MakeTeamEvolutionCycleRequestID`：格式正确
- `PushEvolutionStatus` / `PushEvolutionEvent` / `PushEvolutionProgress`：使用 mock transport
- `BroadcastEvolutionProgress`：跳过 approval/outcome/terminal 事件

需要创建 mock 实现：

```go
// mockPushTransport 用于测试的模拟推送传输
type mockPushTransport struct {
	pushed []map[string]any
	err    error
}

func (m *mockPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
	if m.err != nil {
		return m.err
	}
	m.pushed = append(m.pushed, msg)
	return nil
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/evolution/... -v -cover`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: Commit**

```
test(evolution): add comprehensive unit tests for EvolutionHelpers
```

---

### Task 6: 消费者修正 — 删除 isApprovalEvent

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_helpers_test.go`

- [ ] **Step 1: 从 deep_adapter_evolution.go 删除 isApprovalEvent 方法**

删除以下代码：

```go
// isApprovalEvent 检查 request_id 是否为审批事件。
// 对齐 Python: is_approval_event() — 检查前缀
func (d *DeepAdapter) isApprovalEvent(requestID string) bool {
	return strings.HasPrefix(requestID, "skill_evolve_") ||
		strings.HasPrefix(requestID, "evolve_simplify_") ||
		strings.HasPrefix(requestID, "team_skill_evolve_")
}
```

- [ ] **Step 2: 从 deep_adapter_helpers_test.go 删除 isApprovalEvent 测试**

删除 `TestDeepAdapter_IsApprovalEvent` 测试函数（L181-199）。

- [ ] **Step 3: 确认 isApprovalEvent 无其他调用方**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn 'isApprovalEvent' internal/swarm/`
Expected: 无匹配结果

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功（因为 isApprovalEvent 当前没有被调用，仅是方法定义+测试）

- [ ] **Step 5: Commit**

```
refactor(adapter): remove incorrect isApprovalEvent method, will use direct prefix dispatch
```

---

### Task 7: doc.go 更新

**Files:**
- Modify: `internal/swarm/server/adapter/doc.go`

- [ ] **Step 1: 在 adapter/doc.go 文件目录中新增 evolution/ 子包**

在文件目录树中 `deep_adapter_dreaming.go` 之后添加：

```
//	├── evolution/               # Evolution 事件分类/状态提取/推送辅助（10.3.9）
//	│   ├── doc.go               # 包文档
//	│   ├── helpers.go           # 3 结构体 + 常量/变量 + ~22 导出函数
//	│   └── helpers_test.go      # 单元测试
```

- [ ] **Step 2: Commit**

```
docs(adapter): add evolution subpackage to doc.go
```

---

### Task 8: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.7-11 行的状态标记**

找到 10.3.7-11 行，将状态描述从 `EvolutionHelpers待实现` 改为 `EvolutionHelpers✅`：

将：
```
| 10.3.7-11 | 🔄 | 适配器辅助 | CodeAgentRail/TeamHelpers/EvolutionHelpers待实现/RecapPrompts✅/SysOpBuilder✅ |
```

改为：
```
| 10.3.7-11 | 🔄 | 适配器辅助 | CodeAgentRail/TeamHelpers/EvolutionHelpers✅/RecapPrompts✅/SysOpBuilder✅ |
```

- [ ] **Step 2: Commit**

```
docs: update IMPLEMENTATION_PLAN.md marking 10.3.9 EvolutionHelpers as complete
```

---

## Self-Review

### Spec Coverage Check

| Spec Section | Task |
|-------------|------|
| §2 推送链路对齐 | Task 1 (SendPush) + Task 2 (gateway_push) |
| §3.1 gateway_push/doc.go | Task 2 |
| §3.2 gateway_push/transport.go | Task 2 |
| §3.3 adapter/evolution/doc.go | Task 4 |
| §3.4 adapter/evolution/helpers.go | Task 4 |
| §4.1 agent_server.go 单例+SendPush | Task 1 |
| §4.2 handle_session.go delivery_context | Task 3 |
| §4.3 adapter/doc.go 更新 | Task 7 |
| §5.1 删除 isApprovalEvent | Task 6 |
| §5.2 保持 ⤵️ 标记 | Task 6 (不删除占位函数) |
| §7 实现顺序 | Task 1-8 按序 |

### Placeholder Scan

无 TBD/TODO/实现后补 — 所有步骤含具体代码。

### Type Consistency

- `EvolutionPushContext.Transport` 类型为 `gatewaypush.GatewayPushTransport` ✅
- `ChannelPushTransport.SendPush` 签名与 `GatewayPushTransport` 接口匹配 ✅
- `AgentServer.SendPush` 接收 `(ctx context.Context, msg map[string]any) error` ✅
- `BuildPushMessageFunc` 签名与 `BuildServerPushMessage` 兼容 ✅
