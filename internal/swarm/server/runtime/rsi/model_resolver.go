package rsi

// Resolve JiuwenSwarm 模型选择为引擎可读模型文件。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/model_resolver.py（306 行）。
// Web models.list 暴露选择值（模型名/别名/name#origin_index）；RSI 引擎载入
// 完整 model_client_config/model_request_config 文件。本模块是两个契约间的
// 唯一翻译边界。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	configpkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	serveradapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedRsiModel 从 models.list 同源列表选出的模型条目。
type ResolvedRsiModel struct {
	// Reference 用户引用串。
	Reference string
	// Role 角色名（optimizer/tester）。
	Role string
	// ModelName 模型名。
	ModelName string
	// OriginIndex 全局序号（zen 条目为 nil）。
	OriginIndex *int
	// Path 落盘 YAML 路径。
	Path string
	// ConfigSha256 文件摘要。
	ConfigSha256 string
}

// ToManifest 非敏感任务清单表示（对齐 Python to_manifest）。
func (m ResolvedRsiModel) ToManifest() map[string]any {
	return map[string]any{
		"role":          m.Role,
		"ref":           m.Reference,
		"model_name":    m.ModelName,
		"origin_index":  nullableAny(m.OriginIndex),
		"config_sha256": m.ConfigSha256,
		"path":          m.Path,
	}
}

// ModelConfigResolver 模型解析与物化（装载/构造缝可注入以便测试；
// 生产缺省即 models.list 与 deep adapter 同源函数）。
type ModelConfigResolver struct {
	configLoader   func() map[string]any
	defaultsLoader func(map[string]any) []map[string]any
	// zenLoader ⚠️ D-9-6：Python 缺省接 opencode_zen 免费模型缓存，Go 侧无
	// 对应实现，缺省恒空列表（网络/缓存失败不应使配置模型消失——语义一致）。
	zenLoader func() []map[string]any
	// modelBuilder 构造缝：Go 缺省 adapter.BuildModelFromEntry（仅做校验用）。
	modelBuilder func(mcc, mco map[string]any) (clientData, requestData map[string]any, err error)
}

