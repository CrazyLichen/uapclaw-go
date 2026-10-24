# 11.10 Cron 调度服务实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 11.10 Cron 调度服务全量功能（gateway/cron 包 5 文件 + agents 桥接包 2 文件 + 9 处回填接线），对齐 Python `jiuwenswarm/gateway/cron/`（6 文件 2035 行）与 `jiuwenswarm/agents/harness/common/tools/cron/`（3 文件 865 行），并落地方案 B 委托降级单调度器（修正 Python 单进程双跑缺陷）。

**Architecture:** 三层结构——① gateway/cron 包（models/cron_expr/store/scheduler/controller），scheduler 用事件堆（wake/push/push_update）+ nowFn 假时钟 + 进程级注册表（SetProcessScheduler）；② agents/harness/common/tools/cron 桥接包（CronTools 本地双写 + cronRuntimeBackend 实现 agentcore CronToolBackend 11 方法接口）；③ 接线回填（deep_adapter ctx values 绑定/TaskScheduler cancel、outbound handleCronPushPayload、web_handlers 8 端点、team_manager 注册、app_gateway 装配）。MessagePublisher 接口破环（message_handler 隐式实现）。

**Tech Stack:** Go 1.x, github.com/reugn/go-quartz v0.15.2（已在 module cache，需 go.mod 添加）, container/heap, sync.Mutex/RWMutex, encoding/json, os.Rename 原子写, context values

**设计文档:** `docs/superpowers/specs/2026-10-24-cron-scheduler-design.md`（四决策已锁：B=go-quartz、A=全量对齐 _push_to_targets、A=全量实现含 Agent 侧桥接、B=委托降级单调度器）

---

## 文件结构

| 文件 | 职责 | 变更类型 |
|------|------|---------|
| `go.mod` | 添加 go-quartz v0.15.2 依赖 | 修改 |
| `internal/common/utils/path/paths.go` | 补 CronJobsPath() | 修改 |
| `internal/swarm/gateway/cron/models.go` | CronJob/CronTarget/渠道枚举/校验归一化 | 新建 |
| `internal/swarm/gateway/cron/cron_expr.go` | 表达式归一化 + dow 变换 + NextFireTime 包装 | 新建 |
| `internal/swarm/gateway/cron/store.go` | JSON 持久化 + upsert + 原子写 | 新建 |
| `internal/swarm/gateway/cron/scheduler.go` | 事件堆调度器 + 注册表 + MessagePublisher 接口 | 新建 |
| `internal/swarm/gateway/cron/controller.go` | API 门面 + preview/run_now | 新建 |
| `internal/swarm/gateway/cron/doc.go` | 包文档 | 新建 |
| `internal/swarm/agents/harness/common/tools/cron/cron_tools.go` | CronTools 本地双写 + E2A 转发 + ensureScheduler 方案B | 新建 |
| `internal/swarm/agents/harness/common/tools/cron/cron_runtime.go` | CronRuntimeBridge + cronToolsBackend 11 方法 + _extractLegacyParams | 新建 |
| `internal/swarm/agents/harness/common/tools/cron/doc.go` | 包文档 | 新建 |
| `internal/swarm/gateway/message_handler/outbound.go` | handleCronPushPayload 真实现 | 修改 |
| `internal/swarm/gateway/message_handler/message_handler.go` | SetCronController + cronController 字段 | 修改 |
| `internal/swarm/gateway/channel_manager/web/web_handlers.go` | 8 个 cron.job.* 桩 → 真实 handler | 修改 |
| `internal/swarm/server/adapter/deep_adapter.go` | cronRuntime 类型升级 + 步骤9绑定×2 + cancelSchedulerRunningTasks×2 | 修改 |
| `internal/swarm/agents/harness/team/team_manager_spec.go` | RegisterMemberRuntimeTools cron 部分回填 | 修改 |
| `internal/swarm/gateway/app_gateway.go` | 装配 store/scheduler/controller + Start/Stop | 修改 |
| `IMPLEMENTATION_PLAN.md` | 11.10 ✅ + 10.6.24 备注 | 修改 |

---

## Task 1: go.mod 添加 go-quartz + paths.go CronJobsPath

**Files:**
- Modify: `go.mod`
- Modify: `internal/common/utils/path/paths.go`
- Modify: `internal/common/utils/path/paths_test.go`（如有）

- [ ] **Step 1: 添加 go-quartz 依赖**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uapclaw-gateway && go get github.com/reugn/go-quartz@v0.15.2
```

注意：go-quartz v0.15.2 的模块路径是 `github.com/reugn/go-quartz`（import 路径带 `/quartz` 子目录：`github.com/reugn/go-quartz/quartz`）。已在 module cache（/root/go/pkg/mod/github.com/reugn/go-quartz@v0.15.2）。

- [ ] **Step 2: 在 paths.go 添加 CronJobsPath**

在 `DeepAgentUserMDPath` 之后、`AgentTeamsHomeDir` 之前添加（保持导出函数区块内）：

```go
// CronJobsPath 返回 cron 任务文件路径：WorkspaceDir()/agent/home/cron_jobs.json。
// Python: get_cron_jobs_path()（gateway 与 agentserver 共享同一文件）
func CronJobsPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "home", "cron_jobs.json")
}
```

- [ ] **Step 3: 在 paths_test.go 添加测试**

```go
// TestCronJobsPath 验证 cron 任务文件路径
func TestCronJobsPath(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", "/tmp/test-ws")
	ResetCache()
	defer ResetCache()
	got := CronJobsPath()
	want := filepath.Join("/tmp/test-ws", "agent", "home", "cron_jobs.json")
	if got != want {
		t.Errorf("CronJobsPath() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 4: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/common/utils/path/ && go test ./internal/common/utils/path/
```

提交：`feat(cron): add go-quartz dependency and CronJobsPath`

---

## Task 2: gateway/cron/models.go

**Files:**
- Create: `internal/swarm/gateway/cron/models.go`
- Create: `internal/swarm/gateway/cron/models_test.go`

- [ ] **Step 1: 创建包目录并写 models.go**

完整文件内容（对齐 models.py 250 行）：

```go
package cron

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronJob cron 任务持久化模型。
// 对齐 Python: CronJob dataclass (models.py L88-228)。
// JSON 字段名与 Python to_dict 输出一致（id/name/enabled/expired/cron_expr/timezone/wake_offset_seconds/description/targets/created_at/updated_at，可选 session_id/chat_type/mode/delete_after_run）。
type CronJob struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Name 任务名
	Name string `json:"name"`
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Expired 已过期（one-shot 执行后无下一次）
	Expired bool `json:"expired"`
	// CronExpr cron 表达式
	CronExpr string `json:"cron_expr"`
	// Timezone 时区（IANA）
	Timezone string `json:"timezone"`
	// WakeOffsetSeconds 提前唤醒秒数（默认 300）
	WakeOffsetSeconds int `json:"wake_offset_seconds"`
	// Description 任务内容（运行时发给 assistant 的 payload 文本）
	Description string `json:"description"`
	// Targets 推送频道 ID（如 "web"）；JSON 字段名沿用 targets 兼容旧数据
	Targets string `json:"targets"`
	// SessionID SessionMap 形态（feishu::chat_id::bot_id::...），仅 feishu_enterprise 投递用
	SessionID string `json:"session_id,omitempty"`
	// CreatedAt 创建时间（Unix 秒，nil 为 0）
	CreatedAt float64 `json:"created_at"`
	// UpdatedAt 更新时间（Unix 秒，nil 为 0）
	UpdatedAt float64 `json:"updated_at"`
	// ChatType 群聊("group")或私聊("p2p")标记，推送时决定 IMOutboundPipeline
	ChatType string `json:"chat_type,omitempty"`
	// Mode 执行模式（默认 "agent"）
	Mode string `json:"mode,omitempty"`
	// DeleteAfterRun 执行一次后自动删除（提醒类任务）
	DeleteAfterRun bool `json:"delete_after_run,omitempty"`
}

// ToDict 转为 map（E2A payload 兼容）。
// 对齐 Python: CronJob.to_dict (models.py L115-137)。可选字段空值时不写入。
func (j *CronJob) ToDict() map[string]any {
	d := map[string]any{
		"id":                  j.ID,
		"name":                j.Name,
		"enabled":             j.Enabled,
		"expired":             j.Expired,
		"cron_expr":           j.CronExpr,
		"timezone":            j.Timezone,
		"wake_offset_seconds": j.WakeOffsetSeconds,
		"description":         j.Description,
		"targets":             j.Targets,
		"created_at":          j.CreatedAt,
		"updated_at":          j.UpdatedAt,
	}
	if j.SessionID != "" {
		d["session_id"] = j.SessionID
	}
	if j.ChatType != "" {
		d["chat_type"] = j.ChatType
	}
	if j.Mode != "" {
		d["mode"] = j.Mode
	}
	if j.DeleteAfterRun {
		d["delete_after_run"] = j.DeleteAfterRun
	}
	return d
}

// FromCronJobDict 从 map 反序列化 CronJob（严格校验）。
// 对齐 Python: CronJob.from_dict (models.py L139-228)。
// 错误返回 error（对齐 Python raise ValueError）。
func FromCronJobDict(data map[string]any) (*CronJob, error) {
	if data == nil {
		return nil, fmt.Errorf("data is nil")
	}
	jobID := strings.TrimSpace(strAny(data["id"]))
	name := strings.TrimSpace(strAny(data["name"]))
	cronExpr := strings.TrimSpace(strAny(data["cron_expr"]))
	timezone := strings.TrimSpace(strAny(data["timezone"]))
	if jobID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}
	if timezone == "" {
		return nil, fmt.Errorf("timezone is required")
	}

	enabled := boolAny(data["enabled"], false)
	expired := boolAny(data["expired"], false)

	// wake_offset_seconds：int 转换失败报错，负数钳制为 0
	wos := 300
	if raw, ok := data["wake_offset_seconds"]; ok && raw != nil {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		wos = n
	}
	if wos < 0 {
		wos = 0
	}

	description := strings.TrimSpace(strAny(data["description"]))
	if description == "" {
		return nil, fmt.Errorf("description is required")
	}

	// targets 新格式为字符串；旧格式为 []map（取第一个 channel_id 兼容）
	targetsStr := ""
	if raw, ok := data["targets"]; ok {
		if s, isStr := raw.(string); isStr {
			targetsStr = strings.TrimSpace(s)
		} else if list, isList := raw.([]any); isList {
			for _, item := range list {
				if m, isMap := item.(map[string]any); isMap {
					ch := strings.TrimSpace(strAny(m["channel_id"]))
					if ch != "" {
						targetsStr = ch
						break
					}
				}
			}
		}
	}
	if targetsStr == "" {
		return nil, fmt.Errorf("targets is required")
	}

	// 校验表达式（含时区）
	if err := ValidateCronExpression(cronExpr, timezone); err != nil {
		return nil, err
	}

	targetsStr = NormalizeTargetChannelID(targetsStr, TargetChannelWeb)

	return &CronJob{
		ID:                jobID,
		Name:              name,
		Enabled:           enabled,
		Expired:           expired,
		CronExpr:          cronExpr,
		Timezone:          timezone,
		WakeOffsetSeconds: wos,
		Description:       description,
		Targets:           targetsStr,
		SessionID:         strings.TrimSpace(strAny(data["session_id"])),
		CreatedAt:         floatAny(data["created_at"], 0),
		UpdatedAt:         floatAny(data["updated_at"], 0),
		ChatType:          strings.TrimSpace(strAny(data["chat_type"])),
		Mode:              modeAny(data["mode"]),
		DeleteAfterRun:    boolAny(data["delete_after_run"], false),
	}, nil
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetChannelWeb Web 频道
	TargetChannelWeb = "web"
	// TargetChannelTUI TUI 频道
	TargetChannelTUI = "tui"
	// TargetChannelFeishu 飞书频道
	TargetChannelFeishu = "feishu"
	// TargetChannelFeishuEnterprise 企业飞书前缀（完整形态 feishu_enterprise:<app_id>）
	TargetChannelFeishuEnterprise = "feishu_enterprise"
	// TargetChannelWeCom 企业微信频道
	TargetChannelWeCom = "wecom"
	// TargetChannelDingTalk 钉钉频道
	TargetChannelDingTalk = "dingtalk"
	// TargetChannelWeChat 微信频道
	TargetChannelWeChat = "wechat"
	// TargetChannelWhatsApp WhatsApp 频道
	TargetChannelWhatsApp = "whatsapp"
	// TargetChannelXiaoyi 小艺频道
	TargetChannelXiaoyi = "xiaoyi"
)

// validTargetChannels CronTargetChannel 枚举值集合。
// 对齐 Python: CronTargetChannel(str, Enum) 9 值。
var validTargetChannels = map[string]bool{
	TargetChannelWeb:              true,
	TargetChannelTUI:              true,
	TargetChannelFeishu:           true,
	TargetChannelWeCom:            true,
	TargetChannelDingTalk:         true,
	TargetChannelWeChat:           true,
	TargetChannelWhatsApp:         true,
	TargetChannelXiaoyi:           true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidTargetChannelID 校验目标频道 ID 是否合法。
// 对齐 Python: is_valid_target_channel_id (models.py L31-41)。
// feishu_enterprise:<app_id> 形态校验 app_id 非空。
func IsValidTargetChannelID(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "feishu_enterprise:") {
		return feishuEnterpriseAppID(s) != ""
	}
	return validTargetChannels[strings.ToLower(s)]
}

// NormalizeTargetChannelID 归一化目标频道 ID，非法回退 default。
// 对齐 Python: normalize_target_channel_id (models.py L44-57)。
func NormalizeTargetChannelID(raw string, defaultChannel string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return defaultChannel
	}
	if strings.HasPrefix(s, "feishu_enterprise:") {
		appID := feishuEnterpriseAppID(s)
		if appID != "" {
			return TargetChannelFeishuEnterprise + ":" + appID
		}
		return defaultChannel
	}
	if validTargetChannels[strings.ToLower(s)] {
		return strings.ToLower(s)
	}
	return defaultChannel
}

// ──────────────────────────── 靶向辅助 ────────────────────────────

// feishuEnterpriseAppID 提取 feishu_enterprise:<app_id> 的 app_id。
// 对齐 Python: _feishu_enterprise_app_id (models.py L23-28)。忽略 :chat: 等后续后缀。
func feishuEnterpriseAppID(s string) string {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || strings.ToLower(strings.TrimSpace(parts[0])) != "feishu_enterprise" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// strAny 安全取字符串（nil/非字符串返回空串）。
func strAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolAny 安全取布尔（nil 用默认值）。
func boolAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// intAny 安全取整数（支持 float64/int/json.Number）。
func intAny(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case json.Number:
		return n.Int64()
	}
	return 0, fmt.Errorf("not an int")
}

// floatAny 安全取浮点（nil 用默认值）。
func floatAny(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return def
}

// modeAny 归一化 mode 字段（空/非字符串默认 "agent"）。
// 对齐 Python: mode 处理 (models.py L203-208)。
func modeAny(v any) string {
	s := strAny(v)
	if s == "" {
		return "agent"
	}
	return strings.ToLower(strings.TrimSpace(s))
}
```

注意：`intAny` 中用到 `json.Number` 需 import `encoding/json`。声明顺序遵循项目规范（结构体→枚举→常量→全局变量→导出函数→非导出函数）。

- [ ] **Step 2: 写 models_test.go**

覆盖点：ToDict 可选字段省略、FromCronJobDict 必填校验（id/name/cron_expr/timezone/description/targets 各缺一报错）、wake_offset_seconds 钳制、旧 targets list 兼容、IsValidTargetChannelID（9 枚举+feishu_enterprise:app_id+非法值）、NormalizeTargetChannelID（大写归一、非法回退 default）、mode 默认 agent。

```go
package cron

import "testing"

// TestCronJobToDict 可选字段空值时省略
func TestCronJobToDict(t *testing.T) {
	j := &CronJob{ID: "j1", Name: "n", Enabled: true, CronExpr: "0 9 * * *",
		Timezone: "Asia/Shanghai", WakeOffsetSeconds: 300, Description: "d", Targets: "web"}
	d := j.ToDict()
	if _, exists := d["session_id"]; exists {
		t.Error("session_id should be omitted when empty")
	}
	if _, exists := d["delete_after_run"]; exists {
		t.Error("delete_after_run should be omitted when false")
	}
	j.DeleteAfterRun = true
	if v, ok := j.ToDict()["delete_after_run"].(bool); !ok || !v {
		t.Error("delete_after_run should be true when set")
	}
}

// TestFromCronJobDict_必填校验
func TestFromCronJobDict_必填校验(t *testing.T) {
	base := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web"}
	for _, key := range []string{"id", "name", "cron_expr", "timezone", "description", "targets"} {
		bad := map[string]any{}
		for k, v := range base {
			bad[k] = v
		}
		bad[key] = ""
		if _, err := FromCronJobDict(bad); err == nil {
			t.Errorf("missing %s should error", key)
		}
	}
}

// TestFromCronJobDict_wakeOffset钳制
func TestFromCronJobDict_wakeOffset钳制(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web", "wake_offset_seconds": -5}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.WakeOffsetSeconds != 0 {
		t.Errorf("negative wake_offset should clamp to 0, got %d", j.WakeOffsetSeconds)
	}
}

// TestFromCronJobDict_legacyTargetsList
func TestFromCronJobDict_legacyTargetsList(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d",
		"targets": []any{map[string]any{"channel_id": "feishu"}}}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Targets != "feishu" {
		t.Errorf("legacy targets list should resolve, got %q", j.Targets)
	}
}

// TestIsValidTargetChannelID
func TestIsValidTargetChannelID(t *testing.T) {
	for _, valid := range []string{"web", "tui", "feishu", "wecom", "dingtalk", "wechat", "whatsapp", "xiaoyi"} {
		if !IsValidTargetChannelID(valid) {
			t.Errorf("%s should be valid", valid)
		}
	}
	if !IsValidTargetChannelID("feishu_enterprise:cli_123") {
		t.Error("feishu_enterprise:cli_123 should be valid")
	}
	for _, invalid := range []string{"", "foo", "feishu_enterprise:", "feishu_enterprise"} {
		if IsValidTargetChannelID(invalid) {
			t.Errorf("%q should be invalid", invalid)
		}
	}
}

// TestNormalizeTargetChannelID
func TestNormalizeTargetChannelID(t *testing.T) {
	if got := NormalizeTargetChannelID("WEB", "web"); got != "web" {
		t.Errorf("uppercase should normalize, got %q", got)
	}
	if got := NormalizeTargetChannelID("foo", "web"); got != "web" {
		t.Errorf("invalid should fallback, got %q", got)
	}
	if got := NormalizeTargetChannelID("", "tui"); got != "tui" {
		t.Errorf("empty should use default, got %q", got)
	}
	if got := NormalizeTargetChannelID("feishu_enterprise:cli_1:chat_2", "web"); got != "feishu_enterprise:cli_1" {
		t.Errorf("feishu_enterprise should truncate suffix, got %q", got)
	}
}
```

- [ ] **Step 3: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/cron/ && go test ./internal/swarm/gateway/cron/
```

注意：此时包内引用 `ValidateCronExpression`（Task 3 才实现），本任务先创建 cron_expr.go 的最小桩（仅函数签名返回 error）使编译通过，Task 3 再替换为完整实现。桩内容：

```go
// ValidateCronExpression 校验 cron 表达式与时区（Task 3 完整实现，暂为桩）。
func ValidateCronExpression(expr, timezone string) error { return nil }
```

提交：`feat(cron): add CronJob model and channel validation`

---

## Task 3: gateway/cron/cron_expr.go（含黄金用例矩阵）

**Files:**
- Create: `internal/swarm/gateway/cron/cron_expr.go`
- Create: `internal/swarm/gateway/cron/cron_expr_test.go`

- [ ] **Step 1: 替换桩，写完整 cron_expr.go**

对齐 cron_expr.py 75 行 + scheduler.py `_cron_next_push_dt`。核心是 dow 变换（croniter 0=SUN → Quartz 1=SUN..7=SAT）：

