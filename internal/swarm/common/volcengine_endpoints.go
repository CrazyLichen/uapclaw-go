// 火山方舟官方端点常量与归一（模型预设、发现与生成共用）。
// 对应 Python 代码：gateway/common/volcengine_endpoints.py:1-32。
package common

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// 四常量。Python: volcengine_endpoints.py:8-11。
const (
	// ArkAPIBase 常规区域 /api/v3 基址
	ArkAPIBase = "https://ark.cn-beijing.volces.com/api/v3"
	// ArkCodingAPIBase Coding Plan 基址
	ArkCodingAPIBase = "https://ark.cn-beijing.volces.com/api/coding/v3"
	// ArkAnthropicBase Anthropic 兼容基址
	ArkAnthropicBase = "https://ark.cn-beijing.volces.com/api/compatible"
	// ArkCodingAnthropicBase Coding Plan Anthropic 基址
	ArkCodingAnthropicBase = "https://ark.cn-beijing.volces.com/api/coding"
)

// arkHostPattern ark.<region>.volces.com|bytepluses.com。
// Python: volcengine_endpoints.py:29 re.fullmatch(r"ark\.[\w-]+\.(?:volces\.com|bytepluses\.com)", host)。
var arkHostPattern = regexp.MustCompile(`^ark\.[\w-]+\.(?:volces\.com|bytepluses\.com)$`)

// NormalizeArkAPIBase 解析常规区域 Ark 基址，排除 Coding Plan 与代理。
// Python: volcengine_endpoints.py:14 normalize_ark_api_base。
// 生成与其模型目录使用同一区域 /api/v3 API。接受旧配置的 host 根，
// 但绝不重解释 Coding Plan URL 或切换已配置区域。
func NormalizeArkAPIBase(apiBase string) (string, error) {
	// Python: :24 rstrip("/") 后 urlsplit
	trimmed := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("Use an official regional Ark HTTPS /api/v3 address; Coding Plan does not support generation")
	}
	// Python: :24 rstrip("/") 后 urlsplit；Go 先 TrimSpace 再去尾部 /
	host := parsed.Hostname()
	// Python: :25 valid_origin = scheme == https 且 netloc 即 host（无 userinfo）
	validOrigin := parsed.Scheme == "https" && parsed.Host == host
	validHost := host != "" && arkHostPattern.MatchString(host)
	// Python: :27 path ∈ {"", "/api/v3"} 且无 query/fragment
	validEndpoint := (parsed.Path == "" || parsed.Path == "/api/v3") && parsed.RawQuery == "" && parsed.Fragment == ""
	if !validOrigin || !validHost || !validEndpoint {
		return "", fmt.Errorf("Use an official regional Ark HTTPS /api/v3 address; Coding Plan does not support generation")
	}
	return fmt.Sprintf("https://%s/api/v3", host), nil
}
