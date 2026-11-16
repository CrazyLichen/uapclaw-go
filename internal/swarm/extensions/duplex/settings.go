// Package duplex 提供全双工音视频旁路通道（领域 23.9）。
//
// 一比一移植 Python jiuwenswarm/extensions/video_duplex/：实时音视频问答走
// WebSocket 代理直连 vLLM Omni / JoyAI 实时模型（不进 Agent）；需要真干活的
// 请求经工具调用转持久任务单，由标准 CHAT_SEND 走普通 AgentServer 管线
// （channel_id="video_tool"）。装配层、配置面板、ASR/TTS、搜索供给、遥测
// 落盘与任务账本均在本包内。
//
// 文件目录：
//
//	duplex/
//	├── doc.go                  # 包文档
//	├── settings.go            # 配置持久化与密钥打码（settings.py）
//	├── video_files.go         # 文件资源归一（video_files.py）
//	├── connection_check.go    # 连通性探测（connection_check.py）
//	├── extension.go           # video-duplex 插件装配（extension.py）
//	├── video_voice.go         # ASR/TTS 适配（video_voice.py）
//	├── video_search.go        # 核心 Agent 执行与搜索任务（video_search.py）
//	├── qwen_omni_tools.go     # vLLM-Omni 工具协议（qwen_omni_tools.py）
//	├── qwen_omni_gateway.go  # vLLM-Omni WS 代理（qwen_omni_gateway.py）
//	├── joyai_provider.go      # JoyAI 实时 provider（joyai_provider.py）
//	├── video_live.go          # 实时 RPC 编排与遥测（video_live.py）
//	├── task_adapter.go        # Agent 执行器适配（task_adapter.py）
//	├── conversation_context.go # 会话上下文同步（conversation_context.py）
//	└── tasks/                # 持久任务管理（backend/tasks/ + bridge.py）
//
// 对应 Python 代码：jiuwenswarm/extensions/video_duplex/
// （tasks/bridge.go 对应 gateway/extensions/video_duplex/backend/tasks/bridge.py，
// 归属偏差 O-1 见 spec）
package duplex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutils "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// settingEnvKeys 配置键 → 环境变量映射。
// 对齐 settings.py:14-30 SETTING_ENV_KEYS。
var settingEnvKeys = map[string]string{
	"joyai_api_base":         "JOYAI_API_BASE",
	"joyai_api_key":          "JOYAI_API_KEY",
	"joyai_model":            "JOYAI_MODEL_NAME",
	"qwen_omni_realtime_url": "QWEN_OMNI_REALTIME_URL",
	"qwen_omni_api_key":      "QWEN_OMNI_API_KEY",
	"qwen_omni_model":        "QWEN_OMNI_MODEL_NAME",
	"qwen_omni_voice":        "QWEN_OMNI_VOICE",
	"reply_language":         "VIDEO_DUPLEX_REPLY_LANGUAGE",
	"voice_protocol":         "VOICE_PROTOCOL",
	"voice_asr_endpoint":     "VOICE_ASR_ENDPOINT",
	"voice_tts_endpoint":     "VOICE_TTS_ENDPOINT",
	"voice_api_key":          "VOICE_API_KEY",
	"voice_asr_model":        "VOICE_ASR_MODEL",
	"voice_tts_model":        "VOICE_TTS_MODEL",
	"voice_tts_voice":        "VOICE_TTS_VOICE",
}

// secretSettings secret 类配置键集合。
// 对齐 settings.py:31 SECRET_SETTINGS。
var secretSettings = map[string]bool{
	"joyai_api_key":    true,
	"qwen_omni_api_key": true,
	"voice_api_key":    true,
}

// allowedReplyLanguages 合法回复语言集合。
// 对齐 settings.py:32 ALLOWED_REPLY_LANGUAGES。
var allowedReplyLanguages = map[string]bool{"match": true, "zh-CN": true, "en": true}