```go
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeCronExpr 将表达式归一化为 7 域 Quartz 格式。
// 对齐 Python: normalize_cron_expr (cron_expr.py L12-29)。
// 5 域（分 时 日 月 周）→ 前补 "0 "（秒）后补 " *"（年）。
// 7 域原样返回（含 dow 变换）。其他域数报错。
func NormalizeCronExpr(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	n := len(strings.Fields(s))
	if n == 5 {
		return transformDow("0 " + s + " *"), nil
	}
	if n == 7 {
		return transformDow(s), nil
	}
	return "", fmt.Errorf("cron_expr must have 5 or 7 fields, got %d fields. 5-field: minute hour day month dow. 7-field (Quartz): second minute hour day month dow year", n)
}

// ISOToSevenFieldCron 将 ISO8601 时间转为 7 域 cron 表达式。
// 对齐 Python: iso_to_seven_field_cron (cron_expr.py L32-49)。
// 无时区信息时按 timezone 参数解释。
func ISOToSevenFieldCron(atISO string, timezone string) (string, error) {
	s := strings.TrimSpace(atISO)
	if s == "" {
		return "", fmt.Errorf("at_iso is empty")
	}
	if strings.HasSuffix(s, "Z") {
		s = s[:len(s)-1] + "+00:00"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", err
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	local := t.In(loc)
	// dow 用 ? 通配（Quartz 一次性表达式惯例，对齐 Python 输出 "?"）
	return fmt.Sprintf("%d %d %d %d %d ? %d",
		local.Second(), local.Minute(), local.Hour(), local.Day(), int(local.Month()), local.Year()), nil
}

// ValidateCronExpression 校验表达式（5/7 域）与时区。
// 对齐 Python: validate_cron_expression (cron_expr.py L52-75)。
// 使用 go-quartz ValidateCronExpression 提前暴露非法表达式。
func ValidateCronExpression(expr string, timezone string) error {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return fmt.Errorf("cron_expr is empty")
	}
	normalized, err := NormalizeCronExpr(raw)
	if err != nil {
		return err
	}
	if err := quartz.ValidateCronExpression(normalized); err != nil {
		return fmt.Errorf("invalid cron expression: '%s'", raw)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone: '%s'", timezone)
	}
	return nil
}

// NextFireTime 计算下一次触发时间。
// 对齐 Python: _cron_next_push_dt (scheduler.py L50-66)。
// expr 已是归一化 7 域格式（调用方负责 NormalizeCronExpr）。
// go-quartz ErrTriggerExpired 对应 Python CroniterBadDateError（one-shot 过期）。
func NextFireTime(expr string, base time.Time) (time.Time, error) {
	trigger, err := quartz.NewCronTriggerWithLoc(expr, base.Location())
	if err != nil {
		return time.Time{}, err
	}
	nextNano, err := trigger.NextFireTime(base.UnixNano())
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nextNano).In(base.Location()), nil
}

// IsNoNextDateError 判断是否为"无下一次日期"错误（one-shot 过期）。
// 对齐 Python: _is_croniter_no_next_date (scheduler.py L231-237)。
func IsNoNextDateError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, quartz.ErrTriggerExpired) ||
		strings.Contains(err.Error(), "failed to find next date")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// transformDow 将 croniter dow 约定（0=SUN..6=SAT）变换为 Quartz 约定（1=SUN..7=SAT）。
// 变换规则：
//   - 单值 v → v%7+1（0→1? 不对：0=SUN→1，7=SAT 在 croniter 非法但容错为 7→7）
//     正确推导：croniter 0=SUN,1=MON..6=SAT；Quartz 1=SUN,2=MON..7=SAT
//     v(0..6) → v+1；v==7 容错 → 7
//   - 区间 a-b → (a+1)-(b+1)（a,b∈0..6）；a=0 时 0-4 → 1-5
//   - 步长/名称（MON-FRI/SUN）不变
//   - */n 保持不变（Quartz 步长语义同 croniter）
//
// 该变换只处理第 6 个域（dow），其他域原样。
func transformDow(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 7 {
		return expr
	}
	fields[5] = transformDowField(fields[5])
	return strings.Join(fields, " ")
}

// transformDowField 变换单个 dow 域。
func transformDowField(field string) string {
	if field == "*" || field == "?" {
		return field
	}
	// 名称（SUN/MON/...）不变——两种约定名称映射一致
	upper := strings.ToUpper(field)
	if isDayName(upper) {
		return upper
	}
	// 步长（*/n 或 a/n）：前缀部分变换，步长保持
	if idx := strings.Index(field, "/"); idx > 0 {
		prefix := transformDowField(field[:idx])
		return prefix + field[idx:]
	}
	// 区间 a-b
	if idx := strings.Index(field, "-"); idx > 0 {
		lo, loErr := strconv.Atoi(field[:idx])
		hi, hiErr := strconv.Atoi(field[idx+1:])
		if loErr == nil && hiErr == nil {
			return strconv.Itoa(shiftDowValue(lo)) + "-" + strconv.Itoa(shiftDowValue(hi))
		}
	}
	// 列表 a,b,c
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, transformDowField(p))
		}
		return strings.Join(out, ",")
	}
	// 单值
	if v, err := strconv.Atoi(field); err == nil {
		return strconv.Itoa(shiftDowValue(v))
	}
	return field
}

// shiftDowValue 单值变换：0..6 → 1..7（0=SUN→1=SUN）；7 容错为 7（=SAT）。
func shiftDowValue(v int) int {
	if v < 0 || v > 7 {
		return v
	}
	if v == 7 {
		return 7
	}
	return v + 1
}

// isDayName 判断是否为星期名称。
func isDayName(s string) bool {
	switch s {
	case "SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT":
		return true
	}
	return false
}
```

注意：需 import `errors`。

- [ ] **Step 2: 黄金用例矩阵测试 cron_expr_test.go**

先在 Python 侧生成期望值（croniter 已安装）。探针命令（一次性，结果写入测试常量）：

```bash
cd /home/opensource/jiuwenswarm-develop && python3 -c "
from croniter import croniter
from datetime import datetime
from zoneinfo import ZoneInfo
cases = [
    ('0 9 * * *', 'Asia/Shanghai'),      # 每天 9:00
    ('0 9 * * 1', 'Asia/Shanghai'),      # 每周一 9:00
    ('30 8 * * 1-5', 'Asia/Shanghai'),   # 工作日 8:30
    ('0 */2 * * *', 'Asia/Shanghai'),    # 每 2 小时
    ('15 10 15 * *', 'Asia/Shanghai'),   # 每月 15 日 10:15
]
base = datetime(2026, 12, 25, 0, 0, 0, tzinfo=ZoneInfo('Asia/Shanghai'))
for expr, tz in cases:
    it = croniter(expr, base)
    print(expr, '->', it.get_next(datetime).isoformat())
"
```

Go 测试文件：

```go
package cron

import (
	"strings"
	"testing"
	"time"
)

// goldenCase 黄金用例：表达式 × 时区 × 基准时刻 → 期望下次触发
type goldenCase struct {
	expr string
	tz   string
	base string // RFC3339
	want string // RFC3339 期望值（croniter 生成）
}

// TestNextFireTime_黄金用例 验证 go-quartz 变换后与 croniter 语义一致
func TestNextFireTime_黄金用例(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh) // 周五
	cases := []goldenCase{
		{"0 9 * * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T09:00:00+08:00"},
		{"0 9 * * 1", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-28T09:00:00+08:00"}, // 下周一
		{"30 8 * * 1-5", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-28T08:30:00+08:00"}, // 周五已过 8:30 → 下周一
		{"0 */2 * * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T02:00:00+08:00"},
		{"15 10 15 * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2027-01-15T10:15:00+08:00"},
	}
	for _, tc := range cases {
		normalized, err := NormalizeCronExpr(tc.expr)
		if err != nil {
			t.Fatalf("normalize %q: %v", tc.expr, err)
		}
		got, err := NextFireTime(normalized, base)
		if err != nil {
			t.Fatalf("NextFireTime %q: %v", tc.expr, err)
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("expr=%q got %s want %s", tc.expr, got.Format(time.RFC3339), tc.want)
		}
	}
}

// TestNormalizeCronExpr_dow变换
func TestNormalizeCronExpr_dow变换(t *testing.T) {
	cases := map[string]string{
		"0 9 * * *":      "0 9 * * * *",        // dow=* 不变
		"0 9 * * 1":      "0 9 * * * 2 *",      // croniter 1=MON → Quartz 2=MON
		"0 9 * * 0":      "0 9 * * * 1 *",      // croniter 0=SUN → Quartz 1=SUN
		"30 8 * * 1-5":   "0 30 8 * * 2-6 *",   // 工作日 → 2-6
		"0 9 * * MON":    "0 9 * * * MON *",    // 名称不变
		"0 9 * * mon":    "0 9 * * * MON *",    // 小写归大写
		"0 9 * * sun,mon": "0 9 * * * SUN,MON *",
	}
	for input, want := range cases {
		got, err := NormalizeCronExpr(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if got != want {
			t.Errorf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestNormalizeCronExpr_域数校验
func TestNormalizeCronExpr_域数校验(t *testing.T) {
	for _, bad := range []string{"0 9 * *", "0 9 * * * *  * *", "", "9"} {
		if _, err := NormalizeCronExpr(bad); err == nil {
			t.Errorf("%q should error", bad)
		}
	}
}

// TestISOToSevenFieldCron
func TestISOToSevenFieldCron(t *testing.T) {
	got, err := ISOToSevenFieldCron("2026-12-28T17:00:00", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("ISOToSevenFieldCron = %q", got)
	}
}

// TestValidateCronExpression
func TestValidateCronExpression(t *testing.T) {
	if err := ValidateCronExpression("0 9 * * *", "Asia/Shanghai"); err != nil {
		t.Errorf("valid expr should pass: %v", err)
	}
	if err := ValidateCronExpression("99 9 * * *", "Asia/Shanghai"); err == nil {
		t.Error("invalid minute should fail")
	}
	if err := ValidateCronExpression("0 9 * * *", "Not/AZone"); err == nil {
		t.Error("invalid timezone should fail")
	}
}

// TestNextFireTime_过期一次性表达式
func TestNextFireTime_过期一次性表达式(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh)
	// 2020 年的一次性表达式（年固定过去）
	_, err := NextFireTime("0 0 17 28 12 ? 2020", base)
	if !IsNoNextDateError(err) {
		t.Errorf("past one-shot should report no-next-date, got %v", err)
	}
}

// TestIsNoNextDateError
func TestIsNoNextDateError(t *testing.T) {
	if IsNoNextDateError(nil) {
		t.Error("nil should be false")
	}
	if !IsNoNextDateError(errFakeExpired) {
		t.Error("ErrTriggerExpired should be true")
	}
	if !strings.Contains("placeholder", "placeholder") {
		t.Error("sanity")
	}
}
```

注意：`errFakeExpired` 需在测试文件中定义（或直接用 `quartz.ErrTriggerExpired`）。

- [ ] **Step 3: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/cron/ -run "TestNormalize|TestNextFire|TestISO|TestValidate|TestIsNo" -v
```

若黄金用例失败（go-quartz 与 croniter 语义差异），先修正 transformDow（勿改期望值——期望值来自 croniter 基准）。已知差异点：croniter `1-5` 是 MON-FRI，Quartz 变换后 `2-6` 也是 MON-FRI（Quartz 2=MON）。

提交：`feat(cron): add cron expression normalization with dow transform and golden tests`

---

## Task 4: gateway/cron/store.go

**Files:**
- Create: `internal/swarm/gateway/cron/store.go`
- Create: `internal/swarm/gateway/cron/store_test.go`

- [ ] **Step 1: 写 store.go**

对齐 store.py 238 行。Go 用 sync.Mutex 替代 asyncio.Lock：

```go
package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronJobStore cron 任务 JSON 持久化。
// 对齐 Python: CronJobStore (store.py L15-238)。
// 路径默认 path.CronJobsPath()（gateway 与 agentserver 共享同一文件）。
type CronJobStore struct {
	// storePath JSON 文件路径
	storePath string
	// mu 互斥锁（对齐 Python asyncio.Lock）
	mu sync.Mutex
}

// cronStoreFile 文件结构（对齐 Python {"version": 1, "jobs": [...]}）
type cronStoreFile struct {
	Version int            `json:"version"`
	Jobs    []map[string]any `json:"jobs"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronJobStore 创建 store。path 为空时用默认 CronJobsPath()。
// 对齐 Python: CronJobStore.__init__(path=None)。
func NewCronJobStore(p string) *CronJobStore {
	if strings.TrimSpace(p) == "" {
		p = path.CronJobsPath()
	}
	return &CronJobStore{storePath: p}
}

// Path 返回存储路径。
// 对齐 Python: path 属性。
func (s *CronJobStore) Path() string {
	return s.storePath
}

// ListJobs 列出全部任务（updated_at+created_at 降序）。
// 对齐 Python: list_jobs (store.py L26-41)。损坏条目跳过保持系统健壮。
func (s *CronJobStore) ListJobs() ([]*CronJob, error) {
	data := s.readJSON()
	var jobs []*CronJob
	for _, item := range data.Jobs {
		job, err := FromCronJobDict(item)
		if err != nil {
			continue // 对齐 Python: ignore invalid entries
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].UpdatedAt != jobs[j].UpdatedAt {
			return jobs[i].UpdatedAt > jobs[j].UpdatedAt
		}
		return jobs[i].CreatedAt > jobs[j].CreatedAt
	})
	return jobs, nil
}

// GetJob 按 ID 获取任务。
// 对齐 Python: get_job (store.py L43-50)。
func (s *CronJobStore) GetJob(jobID string) (*CronJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, nil
	}
	jobs, err := s.ListJobs()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.ID == jobID {
			return j, nil
		}
	}
	return nil, nil
}

// CreateJobParams CreateJob 参数。
// 对齐 Python create_job 关键字参数（指针字段区分"未提供"与"零值"）。
type CreateJobParams struct {
	JobID            string
	Name             string
	CronExpr         string
	Timezone         string
	Description      string
	Targets          string
	Enabled          bool
	WakeOffsetSeconds *int // nil → 默认 300
	SessionID        string
	ChatType         string
	Mode             string
	DeleteAfterRun   *bool // nil → false
}

// CreateJob 创建任务（upsert 语义：job_id 已存在直接覆盖）。
// 对齐 Python: create_job (store.py L52-94)。无唯一性校验——Agent 双写 + Gateway 转发双写天然幂等。
func (s *CronJobStore) CreateJob(params CreateJobParams) (*CronJob, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	wos := 300
	if params.WakeOffsetSeconds != nil {
		wos = *params.WakeOffsetSeconds
	}
	dar := false
	if params.DeleteAfterRun != nil {
		dar = *params.DeleteAfterRun
	}
	mode := params.Mode
	if strings.TrimSpace(mode) == "" {
		mode = "agent"
	} else {
		mode = strings.ToLower(strings.TrimSpace(mode))
	}
	job := &CronJob{
		ID:                strings.TrimSpace(params.JobID),
		Name:              strings.TrimSpace(params.Name),
		Enabled:           params.Enabled,
		CronExpr:          strings.TrimSpace(params.CronExpr),
		Timezone:          strings.TrimSpace(params.Timezone),
		WakeOffsetSeconds: wos,
		Description:       params.Description,
		Targets:           strings.TrimSpace(params.Targets),
		SessionID:         strings.TrimSpace(params.SessionID),
		CreatedAt:         now,
		UpdatedAt:         now,
		ChatType:          strings.TrimSpace(params.ChatType),
		Mode:              mode,
		DeleteAfterRun:    dar,
	}
	if job.ID == "" {
		job.ID = uuid.New().Hex()
	}
	// round-trip 校验（对齐 Python: CronJob.from_dict(job.to_dict())）
	if _, err := FromCronJobDict(job.ToDict()); err != nil {
		return nil, err
	}
	if err := s.upsertJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

// UpdateJob 更新任务（patch 合并）。任务不存在返回错误。
// 对齐 Python: update_job (store.py L94-150)。
func (s *CronJobStore) UpdateJob(jobID string, patch map[string]any) (*CronJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("id is required")
	}
	existing, err := s.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("job not found")
	}

	if _, ok := patch["name"]; ok {
		existing.Name = strings.TrimSpace(strAny(patch["name"]))
	}
	if _, ok := patch["enabled"]; ok {
		existing.Enabled = boolAny(patch["enabled"], false)
		// 重新启用隐含不再过期（除非显式设置 expired）
		if existing.Enabled {
			if _, hasExpired := patch["expired"]; !hasExpired {
				existing.Expired = false
			}
		}
	}
	if _, ok := patch["cron_expr"]; ok {
		existing.CronExpr = strings.TrimSpace(strAny(patch["cron_expr"]))
		if _, hasExpired := patch["expired"]; !hasExpired {
			existing.Expired = false
		}
	}
	if _, ok := patch["timezone"]; ok {
		existing.Timezone = strings.TrimSpace(strAny(patch["timezone"]))
	}
	if raw, ok := patch["wake_offset_seconds"]; ok {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		if n < 0 {
			n = 0
		}
		existing.WakeOffsetSeconds = n
	}
	if _, ok := patch["description"]; ok {
		existing.Description = strAny(patch["description"])
	}
	if _, ok := patch["targets"]; ok {
		existing.Targets = strings.TrimSpace(strAny(patch["targets"]))
	}
	if _, ok := patch["session_id"]; ok {
		existing.SessionID = strings.TrimSpace(strAny(patch["session_id"]))
	}
	if _, ok := patch["chat_type"]; ok {
		existing.ChatType = strings.TrimSpace(strAny(patch["chat_type"]))
	}
	if _, ok := patch["expired"]; ok {
		existing.Expired = boolAny(patch["expired"], false)
	}
	if _, ok := patch["mode"]; ok {
		mode := strAny(patch["mode"])
		if strings.TrimSpace(mode) == "" {
			existing.Mode = "agent"
		} else {
			existing.Mode = strings.ToLower(strings.TrimSpace(mode))
		}
	}
	if _, ok := patch["delete_after_run"]; ok {
		existing.DeleteAfterRun = boolAny(patch["delete_after_run"], false)
	}

	existing.UpdatedAt = float64(time.Now().UnixNano()) / 1e9
	// round-trip 校验
	if _, err := FromCronJobDict(existing.ToDict()); err != nil {
		return nil, err
	}
	if err := s.upsertJob(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteJob 删除任务。返回是否实际删除。
// 对齐 Python: delete_job (store.py L152-174)。
func (s *CronJobStore) DeleteJob(jobID string) (bool, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readJSONUnlocked()
	kept := make([]map[string]any, 0, len(data.Jobs))
	deleted := false
	for _, item := range data.Jobs {
		if strings.TrimSpace(strAny(item["id"])) == jobID {
			deleted = true
			continue
		}
		kept = append(kept, item)
	}
	if deleted {
		data.Jobs = kept
		if err := s.writeJSONUnlocked(data); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// upsertJob 插入或覆盖任务。
// 对齐 Python: _upsert_job (store.py L176-196)。
func (s *CronJobStore) upsertJob(job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.readJSONUnlocked()
	out := make([]map[string]any, 0, len(data.Jobs)+1)
	found := false
	for _, item := range data.Jobs {
		if strings.TrimSpace(strAny(item["id"])) == job.ID {
			out = append(out, job.ToDict())
			found = true
		} else {
			out = append(out, item)
		}
	}
	if !found {
		out = append(out, job.ToDict())
	}
	data.Jobs = out
	return s.writeJSONUnlocked(data)
}

// readJSON 读文件（加锁）。
func (s *CronJobStore) readJSON() *cronStoreFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readJSONUnlocked()
}

// readJSONUnlocked 读文件（不加锁）。
// 对齐 Python: _read_json_unlocked (store.py L202-217)。任何异常回退空 store。
func (s *CronJobStore) readJSONUnlocked() *cronStoreFile {
	data := &cronStoreFile{Version: 1}
	raw, err := os.ReadFile(s.storePath)
	if err != nil {
		return data
	}
	if err := json.Unmarshal(raw, data); err != nil {
		return &cronStoreFile{Version: 1}
	}
	if data.Version == 0 {
		data.Version = 1
	}
	if data.Jobs == nil {
		data.Jobs = []map[string]any{}
	}
	return data
}

// writeJSONUnlocked 原子写（tmp + rename）。
// 对齐 Python: _write_json_unlocked (store.py L219-225)。
func (s *CronJobStore) writeJSONUnlocked(data *cronStoreFile) error {
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.storePath)
}
```

注意：
- CreatedAt/UpdatedAt 用 `float64(time.Now().UnixNano())/1e9` 保持小数秒（对齐 Python time.time()）
- `sort.Slice` 降序对齐 Python `reverse=True`
- import "github.com/google/uuid"（项目已有依赖）

- [ ] **Step 2: 写 store_test.go**

```go
package cron

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestStoreCrud 基本 CRUD
func TestStoreCrud(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))

	job, err := s.CreateJob(CreateJobParams{
		Name: "daily-report", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "daily", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" {
		t.Fatal("ID should be auto-generated")
	}

	got, _ := s.GetJob(job.ID)
	if got == nil || got.Name != "daily-report" {
		t.Fatal("GetJob should return created job")
	}

	// upsert：相同 job_id 覆盖
	_, err = s.CreateJob(CreateJobParams{
		JobID: job.ID, Name: "updated", CronExpr: "0 10 * * *", Timezone: "Asia/Shanghai",
		Description: "d2", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("upsert should not duplicate, got %d", len(jobs))
	}
	if jobs[0].Name != "updated" {
		t.Fatal("upsert should overwrite name")
	}

	// update
	updated, err := s.UpdateJob(job.ID, map[string]any{"enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("update should disable")
	}
	// 重新启用隐含 expired=false
	s.UpdateJob(job.ID, map[string]any{"enabled": true, "expired": true})
	got, _ = s.GetJob(job.ID)
	if got.Expired {
		t.Fatal("re-enable should clear expired")
	}

	// delete
	ok, _ := s.DeleteJob(job.ID)
	if !ok {
		t.Fatal("delete should succeed")
	}
	ok, _ = s.DeleteJob(job.ID)
	if ok {
		t.Fatal("second delete should return false")
	}
}

// TestStore损坏JSON兜底
func TestStore损坏JSON兜底(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	os.WriteFile(p, []byte("{invalid json"), 0o644)
	s := NewCronJobStore(p)
	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("corrupted JSON should fallback to empty, err=%v jobs=%d", err, len(jobs))
	}
}

// TestStore并发读写
func TestStore并发读写(t *testing.T) {
	dir := t.TempDir()
	s := NewCronJobStore(filepath.Join(dir, "cron_jobs.json"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.CreateJob(CreateJobParams{
				Name: "job", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
				Description: "d", Targets: "web", Enabled: true,
			})
			if err != nil {
				t.Errorf("concurrent create failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	jobs, _ := s.ListJobs()
	if len(jobs) != 20 {
		t.Fatalf("concurrent creates should all persist, got %d", len(jobs))
	}
}

// TestStore原子写
func TestStore原子写(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	s := NewCronJobStore(p)
	_, err := s.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 主文件存在且合法
	if _, err := os.Stat(p); err != nil {
		t.Fatal("main file should exist after write")
	}
	// 残留 tmp 不影响读取（模拟中断后恢复）
	os.WriteFile(p+".tmp", []byte("garbage"), 0o644)
	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatal("leftover tmp should not affect reads")
	}
}

// TestStoreUpdateNotFound
func TestStoreUpdateNotFound(t *testing.T) {
	s := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	if _, err := s.UpdateJob("nope", map[string]any{"name": "x"}); err == nil {
		t.Fatal("update nonexistent should error")
	}
}
```

- [ ] **Step 3: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/cron/ -run TestStore -v
```

提交：`feat(cron): add CronJobStore with atomic JSON persistence`

---

## Task 5: gateway/cron/scheduler.go（事件堆 + 注册表 + MessagePublisher）

**Files:**
- Create: `internal/swarm/gateway/cron/scheduler.go`
- Create: `internal/swarm/gateway/cron/scheduler_test.go`

- [ ] **Step 1: 写 scheduler.go**

对齐 scheduler.py 713 行。这是本计划最大的文件，核心结构：

```go
package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchedulerAgentClient 调度器所需的最小 AgentClient 接口。
// routing.AgentClient 隐式满足；测试 fake 零成本。
type SchedulerAgentClient interface {
	// SendRequest 发送非流式请求并等待完整响应
	SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error)
}

// MessagePublisher 消息发布接口（破环关键：message_handler 隐式实现）。
// 对齐 Python: MessageHandler.publish_robot_messages。
type MessagePublisher interface {
	// PublishRobotMessages 发布机器人消息
	PublishRobotMessages(msg *schema.Message)
}

