package personal_context

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// Provider fetch provider 白名单（Literal 类型，值集合必须完整映射）。
// Python: config.py:25-34 Provider
const (
	ProviderLocalFiles       = "local_files"
	ProviderGithub           = "github"
	ProviderGitcode          = "gitcode"
	ProviderFeishu           = "feishu"
	ProviderBrowserBookmarks = "browser_bookmarks"
	ProviderZhihuReader      = "zhihu_reader"
	ProviderToutiaoReader    = "toutiao_reader"
	ProviderRssFeed          = "rss_feed"
)

// ──────────────────────────── 常量 ────────────────────────────

// safeSegmentPattern 安全路径段正则（复用 models.go）。
// Python: config.py:37 _SAFE_SEGMENT
// windowsSensitivePathPattern Windows 敏感路径正则。Python: config.py:38
var (
	windowsSensitivePathPattern = regexp.MustCompile(`(?i)^[a-z]:\\windows\\(?:system32|syswow64|system)(?:\\|$)`)
	rfc3339TimestampPattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$`)
)

// repositoryResources 仓库类资源白名单。Python: config.py:39 _REPOSITORY_RESOURCES
var repositoryResources = []string{"readme", "issues", "pull_requests", "commits", "code"}

// feishuResources 飞书资源白名单。Python: config.py:40 _FEISHU_RESOURCES
var feishuResources = []string{"docs", "tasks", "calendar"}

// DefaultMaxPagesPerDirectory 每目录默认最大页数。
// Python: config.py:46 DEFAULT_MAX_PAGES_PER_DIRECTORY（挂账③锚点：Host 改读此常量）
const DefaultMaxPagesPerDirectory = 20

// DefaultMaxSubdirectoriesPerDirectory 每目录默认最大子目录数。
// Python: config.py:47 DEFAULT_MAX_SUBDIRECTORIES_PER_DIRECTORY
const DefaultMaxSubdirectoriesPerDirectory = 20

// ──────────────────────────── 结构体 ────────────────────────────

// PersonalContextFetchServiceConfig 一个独立调度、固定 provider 的 fetch 服务。
// Python: config.py:323 PersonalContextFetchServiceConfig
// 偏差 D-24.3a-5：pydantic 校验收敛为构造函数显式调用，步骤顺序对齐。
type PersonalContextFetchServiceConfig struct {
	// ServiceID 服务标识
	ServiceID string `json:"service_id"`
	// Provider provider 白名单值
	Provider string `json:"provider"`
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// IntervalSeconds 调度间隔（秒）
	IntervalSeconds float64 `json:"interval_seconds"`
	// MaxItemsPerRun 单次 run 最大条目数（nil=不限）
	MaxItemsPerRun *int `json:"max_items_per_run"`
	// TimeRange 时间范围归一化结果
	TimeRange map[string]any `json:"time_range"`
	// Source 源归一化结果
	Source map[string]any `json:"source"`
	// Credentials 凭据（仓库类 token/pat，其余恒空）
	Credentials map[string]string `json:"credentials"`
}

// PersonalContextConfig PersonalContext 完整配置。
// Python: config.py:359 PersonalContextConfig
type PersonalContextConfig struct {
	// CollectionEnabled 是否启用采集
	CollectionEnabled bool `json:"collection_enabled"`
	// AgentUseEnabled 是否启用 agent 消费
	AgentUseEnabled bool `json:"agent_use_enabled"`
	// StrategyProfile 目录组织策略档（rules/balanced/agent）
	StrategyProfile string `json:"strategy_profile"`
	// MaxPagesPerDirectory 每目录最大页数
	MaxPagesPerDirectory int `json:"max_pages_per_directory"`
	// MaxSubdirectoriesPerDirectory 每目录最大子目录数
	MaxSubdirectoriesPerDirectory int `json:"max_subdirectories_per_directory"`
	// ModelClient 模型客户端配置（nil=无）
	ModelClient *llm.ModelClientConfig `json:"model_client"`
	// ModelRequest 模型请求配置（nil=无）
	ModelRequest *llm.ModelRequestConfig `json:"model_request"`
	// FetchServices fetch 服务列表（service_id 排序）
	FetchServices []PersonalContextFetchServiceConfig `json:"fetch_services"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigFromDict 校验并归一化纯字典配置。
// Python: config.py:384 PersonalContextConfig.from_dict —— 边界处统一转为
// CONTEXT_PROACTIVE_CONFIG_INVALID。
func ConfigFromDict(config map[string]any) (*PersonalContextConfig, error) {
	result, err := normalizePersonalConfig(config)
	if err != nil {
		return nil, BuildError(StatusCodeContextProactiveConfigInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("invalid PersonalContext configuration: %v", err)))
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizePersonalConfig 归一化链入口（before → 字段 → after 顺序对齐）。
// Python: config.py:411 normalize_input / 字段校验 / :424 validate_semantics
func normalizePersonalConfig(value map[string]any) (*PersonalContextConfig, error) {
	copied := deepCopyJSON(value)
	if raw, present := copied["fetch_services"]; present {
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("fetch_services must be a list")
		}
		normalized := make([]PersonalContextFetchServiceConfig, 0, len(list))
		for _, entry := range list {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("fetch_services entries must be objects")
			}
			service, err := normalizeFetchService(entryMap)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, *service)
		}
		// Python: sorted by service_id（normalize_input :417-421）
		sort.SliceStable(normalized, func(i, j int) bool {
			return normalized[i].ServiceID < normalized[j].ServiceID
		})
		config := &PersonalContextConfig{FetchServices: normalized}
		if err := decodePersonalConfigFields(copied, config); err != nil {
			return nil, err
		}
		if err := config.ValidateSemantics(); err != nil {
			return nil, err
		}
		return config, nil
	}
	config := &PersonalContextConfig{}
	if err := decodePersonalConfigFields(copied, config); err != nil {
		return nil, err
	}
	if err := config.ValidateSemantics(); err != nil {
		return nil, err
	}
	return config, nil
}

