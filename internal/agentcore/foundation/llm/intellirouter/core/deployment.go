package core

import (
	"fmt"

	"github.com/google/uuid"
)

// ──────────────────────────── 枚举 ────────────────────────────

// DeploymentStatus 部署状态枚举。← core/deployment.py:13 DeploymentStatus
type DeploymentStatus int

// 部署状态取值 ← deployment.py:14-16
const (
	// DeploymentStatusHealthy 健康
	DeploymentStatusHealthy DeploymentStatus = iota
	// DeploymentStatusCooldown 冷却中
	DeploymentStatusCooldown
)

// String 枚举字符串值（to_dict 输出，对齐 Python .value）。
func (s DeploymentStatus) String() string {
	switch s {
	case DeploymentStatusHealthy:
		return "healthy"
	case DeploymentStatusCooldown:
		return "cooldown"
	default:
		return "healthy"
	}
}

// DeploymentStatusFrom 从字符串解析状态。← deployment.py:135 DeploymentStatus(...)
func DeploymentStatusFrom(raw string) (DeploymentStatus, error) {
	switch raw {
	case "healthy":
		return DeploymentStatusHealthy, nil
	case "cooldown":
		return DeploymentStatusCooldown, nil
	default:
		return 0, fmt.Errorf("unknown DeploymentStatus value: %q", raw)
	}
}

// ──────────────────────────── 结构体 ────────────────────────────

// DeploymentParams Deployment 的构造参数（对齐 dataclass 字段清单）。
// ← core/deployment.py:19 Deployment
type DeploymentParams struct {
	// ModelName 型号名
	ModelName string
	// APIKey API 密钥
	APIKey string
	// APIBase API 端点 URL
	APIBase string
	// ID 部署 ID（空则自动生成 uuid 前 8 位）← py:28 default_factory
	ID string
	// ModelID 业务模型 ID ← py:29
	ModelID string
	// Status 状态 ← py:30
	Status DeploymentStatus
	// ConsecutiveFailures 连续失败次数 ← py:31
	ConsecutiveFailures int
	// CooldownUntil 冷却时间戳（nil = 未在冷却）← py:32
	CooldownUntil *float64
	// Tags 标签列表 ← py:33
	Tags []string
	// TPM tokens-per-minute 容量提示 ← py:36
	TPM *int
	// RPM requests-per-minute 容量提示 ← py:37
	RPM *int
	// Timeout 路由级超时 ← py:38
	Timeout *float64
	// Provider provider 名 ← py:40
	Provider string
	// VerifySSL TLS 校验开关 ← py:41
	VerifySSL bool
	// RequestDefaults 请求默认参数 ← py:42
	RequestDefaults map[string]any
	// EndpointProfile provider 端点方言 ← py:43
	EndpointProfile *string
	// CustomHeaders 路由级自定义头 ← py:44
	CustomHeaders map[string]string
	// FallbackTag tag-filtered 路由的官方模型名 ← py:45
	FallbackTag *string
	// ModelDescription 用户提供的模型能力描述 ← py:46
	ModelDescription *string
}