// CronSchedulerService 异步调度器：唤醒 agent 并将结果推送到频道。
// 对齐 Python: CronSchedulerService (scheduler.py L78-713)。
type CronSchedulerService struct {
	// store 任务存储
	store *CronJobStore
	// agentClient AgentServer 客户端（接口注入）
	agentClient SchedulerAgentClient
	// publisher 消息发布器（接口注入破环）
	publisher MessagePublisher
	// nowFn 时钟函数（可注入假时钟做确定性测试）
	// 对齐 Python: now_fn 参数
	nowFn func() float64
	// configLoader 读取 config 原始数据（channels 元数据路由用）
	configLoader func() map[string]any

	// running 是否运行中（atomic，对齐 Python self._running）
	running atomic.Bool
	// reloadCh 重载通知通道（对齐 Python _reload_event）
	reloadCh chan struct{}
	// cancel 主循环取消函数
	cancel context.CancelFunc
	// wg 等待 runAgent goroutine 退出
	wg sync.WaitGroup

	// mu 保护 jobs/events/runs/runTasks
	mu sync.Mutex
	// jobs 内存中任务快照：jobID → job
	jobs map[string]*CronJob
	// events 事件堆（按 at_ts+seq 排序）
	events eventHeap
	// seq 事件序号（同刻稳定序）
	seq int
	// runs 运行状态：runID → state
	runs map[string]*CronRunState
	// lastStoreMtime 上次 store 文件 mtime 快照
	lastStoreMtime float64
}

// CronRunState 单次运行内存状态（不持久化）。
// 对齐 Python: CronRunState (models.py L231-250)。
type CronRunState struct {
	// RunID 运行标识（{job_id}:{push_ts}）
	RunID string
	// JobID 任务标识
	JobID string
	// WakeAtISO 唤醒时间 ISO
	WakeAtISO string
	// PushAtISO 推送时间 ISO
	PushAtISO string
	// Status 状态 pending|running|succeeded|failed
	Status string
	// PlaceholderSent 占位符已发
	PlaceholderSent bool
	// PushedFinal 终稿已推
	PushedFinal bool
	// StartedAt 开始时间
	StartedAt float64
	// FinishedAt 结束时间
	FinishedAt float64
	// ResultText 结果文本
	ResultText string
	// Error 错误信息
	Error string
	// JobName 任务名快照
	JobName string
	// Targets 目标频道快照
	Targets string
	// SessionID 会话快照
	SessionID string
	// ChatType 群聊类型快照
	ChatType string
	// Timezone 时区快照
	Timezone string
}

// cronEvent 调度事件（对齐 Python _Event dataclass）。
type cronEvent struct {
	// AtTS 触发时间戳（秒）
	AtTS float64
	// Seq 序号（同刻稳定序）
	Seq int
	// Kind 事件类型 wake|push|push_update
	Kind string
	// JobID 任务标识
	JobID string
	// RunID 运行标识
	RunID string
}

// eventHeap 事件堆（container/heap 实现）。
type eventHeap []*cronEvent

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// eventKindWake 唤醒事件
	eventKindWake = "wake"
	// eventKindPush 推送事件
	eventKindPush = "push"
	// eventKindPushUpdate 终稿补发事件
	eventKindPushUpdate = "push_update"
	// storePollInterval store 文件 mtime 轮询间隔（对齐 Python 5.0s）
	storePollInterval = 5.0
	// defaultWakeOffset 默认提前唤醒秒数（对齐 Python 300）
	defaultWakeOffset = 300
)

// logComponent 日志组件
const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// processSchedulerMu 进程级调度器注册表锁
	processSchedulerMu sync.RWMutex
	// processScheduler 进程级调度器（方案 B 核心）
	processScheduler *CronSchedulerService
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronSchedulerService 创建调度器。
// 对齐 Python: CronSchedulerService.__init__(store, agent_client, message_handler, now_fn)。
func NewCronSchedulerService(
	store *CronJobStore,
	agentClient SchedulerAgentClient,
	publisher MessagePublisher,
	nowFn func() float64,
) *CronSchedulerService {
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	return &CronSchedulerService{
		store:       store,
		agentClient: agentClient,
		publisher:   publisher,
		nowFn:       nowFn,
		reloadCh:    make(chan struct{}, 1),
		jobs:        make(map[string]*CronJob),
		runs:        make(map[string]*CronRunState),
	}
}

// SetConfigLoader 注入 config 读取函数（pushToTargets 渠道元数据路由用）。
func (s *CronSchedulerService) SetConfigLoader(fn func() map[string]any) {
	s.configLoader = fn
}

// SetProcessScheduler 注册进程级调度器（方案 B：委托降级单调度器）。
// Gateway 启动时调用；CronTools.ensureScheduler 检查此注册表，
// 已注册且运行中则委托 reload 而非启动内嵌 fallback。
func SetProcessScheduler(svc *CronSchedulerService) {
	processSchedulerMu.Lock()
	processScheduler = svc
	processSchedulerMu.Unlock()
}

// GetProcessScheduler 获取进程级调度器（未注册返回 nil）。
func GetProcessScheduler() *CronSchedulerService {
	processSchedulerMu.RLock()
	defer processSchedulerMu.RUnlock()
	return processScheduler
}

// ResetProcessSchedulerForTest 清空注册表（仅供测试）。
func ResetProcessSchedulerForTest() {
	processSchedulerMu.Lock()
	processScheduler = nil
	processSchedulerMu.Unlock()
}

// IsRunning 是否运行中。
func (s *CronSchedulerService) IsRunning() bool {
	return s.running.Load()
}

// Start 启动调度器。
// 对齐 Python: start (scheduler.py L133-139)。
func (s *CronSchedulerService) Start(ctx context.Context) error {
	if s.running.Load() {
		return nil
	}
	s.running.Store(true)
	if err := s.Reload(); err != nil {
		// 对齐 Python：reload 失败不阻止启动
		logger.Warn(logComponent).Err(err).Msg("[Cron] initial reload failed")
	}
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	go s.loop(loopCtx)
	logger.Info(logComponent).Msg("[Cron] scheduler started")
	return nil
}

// Stop 停止调度器。
// 对齐 Python: stop (scheduler.py L141-155)。
func (s *CronSchedulerService) Stop() {
	s.running.Store(false)
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.wg.Wait()
	logger.Info(logComponent).Msg("[Cron] scheduler stopped")
}

// Reload 重载任务并重建事件队列。
// 对齐 Python: reload (scheduler.py L157-198)。保留飞行中 push_update 事件。
// 加 sync.Mutex（Go 并发补齐：Python asyncio 单线程天然安全，Go 委托路径与 controller 路径可并发触发）。
func (s *CronSchedulerService) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs, err := s.store.ListJobs()
	if err != nil {
		return err
	}
	newJobs := map[string]*CronJob{}
	for _, j := range jobs {
		newJobs[j.ID] = j
	}
	s.jobs = newJobs

	// 保留飞行中 push_update（单次任务 push 后即 disabled，但补发结果还未完成）
	pendingPushUpdates := make([]*cronEvent, 0)
	for _, ev := range s.events {
		if ev.Kind == eventKindPushUpdate {
			pendingPushUpdates = append(pendingPushUpdates, ev)
		}
	}
	s.events = s.events[:0]
	s.seq = 0
	for _, ev := range pendingPushUpdates {
		heapPush(&s.events, ev)
	}

	now := s.nowFn()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		pushDT, wakeDT, runID, err := s.computeNextRun(job, now)
		if err != nil {
			if IsNoNextDateError(err) {
				// 已过期 one-shot：标记 expired 并停用
				job.Enabled = false
				job.Expired = true
				if _, uerr := s.store.UpdateJob(job.ID, map[string]any{"enabled": false, "expired": true}); uerr != nil {
					logger.Warn(logComponent).Str("job_id", job.ID).Err(uerr).
						Msg("[Cron] mark expired failed")
				}
			} else {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
					Msg("[Cron] compute next run failed")
			}
			continue
		}
		s.scheduleEventLocked(wakeDT, eventKindWake, job.ID, runID)
		s.scheduleEventLocked(pushDT, eventKindPush, job.ID, runID)
	}

	s.syncStoreMtime()
	s.notifyReload()
	return nil
}

// TriggerRunNow 立即触发一次运行。
// 对齐 Python: trigger_run_now (scheduler.py L200-212)。
func (s *CronSchedulerService) TriggerRunNow(jobID string) (string, error) {
	jobID = TrimSpace(jobID)
	s.mu.Lock()
	job := s.jobs[jobID]
	s.mu.Unlock()
	if job == nil {
		var err error
		job, err = s.store.GetJob(jobID)
		if err != nil {
			return "", err
		}
	}
	if job == nil {
		return "", fmt.Errorf("job not found")
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return "", err
	}
	now := time.Now().In(loc)
	runID := fmt.Sprintf("%s:%d", job.ID, now.Unix())
	s.mu.Lock()
	s.scheduleEventLocked(now, eventKindWake, job.ID, runID)
	s.scheduleEventLocked(now, eventKindPush, job.ID, runID)
	s.mu.Unlock()
	s.notifyReload()
	return runID, nil
}
```

（此为前半部分；后半部分 loop/handleEvent/onWake/onPush/onPushUpdate/runAgent/pushToTargets 及 heap 实现在 Step 2）

- [ ] **Step 2: scheduler.go 后半部分（事件处理 + pushToTargets）**

继续同文件追加：

```go
// ──────────────────────────── 非导出函数（事件处理） ────────────────────────────

// loop 主循环（对齐 Python _loop L239-280）。
// 事件堆空时等 reload 或 5s 轮询 mtime；有事件时等到触发时刻。
func (s *CronSchedulerService) loop(ctx context.Context) {
	for s.running.Load() {
		// 检查 ctx 取消（Stop 触发）
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.mu.Lock()
		empty := len(s.events) == 0
		var delay float64
		var ev *cronEvent
		if !empty {
			ev = s.events[0]
			delay = ev.AtTS - s.nowFn()
		}
		s.mu.Unlock()

		if empty {
			select {
			case <-s.reloadCh:
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(storePollInterval * float64(time.Second))):
				s.checkStoreChanged()
			}
			continue
		}

		if delay > 0 {
			wait := delay
			if wait > storePollInterval {
				wait = storePollInterval
			}
			select {
			case <-s.reloadCh:
				continue
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(wait * float64(time.Second))):
				// 检查 store 变更后再判断是否到期
				if s.checkStoreChanged() {
					continue
				}
				if s.nowFn() < ev.AtTS {
					continue
				}
			}
		}

		// 到期：弹出并处理
		s.mu.Lock()
		if len(s.events) == 0 {
			s.mu.Unlock()
			continue
		}
		ev = heapPop(&s.events)
		s.mu.Unlock()
		s.handleEvent(ctx, ev)
	}
}

// handleEvent 处理单个事件（对齐 Python _handle_event L282-339）。
func (s *CronSchedulerService) handleEvent(ctx context.Context, ev *cronEvent) {
	s.mu.Lock()
	job := s.jobs[ev.JobID]
	s.mu.Unlock()

	if job == nil && ev.Kind != eventKindPushUpdate {
		return
	}
	if job == nil && ev.Kind == eventKindPushUpdate {
		// 从 run 状态重建临时 job（任务可能已删除，补发仍需放行）
		state := s.getRun(ev.RunID)
		if state == nil {
			logger.Info(logComponent).
				Str("job_id", ev.JobID).Str("run_id", ev.RunID).
				Msg("[Cron] push_update skipped: no state and no job")
			return
		}
		job = &CronJob{
			ID: state.JobID, Name: state.JobName, Enabled: false, Expired: false,
			Timezone: state.Timezone, Targets: state.Targets,
			SessionID: state.SessionID, ChatType: state.ChatType,
		}
		if job.Timezone == "" {
			job.Timezone = "Asia/Shanghai"
		}
	}
	// push_update 是补发，即使单次任务已过期也必须放行
	if !job.Enabled && ev.Kind != eventKindPushUpdate {
		return
	}

	switch ev.Kind {
	case eventKindWake:
		s.onWake(ctx, job, ev.RunID)
	case eventKindPush:
		s.onPush(job, ev.RunID)
		if job.DeleteAfterRun {
			logger.Info(logComponent).Str("job_id", job.ID).
				Msg("[Cron] delete_after_run job, deleting after push")
			if _, err := s.store.DeleteJob(job.ID); err != nil {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
					Msg("[Cron] delete_after_run failed")
			}
			s.mu.Lock()
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return
		}
		s.rescheduleNext(job)
	case eventKindPushUpdate:
		s.onPushUpdate(job, ev.RunID)
	}
}

// onWake 唤醒事件：启动 runAgent goroutine（对齐 Python _on_wake L341-444）。
func (s *CronSchedulerService) onWake(ctx context.Context, job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runAgent(ctx, job, state)
	}()
}

// runAgent 执行 agent 调用（对齐 Python _run_agent L368-443）。
// finally 语义：即时排 push_update + 迟到补发判定。
func (s *CronSchedulerService) runAgent(ctx context.Context, job *CronJob, state *CronRunState) {
	s.mu.Lock()
	state.Status = "running"
	state.StartedAt = s.nowFn()
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		state.FinishedAt = s.nowFn()
		// 失败运行也产 result_text 供推送
		if state.ResultText == "" && state.Error != "" {
			state.ResultText = "[cron] 任务执行失败: " + state.Error
		}
		// placeholder 已发且终稿未推 → 即时排 push_update
		if state.PlaceholderSent && !state.PushedFinal && state.ResultText != "" {
			logger.Info(logComponent).
				Str("job_id", job.ID).Str("run_id", state.RunID).
				Int("text_len", len(state.ResultText)).
				Msg("[Cron] scheduling immediate push_update after agent finished")
			s.scheduleEventLocked(time.Now(), eventKindPushUpdate, job.ID, state.RunID)
		}
		// push_at 已过 → 迟到补发
		if pushDT, err := time.Parse(time.RFC3339, state.PushAtISO); err == nil {
			if pushDT.Unix() <= int64(s.nowFn()) && !state.PushedFinal && state.ResultText != "" {
				logger.Info(logComponent).
					Str("job_id", job.ID).Str("run_id", state.RunID).
					Msg("[Cron] scheduling late push_update because push_at<=now")
				s.scheduleEventLocked(time.Now(), eventKindPushUpdate, job.ID, state.RunID)
			}
		}
		s.mu.Unlock()
	}()

	// 构造 E2A envelope（对齐 Python e2a_from_agent_fields）
	ts := time.Now().UnixMilli()
	sessionID := fmt.Sprintf("cron_%d_%s", ts, job.ID)
	params := map[string]any{
		"content": job.Description,
		"query":   job.Description,
		"mode":    job.Mode,
		"cron": map[string]any{
			"job_id":   job.ID,
			"job_name": job.Name,
			"run_id":   state.RunID,
			"push_at":  state.PushAtISO,
			"wake_at":  state.WakeAtISO,
		},
	}
	if params["mode"] == "" {
		params["mode"] = "agent"
	}
	envelope := e2a.E2AFromAgentFields("cron-"+state.RunID,
		e2a.WithFieldChannelID("__cron__"),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodChatSend)),
		e2a.WithFieldParams(params),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(s.nowFn()),
		e2a.WithFieldMetadata(map[string]any{
			"cron": map[string]any{"job_id": job.ID, "run_id": state.RunID},
		}),
	)

	resp, err := s.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		s.mu.Lock()
		state.Status = "failed"
		state.Error = err.Error()
		s.mu.Unlock()
		return
	}
	text := extractTextFromAgentPayload(resp.Payload)
	if text == "" {
		text = "[cron] 任务完成，但未返回可展示文本"
	}
	s.mu.Lock()
	state.ResultText = text
	if resp.OK {
		state.Status = "succeeded"
	} else {
		state.Status = "failed"
	}
	s.mu.Unlock()
}

// onPush 推送事件（对齐 Python _on_push L446-480）。
func (s *CronSchedulerService) onPush(job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.mu.Lock()
	pushedFinal := state.PushedFinal
	resultText := state.ResultText
	s.mu.Unlock()

	if pushedFinal {
		return
	}
	if resultText != "" {
		s.pushToTargets(job, state, resultText, false)
		s.mu.Lock()
		state.PushedFinal = true
		s.mu.Unlock()
		return
	}
	// 未就绪：发占位符
	placeholder := fmt.Sprintf("[cron] %s 正在执行中，结果稍后补发（push_at=%s）", job.Name, state.PushAtISO)
	s.pushToTargets(job, state, placeholder, true)
	s.mu.Lock()
	state.PlaceholderSent = true
	s.mu.Unlock()
}

// onPushUpdate 终稿补发事件（对齐 Python _on_push_update L482-501）。
func (s *CronSchedulerService) onPushUpdate(job *CronJob, runID string) {
	s.mu.Lock()
	state := s.runs[runID]
	s.mu.Unlock()
	if state == nil {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: no state")
		return
	}
	s.mu.Lock()
	pushedFinal := state.PushedFinal
	resultText := state.ResultText
	s.mu.Unlock()
	if pushedFinal {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: already pushed_final")
		return
	}
	if resultText == "" {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: empty result_text")
		return
	}
	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", runID).Int("text_len", len(resultText)).
		Msg("[Cron] push_update start")
	s.pushToTargets(job, state, resultText, false)
	s.mu.Lock()
	state.PushedFinal = true
	s.mu.Unlock()
	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", runID).
		Msg("[Cron] push_update done")
}

// pushToTargets 推送结果到目标频道（~150 行，全量对齐 Python L503-713）。
// 9 渠道元数据路由详见设计文档第四节。
func (s *CronSchedulerService) pushToTargets(job *CronJob, state *CronRunState, text string, isPlaceholder bool) {
	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", state.RunID).
		Str("channel", strings.TrimSpace(job.Targets)).
		Bool("is_placeholder", isPlaceholder).
		Int("text_len", len(text)).Str("status", state.Status).
		Msg("[Cron] push_to_targets")

	payloadExtra := map[string]any{
		"content": text,
		"cron": map[string]any{
			"job_id":        job.ID,
			"job_name":      job.Name,
			"run_id":        state.RunID,
			"push_at":       state.PushAtISO,
			"wake_at":       state.WakeAtISO,
			"is_placeholder": isPlaceholder,
			"status":        state.Status,
		},
	}
	channelID := strings.TrimSpace(job.Targets)
	if channelID == "" {
		return
	}

	// ─── 路由 session_id 与 metadata 构造（对齐 Python L529-642）───
	var metadata map[string]any
	msgSessionID := ""
	routingSID := strings.TrimSpace(job.SessionID)
	if routingSID != "" && channelID != "web" && channelID != "tui" {
		msgSessionID = routingSID
	}
	if strings.HasPrefix(channelID, "feishu_enterprise:") && routingSID != "" && strings.Contains(routingSID, "::") {
		parts := strings.Split(routingSID, "::")
		if len(parts) >= 3 && parts[0] == "feishu" {
			chatPart := strings.TrimSpace(parts[1])
			if chatPart != "" {
				metadata = map[string]any{"feishu_chat_id": chatPart}
				if len(parts) >= 6 {
					openPart := strings.TrimSpace(parts[3])
					if openPart != "" {
						metadata["feishu_open_id"] = openPart
					}
				}
				msgSessionID = chatPart
			}
		}
	}

	// config.yaml 最近回发身份（对齐 Python L549-633）
	if metadata == nil {
		metadata = s.channelFallbackMetadata(channelID)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	// dingtalk 兜底（对齐 Python L635-642）
	if channelID == "dingtalk" {
		if routingSID != "" {
			if v, ok := metadata["dingtalk_sender_id"].(string); !ok || strings.TrimSpace(v) == "" {
				metadata["dingtalk_sender_id"] = routingSID
			}
		}
		if v, ok := metadata["conversation_type"].(string); !ok || strings.TrimSpace(v) == "" {
			metadata["conversation_type"] = "1"
		}
	}

	// ─── group_digital_avatar 路由决策（对齐 Python L644-698）───
	groupDigitalAvatar := false
	myUserID := ""
	chCfg := s.channelConfig(channelID)
	switch {
	case channelID == "wecom" || channelID == "feishu":
		groupDigitalAvatar = boolAny(chCfg["group_digital_avatar"], false)
		myUserID = strings.TrimSpace(strAny(chCfg["my_user_id"]))
	case strings.HasPrefix(channelID, "feishu_enterprise:"):
		enterpriseCfg := s.enterpriseBotConfig(channelID)
		groupDigitalAvatar = boolAny(enterpriseCfg["group_digital_avatar"], false)
		myUserID = strings.TrimSpace(strAny(enterpriseCfg["my_user_id"]))
	}

	if groupDigitalAvatar && myUserID != "" {
		if job.ChatType == "group" {
			metadata["chat_type"] = "group"
			if channelID == "wecom" {
				metadata["reply_wecom_user_id"] = myUserID
			} else if channelID == "feishu" || strings.HasPrefix(channelID, "feishu_enterprise:") {
				metadata["reply_candidate_feishu_open_id"] = myUserID
			}
			metadata["reply_candidate_reason"] = "cron_target_user"
			metadata["reply_target_name"] = myUserID
			metadata["is_cron_job"] = true
			logger.Info(logComponent).
				Str("my_user_id", myUserID).Str("channel", channelID).Str("job_id", job.ID).
				Msg("[Cron] job created in group chat, enabling IMOutboundPipeline routing")
		} else {
			logger.Info(logComponent).
				Str("chat_type", job.ChatType).Str("channel", channelID).Str("job_id", job.ID).
				Msg("[Cron] job created in p2p chat, skipping IMOutboundPipeline routing")
		}
	}

	// ─── 构造 Message 并发布（对齐 Python L700-713）───
	msg := schema.NewEventMessage(channelID, msgSessionID, schema.EventTypeChatFinal, payloadExtra,
		schema.WithMetadata(metadata),
		schema.WithGroupDigitalAvatar(groupDigitalAvatar),
	)
	msg.ID = fmt.Sprintf("cron-push-%s-%s", state.RunID, channelID)
	msg.Timestamp = s.nowFn()
	s.publisher.PublishRobotMessages(msg)
}
```

（channelFallbackMetadata/channelConfig/enterpriseBotConfig/checkStoreChanged/getOrCreateRun/getRun/scheduleEventLocked/notifyReload/computeNextRun/rescheduleNext/syncStoreMtime/heap 实现在 Step 3）

- [ ] **Step 3: scheduler.go 辅助函数 + heap 实现**

```go
// channelFallbackMetadata 从 config.yaml 取渠道最近回发身份（对齐 Python L549-633）。
// feishu/xiaoyi/whatsapp/wecom/wechat/dingtalk 六渠道逐字段对齐。
func (s *CronSchedulerService) channelFallbackMetadata(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return nil
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return nil
	}
	chCfg, _ := channelsCfg[channelID].(map[string]any)
	if chCfg == nil {
		chCfg = map[string]any{}
	}
	var metadata map[string]any
	switch channelID {
	case "feishu":
		lastChatID := strings.TrimSpace(strAny(chCfg["last_chat_id"]))
		lastOpenID := strings.TrimSpace(strAny(chCfg["last_open_id"]))
		if lastChatID != "" || lastOpenID != "" {
			metadata = map[string]any{"feishu_chat_id": lastChatID, "feishu_open_id": lastOpenID}
		}
	case "xiaoyi":
		lastSessionID := strings.TrimSpace(strAny(chCfg["last_session_id"]))
		lastTaskID := strings.TrimSpace(strAny(chCfg["last_task_id"]))
		if lastSessionID != "" || lastTaskID != "" {
			metadata = map[string]any{"xiaoyi_session_id": lastSessionID, "xiaoyi_task_id": lastTaskID}
		}
	case "whatsapp":
		lastJID := strings.TrimSpace(strAny(chCfg["last_jid"]))
		if lastJID != "" {
			metadata = map[string]any{"whatsapp_jid": lastJID}
		}
	case "wecom":
		lastChatID := strings.TrimSpace(strAny(chCfg["last_chat_id"]))
		lastUserID := strings.TrimSpace(strAny(chCfg["last_user_id"]))
		if lastChatID != "" || lastUserID != "" {
			metadata = map[string]any{"wecom_chat_id": lastChatID, "wecom_user_id": lastUserID}
		}
	case "wechat":
		lastUserID := strings.TrimSpace(strAny(chCfg["last_user_id"]))
		lastContextToken := strings.TrimSpace(strAny(chCfg["last_context_token"]))
		if lastUserID != "" {
			metadata = map[string]any{
				"wechat_user_id":     lastUserID,
				"reply_to_user_id":   lastUserID,
			}
			if lastContextToken != "" {
				metadata["wechat_context_token"] = lastContextToken
				metadata["context_token"] = lastContextToken
			}
		}
	case "dingtalk":
		lastSenderID := strings.TrimSpace(strAny(chCfg["last_sender_id"]))
		lastConversationID := strings.TrimSpace(strAny(chCfg["last_conversation_id"]))
		lastConversationType := strings.TrimSpace(strAny(chCfg["last_conversation_type"]))
		if lastSenderID != "" || lastConversationID != "" {
			ct := lastConversationType
			if ct == "" {
				ct = "1"
			}
			metadata = map[string]any{
				"dingtalk_sender_id":  lastSenderID,
				"dingtalk_chat_id":    lastConversationID,
				"conversation_id":     lastConversationID,
				"conversation_type":   ct,
			}
		}
	}
	return metadata
}