// decodePersonalConfigFields 解析顶层字段（声明顺序对齐）。
func decodePersonalConfigFields(value map[string]any, config *PersonalContextConfig) error {
	if raw, present := value["collection_enabled"]; present {
		checked, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("collection_enabled must be a boolean")
		}
		config.CollectionEnabled = checked
	}
	if raw, present := value["agent_use_enabled"]; present {
		checked, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("agent_use_enabled must be a boolean")
		}
		config.AgentUseEnabled = checked
	}
	profile, ok := value["strategy_profile"].(string)
	if !ok {
		return fmt.Errorf("strategy_profile must be a string")
	}
	switch profile {
	case "rules", "balanced", "agent":
		config.StrategyProfile = profile
	default:
		return fmt.Errorf("strategy_profile must be rules, balanced, or agent")
	}
	if raw, present := value["max_pages_per_directory"]; present && raw != nil {
		number, ok := raw.(float64)
		if !ok || number != float64(int(number)) || number < 1 || number > 100 {
			return fmt.Errorf("max_pages_per_directory must be an integer in [1, 100]")
		}
		config.MaxPagesPerDirectory = int(number)
	} else {
		config.MaxPagesPerDirectory = DefaultMaxPagesPerDirectory
	}
	if raw, present := value["max_subdirectories_per_directory"]; present && raw != nil {
		number, ok := raw.(float64)
		if !ok || number != float64(int(number)) || number < 2 || number > 100 {
			return fmt.Errorf("max_subdirectories_per_directory must be an integer in [2, 100]")
		}
		config.MaxSubdirectoriesPerDirectory = int(number)
	} else {
		config.MaxSubdirectoriesPerDirectory = DefaultMaxSubdirectoriesPerDirectory
	}
	// ModelClient/ModelRequest 由装配层经独立构造函数注入（frozen 语义），此处只做配对校验
	if (value["model_client"] != nil) != (value["model_request"] != nil) {
		return fmt.Errorf("model_client and model_request must be provided together")
	}
	return nil
}

