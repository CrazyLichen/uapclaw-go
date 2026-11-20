package providers

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 枚举 ────────────────────────────

// providersError providers 包内共享错误类型。
type providersError string

func (e providersError) Error() string { return string(e) }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// errRuntimePromptInvariant projectless 分支内部不变量违反
	// （task_workspace_root 已设而 resolve_member_work_dir 返回空）。
	// Python: RuntimeError（member_rails.py:365-369，显式 raise 保全 -O 语义）
	errRuntimePromptInvariant = providersError("resolve_member_work_dir() returned None despite task_workspace_root being set")
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCronExecutionSession 定时任务执行会话判定。
//
// 覆盖两种 id 形态：`cron_*` 约定（主动 tick 会话 cron_<job_id> 与旧版团队
// 每次运行 cron_<ts>_<job_id>）与 `__cron___<ts>_<rand>`（CronScheduler
// 经内部 __cron__ 渠道分配）。cron 运行没有对端操作者，交互式中断无法
// 作答，不得挂载。
// Python: is_cron_execution_session (common/cron_session.py:8-17)
func isCronExecutionSession(sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	return strings.HasPrefix(sid, "cron") || strings.HasPrefix(sid, "__cron__")
}

// heartbeatUnsupportedChannels 无心跳支持渠道集合。
// Python: HEARTBEAT_UNSUPPORTED_CHANNELS (heartbeat_policy.py:10-12)
var heartbeatUnsupportedChannels = map[string]struct{}{"xiaoyi": {}}

// isHeartbeatSupported 渠道是否支持心跳任务；渠道未知保持原行为（支持）。
// Python: is_heartbeat_supported (heartbeat_policy.py:15-20)
func isHeartbeatSupported(channelID string) bool {
	channel := strings.ToLower(strings.TrimSpace(channelID))
	if channel == "" {
		return true
	}
	head := channel
	if idx := strings.Index(channel, ":"); idx >= 0 {
		head = channel[:idx]
	}
	_, unsupported := heartbeatUnsupportedChannels[head]
	return !unsupported
}

// isSkillRetrievalEnabled Skill 检索开关（config.skill_retrieval.enabled）。
// Python: is_skill_retrieval_enabled
// (common/tools/skill_retrieval_toolkits.py)——基座文件 ⤴️9.11b；
// 开关键位与 config_specs.go 既有 retrievalEnabled 读取逻辑同源。
func isSkillRetrievalEnabled(config map[string]any) bool {
	if config == nil {
		return false
	}
	retrieval, _ := config["skill_retrieval"].(map[string]any)
	enabled, _ := retrieval["enabled"].(bool)
	return enabled
}

// trimSpaceOrEmpty 去空白（空串安全）。
func trimSpaceOrEmpty(s string) string {
	return strings.TrimSpace(s)
}

// stringFieldOrEmpty 串字段，空则取默认（对齐 context_field default）。
func stringFieldOrEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// boolParamOr 布尔参数，缺失取默认（对齐 param_field default）。
func boolParamOr(params map[string]any, key string, fallback bool) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return fallback
}

// orEmpty 首个非空串（对齐 Python `a or b` 的串语义）。
func orEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// copyMapOrEmpty 元数据拷贝（nil 安全）。
func copyMapOrEmpty(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// mapKeyTruthy 元数据键真值判定（非零值即真，对齐 Python truthiness）。
func mapKeyTruthy(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch typed := v.(type) {
	case string:
		return typed != ""
	case bool:
		return typed
	default:
		return true
	}
}

// logHeartbeatSkip 心跳 rail 跳过日志。
// Python: logger.info("[swarm.heartbeat] skip ...")（member_rails.py:169-183）
func logHeartbeatSkip(reason, sessionID string) {
	logger.Info(logger.ComponentAgentServer).
		Str("reason", reason).
		Str("session_id", sessionID).
		Msg("[swarm.heartbeat] skip HeartbeatRail")
}

// logPersonalContextSkip 个人上下文 rail 可选构造失败日志。
// Python: logger.warning("[swarm.personal_context] optional Rail construction failed (%s)")（:209-210）
func logPersonalContextSkip(err error) {
	logger.Warn(logger.ComponentAgentServer).
		Err(err).
		Msg("[swarm.personal_context] optional Rail construction failed")
}
