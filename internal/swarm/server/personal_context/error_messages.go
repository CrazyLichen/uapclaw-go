// 本文件对齐 Python jiuwenswarm/server/personal_context/error_messages.py（201 行，一比一）。
// 公开错误本地化；内部诊断永不成为展示文案。

package personalcontext

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HostError Host 侧错误载体（D-24.3-2：Python 为 Core PersonalContext.Error（host_api.py:228-253
// 经 _status_for_name 构造），Core 未迁暂驻本包；24.3a Core 落地时对齐状态码归属并回收本偏差）。
type HostError struct {
	// StatusName 状态名（Python error.status.name，如 CONTEXT_PROACTIVE_CONFIG_INVALID）
	StatusName string
	// Code 数字码（Python error.code，如 154000；框架级为 BAD_REQUEST）
	Code string
	// Msg 错误消息（Python msg）
	Msg string
	// Reason 显式 reason 标识（Python pcs_reason）
	Reason string
	// Cause 原始原因（Python cause）
	Cause error
}

// Error 实现 error 接口：线格式带 "[<code>] " 前缀，
// 与 Python BaseError.__str__ 的 "^\[(\d+)\] " 解析契约一致（error_messages.py:146-148）。
func (e *HostError) Error() string {
	code := e.Code
	if code == "" {
		code = "0"
	}
	if e.Cause != nil {
		return "[" + code + "] " + e.Msg + ": " + e.Cause.Error()
	}
	return "[" + code + "] " + e.Msg
}

