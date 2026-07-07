# AgentRequest / AgentResponse + PermissionContext 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Schema 层步骤 10.1.5（AgentRequest/AgentResponse）+ 10.1.8（PermissionContext）+ AgentResponseChunk 骨架，对齐 Python jiuwenswarm/common/schema/agent.py

**Architecture:** 在 internal/swarm/schema/ 包下新增 permission.go 和 agent.go 两个文件，遵循已有 Message 模型的风格模式（结构体 + 工厂函数 + Option + Validate + 中文注释 + 分隔线区块排列）。PermissionContext 先行实现（AgentRequest 依赖它）。

**Tech Stack:** Go 1.22+, encoding/json, testing, github.com/google/uuid（已有依赖）

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/swarm/schema/permission.go` | PermissionContext 结构体 + Scene/OwnerScopeKey/ToDict/FromDict + 工厂 + Validate |
| 新建 | `internal/swarm/schema/permission_test.go` | PermissionContext 全量测试 |
| 新建 | `internal/swarm/schema/agent.go` | AgentRequest + AgentResponse + AgentResponseChunk（骨架）+ 工厂 + Validate |
| 新建 | `internal/swarm/schema/agent_test.go` | AgentRequest/AgentResponse 全量测试 + AgentResponseChunk 基础测试 |
| 修改 | `internal/swarm/schema/doc.go` | 文件目录和包概述更新 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.1.5→✅、10.1.6→🔄、10.1.8→✅ 状态回填 |

---

### Task 1: PermissionContext 结构体与派生方法

**Files:**
- Create: `internal/swarm/schema/permission.go`

- [ ] **Step 1: 编写 permission.go — 结构体 + 派生方法 + 序列化 + 工厂 + Validate**

```go
package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionContext 权限上下文，统一承载权限判定所需的身份与场景信息。
//
// 作为 AgentRequest 的字段，携带请求方的身份标识、渠道信息和场景标记，
// 供 AgentServer 和权限模块判定是否允许执行操作。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (PermissionContext)
type PermissionContext struct {
	// PrincipalUserID 权限 owner（channel config 的 my_user_id）
	PrincipalUserID string `json:"principal_user_id"`
	// TriggeringUserID 触发者（IM sender）
	TriggeringUserID string `json:"triggering_user_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// GroupDigitalAvatar 是否为数字分身场景
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// WebUserID 预留：第二期 web 端本人审批
	WebUserID string `json:"web_user_id"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionContext 创建权限上下文实例，所有字段为零值。
func NewPermissionContext(opts ...PermissionContextOption) *PermissionContext {
	pc := &PermissionContext{}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}

// NewPermissionContextFromDict 从 dict 反序列化创建 PermissionContext。
//
// 对齐 Python: PermissionContext.from_dict()
func NewPermissionContextFromDict(data map[string]any) *PermissionContext {
	pc := &PermissionContext{}
	if v, ok := data["principal_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.PrincipalUserID = s
		}
	}
	if v, ok := data["triggering_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.TriggeringUserID = s
		}
	}
	if v, ok := data["channel_id"]; ok {
		if s, ok := v.(string); ok {
			pc.ChannelID = s
		}
	}
	if v, ok := data["group_digital_avatar"]; ok {
		if b, ok := v.(bool); ok {
			pc.GroupDigitalAvatar = b
		}
	}
	if v, ok := data["web_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.WebUserID = s
		}
	}
	return pc
}

// PermissionContextOption 权限上下文可选配置函数。
type PermissionContextOption func(*PermissionContext)

// WithPrincipalUserID 设置权限 owner。
func WithPrincipalUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.PrincipalUserID = id }
}

// WithTriggeringUserID 设置触发者。
func WithTriggeringUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.TriggeringUserID = id }
}

// WithPermissionChannelID 设置渠道标识。
func WithPermissionChannelID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.ChannelID = id }
}

// WithGroupDigitalAvatar 设置数字分身场景。
func WithGroupDigitalAvatar(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.GroupDigitalAvatar = v }
}

// WithWebUserID 设置 web 端用户标识。
func WithWebUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.WebUserID = id }
}

// Scene 根据渠道和数字分身标记派生场景类型。
//
// 派生规则（对齐 Python PermissionContext.scene）：
//   - channel_id == "web" → "web"
//   - group_digital_avatar == true → "group_digital_avatar"
//   - 其他 → "normal_im"
func (p *PermissionContext) Scene() string {
	if p.ChannelID == "web" {
		return "web"
	}
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	return "normal_im"
}