// NewModelConfigResolver 构造解析器（nil 注入 → 缺省装载）。
func NewModelConfigResolver() *ModelConfigResolver {
	return &ModelConfigResolver{
		configLoader:   defaultConfig,
		defaultsLoader: defaultModels,
		zenLoader:      defaultZenModels,
		modelBuilder:   defaultModelBuilder,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Entries 按 models.list 顺序返回条目与全局序号。
// 配置缺省（含 models.agentos）取全局序号；zen 条目尾部追加且故意无序号
// （进程本地，不入 models.defaults）。
func (r *ModelConfigResolver) Entries() []struct {
	Entry map[string]any
	Index *int
} {
	config := r.configLoader()
	defaults := r.defaultsLoader(config)
	result := []struct {
		Entry map[string]any
		Index *int
	}{}
	seenNames := map[string]bool{}
	for index, entry := range defaults {
		idx := index
		result = append(result, struct {
			Entry map[string]any
			Index *int
		}{entry, &idx})
		if name := modelName(entry); name != "" {
			seenNames[name] = true
		}
	}
	// Zen 为可选缓存：装载失败不得使配置模型消失。
	var zenEntries []map[string]any
	func() {
		defer func() { _ = recover() }()
		zenEntries = r.zenLoader()
	}()
	for _, entry := range zenEntries {
		name := modelName(entry)
		if name == "" || seenNames[name] {
			continue
		}
		result = append(result, struct {
			Entry map[string]any
			Index *int
		}{entry, nil})
		seenNames[name] = true
	}
	return result
}

// Resolve 严格 models.list 语义解析引用。
func (r *ModelConfigResolver) Resolve(modelRef string) (map[string]any, *int, error) {
	reference := strings.TrimSpace(modelRef)
	if reference == "" {
		return nil, nil, NewModelNotFoundRef("模型引用不能为空")
	}
	entries := r.Entries()
	if strings.Contains(reference, "#") {
		idx := strings.LastIndex(reference, "#")
		bareName := strings.TrimSpace(reference[:idx])
		rawIndex := reference[idx+1:]
		index, ok := parseIntStrict(rawIndex)
		if !ok {
			return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用 origin_index 非法: %s", reference))
		}
		if bareName == "" || index < 0 {
			return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用 origin_index 非法: %s", reference))
		}
		for _, pair := range entries {
			if pair.Index == nil || *pair.Index != index {
				continue
			}
			if matchesEntry(pair.Entry, bareName) {
				return pair.Entry, pair.Index, nil
			}
			// 序号是全局的：名/序不匹配时不得静默换同名词目。
			break
		}
		return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用未命中: %s", reference))
	}
	matches := []struct {
		Entry map[string]any
		Index *int
	}{}
	for _, pair := range entries {
		if matchesEntry(pair.Entry, reference) {
			matches = append(matches, pair)
		}
	}
	if len(matches) == 0 {
		return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用未命中: %s", reference))
	}
	// 保留纯名语义：先选显式缺省，否则取列表序首个。
	for _, pair := range matches {
		if pair.Entry["is_default"] == true {
			return pair.Entry, pair.Index, nil
		}
	}
	return matches[0].Entry, matches[0].Index, nil
}

// ResolveToFile 解析模型并写出引擎可读 role YAML。
// 返回映射刻意可持久化进 task.json 且永不含 API key；私有 YAML 文件本身
// 可含解密 key（引擎从盘读；位于任务目录下且不登记为 artifact）。
func (r *ModelConfigResolver) ResolveToFile(modelRef, role string, taskModelsDir string) (map[string]any, error) {
	roleName := strings.TrimSpace(role)
	if roleName == "" || filepath.Base(roleName) != roleName {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型 role 非法: %s", role))
	}
	entry, originIndex, err := r.Resolve(modelRef)
	if err != nil {
		return nil, err
	}
	modelNameStr := modelName(entry)
	mcc, _ := entry["model_client_config"].(map[string]any)
	mco, _ := entry["model_config_obj"].(map[string]any)
	if modelNameStr == "" || mcc == nil || mco == nil {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型条目不完整: %s", modelRef))
	}
	if strings.TrimSpace(valueToString(mcc["client_provider"])) == "" {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型缺少 client_provider: %s", modelNameStr))
	}
	if strings.TrimSpace(valueToString(orAny(mcc["api_base"], mcc["base_url"]))) == "" {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型缺少 api_base: %s", modelNameStr))
	}
	// 构造校验（异常文本不上线：部分 provider 客户端异常里带完整输入映射含解密 key）。
	clientData, requestData, err := r.modelBuilder(mcc, mco)
	if err != nil {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型 %s 无法构造", modelNameStr))
	}
	if clientData == nil || requestData == nil {
		return nil, NewModelConfigInvalidErr(fmt.Sprintf("模型 %s 构造结果无效", modelNameStr))
	}
	// RSI 引擎内不重试：重试预算归引擎 stage 策略；成员工厂载入时亦防御性剥离。
	clientData["max_retries"] = 0
	// 任务文件用公开 model 拼写（对齐 openjiuwen standalone 示例）。
	delete(requestData, "model_name")
	requestData["model"] = modelNameStr
	payload := map[string]any{
		"model_client_config":  clientData,
		"model_request_config": requestData,
	}
	targetDir := taskModelsDir
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	target := filepath.Join(targetDir, roleName+".yaml")
	data, err := yaml.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return nil, err
	}
	digest, err := fileSha256(target)
	if err != nil {
		return nil, err
	}
	return ResolvedRsiModel{
		Reference:    strings.TrimSpace(modelRef),
		Role:         roleName,
		ModelName:    modelNameStr,
		OriginIndex:  originIndex,
		Path:         target,
		ConfigSha256: digest,
	}.ToManifest(), nil
}

// SelectRsiModelEntry 从预载列表按严格语义选择（对齐 Python 模块级函数）。
func SelectRsiModelEntry(entries []struct {
	Entry map[string]any
	Index *int
}, modelRef string) (map[string]any, *int, error) {
	reference := strings.TrimSpace(modelRef)
	if reference == "" {
		return nil, nil, NewModelNotFoundRef("模型引用不能为空")
	}
	if strings.Contains(reference, "#") {
		idx := strings.LastIndex(reference, "#")
		bareName := strings.TrimSpace(reference[:idx])
		rawIndex := reference[idx+1:]
		index, ok := parseIntStrict(rawIndex)
		if !ok || bareName == "" || index < 0 {
			return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用 origin_index 非法: %s", reference))
		}
		for _, pair := range entries {
			if pair.Index != nil && *pair.Index == index && matchesEntry(pair.Entry, bareName) {
				return pair.Entry, pair.Index, nil
			}
		}
		return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用未命中: %s", reference))
	}
	matches := []struct {
		Entry map[string]any
		Index *int
	}{}
	for _, pair := range entries {
		if matchesEntry(pair.Entry, reference) {
			matches = append(matches, pair)
		}
	}
	if len(matches) == 0 {
		return nil, nil, NewModelNotFoundRef(fmt.Sprintf("模型引用未命中: %s", reference))
	}
	for _, pair := range matches {
		if pair.Entry["is_default"] == true {
			return pair.Entry, pair.Index, nil
		}
	}
	return matches[0].Entry, matches[0].Index, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchesEntry 名称或别名匹配（对齐 Python _matches）。
func matchesEntry(entry map[string]any, requested string) bool {
	name := modelName(entry)
	alias := strings.TrimSpace(valueToString(entry["alias"]))
	return requested == name || (alias != "" && requested == alias)
}

// modelName 条目模型名（对齐 Python _model_name）。
func modelName(entry map[string]any) string {
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		return ""
	}
	return strings.TrimSpace(valueToString(mcc["model_name"]))
}

// defaultConfig 缺省配置装载。⚠️ 偏差登记：Go GetDefaultModels(nil) 自装载
// 默认 config.yaml（对齐 Python config=None → get_config()），此处返回 nil
// 由 defaultsLoader 兜底。
func defaultConfig() map[string]any {
	return nil
}

// defaultModels 缺省模型列表（对齐 Python get_default_models）。
func defaultModels(cfg map[string]any) []map[string]any {
	return configpkg.GetDefaultModels(cfg)
}

// defaultZenModels D-9-6：Go 无 opencode_zen 缓存，恒空。
func defaultZenModels() []map[string]any { return nil }

// defaultModelBuilder 经 deep adapter 同源构造模型做校验并导出两段配置。
func defaultModelBuilder(mcc, mco map[string]any) (map[string]any, map[string]any, error) {
	model, err := serveradapter.BuildModelFromEntry(mcc, mco)
	if err != nil {
		return nil, nil, err
	}
	clientData, err := structToMap(model.ClientConfig)
	if err != nil {
		return nil, nil, err
	}
	requestData, err := structToMap(model.ModelConfig)
	if err != nil {
		return nil, nil, err
	}
	return clientData, requestData, nil
}

// parseIntStrict 十进制严格解析。
func parseIntStrict(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '-' || s[0] == '+' {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// NewModelNotFoundRef MODEL_NOT_FOUND 引用错误。
func NewModelNotFoundRef(message string) *RsiError {
	return &RsiError{Code: "MODEL_NOT_FOUND", Message: message}
}

// NewModelConfigInvalidErr MODEL_CONFIG_INVALID 配置错误。
func NewModelConfigInvalidErr(message string) *RsiError {
	return &RsiError{Code: "MODEL_CONFIG_INVALID", Message: message}
}

// fileSha256 文件摘要。
func fileSha256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