// channelConfig 读取渠道配置 map。
func (s *CronSchedulerService) channelConfig(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return map[string]any{}
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return map[string]any{}
	}
	chCfg, _ := channelsCfg[channelID].(map[string]any)
	if chCfg == nil {
		return map[string]any{}
	}
	return chCfg
}

// enterpriseBotConfig 读取 feishu_enterprise:<app_id> 对应 bot 配置。
// 对齐 Python L568-585：遍历 channels.feishu_enterprise 下 bot 配置匹配 app_id。
func (s *CronSchedulerService) enterpriseBotConfig(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return map[string]any{}
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return map[string]any{}
	}
	enterpriseCfg, _ := channelsCfg["feishu_enterprise"].(map[string]any)
	if enterpriseCfg == nil {
		return map[string]any{}
	}
	appID := ""
	if parts := strings.SplitN(channelID, ":", 2); len(parts) == 2 {
		appID = strings.TrimSpace(parts[1])
	}
	for _, v := range enterpriseCfg {
		botCfg, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(strAny(botCfg["app_id"])) == appID && appID != "" {
			return botCfg
		}
	}
	return map[string]any{}
}

// loadConfig 读取 config（注入的 loader，nil 返回 nil）。
func (s *CronSchedulerService) loadConfig() map[string]any {
	if s.configLoader == nil {
		return nil
	}
	return s.configLoader()
}

// checkStoreChanged 检查 store 文件 mtime 变更（对齐 Python _check_store_changed L117-128）。
// 方案 B 单调度器下保留——双部署形态另一进程写文件仍需感知。
func (s *CronSchedulerService) checkStoreChanged() bool {
	mtime := s.getStoreMtime()
	s.mu.Lock()
	last := s.lastStoreMtime
	s.mu.Unlock()
	if mtime != 0 && mtime != last && last != 0 {
		logger.Info(logComponent).
			Float64("last_mtime", last).Float64("mtime", mtime).
			Msg("[Cron] store file changed, reloading")
		if err := s.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("[Cron] reload after store change failed")
		}
		return true
	}
	return false
}

// getStoreMtime 读 store 文件 mtime（失败 0.0）。
func (s *CronSchedulerService) getStoreMtime() float64 {
	info, err := os.Stat(s.store.Path())
	if err != nil {
		return 0
	}
	return float64(info.ModTime().UnixNano()) / 1e9
}

// syncStoreMtime 快照当前 mtime（需在 mu 持有下调用）。
func (s *CronSchedulerService) syncStoreMtime() {
	s.lastStoreMtime = s.getStoreMtime()
}

// getOrCreateRun 获取或创建运行状态（对齐 Python _on_wake/_on_push 的 state 构造）。
func (s *CronSchedulerService) getOrCreateRun(job *CronJob, runID string) *CronRunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state, ok := s.runs[runID]; ok {
		return state
	}
	// 从 run_id 尾段恢复 push_ts
	pushTS := int64(s.nowFn())
	if parts := strings.Split(runID, ":"); len(parts) > 0 {
		if n, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil {
			pushTS = n
		}
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		loc = time.UTC
	}
	pushDT := time.Unix(pushTS, 0).In(loc)
	wakeDT := pushDT.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
	state := &CronRunState{
		RunID:     runID,
		JobID:     job.ID,
		WakeAtISO: wakeDT.Format(time.RFC3339),
		PushAtISO: pushDT.Format(time.RFC3339),
		Status:    "pending",
		JobName:   job.Name,
		Targets:   job.Targets,
		SessionID: job.SessionID,
		ChatType:  job.ChatType,
		Timezone:  job.Timezone,
	}
	s.runs[runID] = state
	return state
}

// getRun 读取运行状态。
func (s *CronSchedulerService) getRun(runID string) *CronRunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[runID]
}

// scheduleEventLocked 入队事件（需在 mu 持有下调用）。
// 对齐 Python _schedule_event L214-221：1 秒内到期的事件额外唤醒主循环。
func (s *CronSchedulerService) scheduleEventLocked(at time.Time, kind, jobID, runID string) {
	atTS := float64(at.UnixNano()) / 1e9
	s.seq++
	ev := &cronEvent{AtTS: atTS, Seq: s.seq, Kind: kind, JobID: jobID, RunID: runID}
	heapPush(&s.events, ev)
	if atTS <= s.nowFn()+1.0 {
		s.notifyReload()
	}
}

// notifyReload 非阻塞通知主循环。
func (s *CronSchedulerService) notifyReload() {
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

// computeNextRun 计算下次运行三元组（需在 mu 持有下调用）。
// 对齐 Python _compute_next_run L223-229。
func (s *CronSchedulerService) computeNextRun(job *CronJob, nowTS float64) (time.Time, time.Time, string, error) {
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	base := time.Unix(int64(nowTS), 0).In(loc)
	normalized, err := NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	pushDT, err := NextFireTime(normalized, base)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	wakeDT := pushDT.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
	runID := fmt.Sprintf("%s:%d", job.ID, pushDT.Unix())
	return pushDT, wakeDT, runID, nil
}

// rescheduleNext push 完成后调度下一轮（对齐 Python _handle_event push 分支 L319-337）。
func (s *CronSchedulerService) rescheduleNext(job *CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushDT, wakeDT, nextRunID, err := s.computeNextRun(job, s.nowFn())
	if err != nil {
		if IsNoNextDateError(err) {
			job.Enabled = false
			job.Expired = true
			if _, uerr := s.store.UpdateJob(job.ID, map[string]any{"enabled": false, "expired": true}); uerr != nil {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(uerr).
					Msg("[Cron] mark expired after push failed")
			}
		} else {
			logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
				Msg("[Cron] compute next run failed after push")
		}
		return
	}
	s.scheduleEventLocked(wakeDT, eventKindWake, job.ID, nextRunID)
	s.scheduleEventLocked(pushDT, eventKindPush, job.ID, nextRunID)
}

// extractTextFromAgentPayload 从 agent 响应 payload 提取文本。
// 对齐 Python _extract_text_from_agent_payload L26-47。
func extractTextFromAgentPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if content, ok := payload["content"].(map[string]any); ok {
		if out, ok := content["output"].(string); ok {
			return out
		}
		if out, ok := content["output"]; ok && out != nil {
			return fmt.Sprintf("%v", out)
		}
		return fmt.Sprintf("%v", content)
	}
	if content, ok := payload["content"].(string); ok {
		return content
	}
	if hb, ok := payload["heartbeat"].(string); ok && hb != "" {
		return hb
	}
	if text, ok := payload["text"].(string); ok && text != "" {
		return text
	}
	return ""
}

// ──────────────────────────── heap 实现 ────────────────────────────

// heapPush 入堆（包内简写，避免 container/heap.Interface 样板）。
func heapPush(h *eventHeap, ev *cronEvent) {
	*h = append(*h, ev)
	up(*h, len(*h)-1)
}

// heapPop 弹堆顶。
func heapPop(h *eventHeap) *cronEvent {
	if len(*h) == 0 {
		return nil
	}
	top := (*h)[0]
	last := (*h)[len(*h)-1]
	*h = (*h)[:len(*h)-1]
	if len(*h) > 0 {
		(*h)[0] = last
		down(*h, 0)
	}
	return top
}

// up 上浮。
func up(h eventHeap, i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if eventLess(h[i], h[parent]) {
			h[i], h[parent] = h[parent], h[i]
			i = parent
		} else {
			break
		}
	}
}

// down 下沉。
func down(h eventHeap, i int) {
	n := len(h)
	for {
		l, r := 2*i+1, 2*i+2
		smallest := i
		if l < n && eventLess(h[l], h[smallest]) {
			smallest = l
		}
		if r < n && eventLess(h[r], h[smallest]) {
			smallest = r
		}
		if smallest == i {
			return
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}

// eventLess 事件比较：AtTS 优先，同刻 Seq 稳定序。
func eventLess(a, b *cronEvent) bool {
	if a.AtTS != b.AtTS {
		return a.AtTS < b.AtTS
	}
	return a.Seq < b.Seq
}
```

补充 import：`os`、`strconv`、`fmt`、`strings`。

- [ ] **Step 4: 写 scheduler_test.go（nowFn 假时钟确定性测试）**

```go
package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// fakeClock 可手动推进的假时钟
type fakeClock struct {
	mu  sync.Mutex
	now float64
}

func (c *fakeClock) Now() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(sec float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += sec
}

// fakeAgentClient 可编程 agent 客户端
type fakeAgentClient struct {
	mu        sync.Mutex
	calls     int
	respond   func(call int) (*schema.AgentResponse, error)
}

func (f *fakeAgentClient) SendRequest(ctx context.Context, env *e2aEnvelopeAlias) (*schema.AgentResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	fn := f.respond
	f.mu.Unlock()
	if fn == nil {
		return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "done"}}}, nil
	}
	return fn(n)
}

// fakePublisher 收集推送消息
type fakePublisher struct {
	mu   sync.Mutex
	msgs []*schema.Message
}

func (f *fakePublisher) PublishRobotMessages(msg *schema.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
}

func (f *fakePublisher) Messages() []*schema.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*schema.Message{}, f.msgs...)
}

// TestSchedulerWakePushUpdateOrder 假时钟验证事件三分支顺序
func TestSchedulerWakePushUpdateOrder(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.Local).Unix())}
	// 启动后 next fire 9:00（表达式 0 9 * * *），wake_offset=300 → wake 8:55
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	_, err := store.CreateJob(CreateJobParams{
		Name: "t", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		WakeOffsetSeconds: ptr(300),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			// agent 慢响应：阻塞到 push 时刻之后
			time.Sleep(50 * time.Millisecond)
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "final-result"}}}, nil
		},
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, agent, pub, clock.Now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	// 推进到 8:55（wake 触发，agent 开始执行）
	clock.Advance(3300) // 8:00 → 8:55
	time.Sleep(100 * time.Millisecond)
	// 推进到 9:00（push 触发，agent 未完成 → 占位符）
	clock.Advance(300)
	time.Sleep(100 * time.Millisecond)

	msgs := pub.Messages()
	if len(msgs) < 1 {
		t.Fatal("placeholder should be pushed")
	}
	first := msgs[0]
	content := first.Payload["content"].(string)
	if !strings.Contains(content, "正在执行中") {
		t.Errorf("first push should be placeholder, got %q", content)
	}

	// 等待 agent 完成触发 push_update
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.Messages()) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	msgs = pub.Messages()
	if len(msgs) < 2 {
		t.Fatalf("push_update should fire after agent done, got %d messages", len(msgs))
	}
	second := msgs[1]
	content = second.Payload["content"].(string)
	if !strings.Contains(content, "final-result") {
		t.Errorf("second push should be final result, got %q", content)
	}
}

// TestSchedulerDirectFinalPush agent 完成后 push 直推终稿（无占位符）
func TestSchedulerDirectFinalPush(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.Local).Unix())}
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	_, err := store.CreateJob(CreateJobParams{
		Name: "t", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		WakeOffsetSeconds: ptr(300),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			// agent 立即完成
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "fast"}}}, nil
		},
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, agent, pub, clock.Now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	clock.Advance(3300) // wake at 8:55
	time.Sleep(100 * time.Millisecond)
	clock.Advance(300) // push at 9:00
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.Messages()) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	msgs := pub.Messages()
	if len(msgs) == 0 {
		t.Fatal("final should be pushed directly")
	}
	content := msgs[0].Payload["content"].(string)
	if !strings.Contains(content, "fast") {
		t.Errorf("direct final push expected, got %q", content)
	}
}

// TestSchedulerDeleteAfterRun
func TestSchedulerDeleteAfterRun(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.Local).Unix())}
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "once", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		DeleteAfterRun: ptr(true), WakeOffsetSeconds: ptr(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, clock.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	clock.Advance(3600) // 直接到 push
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := store.GetJob(job.ID); got == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := store.GetJob(job.ID); got != nil {
		t.Fatal("delete_after_run job should be deleted after push")
	}
}

// TestScheduler过期标记
func TestScheduler过期标记(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "old", CronExpr: "0 0 17 28 12 * 2020", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, nil)
	if err := svc.Reload(); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetJob(job.ID)
	if got == nil || got.Enabled || !got.Expired {
		t.Fatal("expired one-shot should be marked disabled+expired")
	}
}

// TestSchedulerMtime轮询外部变更
func TestSchedulerMtime轮询外部变更(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 0, 0, 0, 0, time.Local).Unix())}
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	store := NewCronJobStore(p)
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, clock.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	// 外部进程写新 job（bump mtime）
	external := NewCronJobStore(p)
	_, err := external.CreateJob(CreateJobParams{
		Name: "ext", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等待 5s 轮询周期（预留 7s 上限）
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		_, has := svc.jobs["ext"]
		svc.mu.Unlock()
		if has {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	svc.mu.Lock()
	_, has := svc.jobs["ext"]
	svc.mu.Unlock()
	if !has {
		t.Fatal("mtime polling should detect external change")
	}
}

// TestProcessScheduler注册表
func TestProcessScheduler注册表(t *testing.T) {
	ResetProcessSchedulerForTest()
	if GetProcessScheduler() != nil {
		t.Fatal("initial registry should be nil")
	}
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	SetProcessScheduler(svc)
	if GetProcessScheduler() != svc {
		t.Fatal("registry should return set instance")
	}
	ResetProcessSchedulerForTest()
}

// TestSchedulerReload并发
func TestSchedulerReload并发(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, &fakePublisher{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.Reload()
		}()
	}
	wg.Wait() // -race 下无死锁无竞争即通过
}

// TestSchedulerPushToTargets渠道元数据
func TestSchedulerPushToTargets渠道元数据(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	// 注入 config loader
	svc.SetConfigLoader(func() map[string]any {
		return map[string]any{
			"channels": map[string]any{
				"feishu": map[string]any{
					"last_chat_id": "oc_123", "last_open_id": "ou_456",
					"group_digital_avatar": true, "my_user_id": "u_me",
				},
			},
		}
	})
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "feishu", ChatType: "group", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "hello", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.ChannelID != "feishu" {
		t.Errorf("channel = %q", m.ChannelID)
	}
	md := m.Metadata
	if md["feishu_chat_id"] != "oc_123" || md["feishu_open_id"] != "ou_456" {
		t.Errorf("feishu metadata missing: %v", md)
	}
	if md["chat_type"] != "group" || md["reply_candidate_feishu_open_id"] != "u_me" || md["is_cron_job"] != true {
		t.Errorf("group routing metadata missing: %v", md)
	}
	if !m.GroupDigitalAvatar {
		t.Error("group_digital_avatar should propagate")
	}
}

// TestSchedulerPushToTargets企业飞书路由
func TestSchedulerPushToTargets企业飞书路由(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{
		ID: "j", Name: "n", Targets: "feishu_enterprise:cli_9",
		SessionID: "feishu::oc_chat::bot_1::ou_open::x::y", Timezone: "UTC",
	}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "hi", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatal("expected message")
	}
	m := msgs[0]
	if m.SessionID != "oc_chat" {
		t.Errorf("session = %q, want oc_chat", m.SessionID)
	}
	if m.Metadata["feishu_chat_id"] != "oc_chat" || m.Metadata["feishu_open_id"] != "ou_open" {
		t.Errorf("enterprise metadata: %v", m.Metadata)
	}
}

// TestSchedulerPushToTargetsWeb无session
func TestSchedulerPushToTargetsWeb无session(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "web", SessionID: "some-sid", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	m := pub.Messages()[0]
	if m.SessionID != "" {
		t.Errorf("web should not bind session_id, got %q", m.SessionID)
	}
}

// TestSchedulerPushToTargets空targets
func TestSchedulerPushToTargets空targets(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	if len(pub.Messages()) != 0 {
		t.Error("empty targets should not push")
	}
}

// ptr int 辅助
func ptr[T any](v T) *T { return &v }
```

注意：`e2aEnvelopeAlias` 是 `e2a.E2AEnvelope` 的别名——实际写测试时直接用 `*e2a.E2AEnvelope`。`strings` import 需补。

- [ ] **Step 5: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/cron/ -race -run TestScheduler -v && go test ./internal/swarm/gateway/cron/ -race -run TestProcess -v
```

提交：`feat(cron): add CronSchedulerService with event heap, process registry and channel routing`

---

## Task 6: gateway/cron/controller.go + doc.go

**Files:**
- Create: `internal/swarm/gateway/cron/controller.go`
- Create: `internal/swarm/gateway/cron/controller_test.go`
- Create: `internal/swarm/gateway/cron/doc.go`

- [ ] **Step 1: 写 controller.go**

对齐 controller.py 474 行。**不含 get_tools()**（Python 的 7 个 LocalFunction cron_* 工具已在 9.38 由 agentcore harness/tools/cron 包实现，controller 只做 API 门面）：

