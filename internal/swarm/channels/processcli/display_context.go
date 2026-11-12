package processcli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 本文件是 channels/process_cli/display_context.py 的 Go 对应物。
//
// Python: channels/process_cli/display_context.py（260 行）
//
// 职责：REPL 外壳的轻量展示元数据——不启动 Runtime 地推断「下一个 worker
// 会用哪个模型」，以及 mode 的展示态换算。
//
// 关键语义：模型名推断只是 UI 预览，不验证凭证；Runtime 可能选到更靠后的
// 可用条目（见 display_context.py:100-104 注释）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// unconfiguredModelName 未配置模型时的展示文案
	// Python: display_context.py:22 _UNCONFIGURED_MODEL
	unconfiguredModelName = "未配置"
	// configFileName 配置文件名
	configFileName = "config.yaml"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// envPattern 匹配 ${VAR} 与 ${VAR:-default} 两种展开。
// Python: display_context.py:19 _ENV_PATTERN
var envPattern = regexp.MustCompile(`\$\{([^:}]+)(?::-([^}]*))?\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveConfiguredModelName 不启动 Runtime 地推断下一个 worker 的模型名。
// Python: display_context.py:170-200 resolve_configured_model_name
//
// 查找链：UAPCLAW_CONFIG_YAML → UAPCLAW_CONFIG_DIR → UAPCLAW_DATA_DIR →
// ~/.uapclaw → 包内 resources/config.yaml；全部未命中回落 MODEL_NAME 环境变量。
func ResolveConfiguredModelName() string {
	shellFallback := strings.TrimSpace(os.Getenv("MODEL_NAME"))
	for _, configFile := range configFileCandidates() {
		info, err := os.Stat(configFile)
		if err != nil || info.IsDir() {
			continue
		}
		dotenv := dotenvValues(configFile)
		dotenvFallback := strings.TrimSpace(dotenv["MODEL_NAME"])

		raw, readErr := os.ReadFile(configFile)
		if readErr != nil {
			return firstNonEmptyText(dotenvFallback, shellFallback, unconfiguredModelName)
		}
		var loaded map[string]any
		if unmarshalErr := yaml.Unmarshal(raw, &loaded); unmarshalErr != nil || loaded == nil {
			return firstNonEmptyText(dotenvFallback, shellFallback, unconfiguredModelName)
		}

		if selected := selectConfiguredModelName(modelEntries(loaded), dotenv); selected != "" {
			return selected
		}
		legacy := legacyModelName(loaded, dotenv)
		return firstNonEmptyText(legacy, dotenvFallback, shellFallback, unconfiguredModelName)
	}
	return firstNonEmptyText(shellFallback, unconfiguredModelName)
}

// ResolveDisplayMode 返回 Runtime 等价 mode 的紧凑展示形式（去掉 .normal 后缀）。
// Python: display_context.py:254-261 resolve_display_mode
func ResolveDisplayMode(mode, workMode string) string {
	resolved := swschema.ResolveRequestMode(
		map[string]any{"mode": mode, "work_mode": workMode},
		resolveCliModeLegacy,
		workMode,
	)
	canonical := swschema.DeprecateMode(resolved.CanonicalMode)
	return strings.TrimSuffix(canonical, ".normal")
}

// ResolveCliWorkMode 让 Process CLI 的 work_mode 与自带 profile 的 mode 对齐。
// Python: display_context.py:262-283 resolve_cli_work_mode
//
// 历史可组合值（agent / team）继续采用会话 work_mode 状态；新三段模式与
// 历史 code-profile 模式自描述，内嵌 profile 优先。
func ResolveCliWorkMode(mode, workMode any) string {
	modeText := swschema.CanonicalizeModeText(modeTextOf(mode))
	parts := strings.Split(modeText, ".")
	if len(parts) >= 2 && (parts[0] == "agent" || parts[0] == "team") &&
		(parts[1] == "code" || parts[1] == "work") {
		return parts[1]
	}
	switch modeText {
	case "code", "code.normal", "code.plan", "code.team", "team.plan.code":
		return "code"
	}
	normalized := strings.ToLower(strings.TrimSpace(modeTextOf(workMode)))
	if normalized == "code" || normalized == "work" {
		return normalized
	}
	return "code"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// configFileCandidates 返回配置文件候选路径（按优先级）。
// Python: display_context.py:24-50 _config_file_candidates
func configFileCandidates() []string {
	candidates := make([]string, 0, 4)
	explicitConfigDir := strings.TrimSpace(os.Getenv("UAPCLAW_CONFIG_DIR"))
	if explicitConfigDir != "" {
		explicit := expandHome(explicitConfigDir)
		if strings.HasSuffix(explicit, ".yaml") || strings.HasSuffix(explicit, ".yml") {
			candidates = append(candidates, explicit)
		} else {
			candidates = append(candidates, filepath.Join(explicit, configFileName))
		}
	} else if dataDir := strings.TrimSpace(os.Getenv("UAPCLAW_DATA_DIR")); dataDir != "" {
		candidates = append(candidates, filepath.Join(expandHome(dataDir), "config", configFileName))
	} else {
		home := strings.TrimSpace(os.Getenv("UAPCLAW_HOME"))
		if home == "" {
			if userHome, err := os.UserHomeDir(); err == nil {
				home = userHome
			}
		}
		if home != "" {
			candidates = append(candidates, filepath.Join(expandHome(home), ".uapclaw", "config", configFileName))
		}
	}
	// 显式 UAPCLAW_CONFIG_YAML 优先（config 包 readPermissionsConfigFile 同源）
	if explicit := strings.TrimSpace(os.Getenv("UAPCLAW_CONFIG_YAML")); explicit != "" {
		candidates = append([]string{expandHome(explicit)}, candidates...)
	}
	return dedupeStrings(candidates)
}

// dotenvValues 读取配置文件同目录的 .env（缺失或解析失败返回空 map）。
// Python: display_context.py:53-73 _dotenv_values
func dotenvValues(configFile string) map[string]string {
	envFile := filepath.Join(filepath.Dir(configFile), ".env")
	info, err := os.Stat(envFile)
	if err != nil || info.IsDir() {
		return map[string]string{}
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		return map[string]string{}
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values
}

// resolveEnvText 展开 ${VAR} 与 ${VAR:-default}，结果去空白。
// Python: display_context.py:76-93 _resolve_env_text
func resolveEnvText(value any, dotenv map[string]string) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	resolved := envPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envPattern.FindStringSubmatch(match)
		name := groups[1]
		hasDefault := strings.Contains(match, ":-")
		current, exists := dotenv[name]
		if !exists {
			current = os.Getenv(name)
		}
		if hasDefault && current == "" {
			return groups[2]
		}
		return current
	})
	return strings.TrimSpace(resolved)
}

// selectConfiguredModelName 从 models.defaults 取第一个可解析的 model_name。
// Python: display_context.py:96-121 select_configured_model_name
//
// 意图是轻量 UI 预览：不构造模型客户端，也不校验凭证。
func selectConfiguredModelName(entries []any, dotenv map[string]string) string {
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		mcc, ok := mapping["model_client_config"].(map[string]any)
		if !ok {
			continue
		}
		if name := resolveEnvText(mcc["model_name"], dotenv); name != "" {
			return name
		}
	}
	return ""
}

// modelEntries 取 models.defaults（列表）或 models.default（单个）。
// Python: display_context.py:124-135 _model_entries
func modelEntries(config map[string]any) []any {
	models, ok := config["models"].(map[string]any)
	if !ok {
		return nil
	}
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		return defaults
	}
	if single, ok := models["default"].(map[string]any); ok {
		return []any{single}
	}
	return nil
}

// legacyModelName 读旧版 react.model_client_config.model_name / react.model_name。
// Python: display_context.py:138-160 _legacy_model_name
func legacyModelName(config map[string]any, dotenv map[string]string) string {
	react, ok := config["react"].(map[string]any)
	if !ok {
		return ""
	}
	if mcc, ok := react["model_client_config"].(map[string]any); ok {
		if name := resolveEnvText(mcc["model_name"], dotenv); name != "" {
			return name
		}
	}
	return resolveEnvText(react["model_name"], dotenv)
}

// resolveCliModeLegacy 镜像 Runtime 的无传输 legacy mode 归一，供展示用。
// Python: display_context.py:206-250 _resolve_cli_mode
func resolveCliModeLegacy(mode, workMode string) (string, string, string) {
	modeText := swschema.CanonicalizeModeText(mode)
	if m, s, c, ok := swschema.ResolveNewCanonicalModeText(modeText); ok {
		return m, s, c
	}
	normalizedWork := strings.ToLower(strings.TrimSpace(workMode))

	if modeText == "plan" || modeText == "fast" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	if modeText == "team.plan.normal" {
		return "team", "plan", "team.plan.normal"
	}
	if modeText == "team.plan.code" {
		return "code", "team", "team.plan.code"
	}

	parts := strings.Split(modeText, ".")
	managerMode := parts[0]
	if managerMode == "" {
		managerMode = "agent"
	}
	if managerMode == "agent" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	if managerMode == "team" {
		return "team", "", "team"
	}
	subMode := ""
	if len(parts) > 1 && parts[1] != "" {
		subMode = parts[1]
	} else if managerMode == "code" {
		subMode = "normal"
	}
	if managerMode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	canonical := managerMode
	if subMode != "" {
		canonical = managerMode + "." + subMode
	}
	if canonical == "agent" || canonical == "code" || canonical == "code.normal" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		if normalizedWork == "work" {
			return "agent", "", "agent"
		}
	}
	return managerMode, subMode, canonical
}

// modeTextOf 取 mode 的字符串形式（非字符串按空串处理）。
func modeTextOf(value any) string {
	s, _ := value.(string)
	return s
}

// firstNonEmptyText 返回第一个非空字符串。
func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// dedupeStrings 去重并保序。
func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// expandHome 展开路径开头的 ~。
// Python: display_context.py 中 Path(...).expanduser()
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}
