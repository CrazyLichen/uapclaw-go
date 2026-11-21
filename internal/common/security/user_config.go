package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultSensitivePathStr 默认敏感路径串（Python DEFAULT_SENSITIVE_PATH_STR 逐字复制）
const defaultSensitivePathStr = "/etc/passwd,/etc/shadow,/etc/hosts,/etc/hostname,/etc/ssh/," +
	"C:\\Windows\\System32\\,C:\\Windows\\SysWOW64\\,C:\\Windows\\System\\"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultSensitivePaths 默认敏感路径清单（Python DEFAULT_SENSITIVE_PATHS 逐字复制）
	defaultSensitivePaths = []string{"/etc/passwd", "/etc/shadow", "/etc/hosts", "/etc/hostname", "/etc/ssh/",
		"C:\\Windows\\System32\\", "C:\\Windows\\SysWOW64\\", "C:\\Windows\\System\\"}
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserConfig 用户配置单例（Python user_config.py UserConfig 一比一）。
// Python 类方法（get_config/is_sensitive/get_sensitive_paths/set_is_sensitive）→ Go
// 包级函数；实例字段（is_sensitive/_sensitive_paths）→ 结构体导出/非导出字段。
// 偏差 D-18-2：Python threading.Lock 双检 → Go sync.Once + 包级实例。
type UserConfig struct {
	// IsSensitive 敏感模式开关（Python self.is_sensitive）
	IsSensitive bool
	// settings 节配置（仅 settings 节，configparser 等价最小面）
	settings map[string]string
	// sensitivePathsCache 懒解析缓存（Python self._sensitive_paths，nil=未解析）
	sensitivePathsCache []string
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// userConfigInstance 单例实例（Python UserConfig._instance）
	userConfigInstance *UserConfig
	// userConfigOnce 单例惰性初始化（Python _lock + 双检）
	userConfigOnce sync.Once
	// userConfigPath 预设配置路径（Python cls._user_path）
	userConfigPath string
	// userConfigPathSet 配置路径是否已预设
	userConfigPathSet bool
	// userConfigPathMu 预设路径读写保护（Python 类级 _lock 承担面）
	userConfigPathMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetConfigPath 预设配置文件路径（Python set_config_path）。
// 单例已初始化时报 188001 "config already initialized"。
func SetConfigPath(path string) error {
	userConfigPathMu.Lock()
	defer userConfigPathMu.Unlock()
	if userConfigInstance != nil {
		return exception.BuildError(exception.StatusCommonUserConfigProcessError,
			exception.WithMsg("config already initialized"))
	}
	resolved, err := resolveAndCheck(path)
	if err != nil {
		return err
	}
	userConfigPath = resolved
	userConfigPathSet = true
	return nil
}

// GetUserConfig 返回单例（Python get_config：双检锁 → Go sync.Once）。
func GetUserConfig() *UserConfig {
	userConfigOnce.Do(func() {
		userConfigPathMu.Lock()
		path := userConfigPath
		userConfigPathMu.Unlock()
		userConfigInstance = NewUserConfig(path)
	})
	return userConfigInstance
}

// IsSensitive 敏感模式判定（Python is_sensitive 类方法一比一）：
// 先查环境变量 IS_SENSITIVE=="false" 直接短路返回 False，否则查单例配置。
func IsSensitive() bool {
	// Python: os.getenv("IS_SENSITIVE", "true").lower() == "false"
	if strings.ToLower(os.Getenv("IS_SENSITIVE")) == "false" {
		return false
	}
	return GetUserConfig().IsSensitive
}

// GetSensitivePaths 敏感路径清单（Python get_sensitive_paths 类方法）。
func GetSensitivePaths() []string {
	return GetUserConfig().GetSensitivePathsList()
}

// NewUserConfig 构造实例（Python __init__：configparser 读 INI，缺失/异常回落 DEFAULT）。
// configPath 为空串 ≙ Python config_path=None（落默认 settings）。
func NewUserConfig(configPath string) *UserConfig {
	uc := &UserConfig{settings: map[string]string{
		"is_sensitive":    "True",
		"sensitive_paths": defaultSensitivePathStr,
	}}
	// Python: config_path 存在且是文件才尝试读取；读取异常 → read_dict(DEFAULT) 回落
	if configPath != "" {
		if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
			parsed, perr := parseIniSettings(configPath)
			if perr != nil {
				// Python except Exception → read_dict(DEFAULT)：保留默认 settings
			} else {
				uc.settings = parsed
			}
		}
	}
	// Python: self._cfg.getboolean("settings", "is_sensitive")
	uc.IsSensitive = getBooleanSetting(uc.settings, "is_sensitive", true)
	uc.sensitivePathsCache = nil
	return uc
}