```go
package cron

import (
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronController 高层 cron API（WebChannel handlers 用）。
// 对齐 Python: CronController (controller.py L19-474)。
// Go 不做单例（依赖注入即可）；get_tools() 不移植（9.38 agentcore 已实现工具集）。
type CronController struct {
	// store 任务存储
	store *CronJobStore
	// scheduler 调度器
	scheduler *CronSchedulerService
	// targetChannel 默认目标频道（对齐 Python _target_channel）
	targetChannel string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// descriptionTimeKeywords description 中的时间/频率关键词。
// 对齐 Python: _DESCRIPTION_TIME_KEYWORDS。
var descriptionTimeKeywords = []string{"每天", "每周", "每月", "上午", "下午", "早上", "晚上", "凌晨"}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronController 创建控制器。
func NewCronController(store *CronJobStore, scheduler *CronSchedulerService) *CronController {
	return &CronController{store: store, scheduler: scheduler}
}

// SetTargetChannel 设置默认目标频道。
func (c *CronController) SetTargetChannel(channel string) {
	c.targetChannel = channel
}

// ListJobs 列出全部任务（map 形态）。
// 对齐 Python: list_jobs。
func (c *CronController) ListJobs() ([]map[string]any, error) {
	jobs, err := c.store.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.ToDict())
	}
	return out, nil
}

// GetJob 获取单个任务。
func (c *CronController) GetJob(jobID string) (map[string]any, error) {
	job, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return job.ToDict(), nil
}

// CreateJob 创建任务（含归一化与校验）。
// 对齐 Python: create_job (controller.py L127-159)。
func (c *CronController) CreateJob(params map[string]any) (map[string]any, error) {
	name := strings.TrimSpace(strAny(params["name"]))
	cronExpr, err := NormalizeCronExpr(strings.TrimSpace(strAny(params["cron_expr"])))
	if err != nil {
		return nil, err
	}
	timezone := strings.TrimSpace(strAny(params["timezone"]))
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	enabled := boolAny(params["enabled"], true)
	description := strAny(params["description"])
	targets, err := c.normalizeTargets(params["targets"])
	if err != nil {
		return nil, err
	}
	if err := validateSchedule(cronExpr, timezone); err != nil {
		return nil, err
	}
	description = normalizeDescription(description, name)

	routingSID := routingSessionID(targets, params["session_id"])
	chatType := strings.TrimSpace(strAny(params["chat_type"]))
	deleteAfterRun := false
	if v, ok := params["delete_after_run"]; ok {
		deleteAfterRun = boolAny(v, false)
	}

	wos := (*int)(nil)
	if raw, ok := params["wake_offset_seconds"]; ok && raw != nil {
		n, err := intAny(raw)
		if err != nil {
			return nil, fmt.Errorf("wake_offset_seconds must be int")
		}
		wos = &n
	}

	job, err := c.store.CreateJob(CreateJobParams{
		JobID:            strings.TrimSpace(strAny(params["id"])),
		Name:             name,
		CronExpr:         cronExpr,
		Timezone:         timezone,
		Enabled:          enabled,
		WakeOffsetSeconds: wos,
		Description:      description,
		Targets:          targets,
		SessionID:        routingSID,
		ChatType:         chatType,
		Mode:             strings.TrimSpace(strAny(params["mode"])),
		DeleteAfterRun:   &deleteAfterRun,
	})
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// UpdateJob 更新任务。
// 对齐 Python: update_job (controller.py L161-190)。
func (c *CronController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	patch = copyMap(patch)
	if _, ok := patch["targets"]; ok {
		targets, err := c.normalizeTargets(patch["targets"])
		if err != nil {
			return nil, err
		}
		patch["targets"] = targets
	}
	existing, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("job not found")
	}
	if _, ok := patch["cron_expr"]; ok {
		normalized, err := NormalizeCronExpr(strings.TrimSpace(strAny(patch["cron_expr"])))
		if err != nil {
			return nil, err
		}
		patch["cron_expr"] = normalized
	}
	if _, hasExpr := patch["cron_expr"]; hasExpr {
		if _, hasTz := patch["timezone"]; hasExpr || hasTz {
			cronExpr := strings.TrimSpace(strAny(patch["cron_expr"]))
			if cronExpr == "" {
				cronExpr = existing.CronExpr
			}
			timezone := strings.TrimSpace(strAny(patch["timezone"]))
			if timezone == "" {
				timezone = existing.Timezone
			}
			if err := validateSchedule(cronExpr, timezone); err != nil {
				return nil, err
			}
		}
	}
	if _, ok := patch["description"]; ok {
		name := strings.TrimSpace(strAny(patch["name"]))
		if name == "" {
			name = existing.Name
		}
		patch["description"] = normalizeDescription(strAny(patch["description"]), name)
	}

	finalTargets := strings.TrimSpace(strAny(patch["targets"]))
	if finalTargets == "" {
		finalTargets = existing.Targets
	}
	if _, ok := patch["session_id"]; ok {
		patch["session_id"] = routingSessionID(finalTargets, patch["session_id"])
	} else if _, ok := patch["targets"]; ok {
		patch["session_id"] = routingSessionID(finalTargets, existing.SessionID)
	}

	job, err := c.store.UpdateJob(jobID, patch)
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// DeleteJob 删除任务。
func (c *CronController) DeleteJob(jobID string) (bool, error) {
	deleted, err := c.store.DeleteJob(jobID)
	if err != nil {
		return false, err
	}
	if deleted {
		if err := c.scheduler.Reload(); err != nil {
			return false, err
		}
	}
	return deleted, nil
}

// ToggleJob 启停任务。
func (c *CronController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	job, err := c.store.UpdateJob(jobID, map[string]any{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	if err := c.scheduler.Reload(); err != nil {
		return nil, err
	}
	return job.ToDict(), nil
}

// PreviewJob 预览下 N 次执行时间。
// 对齐 Python: preview_job (controller.py L203-225)。
func (c *CronController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	job, err := c.store.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	pushDT := time.Now().In(loc)
	for i := 0; i < count; i++ {
		next, err := NextFireTime(normalized, pushDT)
		if err != nil {
			if IsNoNextDateError(err) {
				break
			}
			return nil, err
		}
		pushDT = next
		// 去重（同刻重复触发防御）
		if len(out) > 0 && pushDT.Format(time.RFC3339) == strAny(out[len(out)-1]["push_at"]) {
			break
		}
		wakeDT := next.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
		out = append(out, map[string]any{
			"wake_at": wakeDT.Format(time.RFC3339),
			"push_at": pushDT.Format(time.RFC3339),
		})
	}
	return out, nil
}

// RunNow 立即执行。
func (c *CronController) RunNow(jobID string) (string, error) {
	return c.scheduler.TriggerRunNow(jobID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeTargets 归一化 targets 参数。
// 对齐 Python: _normalize_targets (controller.py L76-88)。
func (c *CronController) normalizeTargets(raw any) (string, error) {
	rawStr := strings.TrimSpace(strAny(raw))
	if c.targetChannel == "" && rawStr == "" {
		return "", fmt.Errorf("targets is required when target_channel is not set")
	}
	if rawStr == "" {
		return NormalizeTargetChannelID(c.targetChannel, TargetChannelWeb)
	}
	if !IsValidTargetChannelID(rawStr) {
		return "", fmt.Errorf("targets must be one of tui/web/feishu/dingtalk/whatsapp/wecom/xiaoyi/wechat or feishu_enterprise:<app_id>")
	}
	return NormalizeTargetChannelID(rawStr, TargetChannelWeb)
}

// validateSchedule 校验表达式可计算下次触发。
// 对齐 Python: _validate_schedule (controller.py L68-72)。
func validateSchedule(cronExpr, timezone string) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	normalized, err := NormalizeCronExpr(cronExpr)
	if err != nil {
		return err
	}
	_, err = NextFireTime(normalized, time.Now().In(loc))
	return err
}

// normalizeDescription description 时间关键词兜底。
// 对齐 Python: _normalize_description (controller.py L91-101)。
func normalizeDescription(description string, name string) string {
	description = strings.TrimSpace(description)
	name = strings.TrimSpace(name)
	if name == "" {
		return description
	}
	hasKeyword := false
	for _, kw := range descriptionTimeKeywords {
		if strings.Contains(description, kw) {
			hasKeyword = true
			break
		}
	}
	if !hasKeyword {
		return description
	}
	if strings.Contains(description, name) || strings.HasSuffix(description, name) {
		return name
	}
	return description
}

// routingSessionID routing session_id 推导。
// 对齐 Python: _routing_session_id (controller.py L103-117)。
// feishu_enterprise 必须是 SessionMap 形态（feishu::...::...），否则 None。
func routingSessionID(targets string, raw any) string {
	targetsStr := strings.TrimSpace(targets)
	rawStr := ""
	if s, ok := raw.(string); ok {
		rawStr = strings.TrimSpace(s)
	}
	if rawStr == "" {
		return ""
	}
	if strings.HasPrefix(targetsStr, "feishu_enterprise:") {
		if !strings.Contains(rawStr, "::") {
			return ""
		}
		parts := strings.Split(rawStr, "::")
		if len(parts) < 3 || parts[0] != "feishu" {
			return ""
		}
		return rawStr
	}
	return rawStr
}

// copyMap 浅拷贝 map。
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 2: 写 controller_test.go**

覆盖：normalizeTargets（空+无默认 channel 报错/显式非法报错/合法归一）、normalizeDescription（含"每天"且 name 是子串 → name）、routingSessionID（feishu_enterprise 非 SessionMap → 空/合法 → 原样/其他渠道 → 原样）、CreateJob 全链路（含 cron 归一化+reload 调度）、UpdateJob（cron_expr 触发校验、session_id 联动重推导）、PreviewJob（count 钳制+过期 break）、RunNow 委托。

```go
package cron

import (
	"path/filepath"
	"testing"
)

// TestControllerNormalizeTargets
func TestControllerNormalizeTargets(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	if _, err := c.normalizeTargets(""); err == nil {
		t.Error("empty targets without default channel should error")
	}
	c.SetTargetChannel(TargetChannelTUI)
	if got, _ := c.normalizeTargets(""); got != "tui" {
		t.Errorf("empty targets with default = %q", got)
	}
	if got, _ := c.normalizeTargets("WEB"); got != "web" {
		t.Errorf("case normalize = %q", got)
	}
	if _, err := c.normalizeTargets("nope"); err == nil {
		t.Error("invalid targets should error")
	}
}

// TestControllerNormalizeDescription
func TestControllerNormalizeDescription(t *testing.T) {
	if got := normalizeDescription("每天早上九点汇报", "汇报"); got != "汇报" {
		t.Errorf("time keyword + name substring should use name, got %q", got)
	}
	if got := normalizeDescription("写周报", "周报"); got != "写周报" {
		t.Errorf("no time keyword should keep description, got %q", got)
	}
}

// TestControllerRoutingSessionID
func TestControllerRoutingSessionID(t *testing.T) {
	if got := routingSessionID("feishu_enterprise:app1", "plain-sid"); got != "" {
		t.Errorf("enterprise non-SessionMap should be empty, got %q", got)
	}
	want := "feishu::oc_1::bot::ou_2"
	if got := routingSessionID("feishu_enterprise:app1", want); got != want {
		t.Errorf("enterprise SessionMap should pass, got %q", got)
	}
	if got := routingSessionID("web", "plain-sid"); got != "plain-sid" {
		t.Errorf("web should accept plain sid, got %q", got)
	}
}

// TestControllerCreateJob全链路
func TestControllerCreateJob全链路(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)

	d, err := c.CreateJob(map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "每天早上汇报", "targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d["description"] != "n" {
		t.Errorf("description should collapse to name, got %v", d["description"])
	}
	if d["cron_expr"] != "0 9 * * * *" {
		t.Errorf("cron_expr should be normalized, got %v", d["cron_expr"])
	}
	// scheduler jobs 已加载
	svc.mu.Lock()
	n := len(svc.jobs)
	svc.mu.Unlock()
	if n != 1 {
		t.Errorf("scheduler should reload 1 job, got %d", n)
	}
}

// TestControllerPreviewJob
func TestControllerPreviewJob(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "c.json"))
	job, _ := store.CreateJob(CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	svc := NewCronSchedulerService(store, nil, nil, nil)
	c := NewCronController(store, svc)
	rows, err := c.PreviewJob(job.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("preview should return 3 rows, got %d", len(rows))
	}
	// count 钳制
	rows, _ = c.PreviewJob(job.ID, 0)
	if len(rows) != 1 {
		t.Errorf("count=0 should clamp to 1, got %d", len(rows))
	}
	// 不存在
	if _, err := c.PreviewJob("nope", 5); err == nil {
		t.Error("nonexistent job should error")
	}
}
```

- [ ] **Step 3: 写 doc.go**

```go
// Package cron 提供定时任务（Cron）调度服务：表达式归一化、任务持久化、
// 事件堆调度（wake/push/push_update 三分支）、API 门面。
//
// 核心组件：
//   - CronJob/CronRunState：数据模型（models.go）
//   - NormalizeCronExpr/NextFireTime：cron 表达式处理（cron_expr.go，go-quartz 驱动，
//     含 croniter→Quartz dow 约定变换）
//   - CronJobStore：JSON 持久化（store.go，tmp+rename 原子写）
//   - CronSchedulerService：调度器（scheduler.go，事件堆 + 进程级注册表 + MessagePublisher 接口）
//   - CronController：API 门面（controller.go，WebChannel handlers 用）
//
// 方案 B 委托降级单调度器：Gateway 启动时 SetProcessScheduler 注册；
// Agent 侧 CronTools.ensureScheduler 检查注册表，已注册则委托 Reload
// 而非启动内嵌 fallback（修正 Python 单进程双跑缺陷）。
//
// 不移植：cron_json_convert.py（CLI 工具非运行时）；
// controller.py get_tools()（工具集已由 9.38 agentcore/harness/tools/cron 实现）。
//
// 文件目录：
//
//	cron/
//	├── doc.go           # 包文档
//	├── models.go        # CronJob/CronRunState 模型 + 渠道校验归一化
//	├── cron_expr.go     # 表达式归一化 + dow 变换 + NextFireTime
//	├── store.go         # CronJobStore JSON 持久化
//	├── scheduler.go     # CronSchedulerService 事件堆调度器
//	└── controller.go    # CronController API 门面
//
// 对应 Python 代码：jiuwenswarm/gateway/cron/
package cron
```

- [ ] **Step 4: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/cron/ -v
```

提交：`feat(cron): add CronController facade with validation and preview`

---

## Task 7: agents 桥接包 cron_tools.go（方案 B ensureScheduler）

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/cron/cron_tools.go`
- Create: `internal/swarm/agents/harness/common/tools/cron/cron_tools_test.go`

- [ ] **Step 1: 写 cron_tools.go**

对齐 cron_tools.py ~430 行。包名用 `cronbridge`（避免与 agentcore cron 包同名混淆）：

```go
// Package cronbridge 提供 Agent 侧 cron 工具桥接：本地 store 双写 +
// E2A 转发到 Gateway + 委托降级单调度器（方案 B）。
package cronbridge

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronToolRoute 当前请求同步到 Gateway 时使用的路由。
// 对齐 Python: CronToolRoute (cron_tools.py L34-42)。
// Go 用 ctx values 传递（Python ContextVar）；pushCronRoute/resetCronRoute 配对。
type CronToolRoute struct {
	// RequestID 请求标识
	RequestID string
	// ChannelID 频道标识（默认 web）
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// ChatType 群聊标记（"group" 或空）
	ChatType string
}

// CronTools Agent 侧 cron 工具（本地 cron_jobs.json 为 source of truth）。
// 对齐 Python: CronTools (cron_tools.py L44-467)。
type CronTools struct {
	// gatewayPush E2A 推送函数（对齐 Python GatewayPushTransport.send_push）
	gatewayPush func(ctx context.Context, payload map[string]any) error
	// localStore 本地存储（与 Gateway 同路径同文件）
	localStore *gwcron.CronJobStore
	// scheduler 内嵌调度器（standalone fallback）
	scheduler *gwcron.CronSchedulerService
	// schedulerStarted 已尝试启动标志
	schedulerStarted bool
	// agentClient 调度器用 agent 客户端（可空）
	agentClient gwcron.SchedulerAgentClient
	// publisher 调度器用消息发布器（可空）
	publisher gwcron.MessagePublisher
	// configLoader 调度器用 config 读取器（可空）
	configLoader func() map[string]any
	// mu 保护 scheduler/schedulerStarted
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// routeCtxKey 路由上下文 key（ctx values 传递，对齐 Python ContextVar）
type routeCtxKeyType struct{}

var routeCtxKey = routeCtxKeyType{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronTools 创建 Agent 侧工具集。
// gatewayPush 为空时 E2A 转发降级为日志（standalone 形态）。
func NewCronTools(gatewayPush func(ctx context.Context, payload map[string]any) error) *CronTools {
	return &CronTools{
		gatewayPush: gatewayPush,
		localStore:  gwcron.NewCronJobStore(path.CronJobsPath()),
	}
}

// SetSchedulerDeps 注入内嵌调度器依赖（agentClient/publisher/configLoader 可空）。
func (t *CronTools) SetSchedulerDeps(agentClient gwcron.SchedulerAgentClient, publisher gwcron.MessagePublisher, configLoader func() map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agentClient = agentClient
	t.publisher = publisher
	t.configLoader = configLoader
}

// PushCronRoute 绑定路由上下文（进入一轮 Agent 执行前调用）。
// 对齐 Python: push_cron_route。返回的 reset 函数须配对调用（defer）。
func PushCronRoute(ctx context.Context, route *CronToolRoute) (context.Context, func()) {
	ctx = context.WithValue(ctx, routeCtxKey, route)
	return ctx, func() {}
}

// route 从 ctx 读取路由（缺省零值路由，对齐 Python _route() 默认值）。
func route(ctx context.Context) *CronToolRoute {
	if r, ok := ctx.Value(routeCtxKey).(*CronToolRoute); ok && r != nil {
		return r
	}
	return &CronToolRoute{ChannelID: gwcron.TargetChannelWeb}
}

// EnsureScheduler 确保调度器可用（方案 B 委托降级单调度器）。
// 对齐 Python ensure_scheduler（cron_tools.py L70-119）+ 方案 B 修正。
// 决策链：
//  1. 内嵌已启动且运行中 → 返回内嵌
//  2. 已尝试启动过 → 返回（可能 nil）
//  3. 进程注册表有运行中的 Gateway 调度器 → 委托 Reload → 标记已尝试 → 返回 nil
//  4. 启动内嵌 fallback（standalone 形态，对齐 Python）
func (t *CronTools) EnsureScheduler(ctx context.Context) *gwcron.CronSchedulerService {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. 内嵌已启动
	if t.scheduler != nil && t.scheduler.IsRunning() {
		return t.scheduler
	}
	// 2. 已尝试启动
	if t.schedulerStarted {
		return t.scheduler
	}

	// 3. 方案 B：委托 Gateway 调度器
	if svc := gwcron.GetProcessScheduler(); svc != nil && svc.IsRunning() {
		if err := svc.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[CronTools] delegated scheduler reload failed")
		} else {
			logger.Debug(logComponent).Msg("[CronTools] delegated scheduler reloaded")
		}
		t.schedulerStarted = true
		return nil
	}

	// 4. standalone fallback
	if t.agentClient == nil {
		logger.Warn(logComponent).
			Msg("[CronTools] Cannot start scheduler: agent client not available")
		t.schedulerStarted = true
		return nil
	}
	svc := gwcron.NewCronSchedulerService(t.localStore, t.agentClient, t.publisher, nil)
	svc.SetConfigLoader(t.configLoader)
	if err := svc.Start(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[CronTools] Failed to start scheduler")
	} else {
		logger.Info(logComponent).Msg("[CronTools] Scheduler started successfully")
	}
	t.scheduler = svc
	t.schedulerStarted = true
	return svc
}

// reloadScheduler 重载调度器（写操作后调用）。
// 对齐 Python _reload_scheduler (cron_tools.py L121-129)。
func (t *CronTools) reloadScheduler(ctx context.Context) {
	svc := t.EnsureScheduler(ctx)
	if svc != nil {
		if err := svc.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[CronTools] Failed to reload scheduler")
		}
	}
}

// SendSplit E2A cron 转发。
// 对齐 Python _send_split (cron_tools.py L145-163)。
// payload: {request_id, channel_id, session_id, response_kind: "cron", body: {action, status: "ok", data, message}}。
// 转发结果丢弃（异步回显），本地写即时生效。
func (t *CronTools) SendSplit(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	r := route(ctx)
	payload := map[string]any{
		"request_id":    r.RequestID,
		"channel_id":    r.ChannelID,
		"session_id":    r.SessionID,
		"response_kind": "cron",
		"body": map[string]any{
			"action":  action,
			"status":  "ok",
			"data":    copyMap(params),
			"message": "",
		},
	}
	if t.gatewayPush != nil {
		if err := t.gatewayPush(ctx, payload); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"action":  action,
		"status":  "forwarded",
		"data":    nil,
		"message": "cron request forwarded to gateway",
	}, nil
}

// ListJobs 列出任务（本地 store）。
func (t *CronTools) ListJobs() ([]map[string]any, error) {
	jobs, err := t.localStore.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.ToDict())
	}
	return out, nil
}

// GetJob 获取单个任务。
func (t *CronTools) GetJob(jobID string) (map[string]any, error) {
	job, err := t.localStore.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return job.ToDict(), nil
}