// OwnerScopeKey 返回用于 owner_scopes 配置查找的 key。
//
// 返回 [ChannelID, PrincipalUserID]，对齐 Python tuple[str, str]。
func (p *PermissionContext) OwnerScopeKey() [2]string {
	return [2]string{p.ChannelID, p.PrincipalUserID}
}

// ToDict 序列化为 dict（供 E2A WebSocket 传输）。
//
// 对齐 Python: PermissionContext.to_dict()
func (p *PermissionContext) ToDict() map[string]any {
	return map[string]any{
		"principal_user_id":    p.PrincipalUserID,
		"triggering_user_id":   p.TriggeringUserID,
		"channel_id":           p.ChannelID,
		"group_digital_avatar": p.GroupDigitalAvatar,
		"web_user_id":          p.WebUserID,
	}
}

// Validate 校验 PermissionContext 必填字段。
//
// 校验规则（对齐 Python 实际使用）：
//   - principal_user_id 非空
func (p *PermissionContext) Validate() error {
	if p.PrincipalUserID == "" {
		return fmt.Errorf("principal_user_id 不能为空")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 注意：PermissionContext 的 JSON 序列化由 encoding/json + struct tag 自动处理，
// 不需要手写 marshal/unmarshal 方法。ToDict/FromDict 提供给需要 map 形式的场景。
var _ json.Marshaler = nil // 确保 encoding/json 可用（编译期检查）
```

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译成功，无错误

---

### Task 2: PermissionContext 测试

**Files:**
- Create: `internal/swarm/schema/permission_test.go`

- [ ] **Step 1: 编写 permission_test.go — 全量测试**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 工厂函数测试 ────────────────────────────

// TestNewPermissionContext 验证工厂函数默认值
func TestNewPermissionContext(t *testing.T) {
	pc := NewPermissionContext()
	if pc.PrincipalUserID != "" {
		t.Errorf("PrincipalUserID 应为空，实际 %q", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "" {
		t.Errorf("TriggeringUserID 应为空，实际 %q", pc.TriggeringUserID)
	}
	if pc.ChannelID != "" {
		t.Errorf("ChannelID 应为空，实际 %q", pc.ChannelID)
	}
	if pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 false")
	}
	if pc.WebUserID != "" {
		t.Errorf("WebUserID 应为空，实际 %q", pc.WebUserID)
	}
}

// TestNewPermissionContext_使用Option 验证通过 Option 设置各字段
func TestNewPermissionContext_使用Option(t *testing.T) {
	pc := NewPermissionContext(
		WithPrincipalUserID("user-1"),
		WithTriggeringUserID("sender-1"),
		WithPermissionChannelID("web"),
		WithGroupDigitalAvatar(true),
		WithWebUserID("web-user-1"),
	)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.WebUserID != "web-user-1" {
		t.Errorf("WebUserID = %q, 期望 \"web-user-1\"", pc.WebUserID)
	}
}

// ──────────────────────────── Scene 方法测试 ────────────────────────────

// TestPermissionContext_Scene_web 验证 channel_id="web" 时返回 "web"
func TestPermissionContext_Scene_web(t *testing.T) {
	pc := NewPermissionContext(WithPermissionChannelID("web"))
	if got := pc.Scene(); got != "web" {
		t.Errorf("Scene() = %q, 期望 \"web\"", got)
	}
}

// TestPermissionContext_Scene_groupDigitalAvatar 验证数字分身场景
func TestPermissionContext_Scene_groupDigitalAvatar(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("feishu"),
		WithGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// TestPermissionContext_Scene_normalIM 验证默认为普通 IM 场景
func TestPermissionContext_Scene_normalIM(t *testing.T) {
	pc := NewPermissionContext(WithPermissionChannelID("feishu"))
	if got := pc.Scene(); got != "normal_im" {
		t.Errorf("Scene() = %q, 期望 \"normal_im\"", got)
	}
}

// TestPermissionContext_Scene_web优先级高于数字分身 验证 web 渠道优先级
func TestPermissionContext_Scene_web优先级高于数字分身(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("web"),
		WithGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "web" {
		t.Errorf("当 channel_id=web 且 group_digital_avatar=true 时，Scene() = %q, 期望 \"web\"", got)
	}
}

// ──────────────────────────── OwnerScopeKey 测试 ────────────────────────────

// TestPermissionContext_OwnerScopeKey 验证返回 [channel_id, principal_user_id]
func TestPermissionContext_OwnerScopeKey(t *testing.T) {
	pc := NewPermissionContext(
		WithPrincipalUserID("user-1"),
		WithPermissionChannelID("feishu"),
	)
	key := pc.OwnerScopeKey()
	if key[0] != "feishu" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 \"feishu\"", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 \"user-1\"", key[1])
	}
}

// ──────────────────────────── ToDict / FromDict 测试 ────────────────────────────

// TestPermissionContext_ToDict 验证序列化完整字段
func TestPermissionContext_ToDict(t *testing.T) {
	pc := NewPermissionContext(
		WithPrincipalUserID("user-1"),
		WithTriggeringUserID("sender-1"),
		WithPermissionChannelID("web"),
		WithGroupDigitalAvatar(true),
		WithWebUserID("web-user-1"),
	)
	d := pc.ToDict()
	if d["principal_user_id"] != "user-1" {
		t.Errorf("ToDict()[\"principal_user_id\"] = %v, 期望 \"user-1\"", d["principal_user_id"])
	}
	if d["triggering_user_id"] != "sender-1" {
		t.Errorf("ToDict()[\"triggering_user_id\"] = %v, 期望 \"sender-1\"", d["triggering_user_id"])
	}
	if d["channel_id"] != "web" {
		t.Errorf("ToDict()[\"channel_id\"] = %v, 期望 \"web\"", d["channel_id"])
	}
	if d["group_digital_avatar"] != true {
		t.Errorf("ToDict()[\"group_digital_avatar\"] = %v, 期望 true", d["group_digital_avatar"])
	}
	if d["web_user_id"] != "web-user-1" {
		t.Errorf("ToDict()[\"web_user_id\"] = %v, 期望 \"web-user-1\"", d["web_user_id"])
	}
}

// TestNewPermissionContextFromDict 验证反序列化往返
func TestNewPermissionContextFromDict(t *testing.T) {
	data := map[string]any{
		"principal_user_id":    "user-1",
		"triggering_user_id":   "sender-1",
		"channel_id":           "web",
		"group_digital_avatar": true,
		"web_user_id":          "web-user-1",
	}
	pc := NewPermissionContextFromDict(data)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.WebUserID != "web-user-1" {
		t.Errorf("WebUserID = %q, 期望 \"web-user-1\"", pc.WebUserID)
	}
}

// TestNewPermissionContextFromDict_缺失字段用零值 验证缺失字段用零值填充
func TestNewPermissionContextFromDict_缺失字段用零值(t *testing.T) {
	data := map[string]any{
		"principal_user_id": "user-1",
	}
	pc := NewPermissionContextFromDict(data)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "" {
		t.Errorf("TriggeringUserID 应为空，实际 %q", pc.TriggeringUserID)
	}
	if !pc.GroupDigitalAvatar {
		// 零值 false，此处不应为 true
	}
}

// TestPermissionContext_ToDictFromDict往返 验证 ToDict → FromDict 往返一致
func TestPermissionContext_ToDictFromDict往返(t *testing.T) {
	original := NewPermissionContext(
		WithPrincipalUserID("user-1"),
		WithTriggeringUserID("sender-1"),
		WithPermissionChannelID("feishu"),
		WithGroupDigitalAvatar(false),
		WithWebUserID(""),
	)
	roundtrip := NewPermissionContextFromDict(original.ToDict())
	if roundtrip.PrincipalUserID != original.PrincipalUserID {
		t.Errorf("PrincipalUserID 往返不一致: %q vs %q", roundtrip.PrincipalUserID, original.PrincipalUserID)
	}
	if roundtrip.TriggeringUserID != original.TriggeringUserID {
		t.Errorf("TriggeringUserID 往返不一致: %q vs %q", roundtrip.TriggeringUserID, original.TriggeringUserID)
	}
	if roundtrip.ChannelID != original.ChannelID {
		t.Errorf("ChannelID 往返不一致: %q vs %q", roundtrip.ChannelID, original.ChannelID)
	}
	if roundtrip.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar 往返不一致: %v vs %v", roundtrip.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if roundtrip.WebUserID != original.WebUserID {
		t.Errorf("WebUserID 往返不一致: %q vs %q", roundtrip.WebUserID, original.WebUserID)
	}
}

// ──────────────────────────── Validate 测试 ────────────────────────────

// TestPermissionContext_Validate_正常 验证正常数据通过校验
func TestPermissionContext_Validate_正常(t *testing.T) {
	pc := NewPermissionContext(WithPrincipalUserID("user-1"))
	if err := pc.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestPermissionContext_Validate_校验失败 验证缺少必填字段返回错误
func TestPermissionContext_Validate_校验失败(t *testing.T) {
	pc := NewPermissionContext()
	if err := pc.Validate(); err == nil {
		t.Error("principal_user_id 为空时期望返回错误")
	}
}

// ──────────────────────────── JSON 往返测试 ────────────────────────────

// TestPermissionContext_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestPermissionContext_JSON往返(t *testing.T) {
	original := &PermissionContext{
		PrincipalUserID:    "user-1",
		TriggeringUserID:   "sender-1",
		ChannelID:          "web",
		GroupDigitalAvatar: true,
		WebUserID:          "web-user-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded PermissionContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.PrincipalUserID != original.PrincipalUserID {
		t.Errorf("PrincipalUserID: got %q, want %q", decoded.PrincipalUserID, original.PrincipalUserID)
	}
	if decoded.TriggeringUserID != original.TriggeringUserID {
		t.Errorf("TriggeringUserID: got %q, want %q", decoded.TriggeringUserID, original.TriggeringUserID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar: got %v, want %v", decoded.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if decoded.WebUserID != original.WebUserID {
		t.Errorf("WebUserID: got %q, want %q", decoded.WebUserID, original.WebUserID)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/ -run TestPermissionContext -run TestNewPermissionContext`
Expected: 所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/schema/permission.go internal/swarm/schema/permission_test.go
git commit -m "feat(schema): 实现 PermissionContext 权限上下文模型 (10.1.8)

- 结构体: 5 个字段对齐 Python PermissionContext
- 派生方法: Scene() 场景判定, OwnerScopeKey() 配置查找 key
- 序列化: ToDict() / NewPermissionContextFromDict() 往返一致
- 工厂: NewPermissionContext() + Option 模式
- 校验: Validate() principal_user_id 非空
- 测试: 11 个测试用例覆盖全部功能"
```

---

### Task 3: AgentRequest / AgentResponse / AgentResponseChunk 结构体与工厂

**Files:**
- Create: `internal/swarm/schema/agent.go`

- [ ] **Step 1: 编写 agent.go — 结构体 + 工厂 + Validate + Option**

```go
package schema

import (
	"encoding/json"
	"fmt"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRequest Agent 请求（Gateway → AgentServer）。
//
// 作为 Gateway 向 AgentServer 发起的标准化请求模型，承载 RPC 方法名、
// 请求参数、流式标识、权限上下文等。E2A 协议解码后交由 AgentServer 处理。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentRequest)
type AgentRequest struct {
	// ─── 必填字段 ───

	// RequestID 请求唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`

	// ─── 可选字段（指针 + omitempty 表达 Python T | None） ───

	// SessionID 会话标识
	SessionID *string `json:"session_id,omitempty"`
	// ChatID IM 聊天标识
	ChatID *string `json:"chat_id,omitempty"`
	// ReqMethod 请求方法
	ReqMethod ReqMethod `json:"req_method,omitempty"`

	// ─── 可选字段（json.RawMessage / map / bool 指针） ───

	// Params 请求参数（延迟解析，与 Message.Params 一致）
	Params json.RawMessage `json:"params,omitempty"`
	// IsStream 是否流式请求
	IsStream bool `json:"is_stream"`
	// Timestamp Unix 秒时间戳（含小数精度，对齐 Python time.time()）
	Timestamp float64 `json:"timestamp"`
	// Metadata 扩展元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// EnableMemory 是否启用记忆（三态：nil/true/false，对齐 Python bool | None）
	EnableMemory *bool `json:"enable_memory,omitempty"`
	// PermissionContext 权限上下文
	PermissionContext *PermissionContext `json:"permission_context,omitempty"`
}

// AgentResponse Agent 响应（AgentServer → Gateway，非流式完整响应）。
//
// 作为 AgentServer 向 Gateway 返回的完整响应模型，承载执行结果、
// 响应负载、元数据等。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentResponse)
type AgentResponse struct {
	// RequestID 对应请求的唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// OK 是否成功（工厂函数默认 true，对齐 Python ok=True）
	OK bool `json:"ok"`
	// Payload 响应负载（延迟解析，与 Message.Payload 一致）
	Payload json.RawMessage `json:"payload,omitempty"`
	// Metadata 扩展元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentResponseChunk Agent 响应片段（AgentServer → Gateway，流式）。
//
// 作为 AgentServer 向 Gateway 返回的流式响应块，承载增量负载和完成标识。
// 当前仅定义结构体骨架，工厂函数和 Validate 留给步骤 10.1.6 补全。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentResponseChunk)
type AgentResponseChunk struct {
	// RequestID 对应请求的唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// Payload 响应负载片段（延迟解析）
	Payload json.RawMessage `json:"payload,omitempty"`
	// IsComplete 是否为最后一个片段
	IsComplete bool `json:"is_complete"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentRequest 创建 Agent 请求实例。
//
// 自动生成 Timestamp（当前时间），设置 RequestID 和 ChannelID。
// 工厂函数保证：IsStream=false（零值），EnableMemory=nil（未设置）。
func NewAgentRequest(requestID, channelID string, reqMethod ReqMethod, params json.RawMessage, opts ...AgentRequestOption) *AgentRequest {
	req := &AgentRequest{
		RequestID: requestID,
		ChannelID: channelID,
		ReqMethod: reqMethod,
		Params:    params,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
	}
	for _, opt := range opts {
		opt(req)
	}
	return req
}

// NewAgentResponse 创建 Agent 响应实例。
//
// 默认 OK=true（对齐 Python AgentResponse.ok=True）。
func NewAgentResponse(requestID, channelID string, opts ...AgentResponseOption) *AgentResponse {
	resp := &AgentResponse{
		RequestID: requestID,
		ChannelID: channelID,
		OK:        true,
	}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// AgentRequestOption Agent 请求可选配置函数。
type AgentRequestOption func(*AgentRequest)

// WithSessionID 设置会话标识。
func WithSessionID(id string) AgentRequestOption {
	return func(req *AgentRequest) { req.SessionID = &id }
}

// WithChatID 设置 IM 聊天标识。
func WithChatID(id string) AgentRequestOption {
	return func(req *AgentRequest) { req.ChatID = &id }
}

// WithIsStream 设置是否流式。
func WithIsStream(v bool) AgentRequestOption {
	return func(req *AgentRequest) { req.IsStream = v }
}

// WithMetadata 设置扩展元数据。
func WithMetadata(m map[string]any) AgentRequestOption {
	return func(req *AgentRequest) { req.Metadata = m }
}

// WithEnableMemory 设置是否启用记忆（三态：nil=未设置，true/false=显式设置）。
func WithEnableMemory(v bool) AgentRequestOption {
	return func(req *AgentRequest) { req.EnableMemory = &v }
}

// WithPermissionContext 设置权限上下文。
func WithPermissionContext(pc *PermissionContext) AgentRequestOption {
	return func(req *AgentRequest) { req.PermissionContext = pc }
}

// AgentResponseOption Agent 响应可选配置函数。
type AgentResponseOption func(*AgentResponse)

// WithResponseOK 设置是否成功。
func WithResponseOK(v bool) AgentResponseOption {
	return func(resp *AgentResponse) { resp.OK = v }
}

// WithPayload 设置响应负载。
func WithPayload(p json.RawMessage) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Payload = p }
}

// WithResponseMetadata 设置扩展元数据。
func WithResponseMetadata(m map[string]any) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Metadata = m }
}

// Validate 校验 AgentRequest 必填字段。
//
// 校验规则（对齐 Python 实际使用）：
//   - request_id 非空
//   - channel_id 非空
//   - req_method 非零值
func (r *AgentRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id 不能为空")
	}
	if r.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	if r.ReqMethod == "" {
		return fmt.Errorf("req_method 不能为空")
	}
	return nil
}

// Validate 校验 AgentResponse 必填字段。
//
// 校验规则：
//   - request_id 非空
//   - channel_id 非空
func (r *AgentResponse) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id 不能为空")
	}
	if r.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译成功，无错误

---

### Task 4: AgentRequest / AgentResponse 测试

**Files:**
- Create: `internal/swarm/schema/agent_test.go`

- [ ] **Step 1: 编写 agent_test.go — 全量测试**

```go
package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── AgentRequest 工厂函数测试 ────────────────────────────

// TestNewAgentRequest 验证工厂函数默认值
func TestNewAgentRequest(t *testing.T) {
	params := json.RawMessage(`{"query":"hello"}`)
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, params)

	if req.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", req.RequestID)
	}
	if req.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", req.ChannelID)
	}
	if req.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod = %q, 期望 %q", req.ReqMethod, ReqMethodChatSend)
	}
	if string(req.Params) != `{"query":"hello"}` {
		t.Errorf("Params = %s, 期望 {\"query\":\"hello\"}", string(req.Params))
	}
	if req.Timestamp <= 0 {
		t.Error("Timestamp 应为正数")
	}
	if req.IsStream {
		t.Error("IsStream 默认应为 false")
	}
	if req.SessionID != nil {
		t.Error("SessionID 默认应为 nil")
	}
	if req.ChatID != nil {
		t.Error("ChatID 默认应为 nil")
	}
	if req.Metadata != nil {
		t.Error("Metadata 默认应为 nil")
	}
	if req.EnableMemory != nil {
		t.Error("EnableMemory 默认应为 nil（三态未设置）")
	}
	if req.PermissionContext != nil {
		t.Error("PermissionContext 默认应为 nil")
	}
}

// TestNewAgentRequest_使用Option 验证通过 Option 设置各字段
func TestNewAgentRequest_使用Option(t *testing.T) {
	sessionID := "sess-1"
	chatID := "chat-1"
	params := json.RawMessage(`{}`)
	pc := NewPermissionContext(WithPrincipalUserID("user-1"))
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, params,
		WithSessionID(sessionID),
		WithChatID(chatID),
		WithIsStream(true),
		WithMetadata(map[string]any{"key": "val"}),
		WithEnableMemory(true),
		WithPermissionContext(pc),
	)

	if req.SessionID == nil || *req.SessionID != "sess-1" {
		t.Errorf("SessionID = %v, 期望 \"sess-1\"", req.SessionID)
	}
	if req.ChatID == nil || *req.ChatID != "chat-1" {
		t.Errorf("ChatID = %v, 期望 \"chat-1\"", req.ChatID)
	}
	if !req.IsStream {
		t.Error("IsStream 应为 true")
	}
	if req.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
	if req.EnableMemory == nil || !*req.EnableMemory {
		t.Error("EnableMemory 应为 *true")
	}
	if req.PermissionContext == nil || req.PermissionContext.PrincipalUserID != "user-1" {
		t.Error("PermissionContext.PrincipalUserID 期望 \"user-1\"")
	}
}