// settingKeysOrder settings 的固定键序（Go map 无序，序列化保序用）。
// Python 侧无对应实现：Python dict 保插入序，Go 用切片固定与 settings.py:14-30 相同的顺序。
var settingKeysOrder = []string{
	"joyai_api_base", "joyai_api_key", "joyai_model",
	"qwen_omni_realtime_url", "qwen_omni_api_key", "qwen_omni_model",
	"qwen_omni_voice", "reply_language", "voice_protocol",
	"voice_asr_endpoint", "voice_tts_endpoint", "voice_api_key",
	"voice_asr_model", "voice_tts_model", "voice_tts_voice",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// envFileOverride 测试注入口：覆盖 .env 文件路径。
// Python 侧无对应实现：Python 经 get_parsed_dotenv/get_env_file 间接定位，
// Go 单测需注入临时路径。
var envFileOverride string

// ──────────────────────────── 导出函数 ────────────────────────────

// SettingsPayload 返回配置面板载荷：默认值 + 已配置值，secret 打码记录长度。
// 对齐 settings.py:92 settings_payload。
func SettingsPayload(enabled bool) map[string]any {
	values := defaultsSnapshot()
	values["video_live_provider"] = provider()
	configuredSecretLengths := map[string]int{}
	for _, key := range settingKeysOrder {
		envKey := settingEnvKeys[key]
		raw := os.Getenv(envKey)
		if raw == "" || strings.TrimSpace(raw) == "" {
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if secretSettings[key] {
			configuredSecretLengths[key] = len(trimmed)
			values[key] = ""
		} else {
			values[key] = trimmed
		}
	}
	if !allowedReplyLanguages[values["reply_language"]] {
		values["reply_language"] = "match"
	}
	return map[string]any{
		"enabled":                   enabled,
		"values":                    values,
		"configured_secret_lengths": configuredSecretLengths,
		"restart_required":          false,
	}
}

// UpdateSettings 校验并持久化配置更新；clear_secrets=true 时允许写空密钥。
// 对齐 settings.py:147 update_settings。
func UpdateSettings(values map[string]any, clearSecrets bool) error {
	normalized, err := validatedValues(values)
	if err != nil {
		return err
	}
	providerName, ok := normalized["video_live_provider"]
	if !ok {
		providerName = provider()
	}
	delete(normalized, "video_live_provider")
	updates := map[string]string{
		"VIDEO_LIVE_MODE":         "realtime",
		"VIDEO_REALTIME_PROVIDER": "qwen_omni",
	}
	if providerName != "qwen_omni" {
		updates["VIDEO_LIVE_MODE"] = "joyai"
		updates["VIDEO_REALTIME_PROVIDER"] = ""
	}
	for _, key := range settingKeysOrder {
		value, ok := normalized[key]
		if !ok {
			continue
		}
		if secretSettings[key] && value == "" && !clearSecrets {
			continue
		}
		updates[settingEnvKeys[key]] = value
	}
	for envKey, value := range updates {
		os.Setenv(envKey, value)
	}
	persistEnvUpdates(updates)
	return nil
}

// ReplyLanguage 返回合法的回复语言配置，非法回默认 match。
// 对齐 settings.py:162 reply_language。
func ReplyLanguage() string {
	raw := strings.TrimSpace(os.Getenv(settingEnvKeys["reply_language"]))
	if allowedReplyLanguages[raw] {
		return raw
	}
	return "match"
}

// ToolLanguage 从双工配置解析工具结果语言；match 时回退 preferred_language 配置。
// 对齐 settings.py:167 tool_language。
func ToolLanguage() string {
	selected := ReplyLanguage()
	if selected == "en" || selected == "zh-CN" {
		return selected
	}
	raw := strings.ToLower(strings.TrimSpace(preferredLanguage()))
	return map[bool]string{true: "en", false: "zh-CN"}[raw == "en" || strings.HasPrefix(raw, "en-")]
}

// SetEnabled 写入启用开关到进程环境与 .env 文件。
// 对齐 settings.py:180 set_enabled。
func SetEnabled(enabled bool) {
	value := "false"
	if enabled {
		value = "true"
	}
	os.Setenv("VIDEO_DUPLEX_ENABLED", value)
	persistEnvUpdates(map[string]string{"VIDEO_DUPLEX_ENABLED": value})
}

// NormalizeFileItems 保留原生的 Jiuwen 文件资源条目（跨双工事件桥）。
// 对齐 video_files.py:5 normalize_file_items。
func NormalizeFileItems(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	files := []map[string]any{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		stringsMap := map[string]string{}
		hasDownloadRoute := false
		for _, key := range []string{"name", "mime_type", "download_url", "download_token", "path"} {
			if s, isStr := m[key].(string); isStr {
				stringsMap[key] = strings.TrimSpace(s)
			} else {
				stringsMap[key] = ""
			}
		}
		for _, key := range []string{"download_url", "download_token", "path"} {
			if stringsMap[key] != "" {
				hasDownloadRoute = true
			}
		}
		if stringsMap["name"] == "" || !hasDownloadRoute {
			continue
		}
		size := 0
		if s, isInt := m["size"].(int64); isInt && s >= 0 {
			size = int(s)
		} else if s, isInt := m["size"].(int); isInt && s >= 0 {
			size = s
		}
		files = append(files, withSize(stringsMap, size))
	}
	return files
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// activeEnvFile 返回当前生效的 .env 文件路径。
// 对齐 settings.py:52 _active_env_file（get_parsed_dotenv() or get_env_file()）。
func activeEnvFile() string {
	if envFileOverride != "" {
		return envFileOverride
	}
	if parsed := dotenv.ParsedDotenv(); parsed != "" {
		return parsed
	}
	return pathutils.EnvFile()
}

// persistEnvUpdates 原子写回 .env：保留无关行、覆盖已知键、追加新键。
// 对齐 settings.py:56 _persist_env_updates（空值写 `KEY=` 行，值用 JSON 引号包裹）。
func persistEnvUpdates(updates map[string]string) {
	if len(updates) == 0 {
		return
	}
	envPath := activeEnvFile()
	var output []string
	remaining := make(map[string]bool, len(updates))
	for key := range updates {
		remaining[key] = true
	}
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			// 保持逐行结构：Python splitlines(keepends=True) 按行读，末尾空行
			// 由追加键自然补齐，此处 split 产生的尾空串行跳过。
			if line == "" {
				continue
			}
			matched := ""
			for key := range remaining {
				if strings.HasPrefix(line, key+"=") {
					matched = key
					break
				}
			}
			if matched == "" {
				output = append(output, line)
				continue
			}
			delete(remaining, matched)
			output = append(output, envLine(matched, updates[matched]))
		}
	}
	keys := make([]string, 0, len(remaining))
	for key := range remaining {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		output = append(output, envLine(key, updates[key]))
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Str("path", envPath).Msg("创建 .env 目录失败")
		return
	}
	temporary := envPath + ".tmp"
	if err := os.WriteFile(temporary, []byte(strings.Join(output, "\n")+"\n"), 0o644); err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Str("path", temporary).Msg("写 .env 临时文件失败")
		return
	}
	if err := os.Rename(temporary, envPath); err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Str("path", envPath).Msg("替换 .env 失败")
	}
}