// CreateJob 本地写 + E2A 转发 + reload。
// 对齐 Python create_job (cron_tools.py L232-273)。
func (t *CronTools) CreateJob(ctx context.Context, params map[string]any) (map[string]any, error) {
	normalized := copyMap(params)
	delete(normalized, "session_id")
	targets, err := t.normalizeTargetsParam(ctx, normalized["targets"])
	if err != nil {
		return nil, err
	}
	normalized["targets"] = targets
	cronExpr, err := gwcron.NormalizeCronExpr(strings.TrimSpace(strVal(normalized["cron_expr"])))
	if err != nil {
		return nil, err
	}
	normalized["cron_expr"] = cronExpr

	r := route(ctx)
	sessionID := strings.TrimSpace(r.SessionID)
	chatType := r.ChatType

	job, err := t.localStore.CreateJob(gwcron.CreateJobParams{
		JobID:            strings.TrimSpace(strVal(normalized["id"])),
		Name:             strings.TrimSpace(strVal(normalized["name"])),
		CronExpr:         strings.TrimSpace(strVal(normalized["cron_expr"])),
		Timezone:         timezoneOr(normalized["timezone"], "Asia/Shanghai"),
		Description:      strVal(normalized["description"]),
		Targets:          strVal(normalized["targets"]),
		Enabled:          boolVal(normalized["enabled"], true),
		WakeOffsetSeconds: intPtrVal(normalized["wake_offset_seconds"]),
		SessionID:        sessionID,
		ChatType:         chatType,
		Mode:             strings.TrimSpace(strVal(normalized["mode"])),
		DeleteAfterRun:   boolPtrVal(normalized["delete_after_run"]),
	})
	if err != nil {
		return nil, err
	}

	// E2A 转发（失败仅 Warn，本地写已生效）
	if _, serr := t.SendSplit(ctx, "create", job.ToDict()); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync create to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// UpdateJob 本地更新 + E2A 转发 + reload。
// 对齐 Python update_job (cron_tools.py L275-300)。
func (t *CronTools) UpdateJob(ctx context.Context, jobID string, patch map[string]any) (map[string]any, error) {
	normalized := copyMap(patch)
	delete(normalized, "session_id")
	if _, ok := normalized["cron_expr"]; ok {
		cronExpr, err := gwcron.NormalizeCronExpr(strings.TrimSpace(strVal(normalized["cron_expr"])))
		if err != nil {
			return nil, err
		}
		normalized["cron_expr"] = cronExpr
	}
	if _, ok := normalized["targets"]; ok {
		targets, err := t.normalizeTargetsParam(ctx, normalized["targets"])
		if err != nil {
			return nil, err
		}
		normalized["targets"] = targets
		t := strings.TrimSpace(strVal(normalized["targets"]))
		if strings.HasPrefix(t, "feishu_enterprise:") {
			r := route(ctx)
			if r.SessionID != "" {
				normalized["session_id"] = r.SessionID
			}
		} else {
			normalized["session_id"] = nil
		}
	}
	r := route(ctx)
	if r.ChatType != "" {
		normalized["chat_type"] = r.ChatType
	} else {
		normalized["chat_type"] = nil
	}
	job, err := t.localStore.UpdateJob(jobID, normalized)
	if err != nil {
		return nil, err
	}
	if _, serr := t.SendSplit(ctx, "update", map[string]any{"job_id": jobID, "patch": normalized}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync update to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// DeleteJob 本地删除 + E2A 转发 + reload。
func (t *CronTools) DeleteJob(ctx context.Context, jobID string) (bool, error) {
	deleted, err := t.localStore.DeleteJob(jobID)
	if err != nil {
		return false, err
	}
	if _, serr := t.SendSplit(ctx, "delete", map[string]any{"job_id": jobID}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync delete to gateway failed")
	}
	t.reloadScheduler(ctx)
	return deleted, nil
}

// ToggleJob 本地启停 + E2A 转发 + reload。
func (t *CronTools) ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error) {
	job, err := t.localStore.UpdateJob(jobID, map[string]any{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	if _, serr := t.SendSplit(ctx, "toggle", map[string]any{"job_id": jobID, "enabled": enabled}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync toggle to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// PreviewJob 预览下次执行（本地计算）。
func (t *CronTools) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	job, err := t.localStore.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	loc, err := timeLoadLocation(job.Timezone)
	if err != nil {
		return nil, err
	}
	normalized, err := gwcron.NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	pushDT := timeNowIn(loc)
	for i := 0; i < count; i++ {
		next, err := gwcron.NextFireTime(normalized, pushDT)
		if err != nil {
			if gwcron.IsNoNextDateError(err) {
				break
			}
			return nil, err
		}
		pushDT = next
		if len(out) > 0 && pushDT.Format(timeRFC3339) == strVal(out[len(out)-1]["push_at"]) {
			break
		}
		wakeDT := next.Add(-timeDuration(max(0, job.WakeOffsetSeconds)))
		out = append(out, map[string]any{
			"wake_at": wakeDT.Format(timeRFC3339),
			"push_at": pushDT.Format(timeRFC3339),
		})
	}
	return out, nil
}

// RunNow 转发立即执行到 Gateway。
// 对齐 Python run_now（纯转发，本地不执行）。
func (t *CronTools) RunNow(ctx context.Context, jobID string) (map[string]any, error) {
	return t.SendSplit(ctx, "run_now", map[string]any{"job_id": jobID})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeTargetsParam targets 参数归一化（显式值 → fallback 推导）。
// 对齐 Python _normalize_targets_param (cron_tools.py L203-222)。
func (t *CronTools) normalizeTargetsParam(ctx context.Context, raw any) (string, error) {
	target := strings.TrimSpace(strVal(raw))
	if gwcron.IsValidTargetChannelID(target) {
		normalized := gwcron.NormalizeTargetChannelID(target, gwcron.TargetChannelWeb)
		logger.Info(logComponent).
			Str("raw", target).Str("normalized", normalized).
			Msg("[CronTools] normalize targets from explicit value")
		return normalized, nil
	}
	fallback := t.defaultTargetFromChannel(ctx)
	logger.Info(logComponent).
		Str("raw", target).Str("fallback", fallback).
		Msg("[CronTools] normalize targets from fallback")
	return fallback, nil
}

// defaultTargetFromChannel 从路由 channel 推导默认 target。
// 对齐 Python _default_target_from_channel (cron_tools.py L171-191)。
func (t *CronTools) defaultTargetFromChannel(ctx context.Context) string {
	channelRaw := t.resolveChannelID(ctx)
	channel := strings.ToLower(channelRaw)
	if strings.HasPrefix(channel, "feishu_enterprise:") {
		return gwcron.NormalizeTargetChannelID(channelRaw, gwcron.TargetChannelWeb)
	}
	for _, prefix := range []string{"feishu", "wecom", "xiaoyi", "whatsapp", "wechat", "dingtalk", "tui"} {
		if strings.HasPrefix(channel, prefix) {
			return prefix
		}
	}
	return gwcron.TargetChannelWeb
}

// resolveChannelID 解析路由 channel（route.channel_id → request_id 前缀）。
// 对齐 Python _resolve_channel_id (cron_tools.py L193-201)。
func (t *CronTools) resolveChannelID(ctx context.Context) string {
	r := route(ctx)
	channelRaw := strings.TrimSpace(r.ChannelID)
	if channelRaw != "" {
		return channelRaw
	}
	requestID := strings.TrimSpace(r.RequestID)
	if !strings.Contains(requestID, ":") {
		return ""
	}
	parts := strings.Split(requestID, ":")
	return strings.TrimSpace(parts[len(parts)-1-0-1]) // rsplit(":", 1)[0]
}

// copyMap 浅拷贝
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// strVal 安全取字符串
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolVal 安全取布尔
func boolVal(v any, def bool) bool {
	if v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// intPtrVal 安全取 int 指针（nil → nil）
func intPtrVal(v any) *int {
	if v == nil {
		return nil
	}
	switch n := v.(type) {
	case int:
		return &n
	case int64:
		i := int(n)
		return &i
	case float64:
		i := int(n)
		return &i
	}
	return nil
}

// boolPtrVal 安全取 bool 指针
func boolPtrVal(v any) *bool {
	if v == nil {
		return nil
	}
	if b, ok := v.(bool); ok {
		return &b
	}
	return nil
}

// timezoneOr timezone 参数兜底
func timezoneOr(v any, def string) string {
	s := strings.TrimSpace(strVal(v))
	if s == "" {
		return def
	}
	return s
}
```

注意：
- `resolveChannelID` 中 rsplit 逻辑：`strings.Split(requestID, ":")` 取倒数第二段。实际写代码时直接 `parts[len(parts)-2]`（需 len >= 2 已由 Contains 保证）
- `timeLoadLocation`/`timeNowIn`/`timeDuration`/`timeRFC3339` 直接用 `time.LoadLocation`/`time.Now().In`/`time.Duration`/`time.RFC3339`（计划中的别名是示意，实际写 time 标准库）
- Python `_route()` 默认 `CronToolRoute()` 的 channel_id 是 "web"——Go route() 零值路由 ChannelID 设为 web 对齐

- [ ] **Step 2: 写 cron_tools_test.go**

覆盖：EnsureScheduler 三分支（内嵌运行中直接返回/委托 Gateway reload+标记/agentClient 空 fallback 不启动）、PushCronRoute ctx 传递、SendSplit payload 结构断言、normalizeTargetsParam 显式/fallback、defaultTargetFromChannel 前缀矩阵、CreateJob 本地写+推送 mock 断言、UpdateJob feishu_enterprise session_id 注入。

```go
package cronbridge

import (
	"context"
	"path/filepath"
	"testing"

	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
)

// pushRecorder 记录 E2A 推送
type pushRecorder struct {
	payloads []map[string]any
	err      error
}

func (p *pushRecorder) push(ctx context.Context, payload map[string]any) error {
	p.payloads = append(p.payloads, payload)
	return p.err
}

// TestEnsureScheduler_委托Gateway 分支3
func TestEnsureScheduler_委托Gateway(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	dir := t.TempDir()
	store := gwcron.NewCronJobStore(filepath.Join(dir, "c.json"))
	pub := &fakePublisherForBridge{}
	svc := gwcron.NewCronSchedulerService(store, nil, pub, nil)
	gwcron.SetProcessScheduler(svc)

	tools := NewCronTools(nil)
	got := tools.EnsureScheduler(context.Background())
	if got != nil {
		t.Fatalf("delegated path should return nil, got %v", got)
	}
	if tools.schedulerStarted != true {
		t.Fatal("should mark as tried")
	}
}

// TestEnsureScheduler_fallback无AgentClient 分支4
func TestEnsureScheduler_fallback无AgentClient(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	tools := NewCronTools(nil)
	got := tools.EnsureScheduler(context.Background())
	if got != nil {
		t.Fatal("no agent client fallback should return nil")
	}
}

// TestSendSplitPayload 断言 E2A payload 结构
func TestSendSplitPayload(t *testing.T) {
	rec := &pushRecorder{}
	tools := NewCronTools(rec.push)
	ctx, _ := PushCronRoute(context.Background(), &CronToolRoute{
		RequestID: "req-1", ChannelID: "feishu", SessionID: "sess-1",
	})
	result, err := tools.SendSplit(ctx, "create", map[string]any{"name": "n"})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "forwarded" {
		t.Errorf("status = %v", result["status"])
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d", len(rec.payloads))
	}
	p := rec.payloads[0]
	if p["request_id"] != "req-1" || p["channel_id"] != "feishu" || p["session_id"] != "sess-1" {
		t.Errorf("route fields: %v", p)
	}
	if p["response_kind"] != "cron" {
		t.Errorf("response_kind = %v", p["response_kind"])
	}
	body := p["body"].(map[string]any)
	if body["action"] != "create" || body["status"] != "ok" {
		t.Errorf("body: %v", body)
	}
}

// TestDefaultTargetFromChannel 前缀矩阵
func TestDefaultTargetFromChannel(t *testing.T) {
	tools := NewCronTools(nil)
	cases := map[string]string{
		"feishu": "feishu", "feishu_enterprise:app": "feishu_enterprise:app",
		"wecom": "wecom", "xiaoyi": "xiaoyi", "whatsapp": "whatsapp",
		"wechat": "wechat", "dingtalk": "dingtalk", "tui": "tui",
		"web": "web", "": "web", "unknown": "web",
	}
	for in, want := range cases {
		ctx, _ := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: in})
		if got := tools.defaultTargetFromChannel(ctx); got != want {
			t.Errorf("defaultTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCreateJob本地写推送
func TestCreateJob本地写推送(t *testing.T) {
	dir := t.TempDir()
	// 重定向默认路径：直接构造后替换 localStore
	rec := &pushRecorder{}
	tools := NewCronTools(rec.push)
	tools.localStore = gwcron.NewCronJobStore(filepath.Join(dir, "c.json"))

	ctx, _ := PushCronRoute(context.Background(), &CronToolRoute{
		RequestID: "r", ChannelID: "web", SessionID: "s1", ChatType: "group",
	})
	d, err := tools.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d["session_id"] != "s1" || d["chat_type"] != "group" {
		t.Errorf("route context should propagate: %v", d)
	}
	if len(rec.payloads) != 1 {
		t.Fatal("E2A push should fire once")
	}
	// 本地可读
	got, _ := tools.GetJob(strVal(d["id"]))
	if got == nil {
		t.Fatal("local write should persist")
	}
}

// fakePublisherForBridge 测试 publisher
type fakePublisherForBridge struct{}

func (f *fakePublisherForBridge) PublishRobotMessages(msg any) {}
```

注意：`fakePublisherForBridge` 需匹配 `gwcron.MessagePublisher` 接口签名（`PublishRobotMessages(msg *schema.Message)`）——实际写时补 schema import。

- [ ] **Step 3: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/tools/cron/ -v
```

提交：`feat(cron): add Agent-side CronTools with delegated scheduler (Plan B)`

---

## Task 8: agents 桥接包 cron_runtime.go（11 方法 backend）

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/cron/cron_runtime.go`
- Create: `internal/swarm/agents/harness/common/tools/cron/cron_runtime_test.go`
- Create: `internal/swarm/agents/harness/common/tools/cron/doc.go`

- [ ] **Step 1: 写 cron_runtime.go**

对齐 cron_runtime.py ~400 行。实现 agentcore `CronToolBackend` 11 方法接口：

```go
package cronbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// cronToolsBackend 适配 CronTools 到 agentcore CronToolBackend 接口。
// 对齐 Python: _CronToolsCronBackend (cron_runtime.py L18-203)。
type cronToolsBackend struct {
	// tools Agent 侧工具集
	tools *CronTools
	// userPublisher wake 用户消息发布器（对齐 Python message_handler.publish_user_messages）
	userPublisher UserMessagePublisher
}

// UserMessagePublisher wake 用户消息发布接口（破环：message_handler 隐式实现）。
// 对齐 Python: MessageHandler.publish_user_messages。
type UserMessagePublisher interface {
	// PublishUserMessagesNowait 非阻塞发布用户消息
	PublishUserMessagesNowait(msg *schema.Message)
}

// CronRuntimeBridge 为 DeepAgents 解析宿主 cron 后端。
// 对齐 Python: CronRuntimeBridge (cron_runtime.py L333-399)。
type CronRuntimeBridge struct {
	// backendOverride 显式覆盖
	backendOverride cron.CronToolBackend
	// resolvedBackend 已解析后端
	resolvedBackend cron.CronToolBackend
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronRuntimeBridge 创建桥接器。
func NewCronRuntimeBridge() *CronRuntimeBridge {
	return &CronRuntimeBridge{}
}

// SetBackend 设置后端覆盖（测试/定制用）。
func (b *CronRuntimeBridge) SetBackend(backend cron.CronToolBackend) {
	b.backendOverride = backend
	b.resolvedBackend = backend
}

// GetBackend 解析后端（对齐 Python get_backend：默认构造 cronToolsBackend）。
// gatewayPush/userPublisher 由全局注入函数提供（对齐 Python MessageHandler.get_instance()）。
func (b *CronRuntimeBridge) GetBackend() cron.CronToolBackend {
	if b.backendOverride != nil {
		return b.backendOverride
	}
	if b.resolvedBackend != nil {
		return b.resolvedBackend
	}
	tools := NewCronTools(globalGatewayPushFunc())
	tools.SetSchedulerDeps(nil, globalUserPublisher(), globalConfigLoader())
	b.resolvedBackend = &cronToolsBackend{tools: tools, userPublisher: globalUserPublisher()}
	logger.Info(logComponent).
		Msg("[CronRuntimeBridge] CronTools backend initialized successfully")
	return b.resolvedBackend
}

// BuildTools 构建 cron 工具集。
// 对齐 Python build_tools (cron_runtime.py L379-399)。
func (b *CronRuntimeBridge) BuildTools(cronCtx *cron.CronToolContext, agentID string, language string) []tool.Tool {
	backend := b.GetBackend()
	if backend == nil {
		logger.Warn(logComponent).
			Msg("[CronRuntimeBridge] cron backend is not ready, skip builtin cron tools")
		return nil
	}
	targetChannels := gwcron.AllTargetChannels()
	return cron.CreateCronTools(backend, cronCtx, language, targetChannels, "", true, agentID)
}

// EnsureSchedulerStarted 确保调度器已启动（死代码入口保留，对齐 Python 一致性）。
// 对齐 Python ensure_scheduler_started (cron_runtime.py L361-377)。全仓零调用方。
func (b *CronRuntimeBridge) EnsureSchedulerStarted(ctx context.Context) {
	backend := b.GetBackend()
	cb, ok := backend.(*cronToolsBackend)
	if !ok {
		return
	}
	cb.tools.EnsureScheduler(ctx)
}

// SetGlobalGatewayPushFunc 注入 E2A 推送函数（app_gateway 装配时调用）。
// 对齐 Python WebSocketGatewayPushTransport 注入路径。
func SetGlobalGatewayPushFunc(fn func(ctx context.Context, payload map[string]any) error) {
	globalGatewayPushMu.Lock()
	globalGatewayPush = fn
	globalGatewayPushMu.Unlock()
}

// SetGlobalUserPublisher 注入 wake 用户消息发布器（app_gateway 装配时调用）。
// Go 无 MessageHandler 单例，用全局注入（SetDefaultSendPushFunc 同款先例）。
func SetGlobalUserPublisher(p UserMessagePublisher) {
	globalUserPublisherMu.Lock()
	globalUserPublisherFn = p
	globalUserPublisherMu.Unlock()
}

// SetGlobalConfigLoader 注入 config 读取函数。
func SetGlobalConfigLoader(fn func() map[string]any) {
	globalConfigLoaderMu.Lock()
	globalConfigLoaderFn = fn
	globalConfigLoaderMu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 全局注入变量（app_gateway 装配）
var (
	globalGatewayPushMu   sync.RWMutex
	globalGatewayPush     func(ctx context.Context, payload map[string]any) error
	globalUserPublisherMu sync.RWMutex
	globalUserPublisherFn UserMessagePublisher
	globalConfigLoaderMu  sync.RWMutex
	globalConfigLoaderFn  func() map[string]any
)

// globalGatewayPushFunc 读取注入的推送函数
func globalGatewayPushFunc() func(ctx context.Context, payload map[string]any) error {
	globalGatewayPushMu.RLock()
	defer globalGatewayPushMu.RUnlock()
	return globalGatewayPush
}

// globalUserPublisher 读取注入的发布器
func globalUserPublisher() UserMessagePublisher {
	globalUserPublisherMu.RLock()
	defer globalUserPublisherMu.RUnlock()
	return globalUserPublisherFn
}

// globalConfigLoader 读取注入的 config 读取器
func globalConfigLoader() func() map[string]any {
	globalConfigLoaderMu.RLock()
	defer globalConfigLoaderMu.RUnlock()
	return globalConfigLoaderFn
}

// ── cronToolsBackend 11 方法实现 ──

// ListJobs 实现 CronToolBackend.ListJobs。
// 对齐 Python list_jobs (cron_runtime.py L45-50)。
func (b *cronToolsBackend) ListJobs(ctx context.Context, includeDisabled bool) ([]map[string]any, error) {
	jobs, err := b.tools.ListJobs()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, toBackendJob(j))
	}
	if includeDisabled {
		return rows, nil
	}
	filtered := rows[:0]
	for _, row := range rows {
		if boolVal(row["enabled"], true) {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

// GetJob 实现 CronToolBackend.GetJob。
func (b *cronToolsBackend) GetJob(ctx context.Context, jobID string) (map[string]any, error) {
	job, err := b.tools.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return toBackendJob(job), nil
}

// CreateJob 实现 CronToolBackend.CreateJob。
// 对齐 Python create_job (cron_runtime.py L58-89)：_extractLegacyParams + 路由绑定。
func (b *cronToolsBackend) CreateJob(ctx context.Context, params map[string]any, cronCtx *cron.CronToolContext) (map[string]any, error) {
	payload, err := extractLegacyParams(params, cronCtx, true)
	if err != nil {
		return nil, err
	}
	ctx2, _ := PushCronRoute(ctx, routeFromContext(cronCtx))
	defer func() {}()
	job, err := b.tools.CreateJob(ctx2, payload)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// UpdateJob 实现 CronToolBackend.UpdateJob。
func (b *cronToolsBackend) UpdateJob(ctx context.Context, jobID string, patch map[string]any, cronCtx *cron.CronToolContext) (map[string]any, error) {
	payload, err := extractLegacyParams(patch, cronCtx, false)
	if err != nil {
		return nil, err
	}
	ctx2, _ := PushCronRoute(ctx, routeFromContext(cronCtx))
	job, err := b.tools.UpdateJob(ctx2, jobID, payload)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// DeleteJob 实现 CronToolBackend.DeleteJob。
func (b *cronToolsBackend) DeleteJob(ctx context.Context, jobID string) (bool, error) {
	return b.tools.DeleteJob(ctx, jobID)
}

// ToggleJob 实现 CronToolBackend.ToggleJob。
func (b *cronToolsBackend) ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error) {
	job, err := b.tools.ToggleJob(ctx, jobID, enabled)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// PreviewJob 实现 CronToolBackend.PreviewJob。
func (b *cronToolsBackend) PreviewJob(ctx context.Context, jobID string, count int) ([]map[string]any, error) {
	return b.tools.PreviewJob(jobID, count)
}

// RunNow 实现 CronToolBackend.RunNow（push_cron_route 空路由对齐 Python）。
func (b *cronToolsBackend) RunNow(ctx context.Context, jobID string) (string, error) {
	ctx2, _ := PushCronRoute(ctx, &CronToolRoute{})
	result, err := b.tools.RunNow(ctx2, jobID)
	if err != nil {
		return "", err
	}
	return strVal(result["run_id"]), nil
}

// Status 实现 CronToolBackend.Status（静态字典对齐 Python）。
func (b *cronToolsBackend) Status(ctx context.Context) (map[string]any, error) {
	jobs, err := b.tools.ListJobs()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"running":   false,
		"job_count": len(jobs),
		"run_count": 0,
	}, nil
}

// GetRuns 实现 CronToolBackend.GetRuns（返回空列表对齐 Python）。
func (b *cronToolsBackend) GetRuns(ctx context.Context, jobID string, limit int) ([]map[string]any, error) {
	return []map[string]any{}, nil
}

// Wake 实现 CronToolBackend.Wake。
// 对齐 Python wake (cron_runtime.py L139-168)。
func (b *cronToolsBackend) Wake(ctx context.Context, text string, cronCtx *cron.CronToolContext, mode string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if cronCtx == nil || strings.TrimSpace(cronCtx.ChannelID) == "" {
		return nil, fmt.Errorf("wake requires an active session context")
	}
	if b.userPublisher == nil {
		return nil, fmt.Errorf("cron wake is unavailable before message handler startup")
	}
	resolvedMode := mode
	if resolvedMode == "" && cronCtx != nil {
		resolvedMode = cronCtx.Mode
	}
	if resolvedMode == "" {
		resolvedMode = "agent.fast"
	}
	msg := schema.NewReqMessage(cronCtx.ChannelID, cronCtx.SessionID, schema.ReqMethodChatSend,
		jsonRawMessage(map[string]any{
			"query":   text,
			"content": text,
			"mode":    resolvedMode,
		}),
		schema.WithMetadata(deepCopyMetadata(cronCtx.Metadata)),
	)
	msg.ID = fmt.Sprintf("cron-wake-%d", time.Now().UnixMilli())
	msg.Timestamp = float64(time.Now().UnixNano()) / 1e9
	b.userPublisher.PublishUserMessagesNowait(msg)
	return map[string]any{"queued": true}, nil
}

// routeFromContext 从 CronToolContext 构造路由。
// 对齐 Python _route_from_context (cron_runtime.py L25-43)。
func routeFromContext(cronCtx *cron.CronToolContext) *CronToolRoute {
	if cronCtx == nil {
		return &CronToolRoute{ChannelID: gwcron.TargetChannelWeb}
	}
	metadata := map[string]any{}
	if cronCtx.Metadata != nil {
		metadata = cronCtx.Metadata
	}
	channelID := strings.TrimSpace(cronCtx.ChannelID)
	if channelID == "" {
		channelID = gwcron.TargetChannelWeb
	}
	return &CronToolRoute{
		RequestID: strings.TrimSpace(strVal(metadata["request_id"])),
		ChannelID: channelID,
		SessionID: strings.TrimSpace(cronCtx.SessionID),
		ChatType:  strings.TrimSpace(strVal(metadata["chat_type"])),
	}
}

// toBackendJob 补齐 backend job 默认字段。
// 对齐 Python _to_backend_job (cron_runtime.py L175-203)。
// schedule/payload/delivery/session_target/compat_mode 五字段默认注入。
func toBackendJob(job map[string]any) map[string]any {
	row := copyMap(job)
	if _, ok := row["schedule"]; !ok {
		expr := strings.TrimSpace(strVal(row["cron_expr"]))
		tz := strings.TrimSpace(strVal(row["timezone"]))
		if tz == "" {
			tz = "Asia/Shanghai"
		}
		row["schedule"] = map[string]any{"kind": "cron", "expr": expr, "tz": tz}
	}
	if _, ok := row["payload"]; !ok {
		row["payload"] = map[string]any{"kind": "agentTurn", "message": strings.TrimSpace(strVal(row["description"]))}
	}
	if _, ok := row["delivery"]; !ok {
		targets := strings.TrimSpace(strVal(row["targets"]))
		if targets == "" {
			targets = gwcron.TargetChannelWeb
		}
		row["delivery"] = map[string]any{"mode": "announce", "channel": targets}
	}
	if _, ok := row["session_target"]; !ok {
		row["session_target"] = "isolated"
	}
	if _, ok := row["compat_mode"]; !ok {
		row["compat_mode"] = "legacy"
	}
	return row
}

// extractLegacyParams 旧版扁平参数 → 新版嵌套参数映射。
// 对齐 Python _extract_legacy_params (cron_runtime.py L206-330)。~110 行三级 target 解析。
func extractLegacyParams(payload map[string]any, cronCtx *cron.CronToolContext, requireSchedule bool) (map[string]any, error) {
	data := copyMap(payload)
	contextChannel := ""
	if cronCtx != nil {
		contextChannel = strings.TrimSpace(cronCtx.ChannelID)
	}
	contextTarget := ""
	if contextChannel != "" {
		if strings.HasPrefix(contextChannel, "feishu_enterprise:") {
			contextTarget = gwcron.NormalizeTargetChannelID(contextChannel, gwcron.TargetChannelWeb)
		} else if gwcron.IsValidTargetChannelID(contextChannel) {
			contextTarget = contextChannel
		}
	}

	if _, hasSchedule := data["schedule"]; !hasSchedule {
		if _, hasPayload := data["payload"]; !hasPayload {
			if _, hasDelivery := data["delivery"]; !hasDelivery {
				// 无嵌套字段：原样返回（对齐 Python return data）
				return data, nil
			}
		}
	}

	// 嵌套结构解析
	schedule, _ := data["schedule"].(map[string]any)
	kind := strings.ToLower(strings.TrimSpace(strVal(schedule["kind"])))
	if kind == "" {
		kind = "cron"
	}
	if kind != "cron" {
		return nil, fmt.Errorf("Only cron schedule is supported by the current gateway bridge")
	}
	cronExpr := strings.TrimSpace(firstNonEmpty(
		strVal(schedule["expr"]), strVal(schedule["cron"]), strVal(data["cron_expr"])))
	timezone := strings.TrimSpace(firstNonEmpty(
		strVal(schedule["tz"]), strVal(schedule["timezone"]), strVal(data["timezone"])))
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}

	payloadBlock, _ := data["payload"].(map[string]any)
	payloadKind := strings.TrimSpace(strVal(payloadBlock["kind"]))
	if payloadKind == "" {
		payloadKind = "agentTurn"
	}
	if payloadKind != "agentTurn" {
		return nil, fmt.Errorf("Only agentTurn cron jobs are supported by the current gateway bridge")
	}
	description := firstNonEmpty(strVal(payloadBlock["message"]), strVal(data["description"]))

	delivery, _ := data["delivery"].(map[string]any)
	targets := strings.TrimSpace(firstNonEmpty(
		strVal(delivery["channel"]), strVal(data["targets"]), contextChannel))
	if targets == "" {
		targets = gwcron.TargetChannelWeb
	}
	// 隐式 web target 映射为请求上下文频道
	hasContextTarget := contextTarget != ""
	isWebTarget := targets == gwcron.TargetChannelWeb
	_, hasExplicitTargets := data["targets"]
	_, hasDeliveryChannel := delivery["channel"]
	if hasContextTarget && isWebTarget && !hasExplicitTargets && hasDeliveryChannel {
		targets = contextTarget
	}

	out := map[string]any{}
	if cronExpr != "" || requireSchedule {
		out["cron_expr"] = cronExpr
	}
	if timezone != "" || requireSchedule {
		out["timezone"] = timezone
	}
	if description != "" {
		out["description"] = description
	}
	if targets != "" {
		out["targets"] = targets
	}
	if _, ok := data["name"]; ok {
		out["name"] = strings.TrimSpace(strVal(data["name"]))
	}
	if _, ok := data["id"]; ok {
		out["id"] = strings.TrimSpace(strVal(data["id"]))
	}
	if _, ok := data["enabled"]; ok {
		out["enabled"] = boolVal(data["enabled"], false)
	}
	if _, ok := data["wake_offset_seconds"]; ok {
		out["wake_offset_seconds"] = data["wake_offset_seconds"]
	}
	if _, ok := data["deleteAfterRun"]; ok {
		out["delete_after_run"] = boolVal(data["deleteAfterRun"], false)
	}

	// context session_id
	if cronCtx != nil && strings.TrimSpace(cronCtx.SessionID) != "" {
		out["session_id"] = strings.TrimSpace(cronCtx.SessionID)
	}
	// mode 解析：context.mode → data.mode → agent.fast
	modeResolved := ""
	if cronCtx != nil {
		modeResolved = cronCtx.Mode
	}
	if modeResolved == "" {
		modeResolved = strVal(data["mode"])
	}
	if modeResolved == "" {
		modeResolved = "agent.fast"
	}
	out["mode"] = strings.ToLower(strings.TrimSpace(modeResolved))
	return out, nil
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// jsonRawMessage map 转 json.RawMessage。
func jsonRawMessage(m map[string]any) json.RawMessage {
	raw, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// deepCopyMetadata 深拷贝 metadata（对齐 Python deepcopy）。
func deepCopyMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
```

注意：
- 需 import `encoding/json`、`sync`、`tool`（agentcore foundation/tool）
- `gwcron.AllTargetChannels()` 需在 gateway/cron models.go 补充导出函数（返回 9 渠道列表）
- `CreateJob` 中的 `defer func() {}()` 是示意（ctx values 不可变无需 reset）；实际写时去掉
- Python `_extract_legacy_params` 的 schedule.kind 校验在无嵌套字段时不执行（return data 直接返回）——Go 保持一致

- [ ] **Step 2: 补充 gwcron.AllTargetChannels 到 models.go**

```go
// AllTargetChannels 返回全部目标频道列表（CronTargetChannel 枚举值）。
// 对齐 Python: [e.value for e in CronTargetChannel]。
func AllTargetChannels() []string {
	return []string{
		TargetChannelWeb, TargetChannelTUI, TargetChannelFeishu,
		TargetChannelWeCom, TargetChannelDingTalk, TargetChannelWeChat,
		TargetChannelWhatsApp, TargetChannelXiaoyi,
	}
}
```

- [ ] **Step 3: 写 cron_runtime_test.go**

覆盖：toBackendJob 五字段默认注入、extractLegacyParams（嵌套全量映射/无嵌套原样返回/kind 非 cron 报错/payloadKind 非 agentTurn 报错/隐式 web 映射 context target/mode 三级解析/deleteAfterRun 映射）、Wake（空 text 报错/无 context 报错/publisher 空报错/正常路径返回 queued+消息字段断言）、routeFromContext（metadata.request_id/chat_type 提取）。

```go
package cronbridge

import (
	"context"
	"strings"
	"testing"

	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
)

// TestToBackendJob默认字段
func TestToBackendJob默认字段(t *testing.T) {
	row := map[string]any{"id": "j", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web"}
	out := toBackendJob(row)
	schedule := out["schedule"].(map[string]any)
	if schedule["kind"] != "cron" || schedule["expr"] != "0 9 * * *" || schedule["tz"] != "Asia/Shanghai" {
		t.Errorf("schedule: %v", schedule)
	}
	payload := out["payload"].(map[string]any)
	if payload["kind"] != "agentTurn" || payload["message"] != "d" {
		t.Errorf("payload: %v", payload)
	}
	delivery := out["delivery"].(map[string]any)
	if delivery["mode"] != "announce" || delivery["channel"] != "web" {
		t.Errorf("delivery: %v", delivery)
	}
	if out["session_target"] != "isolated" || out["compat_mode"] != "legacy" {
		t.Errorf("defaults: session_target=%v compat_mode=%v", out["session_target"], out["compat_mode"])
	}
}

// TestExtractLegacyParams嵌套全量
func TestExtractLegacyParams嵌套全量(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"kind": "cron", "expr": "0 9 * * *", "tz": "Asia/Shanghai"},
		"payload":  map[string]any{"kind": "agentTurn", "message": "task"},
		"delivery": map[string]any{"mode": "announce", "channel": "web"},
		"name":     "n", "id": "j1",
	}
	out, err := extractLegacyParams(data, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["cron_expr"] != "0 9 * * *" || out["timezone"] != "Asia/Shanghai" {
		t.Errorf("schedule mapping: %v", out)
	}
	if out["description"] != "task" || out["targets"] != "web" {
		t.Errorf("payload/delivery mapping: %v", out)
	}
	if out["mode"] != "agent.fast" {
		t.Errorf("default mode = %v, want agent.fast", out["mode"])
	}
}

// TestExtractLegacyParams无嵌套原样
func TestExtractLegacyParams无嵌套原样(t *testing.T) {
	data := map[string]any{"name": "n", "cron_expr": "0 9 * * *"}
	out, err := extractLegacyParams(data, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "n" {
		t.Errorf("flat params should pass through: %v", out)
	}
}

// TestExtractLegacyParams非法Kind
func TestExtractLegacyParams非法Kind(t *testing.T) {
	data := map[string]any{"schedule": map[string]any{"kind": "interval"}}
	if _, err := extractLegacyParams(data, nil, true); err == nil {
		t.Error("non-cron kind should error")
	}
}

// TestExtractLegacyParams隐式web映射
func TestExtractLegacyParams隐式web映射(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"expr": "0 9 * * *"},
		"delivery": map[string]any{"channel": "web"},
	}
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: "s"}
	out, err := extractLegacyParams(data, cronCtx, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["targets"] != "feishu" {
		t.Errorf("implicit web should map to context channel, got %v", out["targets"])
	}
	if out["session_id"] != "s" {
		t.Errorf("context session should inject: %v", out["session_id"])
	}
}

// TestWake校验
func TestWake校验(t *testing.T) {
	backend := &cronToolsBackend{tools: NewCronTools(nil)}
	if _, err := backend.Wake(context.Background(), "  ", nil, ""); err == nil {
		t.Error("empty text should error")
	}
	if _, err := backend.Wake(context.Background(), "hi", nil, ""); err == nil {
		t.Error("nil context should error")
	}
	ctx := &agentcron.CronToolContext{ChannelID: "web", SessionID: "s"}
	if _, err := backend.Wake(context.Background(), "hi", ctx, ""); err == nil {
		t.Error("nil publisher should error")
	}
}

// TestWake正常路径
func TestWake正常路径(t *testing.T) {
	pub := &fakeUserPublisher{}
	backend := &cronToolsBackend{tools: NewCronTools(nil), userPublisher: pub}
	ctx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: "s", Mode: "agent.plan"}
	result, err := backend.Wake(context.Background(), "hello", ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if result["queued"] != true {
		t.Error("should return queued=true")
	}
	if len(pub.msgs) != 1 {
		t.Fatal("publisher should receive 1 message")
	}
	msg := pub.msgs[0]
	if msg.ChannelID != "feishu" || msg.SessionID != "s" {
		t.Errorf("message routing: %v", msg)
	}
	if !strings.HasPrefix(msg.ID, "cron-wake-") {
		t.Errorf("message ID prefix: %q", msg.ID)
	}
}

// fakeUserPublisher 测试发布器
type fakeUserPublisher struct {
	msgs []*schemaMessageAlias
}

func (f *fakeUserPublisher) PublishUserMessagesNowait(msg *schemaMessageAlias) {
	f.msgs = append(f.msgs, msg)
}
```

注意：`schemaMessageAlias` 是 `*schema.Message` 别名示意——实际写时直接 import schema 用 `*schema.Message`。

- [ ] **Step 4: 写 doc.go**

```go
// Package cronbridge 提供 Agent 侧 cron 工具桥接（11.10）：
// CronTools（本地 cron_jobs.json 双写 + E2A 转发 Gateway）+
// CronRuntimeBridge（解析宿主 backend，适配 agentcore CronToolBackend 11 方法接口）+
// 委托降级单调度器（方案 B：Gateway 调度器运行中则委托 Reload，否则启动内嵌 fallback）。
//
// 路由上下文用 ctx values 传递（对齐 Python 4 个 ContextVar），
// PushCronRoute 绑定 + defer 复位配对使用。
//
// 全局注入：SetGlobalGatewayPushFunc/SetGlobalUserPublisher/SetGlobalConfigLoader
// 由 app_gateway 装配时调用（Go 无 MessageHandler 单例）。
//
// 文件目录：
//
//	cron/
//	├── doc.go            # 包文档
//	├── cron_tools.go     # CronTools 本地双写 + E2A 转发 + ensureScheduler
//	└── cron_runtime.go   # CronRuntimeBridge + cronToolsBackend 11 方法 + extractLegacyParams
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/cron/
package cronbridge
```

- [ ] **Step 5: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/tools/cron/ -v
```

提交：`feat(cron): add CronRuntimeBridge with 11-method backend and legacy params extraction`

---

## Task 9: outbound.go handleCronPushPayload 回填

**Files:**
- Modify: `internal/swarm/gateway/message_handler/outbound.go`
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`
- Modify: `internal/swarm/gateway/message_handler/message_handler_test.go`（或新增 outbound_cron_test.go）

- [ ] **Step 1: message_handler.go 添加 cronController 字段与 SetCronController**

在 `MessageHandler` 结构体 `outboundPipeline` 字段后添加：

```go
	// cronController cron 控制器（对齐 Python _cron_controller，11.10）
	// Python: set_cron_controller 注入；Go 由 app_gateway 装配时设置
	cronController croncontroller.CronControllerAPI
```

定义接口（破环：message_handler 只依赖小接口）——新建 `outbound.go` 或直接在 message_handler.go：

```go
// CronControllerAPI handleCronPushPayload 所需的 CronController 接口。
// gateway/cron.CronController 隐式实现（破环：避免 import gateway/cron）。
// 对齐 Python: _handle_cron_push_payload 中调用的 CronController 方法面。
type CronControllerAPI interface {
	ListJobs() ([]map[string]any, error)
	GetJob(jobID string) (map[string]any, error)
	CreateJob(params map[string]any) (map[string]any, error)
	UpdateJob(jobID string, patch map[string]any) (map[string]any, error)
	DeleteJob(jobID string) (bool, error)
	ToggleJob(jobID string, enabled bool) (map[string]any, error)
	PreviewJob(jobID string, count int) ([]map[string]any, error)
	RunNow(jobID string) (string, error)
}
```

注意：gateway/cron 包的 CronController 当前方法签名带 error（Python 无 error 但 Go 需要）——**方法签名须完全匹配**。但 gateway/cron 的 ListJobs 等方法无 ctx 参数（Task 6 设计），隐式实现成立。

添加 setter：

```go
// SetCronController 注入 cron 控制器。
// 对齐 Python: set_cron_controller (message_handler.py L1674-1675)。
func (mh *MessageHandler) SetCronController(cc CronControllerAPI) {
	mh.cronController = cc
}
```

同时补 streamModes 读取辅助（handleCronPushPayload create 分支用）：

```go
// getStreamMode 读取流式请求模式。
// 对齐 Python: self._stream_modes.get(request_id)。
func (mh *MessageHandler) getStreamMode(requestID string) string {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	return mh.streamModes[requestID]
}
```

- [ ] **Step 2: 重写 outbound.go handleCronPushPayload**

对齐 Python `_handle_cron_push_payload` (message_handler.py L1677-1737)：

```go
// handleCronPushPayload 处理 cron 推送消息。
//
// Python: _handle_cron_push_payload (L1677-L1737)：
// action 路由到 CronController，结果以 chat.tool_result event_type 回发。
// create 时从 _stream_modes 取 mode 覆盖 LLM 工具调用的默认值。
func (mh *MessageHandler) handleCronPushPayload(payload map[string]any, requestID, channelID, sessionID string, metadata map[string]any) {
	if payload == nil {
		return
	}
	cc := mh.cronController
	if cc == nil {
		return
	}
	action := strings.TrimSpace(strVal(payload["action"]))
	params, _ := payload["data"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	var data any
	var err error
	switch action {
	case "list":
		data, err = cc.ListJobs()
	case "get":
		data, err = cc.GetJob(strings.TrimSpace(strVal(params["job_id"])))
	case "create":
		// 从原始请求中获取 mode，覆盖 LLM 工具调用的默认值
		if mode := mh.getStreamMode(requestID); mode != "" {
			params["mode"] = mode
		}
		data, err = cc.CreateJob(params)
	case "update":
		patch, _ := params["patch"].(map[string]any)
		data, err = cc.UpdateJob(strings.TrimSpace(strVal(params["job_id"])), patch)
	case "delete":
		var deleted bool
		deleted, err = cc.DeleteJob(strings.TrimSpace(strVal(params["job_id"])))
		data = map[string]any{"deleted": deleted}
	case "toggle":
		data, err = cc.ToggleJob(strings.TrimSpace(strVal(params["job_id"])), boolVal(params["enabled"], false))
	case "preview":
		count := 5
		if v, ok := params["count"].(float64); ok {
			count = int(v)
		}
		data, err = cc.PreviewJob(strings.TrimSpace(strVal(params["job_id"])), count)
	case "run_now":
		var runID string
		runID, err = cc.RunNow(strings.TrimSpace(strVal(params["job_id"])))
		data = map[string]any{"run_id": runID}
	default:
		data = map[string]any{"error": fmt.Sprintf("unknown cron action: %s", action)}
	}
	if err != nil {
		data = map[string]any{"error": err.Error()}
	}

	out := schema.NewEventMessage(channelID, sessionID, schema.EventTypeChatToolResult,
		map[string]any{
			"event_type": "chat.tool_result",
			"tool_name":  "cron",
			"result":     data,
		},
		schema.WithMetadata(metadata),
	)
	out.ID = requestID
	out.Timestamp = float64(time.Now().UnixNano()) / 1e9
	// enable_streaming=false：工具结果不开启流式，避免被发送到群聊（Go 零值即 false）
	mh.PublishRobotMessages(out)
}
```

同时更新调用方（handleAgentServerPush 中）传参：

```go
	// cron 判断：chunk.Payload["event_type"] == "cron.response"
	if chunk.Payload != nil {
		if eventType, ok := chunk.Payload["event_type"].(string); ok && eventType == "cron.response" {
			mh.handleCronPushPayload(chunk.Payload, rid, chunk.ChannelID, sessionID, busMetadata)
			return
		}
	}
```

注意：
- `EventTypeChatToolResult` 若 schema 包不存在需补常量（grep `chat.tool_result` 确认）
- `strVal`/`boolVal` 辅助在 message_handler 包内定义（或复用已有工具函数）
- 需 import `fmt`、`strings`、`time`、`schema`

- [ ] **Step 3: 写测试 outbound_cron_test.go**

```go
package message_handler

import (
	"testing"
)

// fakeCronController fake CronControllerAPI
type fakeCronController struct {
	listed  bool
	created bool
	preview bool
	lastCreateParams map[string]any
}

func (f *fakeCronController) ListJobs() ([]map[string]any, error) {
	f.listed = true
	return []map[string]any{{"id": "j1"}}, nil
}
func (f *fakeCronController) GetJob(jobID string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeCronController) CreateJob(params map[string]any) (map[string]any, error) {
	f.created = true
	f.lastCreateParams = params
	return map[string]any{"id": "new"}, nil
}
func (f *fakeCronController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	return nil, nil
}
func (f *fakeCronController) DeleteJob(jobID string) (bool, error) { return true, nil }
func (f *fakeCronController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	return nil, nil
}
func (f *fakeCronController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	f.preview = true
	return nil, nil
}
func (f *fakeCronController) RunNow(jobID string) (string, error) { return "run-1", nil }

// TestHandleCronPushPayload_路由
func TestHandleCronPushPayload_路由(t *testing.T) {
	mh := NewMessageHandler(nil)
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{
		"event_type": "cron.response",
		"action":     "list",
	}, "req-1", "web", "sess-1", nil)

	if !cc.listed {
		t.Error("list action should route to controller")
	}
}

// TestHandleCronPushPayload_create模式覆盖
func TestHandleCronPushPayload_create模式覆盖(t *testing.T) {
	mh := NewMessageHandler(nil)
	cc := &fakeCronController{}
	mh.SetCronController(cc)
	// 模拟 stream mode 已记录
	mh.streamMu.Lock()
	mh.streamModes["req-1"] = "agent.plan"
	mh.streamMu.Unlock()

	mh.handleCronPushPayload(map[string]any{
		"action": "create",
		"data":   map[string]any{"name": "n"},
	}, "req-1", "web", "sess-1", nil)

	if !cc.created {
		t.Fatal("create action should route")
	}
	if cc.lastCreateParams["mode"] != "agent.plan" {
		t.Errorf("stream mode should override, got %v", cc.lastCreateParams["mode"])
	}
}

// TestHandleCronPushPayload_未知action
func TestHandleCronPushPayload_未知action(t *testingtesting) {
	mh := NewMessageHandler(nil)
	cc := &fakeCronController{}
	mh.SetCronController(cc)
	// 未知 action 不应 panic，结果含 error
	mh.handleCronPushPayload(map[string]any{"action": "bogus"}, "req-1", "web", "s", nil)
}

// TestHandleCronPushPayload_控制器缺失
func TestHandleCronPushPayload_控制器缺失(t *testing.T) {
	mh := NewMessageHandler(nil)
	mh.handleCronPushPayload(map[string]any{"action": "list"}, "r", "web", "s", nil)
	// 无 controller 直接 return，无 panic 即通过
}
```

注意：最后一个测试的参数 `*testingtesting` 是笔误示意——实际写 `*testing.T`。

- [ ] **Step 4: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/ -run TestHandleCronPushPayload -v
```

提交：`feat(cron): implement handleCronPushPayload routing with tool_result response`

---

## Task 10: web_handlers.go 8 个 cron.job.* 回填

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_handlers.go`

- [ ] CronJobAPI 接口定义（web 包内小接口，避免直接 import gateway/cron——若 import 无环则直接用具体类型）

先验证依赖方向：web 包 → gateway/cron 无环（gateway/cron 不 import web）。可以直接 import。但 WebHandlersBindParams.CronController 当前是 `any`——升级为具体接口：

```go
// CronJobController web 端点所需 CronController 接口。
// gateway/cron.CronController 隐式实现。
type CronJobController interface {
	ListJobs() ([]map[string]any, error)
	GetJob(jobID string) (map[string]any, error)
	CreateJob(params map[string]any) (map[string]any, error)
	UpdateJob(jobID string, patch map[string]any) (map[string]any, error)
	DeleteJob(jobID string) (bool, error)
	ToggleJob(jobID string, enabled bool) (map[string]any, error)
	PreviewJob(jobID string, count int) ([]map[string]any, error)
	RunNow(jobID string) (string, error)
}
```

WebHandlersBindParams.CronController 字段类型从 `any` 改为 `CronJobController`。

- [ ] **Step 2: 8 个 handler 实现（对齐 Python app_web_handlers.py L2046-2198）**

替换桩注册段（web_handlers.go:328-337）：

```go
	// ─── 定时任务 ───
	// 11.10 回填：8 个 cron.job.* 端点接真实 CronController
	if bind.CronController != nil {
		cc := bind.CronController
		d.Register("cron.job.list", handleCronJobList(cc))
		d.Register("cron.job.get", handleCronJobGet(cc))
		d.Register("cron.job.create", handleCronJobCreate(cc))
		d.Register("cron.job.update", handleCronJobUpdate(cc))
		d.Register("cron.job.delete", handleCronJobDelete(cc))
		d.Register("cron.job.toggle", handleCronJobToggle(cc))
		d.Register("cron.job.preview", handleCronJobPreview(cc))
		d.Register("cron.job.run_now", handleCronJobRunNow(cc))
	} else {
		cronListStub := stubHandler("cron", map[string]any{"jobs": []any{}})
		cronOkStub := stubHandler("cron", map[string]any{"ok": true})
		d.Register("cron.job.list", cronListStub)
		d.Register("cron.job.create", cronOkStub)
		d.Register("cron.job.update", cronOkStub)
		d.Register("cron.job.delete", cronOkStub)
		d.Register("cron.job.enable", cronOkStub)
		d.Register("cron.job.disable", cronOkStub)
		d.Register("cron.job.run", cronOkStub)
		d.Register("cron.job.status", stubHandler("cron", map[string]any{}))
	}
```

handler 实现（对齐 Python 逐字段）：

```go
// handleCronJobList 处理 cron.job.list。
// Python: _cron_job_list (app_web_handlers.py L2050-2056)。
func handleCronJobList(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		jobs, err := cc.ListJobs()
		if err != nil {
			return errPayload(WsErrInternalError, err.Error()), nil
		}
		return map[string]any{"jobs": jobs}, nil
	}
}

// handleCronJobGet 处理 cron.job.get。
// Python: _cron_job_get (L2058-2074)。id 必填；job 不存在 → NOT_FOUND。
func handleCronJobGet(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		job, err := cc.GetJob(jobID)
		if err != nil {
			return errPayload(WsErrInternalError, err.Error()), nil
		}
		if job == nil {
			return errPayload(WsErrNotFound, "job not found"), nil
		}
		return map[string]any{"job": job}, nil
	}
}

// handleCronJobCreate 处理 cron.job.create。
// Python: _cron_job_create (L2076-2089)。异常 → BAD_REQUEST。
func handleCronJobCreate(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		job, err := cc.CreateJob(params)
		if err != nil {
			return errPayload(WsErrBadRequest, err.Error()), nil
		}
		return map[string]any{"job": job}, nil
	}
}

// handleCronJobUpdate 处理 cron.job.update。
// Python: _cron_job_update (L2091-2115)。KeyError → NOT_FOUND，其余 → BAD_REQUEST。
func handleCronJobUpdate(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		patch, _ := params["patch"].(map[string]any)
		job, err := cc.UpdateJob(jobID, patch)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				return errPayload(WsErrNotFound, "job not found"), nil
			}
			return errPayload(WsErrBadRequest, err.Error()), nil
		}
		return map[string]any{"job": job}, nil
	}
}

// handleCronJobDelete 处理 cron.job.delete。
// Python: _cron_job_delete (L2117-2136)。
func handleCronJobDelete(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string)any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		deleted, err := cc.DeleteJob(jobID)
		if err != nil {
			return errPayload(WsErrInternalError, err.Error()), nil
		}
		if !deleted {
			return errPayload(WsErrNotFound, "job not found"), nil
		}
		return map[string]any{"deleted": true}, nil
	}
}

// handleCronJobToggle 处理 cron.job.toggle。
// Python: _cron_job_toggle (L2138-2161)。enabled 必填。
func handleCronJobToggle(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		enabledRaw, hasEnabled := params["enabled"]
		if !hasEnabled || enabledRaw == nil {
			return errPayload(WsErrBadRequest, "enabled is required"), nil
		}
		enabled := false
		if b, ok := enabledRaw.(bool); ok {
			enabled = b
		}
		job, err := cc.ToggleJob(jobID, enabled)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				return errPayload(WsErrNotFound, "job not found"), nil
			}
			return errPayload(WsErrBadRequest, err.Error()), nil
		}
		return map[string]any{"job": job}, nil
	}
}

// handleCronJobPreview 处理 cron.job.preview。
// Python: _cron_job_preview (L2163-2180)。
func handleCronJobPreview(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		count := 5
		if v, ok := params["count"].(float64); ok {
			count = int(v)
		}
		rows, err := cc.PreviewJob(jobID, count)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				return errPayload(WsErrNotFound, "job not found"), nil
			}
			return errPayload(WsErrBadRequest, err.Error()), nil
		}
		return map[string]any{"next": rows}, nil
	}
}