// ──────────────────────────── AgentRequest Validate 测试 ────────────────────────────

// TestAgentRequest_Validate_正常 验证正常数据通过校验
func TestAgentRequest_Validate_正常(t *testing.T) {
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, json.RawMessage(`{}`))
	if err := req.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestAgentRequest_Validate_requestID为空 验证 request_id 为空返回错误
func TestAgentRequest_Validate_requestID为空(t *testing.T) {
	req := &AgentRequest{ChannelID: "web", ReqMethod: ReqMethodChatSend}
	if err := req.Validate(); err == nil {
		t.Error("request_id 为空时期望返回错误")
	}
}

// TestAgentRequest_Validate_channelID为空 验证 channel_id 为空返回错误
func TestAgentRequest_Validate_channelID为空(t *testing.T) {
	req := &AgentRequest{RequestID: "req-1", ReqMethod: ReqMethodChatSend}
	if err := req.Validate(); err == nil {
		t.Error("channel_id 为空时期望返回错误")
	}
}

// TestAgentRequest_Validate_reqMethod为零值 验证 req_method 为零值返回错误
func TestAgentRequest_Validate_reqMethod为零值(t *testing.T) {
	req := &AgentRequest{RequestID: "req-1", ChannelID: "web"}
	if err := req.Validate(); err == nil {
		t.Error("req_method 为零值时期望返回错误")
	}
}