// Unwrap 暴露原因链（对应 Python cause 链）
func (e *HostError) Unwrap() error {
	return e.Cause
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 本包日志组件（Python _LOGGER = logging.getLogger(__name__)，error_messages.py:11）
const logComponent = logger.ComponentAgentServer

// messages 错误文案表（error_messages.py:12-101 _MESSAGES：reason 键 → (中文, 英文)）
var messages = map[string][2]string{
	"config_invalid": {
		"PersonalContext 配置无效，请检查来源、时间范围和采集参数。",
		"Invalid PersonalContext configuration. Check the source, time range and collection settings.",
	},
	"state_invalid": {
		"当前状态无法执行此操作，请等待任务结束后重试。",
		"The current state does not allow this operation. Wait for the task to finish and retry.",
	},
	"file_error": {
		"文件读取失败，请检查文件是否存在以及访问权限。",
		"Could not read the file. Check that it exists and that access is allowed.",
	},
	"fetch_error": {
		"数据来源采集失败，请检查来源地址、网络和授权后重试。",
		"Could not collect from the source. Check its address, network and authorization, then retry.",
	},
	"pipeline_error": {
		"内容处理失败，请检查模型配置和可用性后重试。",
		"Content processing failed. Check the model configuration and availability, then retry.",
	},
	"publish_error": {
		"内容写入失败，请检查磁盘空间和目录写入权限后重试。",
		"Could not write the content. Check disk space and directory permissions, then retry.",
	},
	"timeout": {
		"操作超时，请等待当前任务结束后刷新状态并重试。",
		"The operation timed out. Wait for the current task to finish, refresh the status and retry.",
	},
	"unknown": {
		"PersonalContext 操作失败，请重试；若仍失败，请查看服务日志。",
		"PersonalContext operation failed. Please retry; if it fails again, check the service logs.",
	},
	"parameter_invalid": {
		"请求参数无效，请检查输入内容后重试。",
		"Invalid request parameter. Check the input and retry.",
	},
	"positive_integer": {
		"采集间隔和每次采集上限必须为正整数。",
		"The collection interval and item limit must be positive integers.",
	},
	"duplicate_service": {
		"来源名称已存在，请使用其他名称。",
		"The source name already exists. Use a different name.",
	},
	"not_configured": {
		"PersonalContext 尚未配置，请先开启总开关。",
		"PersonalContext is not configured. Turn on the master switch first.",
	},
	"collection_disabled": {
		"采集已关闭，请开启采集开关后重试。",
		"PersonalContext collection is disabled. Turn on collection and retry.",
	},
	"unknown_service": {
		"来源不存在，请刷新来源列表后重试。",
		"The source does not exist. Refresh the source list and retry.",
	},
	"service_running": {
		"采集任务正在执行，请停止任务或等待结束后重试。",
		"A collection task is running. Stop it or wait for it to finish, then retry.",
	},
	"service_limit": {
		"同一数据来源最多可添加 20 项，请删除不再使用的来源后重试。",
		"Each provider supports at most 20 sources. Remove an unused source and retry.",
	},
	"model_invalid": {
		"所选模型不可用，请重新选择已配置的模型。",
		"The selected model is unavailable. Select a configured model again.",
	},
	"authorization_required": {
		"请先完成数据来源授权，再创建采集来源。",
		"Authorize the provider before creating a collection source.",
	},
	"authorization_failed": {
		"数据来源授权失败，请检查凭据和网络后重新授权。",
		"Provider authorization failed. Check the credentials and network, then authorize again.",
	},
	"authorization_unsupported": {
		"该数据来源不支持此授权方式，请检查来源类型。",
		"This provider does not support this authorization method. Check the provider type.",
	},
	"credentials_invalid": {
		"授权参数无效，请检查数据来源和所需凭据。",
		"Invalid authorization parameters. Check the provider and required credentials.",
	},
	"config_file_error": {
		"配置文件读写失败，请检查文件格式、磁盘空间和访问权限。",
		"Could not read or write the configuration file. Check its format, disk space and access permissions.",
	},
}

// codeReasons 数字码 → reason（error_messages.py:102-111 _CODE_REASONS）
var codeReasons = map[string]string{
	"154000":      "config_invalid",
	"154001":      "state_invalid",
	"154002":      "file_error",
	"154003":      "fetch_error",
	"154004":      "pipeline_error",
	"154005":      "publish_error",
	"154006":      "timeout",
	"BAD_REQUEST": "parameter_invalid",
}

// statusReasons 状态名 → reason（error_messages.py:112-133 _STATUS_REASONS，zip 构造展开为直写）
var statusReasons = map[string]string{
	"CONTEXT_PROACTIVE_CONFIG_INVALID":           "config_invalid",
	"CONTEXT_PROACTIVE_STATE_INVALID":            "state_invalid",
	"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR":     "file_error",
	"CONTEXT_PROACTIVE_FETCH_EXECUTION_ERROR":    "fetch_error",
	"CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR": "pipeline_error",
	"CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR":  "publish_error",
	"CONTEXT_PROACTIVE_RUNTIME_TIMEOUT":          "timeout",
}

// wireCodePattern BaseError.__str__ 使用的固定数字前缀（error_messages.py:147）
var wireCodePattern = regexp.MustCompile(`^\[(\d+)\] `)

// ──────────────────────────── 导出函数 ────────────────────────────

// LocalizeError 只解析显式 reason 标识与稳定的 Core status/code 值
// （Python error_messages.py:136-152 localize_error）。
// value 支持：map[string]any（含 code/status 键）、*HostError、字符串（含 "[154000] " 线格式前缀）、
// 其他 error（按其 Error() 字符串走线格式解析）。
func LocalizeError(value any, language string, reason string) string {
	var code, status string
	switch v := value.(type) {
	case map[string]any:
		// Python isinstance(error, Mapping) 分支（error_messages.py:138-140）
		code = asString(v["code"])
		status = asString(v["status"])
	case *HostError:
		// Python getattr 分支（error_messages.py:141-144）
		code = v.Code
		status = v.StatusName
		if reason == "" {
			reason = v.Reason
		}
	case string:
		// Python isinstance(error, str) 分支（error_messages.py:145-148）
		if m := wireCodePattern.FindStringSubmatch(v); m != nil {
			code = m[1]
		}
	default:
		if err, ok := value.(error); ok {
			if m := wireCodePattern.FindStringSubmatch(err.Error()); m != nil {
				code = m[1]
			}
		}
	}
	key := reason
	if key == "" {
		key = codeReasons[code]
	}
	if key == "" {
		key = statusReasons[status]
	}
	if _, ok := messages[key]; !ok {
		key = "unknown"
	}
	if language == "en" {
		return messages[key][1]
	}
	return messages[key][0]
}

// LocalizePayload 拷贝有界 status/history/authorization 投影并只翻译错误
// （Python error_messages.py:155-178 localize_payload）。
func LocalizePayload(value any, language string) any {
	switch v := value.(type) {
	case []any:
		// Python isinstance(value, list) 分支（error_messages.py:157-158）
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = LocalizePayload(item, language)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if key == "fetch_service_errors" {
				if sub, ok := item.(map[string]any); ok {
					// Python error_messages.py:163-167
					translated := make(map[string]any, len(sub))
					for service, errValue := range sub {
						translated[service] = LocalizeError(errValue, language, "")
					}
					result[key] = translated
					continue
				}
			}
			if (key == "last_error" || key == "error") && !isEmptyValue(item) {
				// Python error_messages.py:168-173
				if sub, ok := item.(map[string]any); ok {
					copied := make(map[string]any, len(sub))
					for k, val := range sub {
						copied[k] = val
					}
					copied["message"] = LocalizeError(item, language, "")
					result[key] = copied
					continue
				}
				result[key] = LocalizeError(item, language, "")
				continue
			}
			if key == "message" {
				if _, hasCode := v["code"]; hasCode {
					// Python error_messages.py:174-175
					result[key] = LocalizeError(v, language, "")
					continue
				}
			}
			result[key] = LocalizePayload(item, language)
		}
		return result
	default:
		// Python 非 Mapping 原样返回（error_messages.py:159-160）
		return value
	}
}