// handleCronJobRunNow 处理 cron.job.run_now。
// Python: _cron_job_run_now (L2182-2198)。
func handleCronJobRunNow(cc CronJobController) RPCHandlerFunc {
	return func(ctx context.Context, params map[string]any, _ string) (map[string)any, error) {
		if params == nil {
			return errPayload(WsErrBadRequest, "params must be object"), nil
		}
		jobID := strings.TrimSpace(strVal(params["id"]))
		if jobID == "" {
			return errPayload(WsErrBadRequest, "id is required"), nil
		}
		runID, err := cc.RunNow(jobID)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				return errPayload(WsErrNotFound, "job not found"), nil
			}
			return errPayload(WsErrInternalError, err.Error()), nil
		}
		return map[string]any{"run_id": runID}, nil
	}
}

// errPayload 构造错误响应。
func errPayload(code, msg string) map[string]any {
	return map[string]any{"ok": false, "error": msg, "code": code}
}

// strVal 安全取字符串。
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
```

注意：
- Python 的 `cron.job.enable`/`cron.job.disable`/`cron.job.run`/`cron.job.status` 桩（Go 现有）在 Python 中实际不存在（Python 注册的是 get/toggle/preview/run_now）——**对齐 Python：注册方法名以 Python 为准**，Go 现有的 enable/disable/run/status 桩保留但标注为前端兼容（保留注册避免前端 404），或删除。查 Python app_web_handlers.py L2263-2270：注册了 list/get/create/update/delete/toggle/preview/run_now 8 个。Go 现有桩是 enable/disable/run/status——**与 Python 方法名不一致，应对齐替换为 get/toggle/preview/run_now**（保留 list/create/update/delete 不变）。
- Python `strVal`/`errPayload` 若 web 包已有同名需改名
- `handleCronJobDelete` 中笔误 `map[string)any` → `map[string]any`（实际写时修正）
- RPCHandlerFunc 返回 `(map[string]any, error)`，错误用 payload 内 ok=false 表达（对齐现有 handleMemoryForbiddenSet 模式）

- [ ] **Step 3: 写测试（扩展现有 web_handlers 测试模式）**

fake CronJobController 注入 + 8 端点各断言（list 正常/get 缺 id/create 异常/update not found/delete 未找到/toggle 缺 enabled/preview count 解析/run_now 成功）。测试注册到 dispatcher 后用 Dispatch 调用。

- [ ] **Step 4: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/channel_manager/web/ && go test ./internal/swarm/gateway/channel_manager/web/
```