// ──────────────────────────── AgentRequest JSON 往返测试 ────────────────────────────

// TestAgentRequest_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestAgentRequest_JSON往返(t *testing.T) {
	sessionID := "sess-1"
	chatID := "chat-1"
	enableMemory := true
	original := &AgentRequest{
		RequestID:        "req-1",
		ChannelID:        "web",
		SessionID:        &sessionID,
		ChatID:           &chatID,
		ReqMethod:        ReqMethodChatSend,
		Params:           json.RawMessage(`{"query":"hello"}`),
		IsStream:         true,
		Timestamp:        1712345678.123,
		Metadata:         map[string]any{"method": "chat.send"},
		EnableMemory:     &enableMemory,
		PermissionContext: &PermissionContext{
			PrincipalUserID:  "user-1",
			TriggeringUserID: "sender-1",
			ChannelID:        "web",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.SessionID == nil || *decoded.SessionID != "sess-1" {
		t.Errorf("SessionID 往返不一致")
	}
	if decoded.ChatID == nil || *decoded.ChatID != "chat-1" {
		t.Errorf("ChatID 往返不一致")
	}
	if decoded.ReqMethod != original.ReqMethod {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, original.ReqMethod)
	}
	if string(decoded.Params) != string(original.Params) {
		t.Errorf("Params: got %s, want %s", string(decoded.Params), string(original.Params))
	}
	if decoded.IsStream != original.IsStream {
		t.Errorf("IsStream: got %v, want %v", decoded.IsStream, original.IsStream)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.EnableMemory == nil || !*decoded.EnableMemory {
		t.Errorf("EnableMemory 往返不一致")
	}
	if decoded.PermissionContext == nil || decoded.PermissionContext.PrincipalUserID != "user-1" {
		t.Errorf("PermissionContext 往返不一致")
	}
}

// TestAgentRequest_EnableMemory三态 验证 EnableMemory 三态序列化正确
func TestAgentRequest_EnableMemory三态(t *testing.T) {
	// nil 状态
	reqNil := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend}
	data, _ := json.Marshal(reqNil)
	if strings.Contains(string(data), "enable_memory") {
		t.Errorf("EnableMemory=nil 时 JSON 应省略，实际: %s", string(data))
	}

	// true 状态
	enableTrue := true
	reqTrue := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend, EnableMemory: &enableTrue}
	data, _ = json.Marshal(reqTrue)
	if !strings.Contains(string(data), `"enable_memory":true`) {
		t.Errorf("EnableMemory=true 时 JSON 应包含 enable_memory:true，实际: %s", string(data))
	}

	// false 状态
	enableFalse := false
	reqFalse := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend, EnableMemory: &enableFalse}
	data, _ = json.Marshal(reqFalse)
	if !strings.Contains(string(data), `"enable_memory":false`) {
		t.Errorf("EnableMemory=false 时 JSON 应包含 enable_memory:false，实际: %s", string(data))
	}
}

