// 图像模态探测缓存预热。
//
// DeepAgent 构建期在 enable_read_image_multimodal 为 auto 时预热原生图片输入
// 判定：图片输入调用点动态读缓存；无结论时后台调度探测。服务端进程里该机制
// 有一个缺口——create_subagent 不转发 enable_read_image_multimodal，每个子代理
// 都会在缓存为空时补发探测请求（Python: image_modality_warmup.py docstring）。
// 服务启动时（以及模型配置变更时）统一预热一次，让缓存成为权威结论，避免
// 杂散探测请求。
//
// Python: jiuwenswarm/server/runtime/image_modality_warmup.py（一文件一文件移植，23.7）
package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// warmupTotalTimeoutSeconds 整轮预热总超时。
	// 单个探测自带 5s 超时（外加一次去开关重试）且并发执行，本总闸只防
	// 「模型客户端无视自身 deadline」——服务启动绝不能被探测挂住。
	// Python: _WARMUP_TOTAL_TIMEOUT_SECONDS = 30.0 (image_modality_warmup.py:55)
	warmupTotalTimeoutSeconds = 30 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// probeImageSupportFunc 同步探测入口的可替换实现（测试注入点；
// Python 测试以 patch.object(image_modality_warmup, "probe_image_support") 等价）。
var probeImageSupportFunc = modalityprobe.ProbeImageSupport

// ──────────────────────────── 导出函数 ────────────────────────────

// WarmImageModalityCache 对配置的每个模型各探测一次并缓存结论。
//
// 永不返回错误、永不挂住调用方：探测失败由 probe_image_support 自身吞掉，
// 整轮受 warmupTotalTimeoutSeconds 约束。
//
// Python: warm_image_modality_cache:119-200
func WarmImageModalityCache(configBase map[string]any, reason string) {
	warmImageModalityCache(context.Background(), configBase, reason)
}

// RefreshImageModalityCache 清空缓存结论并重探当前配置的模型。
//
// 用于模型配置变更：同 (api_base, model_name) 背后的端点/密钥/后端可能已变，
// 旧结论不得残留。
//
// Python: refresh_image_modality_cache:203-220
func RefreshImageModalityCache(configBase map[string]any, reason string) {
	modalityprobe.ResetImageSupportCache()
	WarmImageModalityCache(configBase, reason)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// warmImageModalityCache 预热主流程；返回整体可用性（全部结论为 true 才算可用）。
// Python: warm_image_modality_cache:119-200（asyncio.gather → 并发 goroutine + WaitGroup）
func warmImageModalityCache(ctx context.Context, configBase map[string]any, reason string) bool {
	// Python: get_configured_read_image_multimodal(effective_config) is not None → 跳过
	// （react.enable_read_image_multimodal 显式设置时以静态策略为准，无需探测）
	if configuredReadImageMultimodal(configBase) != nil {
		logger.Info(logger.ComponentAgentServer).
			Str("reason", reason).
			Msg("[ImageModalityWarmup] skipped: react.enable_read_image_multimodal is set explicitly")
		return true
	}

	models := buildProbeModels(config.GetDefaultModels(configBase))
	if len(models) == 0 {
		logger.Info(logger.ComponentAgentServer).
			Str("reason", reason).
			Msg("[ImageModalityWarmup] skipped: no probeable model configured")
		return true
	}

	logger.Info(logger.ComponentAgentServer).
		Int("model_count", len(models)).
		Str("reason", reason).
		Msg("[ImageModalityWarmup] probing models")

	// Python: asyncio.wait_for(asyncio.gather(...), timeout=30s)
	var wg sync.WaitGroup
	results := make([]*bool, len(models))
	for i, model := range models {
		wg.Add(1)
		go func(idx int, m *llm.Model) {
			defer wg.Done()
			results[idx] = probeImageSupportFunc(ctx, m)
		}(i, model)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(warmupTotalTimeoutSeconds):
		logger.Warn(logger.ComponentAgentServer).
			Int("timeout_seconds", int(warmupTotalTimeoutSeconds.Seconds())).
			Str("reason", reason).
			Msg("[ImageModalityWarmup] probe round timed out; agents fall back to probing on demand")
		return false
	}

	available := true
	for i, model := range models {
		verdict := results[i]
		key := modalityprobe.ProbeCacheKeyOf(model)
		apiBase, modelName := keyFields(key)
		if verdict == nil {
			available = false
			logger.Info(logger.ComponentAgentServer).
				Str("api_base", apiBase).
				Str("model", modelName).
				Str("image_input", "unknown (not cached)").
				Msg("[ImageModalityWarmup] model verdict unknown")
			continue
		}
		logger.Info(logger.ComponentAgentServer).
			Str("api_base", apiBase).
			Str("model", modelName).
			Bool("image_input", *verdict).
			Msg("[ImageModalityWarmup] model probed")
	}
	return available
}

// buildProbeModels 按 models.defaults 条目构建探测用 Model 列表。
//
// 结论按 (api_base, model_name) 缓存，坍缩到同 key 的条目（别名/重复项）只探一次；
// 显式声明 supports_vision 的条目不探测，直接预填缓存。
//
// Python: _build_probe_models:58-116
func buildProbeModels(entries []map[string]any) []*llm.Model {
	models := make([]*llm.Model, 0, len(entries))
	seen := make(map[modalityprobe.ProbeCacheKey]struct{})
	for _, entry := range entries {
		mcc, _ := entry["model_client_config"].(map[string]any)
		if len(mcc) == 0 {
			continue
		}
		if modelName, _ := mcc["model_name"].(string); modelName == "" {
			continue
		}
		mco, _ := entry["model_config_obj"].(map[string]any)
		model, err := adapter.BuildModelFromEntry(mcc, mco)
		if err != nil {
			// Python: 一个坏条目不能拖垮其余条目（:92-98）
			modelName, _ := mcc["model_name"].(string)
			logger.Warn(logger.ComponentAgentServer).
				Str("model_name", modelName).
				Err(err).
				Msg("[ImageModalityWarmup] skipping unusable model entry")
			continue
		}
		key := modalityprobe.ProbeCacheKeyOf(model)
		if key == nil {
			continue
		}
		if _, dup := seen[*key]; dup {
			continue
		}
		seen[*key] = struct{}{}

		// 显式声明 supports_vision：跳过探测，预填缓存
		// Python: _build_probe_models:104-113
		if declared, ok := mcc["supports_vision"].(bool); ok {
			modalityprobe.SetCachedImageSupport(*key, declared)
			logger.Info(logger.ComponentAgentServer).
				Str("api_base", key.APIBase).
				Str("model", key.ModelName).
				Bool("image_input", declared).
				Msg("[ImageModalityWarmup] declared via supports_vision")
			continue
		}
		models = append(models, model)
	}
	return models
}

// configuredReadImageMultimodal 读取 react.enable_read_image_multimodal 显式策略。
// Python: common/config.py:283-295 get_configured_read_image_multimodal
func configuredReadImageMultimodal(configBase map[string]any) *bool {
	react, _ := configBase["react"].(map[string]any)
	v, ok := react["enable_read_image_multimodal"].(bool)
	if !ok {
		return nil
	}
	return &v
}

// keyFields 从缓存键取展示字段；键为 nil 时返回空串。
func keyFields(key *modalityprobe.ProbeCacheKey) (string, string) {
	if key == nil {
		return "", ""
	}
	return key.APIBase, key.ModelName
}