// provider 解析实时 provider：realtime 模式且未指定或指定 qwen_omni 时返回 qwen_omni。
// 对齐 settings.py:86 _provider。
func provider() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VIDEO_LIVE_MODE")))
	if mode == "" {
		mode = "realtime"
	}
	realtimeProvider := strings.ToLower(strings.TrimSpace(os.Getenv("VIDEO_REALTIME_PROVIDER")))
	if mode == "realtime" && (realtimeProvider == "" || realtimeProvider == "qwen_omni") {
		return "qwen_omni"
	}
	return "joyai"
}

// validatedValues 校验并归一配置值；未知键/类型错误/非法枚举/超长均报错。
// 对齐 settings.py:115 _validated_values。
func validatedValues(values map[string]any) (map[string]string, error) {
	allowed := map[string]bool{"video_live_provider": true}
	for key := range settingEnvKeys {
		allowed[key] = true
	}
	unknown := []string{}
	for key := range values {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown full-duplex settings: %s", strings.Join(unknown, ", "))
	}
	normalized := map[string]string{}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, ok := values[key].(string)
		if !ok {
			return nil, fmt.Errorf("setting %q must be a string", key)
		}
		normalized[key] = strings.TrimSpace(value)
	}
	providerName, ok := normalized["video_live_provider"]
	if !ok {
		providerName = provider()
	}
	if providerName != "joyai" && providerName != "qwen_omni" {
		return nil, errors.New("video_live_provider must be joyai or qwen_omni")
	}
	protocol, ok := normalized["voice_protocol"]
	if !ok {
		protocol = strings.ToLower(strings.TrimSpace(os.Getenv("VOICE_PROTOCOL")))
		if protocol == "" {
			protocol = "openai_http"
		}
	}
	if protocol != "native_ws" && protocol != "openai_http" {
		return nil, errors.New("voice_protocol must be native_ws or openai_http")
	}
	if value, ok := normalized["reply_language"]; ok {
		if value == "" {
			normalized["reply_language"] = "match"
		} else if !allowedReplyLanguages[value] {
			return nil, errors.New("reply_language must be match, zh-CN, or en")
		}
	}
	for key, value := range normalized {
		if len(value) > 4096 {
			return nil, fmt.Errorf("setting %q is too long", key)
		}
	}
	return normalized, nil
}