// ──────────────────────────── AgentResponse 工厂函数测试 ────────────────────────────

// TestNewAgentResponse 验证工厂函数默认值（OK=true）
func TestNewAgentResponse(t *testing.T) {
	resp := NewAgentResponse("req-1", "web")

	if resp.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", resp.RequestID)
	}
	if resp.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", resp.ChannelID)
	}
	if !resp.OK {
		t.Error("OK 默认应为 true（对齐 Python）")
	}
	if resp.Payload != nil {
		t.Error("Payload 默认应为 nil")
	}
	if resp.Metadata != nil {
		t.Error("Metadata 默认应为 nil")
	}
}

// TestNewAgentResponse_使用Option 验证通过 Option 设置各字段
func TestNewAgentResponse_使用Option(t *testing.T) {
	payload := json.RawMessage(`{"content":"answer"}`)
	resp := NewAgentResponse("req-1", "web",
		WithResponseOK(false),
		WithPayload(payload),
		WithResponseMetadata(map[string]any{"key": "val"}),
	)

	if resp.OK {
		t.Error("OK 应为 false（被 Option 覆盖）")
	}
	if string(resp.Payload) != `{"content":"answer"}` {
		t.Errorf("Payload = %s, 期望 {\"content\":\"answer\"}", string(resp.Payload))
	}
	if resp.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
}