// ValidateSemantics 语义校验（service_id 唯一、provider 上限、模型配对）。
// Python: config.py:424 validate_semantics（model_validator mode=after）
func (c *PersonalContextConfig) ValidateSemantics() error {
	serviceIDs := make([]string, 0, len(c.FetchServices))
	seen := map[string]struct{}{}
	providerCounts := map[string]int{}
	for _, service := range c.FetchServices {
		serviceIDs = append(serviceIDs, service.ServiceID)
		if _, duplicated := seen[service.ServiceID]; duplicated {
			return fmt.Errorf("fetch_services service_id values must be unique")
		}
		seen[service.ServiceID] = struct{}{}
		providerCounts[service.Provider]++
		if providerCounts[service.Provider] > 20 {
			return fmt.Errorf("%s may define at most 20 fetch services", service.Provider)
		}
	}
	hasClient := c.ModelClient != nil
	hasRequest := c.ModelRequest != nil
	if hasClient != hasRequest {
		return fmt.Errorf("model_client and model_request must be provided together")
	}
	if (c.StrategyProfile == "balanced" || c.StrategyProfile == "agent") && !(hasClient && hasRequest) {
		return fmt.Errorf("%s strategy requires model_client and model_request", c.StrategyProfile)
	}
	return nil
}

// normalizeFetchService 单服务归一化（before → service_id 校验）。
// Python: config.py:339 normalize_provider_payload / :355 validate_service_id
func normalizeFetchService(value map[string]any) (*PersonalContextFetchServiceConfig, error) {
	copied := deepCopyJSON(value)
	service := &PersonalContextFetchServiceConfig{}
	providerRaw, _ := copied["provider"].(string)
	provider := strings.ToLower(strings.TrimSpace(providerRaw))
	service.Provider = provider
	switch provider {
	case ProviderLocalFiles, ProviderGithub, ProviderGitcode, ProviderFeishu,
		ProviderBrowserBookmarks, ProviderZhihuReader, ProviderToutiaoReader, ProviderRssFeed:
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
	// time_range 归一化
	if raw, present := copied["time_range"]; present {
		normalized, err := normalizeTimeRange(raw)
		if err != nil {
			return nil, err
		}
		service.TimeRange = normalized
	} else {
		return nil, fmt.Errorf("time_range is required")
	}
	// source 归一化
	sourceRaw, present := copied["source"]
	if !present || sourceRaw == nil {
		sourceRaw = map[string]any{}
	}
	normalized, err := normalizeServiceSource(provider, sourceRaw)
	if err != nil {
		return nil, err
	}
	service.Source = normalized
	// credentials 归一化
	credentialsRaw, present := copied["credentials"]
	if !present || credentialsRaw == nil {
		credentialsRaw = map[string]any{}
	}
	credentials, err := normalizeCredentials(provider, credentialsRaw)
	if err != nil {
		return nil, err
	}
	service.Credentials = credentials
	// service_id 校验
	serviceID, err := ValidateServiceID(stringOrEmpty(copied["service_id"]))
	if err != nil {
		return nil, err
	}
	service.ServiceID = serviceID
	// enabled 必填布尔
	enabled, ok := copied["enabled"].(bool)
	if !ok {
		return nil, fmt.Errorf("enabled must be a boolean")
	}
	service.Enabled = enabled
	// interval_seconds 缺省 10800，(0, 31536000]
	if raw, present := copied["interval_seconds"]; present && raw != nil {
		number, ok := raw.(float64)
		if !ok || number <= 0 || number > 31_536_000 {
			return nil, fmt.Errorf("interval_seconds must be in (0, 31536000]")
		}
		service.IntervalSeconds = number
	} else {
		service.IntervalSeconds = 10_800.0
	}
	// max_items_per_run 缺省 nil，[1, 10000]
	if raw, present := copied["max_items_per_run"]; present && raw != nil {
		number, ok := raw.(float64)
		if !ok || number != float64(int(number)) || number < 1 || number > 10_000 {
			return nil, fmt.Errorf("max_items_per_run must be an integer in [1, 10000]")
		}
		value := int(number)
		service.MaxItemsPerRun = &value
	}
	return service, nil
}

// normalizeTimeRange 时间范围归一化。
// Python: config.py:85 _normalize_time_range
func normalizeTimeRange(value any) (map[string]any, error) {
	raw, err := mappingValue(value, "time_range")
	if err != nil {
		return nil, err
	}
	mode, _ := raw["mode"].(string)
	switch mode {
	case "all":
		if len(raw) != 1 {
			return nil, fmt.Errorf("all time_range must contain only mode")
		}
		return map[string]any{"mode": "all"}, nil
	case "recent":
		if len(raw) != 2 {
			return nil, fmt.Errorf("recent time_range must contain only mode and recent_days")
		}
		days, ok := raw["recent_days"].(float64)
		if !ok || days != float64(int(days)) || days <= 0 {
			return nil, fmt.Errorf("recent_days must be a positive integer")
		}
		return map[string]any{"mode": "recent", "recent_days": int(days)}, nil
	case "fixed":
		if len(raw) != 3 {
			return nil, fmt.Errorf("fixed time_range must contain only mode, start_at, and end_at")
		}
		startAt, startValue, err := normalizeRFC3339(raw["start_at"], "start_at")
		if err != nil {
			return nil, err
		}
		endAt, endValue, err := normalizeRFC3339(raw["end_at"], "end_at")
		if err != nil {
			return nil, err
		}
		if !startValue.Before(endValue) {
			return nil, fmt.Errorf("fixed time_range start_at must be before end_at")
		}
		return map[string]any{"mode": "fixed", "start_at": startAt, "end_at": endAt}, nil
	}
	return nil, fmt.Errorf("time_range mode must be all, recent, or fixed")
}

// normalizeRFC3339 RFC3339 时间戳归一化为 UTC。
// Python: config.py:71 _normalize_rfc3339
func normalizeRFC3339(value any, name string) (string, time.Time, error) {
	text, err := nonEmptyText(value, name)
	if err != nil {
		return "", time.Time{}, err
	}
	if !rfc3339TimestampPattern.MatchString(text) {
		return "", time.Time{}, fmt.Errorf("%s must be a timezone-aware RFC 3339 timestamp", name)
	}
	parsed, parseErr := time.Parse(time.RFC3339, strings.Replace(text, "Z", "+00:00", 1))
	if parseErr != nil {
		return "", time.Time{}, fmt.Errorf("%s must be a timezone-aware RFC 3339 timestamp", name)
	}
	return parsed.UTC().Format("2006-01-02T15:04:05Z07:00"), parsed.UTC(), nil
}

// normalizePath 路径归一化（展开 ~、敏感路径拒绝）。
// Python: config.py:110 _normalize_path
func normalizePath(value any, name string) (string, error) {
	raw := value
	if text, ok := value.(string); ok {
		raw = text
	}
	text, err := nonEmptyText(raw, name)
	if err != nil {
		return "", err
	}
	expanded := expandUser(text)
	userRelative := text == "~" || strings.HasPrefix(text, "~/") || strings.HasPrefix(text, "~\\")
	if !filepath.IsAbs(expanded) && !userRelative {
		return "", fmt.Errorf("%s must be absolute or start with '~'", name)
	}
	resolved, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("%s must be absolute or start with '~'", name)
	}
	if resolved, err = filepath.EvalSymlinks(resolved); err != nil {
		// Python resolve() 不要求存在；不存在时保留 Abs 结果
		resolved, _ = filepath.Abs(expanded)
	}
	if security.IsSensitivePath(resolved) || windowsSensitivePathPattern.MatchString(strings.ReplaceAll(resolved, "/", "\\")) {
		return "", fmt.Errorf("%s points to a sensitive path", name)
	}
	return resolved, nil
}