// defaultsSnapshot 复制 DEFAULTS 快照。
// 对齐 settings.py:33-49 DEFAULTS。
func defaultsSnapshot() map[string]string {
	return map[string]string{
		"joyai_api_base": "", "joyai_api_key": "", "joyai_model": "",
		"qwen_omni_realtime_url": "", "qwen_omni_api_key": "", "qwen_omni_model": "",
		"qwen_omni_voice": "", "reply_language": "match", "voice_protocol": "openai_http",
		"voice_asr_endpoint": "", "voice_tts_endpoint": "", "voice_api_key": "",
		"voice_asr_model": "", "voice_tts_model": "", "voice_tts_voice": "",
	}
}

// envLine 生成单个 .env 行：非空值 JSON 引号包裹（ensure_ascii=False 语义）。
// 对齐 settings.py:74/:78。
func envLine(key, value string) string {
	if value == "" {
		return key + "="
	}
	return key + "=" + strconv.Quote(value)
}

// withSize 组装单条文件资源条目。
// Python 侧无对应实现：Python 用 dict 字面量拼接，Go 需显式构造保序输出。
func withSize(stringsMap map[string]string, size int) map[string]any {
	return map[string]any{
		"name":           stringsMap["name"],
		"mime_type":      stringsMap["mime_type"],
		"download_url":   stringsMap["download_url"],
		"download_token": stringsMap["download_token"],
		"path":           stringsMap["path"],
		"size":           size,
	}
}

// preferredLanguage 读取全局 preferred_language 配置。
// 对齐 settings.py:173 get_config().get("preferred_language")；异常时回 "zh"。
// Python 侧无对应 config 直读（Go config 包在 batch1 阶段以环境变量桥接，
// 批 3 接入 config 包时更新此注释）。
func preferredLanguage() string {
	if raw := os.Getenv("UAPCLAW_PREFERRED_LANGUAGE"); raw != "" {
		return raw
	}
	return "zh"
}