提交：`feat(cron): wire 8 cron.job.* web endpoints to real CronController`

---

## Task 11: deep_adapter.go 回填（类型升级 + 步骤9绑定 + cancel×2）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_test.go`（扩展）

- [ ] **Step 1: cronRuntime 类型升级（deep_adapter.go:217-219）**

```go
	// cronRuntime Cron 运行时桥接（11.10 已回填）
	// Python: self._cron_runtime = CronRuntimeBridge()
	cronRuntime *cronbridge.CronRuntimeBridge
```

import 添加 `cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"`。

初始化（构造函数中，对齐 Python L538）：

```go
	d.cronRuntime = cronbridge.NewCronRuntimeBridge()
```

- [ ] **Step 2: 步骤 9 cron 上下文绑定（非流式 852 + 流式 1052 两处）**

对齐 Python `_bind_runtime_cron_context`（interface_deep.py L4459/4662）。Go 用 ctx values：

在 cronbridge 包补上下文绑定函数（也可放 deep_adapter 内联）：

```go
// BindRuntimeCronContext 绑定运行时 cron 上下文（对齐 Python _bind_runtime_cron_context）。
// 4 个 ContextVar → ctx values：channelID 归一化默认 web、sessionID、metadata 注入 request_id、mode。
// Go ctx 不可变传递，reset 语义天然满足（无需显式 reset）。
func BindRuntimeCronContext(ctx context.Context, channelID, sessionID string, metadata map[string]any, requestID, mode string) context.Context {
	normalizedChannel := strings.TrimSpace(channelID)
	if normalizedChannel == "" {
		normalizedChannel = gwcron.TargetChannelWeb
	}
	normalizedMode := ""
	if strings.TrimSpace(mode) != "" {
		normalizedMode = mode
	}
	normalizedMetadata := map[string]any{}
	if metadata != nil {
		for k, v := range metadata {
			normalizedMetadata[k] = v
		}
	}
	if strings.TrimSpace(requestID) != "" {
		normalizedMetadata["request_id"] = strings.TrimSpace(requestID)
	}
	return context.WithValue(ctx, runtimeCronCtxKey, &RuntimeCronContext{
		ChannelID: normalizedChannel,
		SessionID: sessionID,
		Metadata:  normalizedMetadata,
		Mode:      normalizedMode,
	})
}

// RuntimeCronContext 运行时 cron 上下文（ctx values 载体）。
type RuntimeCronContext struct {
	ChannelID string
	SessionID string
	Metadata  map[string]any
	Mode      string
}
```

deep_adapter 步骤 9 两处替换：

```go
	// 步骤 9: cron 上下文绑定（11.10 已回填）
	// Python: cron_context_tokens = _bind_runtime_cron_context(channel_id=, session_id=, metadata=, request_id=, mode=)
	ctx = cronbridge.BindRuntimeCronContext(ctx, req.ChannelID, sessionID, req.Metadata, req.RequestID, mode)
```

finally 块（步骤 20）注释更新：Go ctx 不可变值模式，无需 reset（保留 unmarkSessionActive）。

- [ ] **Step 3: cancelSchedulerRunningTasks（1402 + 1643 两处）**

对齐 Python `_cancel_scheduler_running_tasks`（interface_deep.py L3478-3506）——取消 agentcore TaskScheduler 的 runningTasks（与 cron 调度器无关，取消 in-flight LLM HTTP）：

TaskScheduler 需补导出方法（task_scheduler.go）：

```go
// CancelRunningTasks 取消全部运行中任务（interrupt/abort 场景用）。
// 对齐 Python: _cancel_scheduler_running_tasks 遍历 _running_tasks 逐个 cancel。
// 返回实际取消数。
func (s *TaskScheduler) CancelRunningTasks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := 0
	for _, entry := range s.runningTasks {
		if entry != nil && entry.cancel != nil {
			entry.cancel()
			cancelled++
		}
	}
	return cancelled
}
```

deep_adapter 添加私有方法：

```go
// cancelSchedulerRunningTasks 取消 TaskScheduler 运行中的任务。
// 对齐 Python: _cancel_scheduler_running_tasks (interface_deep.py L3478-3506)。
// 协作式中断（rail.abort + instance.abort）只在检查点生效，
// in-flight LLM HTTP 需要在 await 点直接注入取消才能立即中止。
func (d *DeepAdapter) cancelSchedulerRunningTasks() {
	if d.instance == nil {
		return
	}
	controller := d.instance.LoopController()
	if controller == nil {
		return
	}
	scheduler := controller.TaskScheduler()
	if scheduler == nil {
		return
	}
	cancelled := scheduler.CancelRunningTasks()
	if cancelled > 0 {
		logger.Info(logComponent).
			Int("cancelled", cancelled).
			Msg("[DeepAdapter] interrupt: cancelled running TaskScheduler tasks")
	}
}
```

注意：`controller.ControllerInterface.TaskScheduler()` 返回类型需确认是 `*modules.TaskScheduler`（controller.go:431 确认返回 `*modules.TaskScheduler`），ControllerInterface 是否暴露该方法需 grep。若接口未暴露，加接口方法或用类型断言。

1402 处（interrupt cancel 分支）：

```go
		// 11.10 已回填：取消调度器运行中任务
		d.cancelSchedulerRunningTasks()
```

1643 处（AbortOnGatewayDisconnect 步骤 3）：

```go
	// 步骤 3: 取消调度器任务（11.10 已回填）
	d.cancelSchedulerRunningTasks()
```

- [ ] **Step 4: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/ && go test ./internal/swarm/server/adapter/ -run TestInterrupt -v
```

提交：`feat(cron): backfill deep_adapter cron context binding and task cancellation`

---

## Task 12: team_manager 注册点 + app_gateway 装配

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_spec.go`
- Modify: `internal/swarm/gateway/app_gateway.go`
- Modify: `internal/swarm/server/agent_server.go`（如需 bridge 全局注入）

- [ ] **Step 1: RegisterMemberRuntimeTools cron 部分回填（team_manager_spec.go:269）**

对齐 Python team_manager.py L492-515。替换桩：

```go
	// 11.10 已回填：CronRuntimeBridge 工具注册
	// Python: cron_runtime.build_tools(context=...) → Runner.resource_mgr.add_tool → agent.ability_manager.add
	cronCtx := &agentcron.CronToolContext{
		ChannelID: derefString(channelID),
		SessionID: sessionID,
		Metadata: map[string]any{
			"request_id": derefString(requestID),
		},
		Mode: "",
	}
	if requestMetadata != nil {
		for k, v := range requestMetadata {
			cronCtx.Metadata[k] = v
		}
	}
	if bridge := m.GetCronRuntimeBridge(); bridge != nil {
		tools := bridge.BuildTools(cronCtx, agentID, "cn")
		registeredCount := 0
		for _, cronTool := range tools {
			// 对齐 Python: if not Runner.resource_mgr.get_tool(cron_tool.card.id)
			card := cronTool.Card()
			if _, gerr := runner.GetResourceMgr().GetTool([]string{card.ID}); gerr != nil || !toolAlreadyRegistered(agent, card.ID) {
				if aerr := runner.GetResourceMgr().AddTool(cronTool); aerr != nil {
					logger.Warn(logComponent).Err(aerr).Str("tool_id", card.ID).
						Msg("[TeamManager] cron tool add to resource manager failed")
					continue
				}
			}
			agent.AbilityManager().Add(card)
			registeredCount++
		}
		logger.Info(logComponent).
			Str("agent_id", agentID).
			Int("registered", registeredCount).
			Msg("[TeamManager] cron tools registered")
	}
```

TeamManager 需补 `GetCronRuntimeBridge()`（返回共享 bridge 实例或惰性构造）。agentID 从 agent.Card().ID 取（函数开头已有）。`toolAlreadyRegistered` 辅助遍历 abilityManager.List()。`derefString` 简单解引用。

- [ ] **Step 2: app_gateway.go 装配（:158 TODO 回填）**

对齐 Python app_gateway.py L770-1070 装配段 + L1615 start + L1736 stop。替换 Start 中的 TODO：

```go
	// 11.10 已回填：CronJobStore + CronSchedulerService + CronController 装配
	// Python: app_gateway.py L858-865, L1615
	cronStore := gwcron.NewCronJobStore(path.CronJobsPath())
	cronScheduler := gwcron.NewCronSchedulerService(cronStore, agentClient, msgHandler, nil)
	// config loader 注入（pushToTargets 渠道元数据路由用）
	if s.config != nil {
		cronScheduler.SetConfigLoader(func() map[string]any {
			data, err := s.config.Raw()
			if err != nil {
				return nil
			}
			return data
		})
	}
	gwcron.SetProcessScheduler(cronScheduler) // 方案 B 注册
	cronController := gwcron.NewCronController(cronStore, cronScheduler)
	s.msgHandler.SetCronController(cronController) // handleCronPushPayload 路由用
	if err := cronScheduler.Start(ctx); err != nil {
		logger.Error(logComponent).Err(err).Msg("failed to start cron scheduler")
	}
	s.cronScheduler = cronScheduler

	// bridge 全局注入（wake 用户消息发布 + E2A push + config）
	// Go 无 MessageHandler 单例，用全局注入（SetDefaultSendPushFunc 同款先例）
	cronbridge.SetGlobalUserPublisher(s.msgHandler)     // PublishUserMessagesNowait 隐式满足
	cronbridge.SetGlobalConfigLoader(func() map[string]any {
		if s.config == nil {
			return nil
		}
		data, err := s.config.Raw()
		if err != nil {
			return nil
		}
		return data
	})
	// E2A push：经 agentClient 的 server_push 通道（若 sendPushFunc 不可用则 nil）
```

AppGatewayServer 结构体补字段：

```go
	// cronScheduler cron 调度器（11.10）
	cronScheduler *gwcron.CronSchedulerService
```

Stop() 中追加（对齐 Python L1736 `await cron_scheduler.stop()`）：

```go
	if s.cronScheduler != nil {
		s.cronScheduler.Stop()
	}
```

WebHandlersBindParams.CronController 注入（步骤 5 注册处）：

```go
	web.RegisterWebHandlers(&web.WebHandlersBindParams{
		Channel:        wc,
		AgentClient:    agentClient,
		ChannelManager: channelMgr,
		OnConfigSaved:  onConfigSavedCb,
		CryptoProvider: crypto.GetCryptoProvider(),
		CronController: cronController, // 11.10 注入
	})
```

注意装配顺序：cronController 构造需在 RegisterWebHandlers 之前——把 cron 装配段从 Start 移到 NewAppGatewayServer（构造时装配 controller，Start 时启 scheduler）更贴近 Python（Python 构造段 L858-865 在 _run 内、start 在 L1615）。实际安排：NewAppGatewayServer 中构造 store/scheduler/controller + SetProcessScheduler + SetCronController + 注入 WebHandlersBindParams；Start 中 scheduler.Start(ctx)。

E2A push（cronbridge SetGlobalGatewayPushFunc）：Gateway 侧进程内 bridge 无 server_push 通道（bridge 主要在 AgentServer 进程用）——**gateway 装配时注入 nil 或一个经 agentClient 发送的 shim**。Python 中 AgentServer 侧 WebSocketGatewayPushTransport 直连 Gateway WS。Go 单体形态下 AgentServer 的 sendPushFunc 已有；**bridge 的 gatewayPush 由 AgentServer 侧注入**（agent_server.go）：

```go
	// agent_server.go 装配段（对齐 Python WebSocketGatewayPushTransport 注入）
	cronbridge.SetGlobalGatewayPushFunc(func(ctx context.Context, payload map[string]any) error {
		return s.SendPush(ctx, payload)
	})
```

（SendPush 已存在于 agent_server.go:116 先例）

- [ ] **Step 3: 验证 + 提交**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/ ./internal/swarm/server/ ./internal/swarm/agents/... && go test ./internal/swarm/gateway/ ./internal/swarm/agents/harness/team/
```

提交：`feat(cron): assemble cron services in gateway and register member runtime tools`

---

## Task 13: 全量回归 + 覆盖率 + 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量回归（CI 同款构建标签）**

```bash
cd /home/opensource/uapclaw-gateway && export CGO_ENABLED=1 && go test -tags "sqlite_fts5 test" ./... 2>&1 | tail -30
```

- [ ] **Step 2: 覆盖率检查（≥85%）**

```bash
cd /home/opensource/uapclaw-gateway && export CGO_ENABLED=1 && go test -tags "sqlite_fts5 test" -coverprofile=coverage.out ./internal/swarm/gateway/cron/ ./internal/swarm/agents/harness/common/tools/cron/ && go tool cover -func=coverage.out | tail -5
```

未达标补测试（设计文档第七节测试策略矩阵）。

- [ ] **Step 3: IMPLEMENTATION_PLAN.md 状态更新**

11.10 行（:740）：

```markdown
| 11.10 | ✅ | Cron 调度服务 | go-quartz v0.15.2（croniter→Quartz dow 变换）；CronJobStore tmp+rename 原子写；CronSchedulerService 事件堆三分支（wake/push/push_update）+方案B委托降级单调度器（进程注册表修正 Python 单进程双跑缺陷）；MessagePublisher 接口破环；9渠道元数据全量对齐；CronController+8个cron.job.*端点+handleCronPushPayload；Agent侧CronTools双写+E2A转发+cronToolsBackend 11方法；deep_adapter步骤9绑定+TaskScheduler取消×2；team_manager注册点；app_gateway装配 | `jiuwenswarm/gateway/cron/` |
```

10.6.24 行备注追加：`cron 工具集已随 11.10 实现回填（team_manager 注册点已接）`（其余工具保留 ⤵️）。

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add IMPLEMENTATION_PLAN.md && git commit -m "docs: mark 11.10 cron scheduler complete"
```

---

## 自检清单（实施者完成全部任务后执行）

1. **spec 覆盖**：对照设计文档第四节交付物清单逐项核对（5+3 新建文件 + 9 修改文件）
2. **⤵️ 处置**：对照设计文档第五节 10 处回填点表核对（#1 A2X 保留不动；#2-#10 全回填）
3. **编译**：`go build ./...` 零错误
4. **测试**：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全绿
5. **覆盖率**：gateway/cron 与 agents/.../tools/cron 两包 ≥85%
6. **依赖方向**：gateway/cron 不 import message_handler（接口破环）；gateway 包不 import server/；agents/.../tools/cron import gateway/cron 镜像 Python
7. **日志对齐**：Python logger 调用点在 Go 等价位置有结构化日志（日志文案英文，注释中文——对齐既有 feedback）
8. **品牌映射**：~/.uapclaw 路径（非 jiuwenswarm）；日志中无中文品牌残留
