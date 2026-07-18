package browser_move

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserProfile 持久化的浏览器配置元数据。
//
// 对齐 Python: BrowserProfile (profiles.py L15-46)
type BrowserProfile struct {
	// Name 配置名称
	Name string `json:"name"`
	// DriverType 驱动类型（默认 "remote"）
	DriverType string `json:"driver_type"`
	// CDPURL Chrome DevTools Protocol URL
	CDPURL string `json:"cdp_url"`
	// BrowserBinary 浏览器可执行文件路径
	BrowserBinary string `json:"browser_binary"`
	// UserDataDir 用户数据目录
	UserDataDir string `json:"user_data_dir"`
	// DebugPort 调试端口
	DebugPort int `json:"debug_port"`
	// Host 主机地址
	Host string `json:"host"`
	// ExtraArgs 额外启动参数
	ExtraArgs []string `json:"extra_args"`
}

// BrowserProfileStore JSON 后端配置存储，支持选中配置追踪。
//
// 对齐 Python: BrowserProfileStore (profiles.py L49-136)
type BrowserProfileStore struct {
	// path 存储文件路径
	path string
	// profiles 配置映射
	profiles map[string]*BrowserProfile
	// selected 选中配置名称
	selected string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserProfileFromDict 从字典创建 BrowserProfile。
//
// 对齐 Python: BrowserProfile.from_dict
func NewBrowserProfileFromDict(raw map[string]any) *BrowserProfile {
	debugPort := 0
	if rawPort := raw["debug_port"]; rawPort != nil {
		switch v := rawPort.(type) {
		case int:
			debugPort = v
		case int64:
			debugPort = int(v)
		case float64:
			debugPort = int(v)
		default:
			// 忽略无法解析的 debug_port 类型
			_ = fmt.Sprintf("%v", rawPort)
		}
	}

	driverType := "remote"
	if v, ok := raw["driver_type"]; ok && v != nil {
		s := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", v)))
		if s != "" {
			driverType = s
		}
	}

	host := "127.0.0.1"
	if v, ok := raw["host"]; ok && v != nil {
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s != "" {
			host = s
		}
	}

	var extraArgs []string
	if v, ok := raw["extra_args"]; ok && v != nil {
		if arr, ok := v.([]any); ok {
			for _, item := range arr {
				s := strings.TrimSpace(fmt.Sprintf("%v", item))
				if s != "" {
					extraArgs = append(extraArgs, s)
				}
			}
		}
	}

	return &BrowserProfile{
		Name:          strValOrEmpty(raw["name"]),
		DriverType:    driverType,
		CDPURL:        strValOrEmpty(raw["cdp_url"]),
		BrowserBinary: strValOrEmpty(raw["browser_binary"]),
		UserDataDir:   strValOrEmpty(raw["user_data_dir"]),
		DebugPort:     debugPort,
		Host:          host,
		ExtraArgs:     extraArgs,
	}
}

// ToDict 将 BrowserProfile 转换为字典。
//
// 对齐 Python: BrowserProfile.to_dict
func (p *BrowserProfile) ToDict() map[string]any {
	return map[string]any{
		"name":           p.Name,
		"driver_type":    p.DriverType,
		"cdp_url":        p.CDPURL,
		"browser_binary": p.BrowserBinary,
		"user_data_dir":  p.UserDataDir,
		"debug_port":     p.DebugPort,
		"host":           p.Host,
		"extra_args":     p.ExtraArgs,
	}
}

// NewBrowserProfileStore 创建 JSON 后端配置存储。
//
// 对齐 Python: BrowserProfileStore.__init__
func NewBrowserProfileStore(path string) *BrowserProfileStore {
	store := &BrowserProfileStore{
		path:     expandHome(path),
		profiles: make(map[string]*BrowserProfile),
		selected: "",
	}
	store.load()
	return store
}

// Path 返回存储文件路径。
func (s *BrowserProfileStore) Path() string {
	return s.path
}

// Save 将配置持久化到 JSON 文件。
//
// 对齐 Python: BrowserProfileStore.save
func (s *BrowserProfileStore) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	payload := map[string]any{
		"selected_profile": s.selected,
		"profiles":         s.sortedProfileDicts(),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// ListProfiles 返回按名称排序的配置列表。
//
// 对齐 Python: BrowserProfileStore.list_profiles
func (s *BrowserProfileStore) ListProfiles() []*BrowserProfile {
	return s.sortedProfiles()
}

// GetProfile 根据名称获取配置。
//
// 对齐 Python: BrowserProfileStore.get_profile
func (s *BrowserProfileStore) GetProfile(name string) *BrowserProfile {
	key := strings.TrimSpace(name)
	if key == "" {
		return nil
	}
	return s.profiles[key]
}

// UpsertProfile 插入或更新配置，可选设为选中。
//
// 对齐 Python: BrowserProfileStore.upsert_profile
func (s *BrowserProfileStore) UpsertProfile(profile *BrowserProfile, selectProfile bool) (*BrowserProfile, error) {
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		return nil, fmt.Errorf("profile.name is required")
	}
	profile.Name = name
	driverType := strings.TrimSpace(strings.ToLower(profile.DriverType))
	if driverType == "" {
		driverType = "remote"
	}
	profile.DriverType = driverType

	s.profiles[name] = profile
	if selectProfile {
		s.selected = name
	} else if s.selected != "" {
		if _, exists := s.profiles[s.selected]; !exists {
			s.selected = ""
		}
	}

	if err := s.Save(); err != nil {
		return profile, err
	}
	return profile, nil
}

// RemoveProfile 移除配置。
//
// 对齐 Python: BrowserProfileStore.remove_profile
func (s *BrowserProfileStore) RemoveProfile(name string) bool {
	key := strings.TrimSpace(name)
	if key == "" {
		return false
	}
	if _, exists := s.profiles[key]; !exists {
		return false
	}
	delete(s.profiles, key)
	if s.selected == key {
		s.selected = ""
	}
	_ = s.Save()
	return true
}

// SelectProfile 选中指定配置。
//
// 对齐 Python: BrowserProfileStore.select_profile
func (s *BrowserProfileStore) SelectProfile(name string) (*BrowserProfile, error) {
	key := strings.TrimSpace(name)
	profile, exists := s.profiles[key]
	if !exists {
		return nil, fmt.Errorf("profile not found: %s", key)
	}
	s.selected = key
	_ = s.Save()
	return profile, nil
}

// SelectedName 返回选中配置名称。
//
// 对齐 Python: BrowserProfileStore.selected_name
func (s *BrowserProfileStore) SelectedName() string {
	return s.selected
}

// SelectedProfile 返回选中配置。
//
// 对齐 Python: BrowserProfileStore.selected_profile
func (s *BrowserProfileStore) SelectedProfile() *BrowserProfile {
	if s.selected == "" {
		return nil
	}
	return s.profiles[s.selected]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// load 从 JSON 文件加载配置。
//
// 对齐 Python: BrowserProfileStore._load
func (s *BrowserProfileStore) load() {
	s.profiles = make(map[string]*BrowserProfile)
	s.selected = ""

	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	if _, ok := payload["selected_profile"]; ok {
		s.selected = strings.TrimSpace(fmt.Sprintf("%v", payload["selected_profile"]))
	}

	profilesRaw, ok := payload["profiles"]
	if !ok || profilesRaw == nil {
		return
	}
	profilesList, ok := profilesRaw.([]any)
	if !ok {
		return
	}

	for _, item := range profilesList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		profile := NewBrowserProfileFromDict(itemMap)
		if profile.Name == "" {
			continue
		}
		s.profiles[profile.Name] = profile
	}
}

// sortedProfiles 返回按名称排序的配置列表。
func (s *BrowserProfileStore) sortedProfiles() []*BrowserProfile {
	result := make([]*BrowserProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// sortedProfileDicts 返回按名称排序的配置字典列表。
func (s *BrowserProfileStore) sortedProfileDicts() []map[string]any {
	profiles := s.sortedProfiles()
	result := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		result = append(result, p.ToDict())
	}
	return result
}

// expandHome 展开 ~ 为用户主目录。
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// strValOrEmpty 从 any 值取字符串，nil 或 "<nil>" 返回空字符串。
func strValOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "<nil>" {
		return ""
	}
	return s
}