// LogError 保留脱敏诊断，不泄露凭据或携带凭据的原因链
// （Python error_messages.py:181-201 log_error）。
func LogError(err error) {
	message := credentialPattern1.ReplaceAllString(err.Error(), "[REDACTED]")
	message = strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " ")
	message = bearerPattern.ReplaceAllString(message, "[REDACTED]")
	message = credentialPattern2.ReplaceAllString(message, "[REDACTED]")
	message = urlPattern.ReplaceAllString(message, "[URL REDACTED]")
	logger.Warn(logComponent).Str("error_type", errorTypeName(err)).Str("message", truncate(message, 512)).Msg("PersonalContext error")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// credentialPattern1 authorization/cookie 头脱敏（error_messages.py:185-189：
// 头部值可含空格（Basic auth）与分号（cookies），丢弃该诊断行剩余部分而非泄露第二个值）
var credentialPattern1 = regexp.MustCompile(`(?i)["']?(?:authorization|cookie)["']?\s*[:=][^\r\n]*`)

// bearerPattern Bearer 令牌脱敏（error_messages.py:190）
var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`)

// credentialPattern2 通用凭据键值脱敏（error_messages.py:191-197，verbose 模式展开）
var credentialPattern2 = regexp.MustCompile(`(?i)["']?(?:api[_\ -]?key|(?:access[_\ -]?|refresh[_\ -]?)?token|pat|secret|password|authorization|cookie|credentials?)["']?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)

// urlPattern URL 脱敏（error_messages.py:198）
var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

// asString 宽松字符串化：None/缺失返回空串（对应 Python getattr 链的 None 语义）
func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// isEmptyValue 对应 Python `and item` 真值判断（error_messages.py:168）
func isEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

// errorTypeName 取错误动态类型名（对应 Python type(error).__name__，error_messages.py:200）
func errorTypeName(err error) string {
	if err == nil {
		return "None"
	}
	t := fmt.Sprintf("%T", err)
	// 去掉包路径前缀，仅保留类型名
	if idx := strings.LastIndexByte(t, '.'); idx >= 0 {
		t = t[idx+1:]
	}
	return t
}

// truncate 按字符截断（对应 Python message[:512]，error_messages.py:200）
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
