package runtime

import (
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInputs 构建 adapter 所需的 inputs 字典。
//
// 对齐 Python: JiuWenClaw._build_inputs(request) -> (inputs, memoryMode, rawQuery)
//
// 返回: inputs 字典、memoryMode 字符串、原始 query。
func (uc *UapClaw) BuildInputs(request *schema.AgentRequest) (map[string]any, string, string) {
	// 1. 获取配置
	var configBase map[string]any
	if cfg, err := config.New(""); err == nil {
		if raw, err2 := cfg.Load(); err2 == nil {
			configBase = raw
		}
	}

	memoryMode := ""
	if configBase != nil {
		if mm, ok := configBase["memory_mode"]; ok {
			if mmStr, ok := mm.(string); ok {
				memoryMode = mmStr
			}
		}
	}

	// 2. 解析 params
	params := parseRequestParams(request)

	// 3. 提取基础字段
	query, _ := params["query"].(string)
	channel := extractChannelFromSessionID(request)
	// 对齐 Python: language = resolve_language(config_base.get("preferred_language", "zh"))
	// 使用 prompts.ResolveLanguage 标准化，对齐 DeepAdapter.resolveRuntimeLanguage
	rawLang := "zh"
	if configBase != nil {
		if lang, ok := configBase["preferred_language"]; ok {
			if langStr, ok := lang.(string); ok && langStr != "" {
				rawLang = langStr
			}
		}
	}
	language := prompts.ResolveLanguage(rawLang)

	// 4. 提取 trusted_dirs
	var trustedDirs []string
	if rawDirs, ok := params["trusted_dirs"]; ok {
		if dirsSlice, ok := rawDirs.([]any); ok {
			for _, d := range dirsSlice {
				if dirStr, ok := d.(string); ok && strings.TrimSpace(dirStr) != "" {
					trustedDirs = append(trustedDirs, strings.TrimSpace(dirStr))
				}
			}
		}
	}

	// 5. 提取 project_dir / cwd
	metadata := request.Metadata
	projectDir := extractStringWithFallback(params, "project_dir", metadata, "project_dir")
	cwd := extractStringWithFallback(params, "cwd", metadata, "cwd")

	// 6. 构建 finalQuery
	var finalQuery any
	// ⤵️ 10.3.2: InteractiveInput 类型判断（当前 query 为 string，直接走 BuildUserPrompt）
	// ⤵️ 10.3.2: answers 分支 → 构建 InteractiveInput（当前 stub，fallback 到 BuildUserPrompt）

	files, _ := params["files"].(map[string]any)
	finalQuery = BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)

	// 对齐 Python：interaction_context 存在时记录 debug 日志
	if metadata != nil {
		if ctx, ok := metadata["interaction_context"]; ok {
			if ctxStr, ok := ctx.(string); ok && strings.TrimSpace(ctxStr) != "" {
				truncated := query
				if len(truncated) > 2000 {
					truncated = truncated[:2000]
				}
				logger.Info(logComponent).
					Str("event_type", "build_inputs_debug").
					Str("query", truncated).
					Msg("[_build_inputs][DEBUG] interaction_context 存在")
			}
		}
	}

	// 7. 组装 inputs 字典
	sessionIDStr := ""
	if request.SessionID != nil {
		sessionIDStr = *request.SessionID
	}
	inputs := map[string]any{
		"conversation_id": sessionIDStr,
		"query":           finalQuery,
		"channel":         channel,
		"language":        language,
	}

	// 是否启用记忆
	enableMemory := true
	if metadata != nil {
		if em, ok := metadata["enable_memory"]; ok {
			if emBool, ok := em.(bool); ok {
				enableMemory = emBool
			}
		}
	}
	inputs["enable_memory"] = enableMemory

	// 可选字段
	if len(trustedDirs) > 0 {
		inputs["trusted_dirs"] = trustedDirs
	}
	if projectDir != "" {
		inputs["project_dir"] = projectDir
	}
	if cwd != "" {
		inputs["cwd"] = cwd
	}

	// run 字段
	if run, ok := params["run"]; ok {
		inputs["run"] = run
	}

	// cron 字段转换
	if cron, ok := params["cron"]; ok {
		inputs["run"] = map[string]any{
			"kind":    "cron",
			"context": map[string]any{"extra": map[string]any{"cron": cron}},
		}
	}

	return inputs, memoryMode, query
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseRequestParams 解析 AgentRequest.Params（json.RawMessage）为 map。
func parseRequestParams(request *schema.AgentRequest) map[string]any {
	if len(request.Params) == 0 {
		return make(map[string]any)
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return make(map[string]any)
	}
	return params
}

// extractChannelFromSessionID 从 sessionID 提取 channel（第一个 _ 前部分）。
func extractChannelFromSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil && *request.SessionID != "" {
		parts := strings.SplitN(*request.SessionID, "_", 2)
		if parts[0] != "" {
			return parts[0]
		}
	}
	return "web"
}

// extractStringWithFallback 从 params 和 metadata 提取字符串，params 优先。
func extractStringWithFallback(params map[string]any, paramKey string, metadata map[string]any, metaKey string) string {
	// params 优先
	if val, ok := params[paramKey]; ok {
		if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
			return strings.TrimSpace(str)
		}
	}
	// metadata 兜底
	if metadata != nil {
		if val, ok := metadata[metaKey]; ok {
			if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}
