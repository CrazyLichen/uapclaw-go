// zhipu_free_models 内置智谱免费模型域（用户拍板 2026-12-06）。
//
// 对应 Python 代码：无（本仓新增域，登记偏差——上游免费模型来源为
// opencode zen 免费档，因服务端对第三方客户端限额不可用，替换为国内
// 厂商长期免费模型；zen 域保留且默认关断）。
//
// 设计：条目为 models.defaults 形状（与 zen 条目同构，下游消费者统一
// 处理）；key/api_base 内置——用户零配置，models.list 直接展示、选中
// 即推理。bearer 键支持 env ZHIPU_BUILTIN_API_KEY 覆盖内置常量。
package configpanel

import (
	"fmt"
	"os"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// ZhipuAPIBase 智谱 OpenAI 兼容端点。
const ZhipuAPIBase = "https://open.bigmodel.cn/api/paas/v4"

// ZhipuBuiltinAPIKey 内置 bearer 键（占位；部署时以 env
// ZHIPU_BUILTIN_API_KEY 注入覆盖，或直接改此常量）。
// ⚠️ 入库风险提示：key 随源码分发即可被提取滥用额度，建议用
// 限额度子账号 key 并定期轮换。
const ZhipuBuiltinAPIKey = ""

// zhipuFreeModelIDs 界面展示的 GLM 免费模型（智谱官方长期免费档；
// 模型轮换时改此表即可）。
var zhipuFreeModelIDs = []string{"glm-4.5-flash"}

// ──────────────────────────── 导出函数 ────────────────────────────

// resolveZhipuAPIKey 解析内置 bearer：env ZHIPU_BUILTIN_API_KEY 优先，
// 未配置回内置常量。
func resolveZhipuAPIKey() string {
	if k := os.Getenv("ZHIPU_BUILTIN_API_KEY"); k != "" {
		return k
	}
	return ZhipuBuiltinAPIKey
}

// ZhipuFreeModelEntries 内置免费模型条目（models.defaults 形状，与
// buildZenModelEntry 同构：client_provider=OpenAI 走 OpenAI 兼容协议）。
func ZhipuFreeModelEntries() []map[string]any {
	key := resolveZhipuAPIKey()
	entries := make([]map[string]any, 0, len(zhipuFreeModelIDs))
	for _, id := range zhipuFreeModelIDs {
		entries = append(entries, map[string]any{
			"model_client_config": map[string]any{
				"api_base":        ZhipuAPIBase,
				"api_key":         key,
				"model_name":      id,
				"client_provider": "OpenAI",
				"timeout":         360,
				"verify_ssl":      true,
				"max_retries":     0,
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"alias":                 id,
			"context_window_tokens": 128 * 1024,
			"is_free":               true,
		})
	}
	return entries
}

// AppendZhipuFreeModels 追加内置免费模型行（公开行形状与
// AppendZenFreeModels 一致：model_name/api_base/api_key/is_free/...）。
// key 未配置（内置常量为空且无 env）时回空——避免展示一个选中即报错的条目。
func AppendZhipuFreeModels(models []map[string]any) []map[string]any {
	if resolveZhipuAPIKey() == "" {
		return models
	}
	existingNames := map[string]bool{}
	for _, item := range models {
		if name := strings.TrimSpace(fmt.Sprintf("%v", item["model_name"])); name != "" && name != "<nil>" {
			existingNames[name] = true
		}
	}
	for _, entry := range ZhipuFreeModelEntries() {
		modelConfig, _ := entry["model_client_config"].(map[string]any)
		modelConfigObj, _ := entry["model_config_obj"].(map[string]any)
		modelName := strings.TrimSpace(fmt.Sprintf("%v", modelConfig["model_name"]))
		if modelName == "" || existingNames[modelName] {
			continue
		}
		models = append(models, map[string]any{
			"model_name":            modelName,
			"api_base":              orEmptyAny(modelConfig["api_base"]),
			"api_key":               orEmptyAny(modelConfig["api_key"]),
			"model_provider":        orEmptyAny(modelConfig["client_provider"]),
			"temperature":           modelConfigObj["temperature"],
			"reasoning_level":       ReasoningLevelDisplay(modelConfigObj["reasoning_level"]),
			"is_default":            entry["is_default"],
			"is_agentos":            false,
			"is_free":               true,
			"alias":                 orEmptyAny(entry["alias"]),
			"context_window_tokens": parsePositiveIntOr(entry["context_window_tokens"], 0),
		})
		existingNames[modelName] = true
	}
	return models
}