// normalizeURL 受限 https URL 归一化。
// Python: config.py:124 _normalize_url
func normalizeURL(value any, name, hostSuffix, pathPrefix string) (string, error) {
	text, err := nonEmptyText(value, name)
	if err != nil {
		return "", err
	}
	parsed, parseErr := url.Parse(text)
	if parseErr != nil {
		return "", fmt.Errorf("%s must be an https URL", name)
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an https URL", name)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%s must be an https URL", name)
	}
	if parsed.Port() != "" {
		return "", fmt.Errorf("%s must not contain a custom port", name)
	}
	if strings.Contains(text, "?") || strings.Contains(text, "#") {
		return "", fmt.Errorf("%s must not contain query or fragment", name)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != hostSuffix && !strings.HasSuffix(host, "."+hostSuffix) {
		return "", fmt.Errorf("%s must point to %s", name, hostSuffix)
	}
	requestPath := strings.TrimRight(parsed.Path, "/")
	if pathPrefix != "" &&
		!strings.HasPrefix(strings.ToLower(requestPath), strings.ToLower(strings.TrimRight(pathPrefix, "/"))+"/") {
		return "", fmt.Errorf("%s must contain %s", name, pathPrefix)
	}
	if requestPath == "" || requestPath == pathPrefix {
		return "", fmt.Errorf("%s must include a resource path", name)
	}
	return (&url.URL{Scheme: "https", Host: parsed.Host, Path: requestPath}).String(), nil
}