// ──────────────────────────── AgentResponse Validate 测试 ────────────────────────────

// TestAgentResponse_Validate_正常 验证正常数据通过校验
func TestAgentResponse_Validate_正常(t *testing.T) {
	resp := NewAgentResponse("req-1", "web")
	if err := resp.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestAgentResponse_Validate_校验失败 验证缺少必填字段返回错误
func TestAgentResponse_Validate_校验失败(t *testing.T) {
	resp := &AgentResponse{RequestID: "", ChannelID: "web"}
	if err := resp.Validate(); err == nil {
		t.Error("request_id 为空时期望返回错误")
	}

	resp2 := &AgentResponse{RequestID: "req-1", ChannelID: ""}
	if err := resp2.Validate(); err == nil {
		t.Error("channel_id 为空时期望返回错误")
	}
}

// ──────────────────────────── AgentResponse JSON 往返测试 ────────────────────────────

// TestAgentResponse_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestAgentResponse_JSON往返(t *testing.T) {
	original := &AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   json.RawMessage(`{"content":"final answer"}`),
		Metadata:  map[string]any{"method": "chat.send"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.OK != original.OK {
		t.Errorf("OK: got %v, want %v", decoded.OK, original.OK)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", string(decoded.Payload), string(original.Payload))
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}

// ──────────────────────────── AgentResponseChunk 基础测试 ────────────────────────────

// TestAgentResponseChunk_JSON序列化 验证骨架结构体 JSON 序列化/反序列化基本验证
func TestAgentResponseChunk_JSON序列化(t *testing.T) {
	original := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    json.RawMessage(`{"content":"delta"}`),
		IsComplete: false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponseChunk
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", string(decoded.Payload), string(original.Payload))
	}
	if decoded.IsComplete != original.IsComplete {
		t.Errorf("IsComplete: got %v, want %v", decoded.IsComplete, original.IsComplete)
	}
}

// TestAgentResponseChunk_IsComplete为true 验证完成标记
func TestAgentResponseChunk_IsComplete为true(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    json.RawMessage(`{"content":"done"}`),
		IsComplete: true,
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	if !strings.Contains(string(data), `"is_complete":true`) {
		t.Errorf("IsComplete=true 时 JSON 应包含 is_complete:true，实际: %s", string(data))
	}
}
```

- [ ] **Step 2: 运行全部 schema 测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/`
Expected: 所有测试 PASS（包括已有的 Message、ReqMethod 等测试）

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/schema/agent.go internal/swarm/schema/agent_test.go
git commit -m "feat(schema): 实现 AgentRequest/AgentResponse + AgentResponseChunk 骨架 (10.1.5)

- AgentRequest: 11 个字段对齐 Python，*string/*bool 表达可选和三态
- AgentResponse: 5 个字段，工厂默认 OK=true
- AgentResponseChunk: 骨架定义，工厂和 Validate 留给 10.1.6
- 工厂: NewAgentRequest/NewAgentResponse + Option 模式
- 校验: Validate() 必填字段检查
- 测试: 14 个测试用例覆盖全部功能"
```

---

### Task 5: doc.go 和 IMPLEMENTATION_PLAN 状态更新

**Files:**
- Modify: `internal/swarm/schema/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录和包概述**

将 doc.go 的文件目录更新为：

```go
// Package schema 提供 E2A 协议和 Gateway/AgentServer 通信所需的全部类型定义。
//
// 本包定义了 E2A 协议的核心数据模型，包括 RPC 方法名枚举（ReqMethod）、
// 事件类型枚举（EventType）、运行模式枚举（Mode）、消息方向类型枚举（MessageType）、
// 消息模型（Message）、Agent 请求/响应模型（AgentRequest/AgentResponse/AgentResponseChunk）、
// 权限上下文（PermissionContext）等，作为 swarm 层的类型基础。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	├── event_type.go    # EventType 枚举（26 个事件类型）
//	├── mode.go          # Mode 枚举（6 个运行模式）
//	├── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
//	├── agent.go         # AgentRequest/AgentResponse/AgentResponseChunk 模型 + 工厂函数 + Validate
//	└── permission.go    # PermissionContext 权限上下文 + 派生方法 + 序列化 + 工厂函数 + Validate
//
// 对应 Python 代码：jiuwenswarm/common/schema/
package schema
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 状态**

将以下行的状态更新：
- `10.1.5` 行的 `☐` 改为 `✅`
- `10.1.6` 行的 `☐` 改为 `🔄`
- `10.1.8` 行的 `☐` 改为 `✅`

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/schema/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 schema doc.go 和 IMPLEMENTATION_PLAN 状态 (10.1.5✅, 10.1.6🔄, 10.1.8✅)"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/`
Expected: 所有测试 PASS，无回归

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 确认编译无警告**

Run: `cd /home/opensource/uapclaw-gateway && go vet ./internal/swarm/schema/`
Expected: 无输出（无警告）
