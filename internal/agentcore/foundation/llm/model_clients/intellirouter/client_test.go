package intellirouter

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──── NewIntelliRouterModelClient 测试 ────

// TestNewIntelliRouterModelClient_Success 测试正常创建客户端。
func TestNewIntelliRouterModelClient_Success(t *testing.T) {
	// 清空路由缓存
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "sk-test",
					"api_base":   "https://api.test.com",
					"tpm":        100000,
					"rpm":        60,
				},
			},
			"intelli_router_strategy":    "simple-shuffle",
			"intelli_router_num_retries": 3,
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建客户端不应报错: %v", err)
	}
	if client == nil {
		t.Fatal("客户端不应为 nil")
	}
	if client.router == nil {
		t.Error("router 不应为 nil")
	}
	if client.config == nil {
		t.Error("config 不应为 nil")
	}
	// 验证 WithSkipValidate 生效：客户端 api_key 为 placeholder 但 ValidateConfig 不报错
	if err := client.ValidateConfig(); err != nil {
		t.Errorf("WithSkipValidate 下 ValidateConfig 不应报错: %v", err)
	}
}

// TestNewIntelliRouterModelClient_NoDeployments 测试无部署端点时报错。
func TestNewIntelliRouterModelClient_NoDeployments(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("intelli_router", "", "",
		llmschema.WithVerifySSL(false),
		// 不提供 intelli_router_deployments
	)

	_, err := NewIntelliRouterModelClient(mc, cc)
	if err == nil {
		t.Error("无 deployment 应报错")
	}
	if _, ok := err.(*exception.BaseError); !ok {
		t.Error("错误应为 BaseError 类型")
	}
}

// TestNewIntelliRouterModelClient_MultipleDeployments 测试多部署端点。
func TestNewIntelliRouterModelClient_MultipleDeployments(t *testing.T) {
	// 清空路由缓存
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-v4-flash"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "deepseek-v4-flash",
					"api_key":    "sk-key1",
					"api_base":   "https://api1.deepseek.com",
					"tpm":        100000,
					"rpm":        60,
					"tags":       []any{"primary"},
				},
				{
					"id":         "dep2",
					"model_name": "deepseek-v4-flash",
					"api_key":    "sk-key2",
					"api_base":   "https://api2.deepseek.com",
					"tpm":        200000,
					"rpm":        120,
					"tags":       []any{"backup"},
				},
			},
			"intelli_router_strategy": "adaptive",
			"intelli_router_strategy_kwargs": map[string]any{
				"exploration_ratio": 0.0, // 禁用探索以便测试
				"w_latency":        1.0,
			},
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建客户端不应报错: %v", err)
	}

	// 验证路由器有两个部署端点
	stats := client.GetRouterStats()
	if stats["total_deployments"] != 2 {
		t.Errorf("total_deployments = %v, 期望 2", stats["total_deployments"])
	}
}

// TestNewIntelliRouterModelClient_RouterSharing 测试路由器缓存共享。
func TestNewIntelliRouterModelClient_RouterSharing(t *testing.T) {
	// 清空路由缓存
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "sk-test",
					"api_base":   "https://api.test.com",
				},
			},
			"intelli_router_strategy": "simple-shuffle",
		}),
	)

	client1, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建 client1 不应报错: %v", err)
	}

	client2, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建 client2 不应报错: %v", err)
	}

	// 相同配置应共享同一个路由器实例
	if client1.router != client2.router {
		t.Error("相同配置的客户端应共享同一个路由器实例")
	}
}

// ──── 不支持方法测试 ────

// TestIntelliRouter_GenerateImageNotSupported 测试图片生成不支持。
func TestIntelliRouter_GenerateImageNotSupported(t *testing.T) {
	client := createTestClient(t)
	_, err := client.GenerateImage(nil, nil)
	if err == nil {
		t.Error("GenerateImage 应返回不支持错误")
	}
}

// TestIntelliRouter_GenerateSpeechNotSupported 测试语音生成不支持。
func TestIntelliRouter_GenerateSpeechNotSupported(t *testing.T) {
	client := createTestClient(t)
	_, err := client.GenerateSpeech(nil, nil)
	if err == nil {
		t.Error("GenerateSpeech 应返回不支持错误")
	}
}

// TestIntelliRouter_GenerateVideoNotSupported 测试视频生成不支持。
func TestIntelliRouter_GenerateVideoNotSupported(t *testing.T) {
	client := createTestClient(t)
	_, err := client.GenerateVideo(nil, nil)
	if err == nil {
		t.Error("GenerateVideo 应返回不支持错误")
	}
}

// TestIntelliRouter_ReleaseNotSupported 测试 KV Cache 释放不支持。
func TestIntelliRouter_ReleaseNotSupported(t *testing.T) {
	client := createTestClient(t)
	_, err := client.Release(nil)
	if err == nil {
		t.Error("Release 应返回不支持错误")
	}
}

// ──── GetRouterStats 测试 ────

// TestGetRouterStats 测试路由器统计信息。
func TestGetRouterStats(t *testing.T) {
	client := createTestClient(t)
	stats := client.GetRouterStats()
	if stats["total_deployments"] == nil {
		t.Error("stats 应包含 total_deployments")
	}
}

// ──── 非导出辅助函数 ────

// createTestClient 创建测试用 IntelliRouter 客户端。
func createTestClient(t *testing.T) *IntelliRouterModelClient {
	t.Helper()

	// 清空路由缓存
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "sk-test",
					"api_base":   "https://api.test.com",
				},
			},
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建测试客户端失败: %v", err)
	}
	return client
}