// normalizeFeedUrl feed URL 归一化。
// Python: config.py:150 _normalize_feed_url
func normalizeFeedURL(value any) (string, error) {
	text, err := nonEmptyText(value, "feed_url")
	if err != nil {
		return "", err
	}
	parsed, parseErr := url.Parse(text)
	if parseErr != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("feed_url must be an https URL")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("feed_url must be an https URL")
	}
	if parsed.Port() != "" {
		return "", fmt.Errorf("feed_url must not contain a custom port")
	}
	feedPath := parsed.Path
	if feedPath == "" {
		feedPath = "/"
	}
	return (&url.URL{Scheme: "https", Host: parsed.Host, Path: feedPath, RawQuery: parsed.RawQuery}).String(), nil
}

// orderedResources 资源白名单排序归一化。
// Python: config.py:166 _ordered_resources
func orderedResources(value any, allowed []string, name string) ([]string, error) {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty list", name)
	}
	values := make([]string, 0, len(list))
	for _, item := range list {
		text, err := nonEmptyText(item, name)
		if err != nil {
			return nil, err
		}
		values = append(values, strings.ToLower(text))
	}
	for _, item := range values {
		if !containsString(allowed, item) {
			return nil, fmt.Errorf("%s contains an unsupported resource", name)
		}
	}
	result := make([]string, 0, len(values))
	for _, item := range allowed {
		if containsString(values, item) {
			result = append(result, item)
		}
	}
	return result, nil
}