// Deployment 一个 API 端点。← core/deployment.py:19 Deployment
type Deployment struct {
	// ModelName 型号名
	ModelName string
	// APIKey API 密钥
	APIKey string
	// APIBase API 端点 URL
	APIBase string
	// ID 部署 ID
	ID string
	// ModelID 业务模型 ID
	ModelID string
	// Status 状态
	Status DeploymentStatus
	// ConsecutiveFailures 连续失败次数
	ConsecutiveFailures int
	// CooldownUntil 冷却时间戳
	CooldownUntil *float64
	// Tags 标签列表
	Tags []string
	// TPM tokens-per-minute 容量提示
	TPM *int
	// RPM requests-per-minute 容量提示
	RPM *int
	// Timeout 路由级超时
	Timeout *float64
	// Provider provider 名
	Provider string
	// VerifySSL TLS 校验开关
	VerifySSL bool
	// RequestDefaults 请求默认参数
	RequestDefaults map[string]any
	// EndpointProfile provider 端点方言
	EndpointProfile *string
	// CustomHeaders 路由级自定义头
	CustomHeaders map[string]string
	// FallbackTag tag-filtered 路由的官方模型名
	FallbackTag *string
	// ModelDescription 用户提供的模型能力描述
	ModelDescription *string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeployment 构造并校验 Deployment，内联对齐 deployment.py:56 __post_init__。
//
// 偏差（D-2）：py:60-66 的 aws-bedrock 空 api_key 豁免分支不移植
// （bedrock provider 裁剪，其余 provider 仍强制非空）。
func NewDeployment(p DeploymentParams) (*Deployment, error) {
	d := &Deployment{
		ModelName:           p.ModelName,
		APIKey:              p.APIKey,
		APIBase:             p.APIBase,
		ID:                  p.ID,
		ModelID:             p.ModelID,
		Status:              p.Status,
		ConsecutiveFailures: p.ConsecutiveFailures,
		CooldownUntil:       p.CooldownUntil,
		Tags:                p.Tags,
		TPM:                 p.TPM,
		RPM:                 p.RPM,
		Timeout:             p.Timeout,
		Provider:            p.Provider,
		VerifySSL:           p.VerifySSL,
		RequestDefaults:     p.RequestDefaults,
		EndpointProfile:     p.EndpointProfile,
		CustomHeaders:       p.CustomHeaders,
		FallbackTag:         p.FallbackTag,
		ModelDescription:    p.ModelDescription,
	}
	if d.ID == "" {
		// ← py:28 default_factory=lambda: str(uuid.uuid4())[:8]
		d.ID = uuid.NewString()[:8]
	}
	if d.Provider == "" {
		// ← py:40 provider: str = "openai"
		d.Provider = "openai"
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// Validate 字段类型与数值校验。← deployment.py:56-97 __post_init__
func (d *Deployment) Validate() error {
	// ← py:57-68 四个必填字符串非空（bedrock 豁免分支裁剪，见 NewDeployment 注释）
	for name, value := range map[string]string{
		"model_name": d.ModelName,
		"api_key":    d.APIKey,
		"api_base":   d.APIBase,
		"provider":   d.Provider,
	} {
		if value == "" {
			return fmt.Errorf("Deployment.%s must be a non-empty string, got %q", name, value)
		}
	}

	// ← py:70-80 tpm/rpm 拒绝 bool 与非正整数（bool 是 int 子类需显式排除）
	for name, value := range map[string]*int{"tpm": d.TPM, "rpm": d.RPM} {
		if value == nil {
			continue
		}
		if *value <= 0 {
			return fmt.Errorf("Deployment.%s must be a positive int, got %d", name, *value)
		}
	}

	// ← py:82-92 consecutive_failures 必须 >= 0
	if d.ConsecutiveFailures < 0 {
		return fmt.Errorf("Deployment.consecutive_failures must be >= 0, got %d", d.ConsecutiveFailures)
	}

	// ← py:94-103 timeout 必须为正数（bool 排除分支在 Go 类型系统下不可达）
	if d.Timeout != nil && *d.Timeout <= 0 {
		return fmt.Errorf("Deployment.timeout must be a positive number, got %v", *d.Timeout)
	}
	return nil
}

// ToDict 序列化为字典。← deployment.py:105-125 to_dict
func (d *Deployment) ToDict() map[string]any {
	return map[string]any{
		"id":                   d.ID,
		"model_id":             d.ModelID,
		"model_name":           d.ModelName,
		"api_key":              d.APIKey,
		"api_base":             d.APIBase,
		"status":               d.Status.String(),
		"consecutive_failures": d.ConsecutiveFailures,
		"cooldown_until":       d.CooldownUntil,
		"tags":                 d.Tags,
		"tpm":                  d.TPM,
		"rpm":                  d.RPM,
		"timeout":              d.Timeout,
		"provider":             d.Provider,
		"verify_ssl":           d.VerifySSL,
		"request_defaults":     d.RequestDefaults,
		"endpoint_profile":     d.EndpointProfile,
		"custom_headers":       d.CustomHeaders,
		"fallback_tag":         d.FallbackTag,
		"model_description":    d.ModelDescription,
	}
}

// DeploymentFromDict 从字典反序列化。← deployment.py:128-135 from_dict
func DeploymentFromDict(data map[string]any) (*Deployment, error) {
	p := DeploymentParams{
		ModelName:           strFrom(data["model_name"]),
		APIKey:              strFrom(data["api_key"]),
		APIBase:             strFrom(data["api_base"]),
		ID:                  strFrom(data["id"]),
		ModelID:             strFrom(data["model_id"]),
		ConsecutiveFailures: intFrom(data["consecutive_failures"]),
		VerifySSL:           boolFrom(data["verify_ssl"], true),
		Provider:            strFrom(data["provider"]),
	}
	if v, ok := data["status"]; ok {
		status, err := DeploymentStatusFrom(strFrom(v))
		if err != nil {
			return nil, err
		}
		p.Status = status
	}
	if v, ok := data["cooldown_until"].(float64); ok {
		p.CooldownUntil = &v
	}
	if v, ok := data["tags"].([]any); ok {
		for _, item := range v {
			p.Tags = append(p.Tags, strFrom(item))
		}
	}
	if v, ok := data["tpm"].(float64); ok {
		i := int(v)
		p.TPM = &i
	}
	if v, ok := data["rpm"].(float64); ok {
		i := int(v)
		p.RPM = &i
	}
	if v, ok := data["timeout"].(float64); ok {
		p.Timeout = &v
	}
	if v, ok := data["endpoint_profile"].(string); ok {
		p.EndpointProfile = &v
	}
	if v, ok := data["request_defaults"].(map[string]any); ok {
		p.RequestDefaults = v
	}
	if v, ok := data["custom_headers"].(map[string]any); ok {
		p.CustomHeaders = make(map[string]string, len(v))
		for k, item := range v {
			p.CustomHeaders[k] = strFrom(item)
		}
	}
	if v, ok := data["fallback_tag"].(string); ok {
		p.FallbackTag = &v
	}
	if v, ok := data["model_description"].(string); ok {
		p.ModelDescription = &v
	}
	return NewDeployment(p)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strFrom any → string（nil 安全）。
func strFrom(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// intFrom any → int（JSON float64 安全）。
func intFrom(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// boolFrom any → bool（缺省取默认值）。
func boolFrom(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