// ──────────────────────────── UserConfig 方法 ────────────────────────────

// SetIsSensitive 运行期翻转敏感开关（Python set_is_sensitive）。
func (uc *UserConfig) SetIsSensitive(isSensitive bool) {
	uc.IsSensitive = isSensitive
}

// GetSensitivePathsList 敏感路径清单懒解析（Python get_sensitive_paths_list 一比一：
// 首次解析 settings.sensitive_paths 逗号拆分去空格，空串回落默认清单，缓存后返回副本）。
func (uc *UserConfig) GetSensitivePathsList() []string {
	if uc.sensitivePathsCache == nil {
		raw := uc.settings["sensitive_paths"]
		if raw != "" {
			list := []string{}
			for _, part := range strings.Split(raw, ",") {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					list = append(list, trimmed)
				}
			}
			uc.sensitivePathsCache = list
		} else {
			uc.sensitivePathsCache = append([]string(nil), defaultSensitivePaths...)
		}
	}
	// Python return self._sensitive_paths.copy()
	return append([]string(nil), uc.sensitivePathsCache...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveAndCheck 路径解析与根目录校验（Python _resolve_and_check 一比一：
// expandvars/expanduser/resolve → 必须位于当前工作目录之内，否则 188001）。
func resolveAndCheck(path string) (string, error) {
	// Python: os.path.expandvars(path.expanduser()).resolve()
	expanded := os.ExpandEnv(path)
	if expanded != "" && expanded[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = filepath.Join(home, expanded[1:])
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", exception.BuildError(exception.StatusCommonUserConfigProcessError,
			exception.WithMsg("config file must inside root"))
	}
	// Python: path.relative_to(Path.cwd()) 失败即报错
	cwd, _ := os.Getwd()
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", exception.BuildError(exception.StatusCommonUserConfigProcessError,
			exception.WithMsg("config file must inside root"))
	}
	return abs, nil
}

// parseIniSettings 最小 configparser 等价：解析 [settings] 节 key = value。
// Python configparser.read(encoding="utf-8") 等价面；非 settings 节忽略。
func parseIniSettings(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	settings := map[string]string{}
	inSettings := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// configparser 默认忽略空行与 #/; 注释行
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSettings = strings.TrimSpace(line[1:len(line)-1]) == "settings"
			continue
		}
		if !inSettings {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			// configparser 对无分隔符行抛 MissingSectionHeaderError/解析异常 → 回落默认
			return nil, fmt.Errorf("invalid ini line: %s", line)
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		settings[key] = value
	}
	if len(settings) == 0 {
		return nil, fmt.Errorf("empty settings section")
	}
	return settings, nil
}

// getBooleanSetting configparser.getboolean 等价（True/False/true/false/1/0/yes/no）。
// 解析失败时返回 fallback（Python getboolean 抛 ValueError → __init__ 无兜底，
// 此处回落默认并登记偏差 D-18-2 附注：Python 异常上抛 vs Go 回落）。
func getBooleanSetting(settings map[string]string, key string, fallback bool) bool {
	raw, ok := settings[key]
	if !ok {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		if _, err := strconv.ParseBool(raw); err != nil {
			return fallback
		}
		v, _ := strconv.ParseBool(raw)
		return v
	}
}