// normalizeServiceSource 源归一化（8 provider 分支逐条对照）。
// Python: config.py:175 _normalize_service_source
func normalizeServiceSource(provider string, source any) (map[string]any, error) {
	raw, err := mappingValue(source, "source")
	if err != nil {
		return nil, err
	}
	switch provider {
	case ProviderLocalFiles:
		if len(raw) != 1 {
			return nil, fmt.Errorf("local_files source must contain only root_dir")
		}
		rootDir, err := normalizePath(raw["root_dir"], "root_dir")
		if err != nil {
			return nil, err
		}
		return map[string]any{"root_dir": rootDir}, nil

	case ProviderGithub, ProviderGitcode:
		for key := range raw {
			if key != "owner" && key != "repo" && key != "resources" {
				return nil, fmt.Errorf("%s source requires owner and repo", provider)
			}
		}
		if _, hasOwner := raw["owner"]; !hasOwner {
			return nil, fmt.Errorf("%s source requires owner and repo", provider)
		}
		if _, hasRepo := raw["repo"]; !hasRepo {
			return nil, fmt.Errorf("%s source requires owner and repo", provider)
		}
		resourcesRaw, present := raw["resources"]
		if !present {
			list := make([]any, 0, len(repositoryResources))
			for _, item := range repositoryResources {
				list = append(list, item)
			}
			resourcesRaw = list
		}
		resources, err := orderedResources(resourcesRaw, repositoryResources, "resources")
		if err != nil {
			return nil, err
		}
		owner, err := safeSegment(raw["owner"], "owner")
		if err != nil {
			return nil, err
		}
		repo, err := safeSegment(raw["repo"], "repo")
		if err != nil {
			return nil, err
		}
		return map[string]any{"owner": owner, "repo": repo, "resources": toStringAny(resources)}, nil

	case ProviderFeishu:
		modeText, err := nonEmptyText(raw["mode"], "mode")
		if err != nil {
			return nil, err
		}
		mode := strings.ToLower(modeText)
		if mode == "account" {
			for key := range raw {
				switch key {
				case "mode", "resources", "document_ids", "query", "start", "end":
				default:
					return nil, fmt.Errorf("feishu account source contains unsupported fields")
				}
			}
			resources, err := orderedResources(raw["resources"], feishuResources, "resources")
			if err != nil {
				return nil, err
			}
			result := map[string]any{"mode": mode, "resources": toStringAny(resources)}
			hasDocumentIDs := raw["document_ids"] != nil
			hasQuery := raw["query"] != nil
			if hasDocumentIDs && hasQuery {
				return nil, fmt.Errorf("feishu docs source accepts document_ids or query, not both")
			}
			docsEnabled := containsString(resources, "docs")
			if (hasDocumentIDs || hasQuery) && !docsEnabled {
				return nil, fmt.Errorf("document_ids/query require docs resource")
			}
			calendarEnabled := containsString(resources, "calendar")
			if (raw["start"] != nil || raw["end"] != nil) && !calendarEnabled {
				return nil, fmt.Errorf("start/end require calendar resource")
			}
			if hasDocumentIDs {
				list, ok := raw["document_ids"].([]any)
				if !ok {
					return nil, fmt.Errorf("document_ids must be a list")
				}
				ids := make([]any, 0, len(list))
				for _, item := range list {
					text, err := nonEmptyText(item, "document_ids")
					if err != nil {
						return nil, err
					}
					ids = append(ids, text)
				}
				result["document_ids"] = ids
			}
			for _, fieldName := range []string{"query", "start", "end"} {
				if raw[fieldName] != nil {
					text, err := nonEmptyText(raw[fieldName], fieldName)
					if err != nil {
						return nil, err
					}
					result[fieldName] = text
				}
			}
			return result, nil
		}
		if mode == "wiki_space" {
			for key := range raw {
				switch key {
				case "mode", "wiki_space_id", "root_node_token", "max_depth", "max_nodes":
				default:
					return nil, fmt.Errorf("feishu wiki_space source requires wiki_space_id")
				}
			}
			if _, ok := raw["wiki_space_id"]; !ok {
				return nil, fmt.Errorf("feishu wiki_space source requires wiki_space_id")
			}
			maxDepth := 3
			if raw["max_depth"] != nil {
				number, ok := raw["max_depth"].(float64)
				if !ok || number != float64(int(number)) || number < 0 || number > 20 {
					return nil, fmt.Errorf("max_depth must be between 0 and 20")
				}
				maxDepth = int(number)
			}
			maxNodes := 200
			if raw["max_nodes"] != nil {
				number, ok := raw["max_nodes"].(float64)
				if !ok || number != float64(int(number)) || number < 1 || number > 10_000 {
					return nil, fmt.Errorf("max_nodes must be between 1 and 10000")
				}
				maxNodes = int(number)
			}
			spaceID, err := nonEmptyText(raw["wiki_space_id"], "wiki_space_id")
			if err != nil {
				return nil, err
			}
			result := map[string]any{
				"mode": mode, "wiki_space_id": spaceID,
				"max_depth": maxDepth, "max_nodes": maxNodes,
			}
			if raw["root_node_token"] != nil {
				token, err := nonEmptyText(raw["root_node_token"], "root_node_token")
				if err != nil {
					return nil, err
				}
				result["root_node_token"] = token
			}
			return result, nil
		}
		return nil, fmt.Errorf("feishu mode must be account or wiki_space")

	case ProviderBrowserBookmarks:
		for key := range raw {
			switch key {
			case "profile", "bookmarks_path", "bookmark_folder_paths", "include_subfolders", "fetch_page_content":
			default:
				return nil, fmt.Errorf("browser_bookmarks source contains unsupported fields")
			}
		}
		profileValue := raw["profile"]
		if profileValue == nil {
			profileValue = "Default"
		}
		profile, err := safeProfile(profileValue)
		if err != nil {
			return nil, err
		}
		bookmarksPath := raw["bookmarks_path"]
		if bookmarksPath == nil {
			// D-24.3-3：LOCALAPPDATA 平台默认路径按 runtime.GOOS 处理（24.3b 裁决）
			localAppData := os.Getenv("LOCALAPPDATA")
			if localAppData == "" {
				home, err := userHomeDir()
				if err != nil {
					return nil, err
				}
				localAppData = filepath.Join(home, "AppData", "Local")
			}
			bookmarksPath = filepath.Join(localAppData, "Microsoft", "Edge", "User Data", profile, "Bookmarks")
		} else if _, ok := bookmarksPath.(string); !ok {
			return nil, fmt.Errorf("bookmarks_path must be a path")
		}
		normalizedPath, err := normalizePath(bookmarksPath, "bookmarks_path")
		if err != nil {
			return nil, err
		}
		folders := []any{}
		if raw["bookmark_folder_paths"] != nil {
			list, ok := raw["bookmark_folder_paths"].([]any)
			if !ok {
				return nil, fmt.Errorf("bookmark_folder_paths must be a list")
			}
			for _, item := range list {
				text, err := nonEmptyText(item, "bookmark_folder_paths")
				if err != nil {
					return nil, err
				}
				folders = append(folders, text)
			}
		}
		includeSubfolders := true
		if raw["include_subfolders"] != nil {
			checked, ok := raw["include_subfolders"].(bool)
			if !ok {
				return nil, fmt.Errorf("bookmark boolean options must be booleans")
			}
			includeSubfolders = checked
		}
		fetchPageContent := true
		if raw["fetch_page_content"] != nil {
			checked, ok := raw["fetch_page_content"].(bool)
			if !ok {
				return nil, fmt.Errorf("bookmark boolean options must be booleans")
			}
			fetchPageContent = checked
		}
		return map[string]any{
			"profile": profile, "bookmarks_path": normalizedPath,
			"bookmark_folder_paths": folders,
			"include_subfolders":    includeSubfolders, "fetch_page_content": fetchPageContent,
		}, nil

	case ProviderZhihuReader:
		if len(raw) != 1 {
			return nil, fmt.Errorf("zhihu_reader source must contain only column_url")
		}
		columnURL, err := normalizeURL(raw["column_url"], "column_url", "zhihu.com", "/column/")
		if err != nil {
			return nil, err
		}
		parsed, _ := url.Parse(columnURL)
		parts := nonEmptySplit(parsed.Path)
		if len(parts) != 2 || parts[0] != "column" || parts[1] == "" {
			return nil, fmt.Errorf("column_url must be a single Zhihu column URL")
		}
		return map[string]any{"column_url": columnURL}, nil

	case ProviderToutiaoReader:
		if len(raw) != 1 {
			return nil, fmt.Errorf("toutiao_reader source must contain only profile_url")
		}
		rawProfileURL, err := nonEmptyText(raw["profile_url"], "profile_url")
		if err != nil {
			return nil, err
		}
		profileURL, err := normalizeURL(rawProfileURL, "profile_url", "toutiao.com", "/c/user/token/")
		if err != nil {
			return nil, err
		}
		// 头条公开主页端点重定向无斜杠形态；保留用户提供的尾斜杠避免不可信重定向
		rawParsed, _ := url.Parse(rawProfileURL)
		if strings.HasSuffix(rawParsed.Path, "/") {
			profileURL += "/"
		}
		parsed, _ := url.Parse(profileURL)
		parts := nonEmptySplit(parsed.Path)
		if len(parts) != 4 || parts[0] != "c" || parts[1] != "user" || parts[2] != "token" || parts[3] == "" {
			return nil, fmt.Errorf("profile_url must be a Toutiao profile homepage URL")
		}
		return map[string]any{"profile_url": profileURL}, nil

	case ProviderRssFeed:
		if len(raw) != 1 {
			return nil, fmt.Errorf("rss_feed source must contain only feed_url")
		}
		feedURL, err := normalizeFeedURL(raw["feed_url"])
		if err != nil {
			return nil, err
		}
		return map[string]any{"feed_url": feedURL}, nil
	}
	return nil, fmt.Errorf("unsupported provider")
}

// normalizeCredentials 凭据白名单归一化（缝1 空表语义上游依据）。
// Python: config.py:310 _normalize_credentials
func normalizeCredentials(provider string, credentials any) (map[string]string, error) {
	raw, err := mappingValue(credentials, "credentials")
	if err != nil {
		return nil, err
	}
	switch provider {
	case ProviderLocalFiles, ProviderFeishu, ProviderBrowserBookmarks,
		ProviderZhihuReader, ProviderToutiaoReader, ProviderRssFeed:
		if len(raw) != 0 {
			return nil, fmt.Errorf("%s does not accept credentials", provider)
		}
		return map[string]string{}, nil
	}
	required := "pat"
	if provider == ProviderGithub {
		required = "token"
	}
	if len(raw) != 1 {
		return nil, fmt.Errorf("%s credentials must contain only %s", provider, required)
	}
	if _, ok := raw[required]; !ok {
		return nil, fmt.Errorf("%s credentials must contain only %s", provider, required)
	}
	value, err := nonEmptyText(raw[required], required)
	if err != nil {
		return nil, err
	}
	return map[string]string{required: value}, nil
}

// mappingValue 类型守卫：要求 map[string]any。Python: config.py:45 _mapping
func mappingValue(value any, name string) (map[string]any, error) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return deepCopyJSON(mapping), nil
}

// nonEmptyText 非空文本守卫。Python: config.py:51 _non_empty_text
func nonEmptyText(value any, name string) (string, error) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return strings.TrimSpace(text), nil
}

// safeSegment 安全路径段守卫。Python: config.py:57 _safe_segment
func safeSegment(value any, name string) (string, error) {
	text, err := nonEmptyText(value, name)
	if err != nil {
		return "", err
	}
	if text == "." || text == ".." || !safeSegmentPattern.MatchString(text) {
		return "", fmt.Errorf("%s must be a safe path segment", name)
	}
	return text, nil
}

// safeProfile 单段 profile 守卫。Python: config.py:64 _safe_profile
func safeProfile(value any) (string, error) {
	text, err := nonEmptyText(value, "profile")
	if err != nil {
		return "", err
	}
	if text == "." || text == ".." || strings.ContainsAny(text, "/\\") {
		return "", fmt.Errorf("profile must be a single path segment")
	}
	return text, nil
}

// expandUser 展开 ~ 前缀（os.path.expanduser 等价）。
func expandUser(text string) string {
	if text == "~" || strings.HasPrefix(text, "~/") || strings.HasPrefix(text, "~\\") {
		home, err := userHomeDir()
		if err != nil {
			return text
		}
		if text == "~" {
			return home
		}
		return filepath.Join(home, text[2:])
	}
	return text
}

// stringOrEmpty 字符串取值守卫（缺省空串）。
func stringOrEmpty(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// containsString 列表包含判断。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// toStringAny 字符串切片转 []any。
func toStringAny(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

// nonEmptySplit 按斜杠切分并去掉空段。
func nonEmptySplit(value string) []string {
	parts := strings.Split(value, "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
