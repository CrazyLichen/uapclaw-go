package external

import (
	"slices"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// init 测试注入：adapter 集经缝提供（D-15.16-e14）——原测试与 adapters
// 同包，迁移后以 fake adapter 名集保持断言面不变。
func init() {
	SetAdaptersProvider(func() []string { return []string{"openclaw", "hermes", "gemini", "generic"} })
	SetAdaptersBuildProvider(func(name string) (*AdapterBuildResult, error) {
		return &AdapterBuildResult{Name: name}, nil
	})
}

// TestBackendFor_sdk与adapter与provider 对齐 backend_for 查找顺序（backends.py:92-112）。
func TestBackendFor_sdk与adapter与provider(t *testing.T) {
	b := BackendFor("claude")
	if b == nil || b.Kind != BackendKindSDK || b.Provider != "claudecode" || b.SupportsCommandOverride {
		t.Fatalf("claude: %+v", b)
	}
	if b.SDKRequirement == nil || b.SDKRequirement.Module != "claude_agent_sdk" {
		t.Fatalf("claude SDK 元数据: %+v", b.SDKRequirement)
	}
	b = BackendFor("codex")
	if b == nil || b.Kind != BackendKindSDK || b.Provider != "codex" {
		t.Fatalf("codex: %+v", b)
	}
	b = BackendFor("openclaw")
	if b == nil || b.Kind != BackendKindAdapter || !b.SupportsCommandOverride {
		t.Fatalf("openclaw: %+v", b)
	}
	if b.InjectsSystemPromptViaArg {
		t.Fatal("openclaw 不经启动参数注入 system prompt")
	}
	if BackendFor("totally-unknown") != nil {
		t.Fatal("未知名应返回 nil")
	}
}

// TestBackendFor_第三方provider命中 对齐第三方 provider 查找分支：注册一个
// 测试 provider 后 BackendFor 应返回 kind=provider。
func TestBackendFor_第三方provider命中(t *testing.T) {
	name := "testcli-backend-probe"
	err := harness_providers.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &fakeBackendProbeProvider{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer harness_providers.UnregisterHarnessProvider(name)
	b := BackendFor(name)
	if b == nil || b.Kind != BackendKindProvider || b.Provider != name {
		t.Fatalf("provider 查找: %+v", b)
	}
	if b.SupportsCommandOverride {
		t.Fatal("provider 后端不支持 command 覆盖")
	}
}

// TestAvailableBackends_包含三类 对齐 available_backends。
func TestAvailableBackends_包含三类(t *testing.T) {
	names := AvailableBackends()
	for _, want := range []string{"claude", "codex", "openclaw", "hermes", "gemini", "generic"} {
		if !slices.Contains(names, want) {
			t.Fatalf("缺少 %q: %v", want, names)
		}
	}
}

// TestThirdPartyProviders_闭集收编例外 偏差 D-15.22-e：codebuddy 已进
// ProviderNames 闭集（D-15.22-b）但经白名单例外保留成员后端资格；其余闭集
// 名（如 dsh）仍按上游 backends.py:78-79 语义排除。
func TestThirdPartyProviders_闭集收编例外(t *testing.T) {
	third := ThirdPartyProviders()
	if !slices.Contains(third, "codebuddy") {
		t.Fatalf("codebuddy 应在成员后端清单: %v", third)
	}
	if slices.Contains(third, "dsh") {
		t.Fatalf("dsh 是内置闭集名，不应在成员后端清单: %v", third)
	}
	b := BackendFor("codebuddy")
	if b == nil || b.Kind != BackendKindProvider || b.Provider != "codebuddy" {
		t.Fatalf("codebuddy 成员后端解析: %+v", b)
	}
	if !IsKnownBackend("codebuddy") {
		t.Fatal("codebuddy 应为已知后端")
	}
}

// TestIsKnownBackend 对齐 is_known_backend。
func TestIsKnownBackend(t *testing.T) {
	if !IsKnownBackend("gemini") {
		t.Fatal("gemini 应为已知后端")
	}
	if IsKnownBackend("nope") {
		t.Fatal("未知名不应为已知后端")
	}
}

// TestTakesAuthFallback_adapter恒false 对齐 takes_auth_fallback 的 adapter 短路。
func TestTakesAuthFallback_adapter恒false(t *testing.T) {
	if TakesAuthFallback("openclaw") {
		t.Fatal("子进程 adapter 永不做 auth fallback")
	}
	if TakesAuthFallback("nope") {
		t.Fatal("未知后端恒 false")
	}
}

// fakeBackendProbeProvider provider 注册探测桩。
type fakeBackendProbeProvider struct{}

func (f *fakeBackendProbeProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{}
}

// Create 满足 HarnessProvider 接口（探测桩不会真正创建）。
func (f *fakeBackendProbeProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return nil, nil
}

// TestProviderTakesEndpointModels_未实现ModelConfigProvider恒false 对齐
// backends.py:127-129/141-145：card 声明 ENDPOINT_MODELS 但未实现
// ModelConfigProvider 可选接口（protocol.py:65）时，两个判定都为 False。
func TestProviderTakesEndpointModels_未实现ModelConfigProvider恒false(t *testing.T) {
	name := "testcli-nomcp-provider"
	err := harness_providers.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &fakeNoModelConfigProvider{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer harness_providers.UnregisterHarnessProvider(name)
	if ProviderTakesEndpointModels(name) {
		t.Fatal("未实现 ModelConfigProvider 应恒 false")
	}
	if TakesAuthFallback(name) {
		t.Fatal("未实现 ModelConfigProvider 应恒 false")
	}
}

// TestTakesAuthFallback_实现ModelConfigProvider且双能力 对齐 backends.py:141-145
// 的 isinstance 前置：实现接口 + card 声明双能力时为 true。
func TestTakesAuthFallback_实现ModelConfigProvider且双能力(t *testing.T) {
	name := "testcli-fullcap-provider"
	err := harness_providers.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &fakeEndpointModelsProvider{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer harness_providers.UnregisterHarnessProvider(name)
	if !ProviderTakesEndpointModels(name) {
		t.Fatal("实现 ModelConfigProvider + ENDPOINT_MODELS 应为 true")
	}
	if !TakesAuthFallback(name) {
		t.Fatal("实现 ModelConfigProvider + 双能力应为 true")
	}
}

// fakeEndpointModelsProvider card 声明 ENDPOINT_MODELS/AUTH_FALLBACK 且实现
// ModelConfigProvider 可选接口的探测桩。
type fakeEndpointModelsProvider struct{}

func (f *fakeEndpointModelsProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Capabilities: map[harnessprotocol.HarnessCapability]bool{
			harnessprotocol.HarnessCapabilityEndpointModels: true,
			harnessprotocol.HarnessCapabilityAuthFallback:   true,
		},
	}
}

// Create 满足 HarnessProvider 接口（探测桩不会真正创建）。
func (f *fakeEndpointModelsProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return nil, nil
}

// ModelConfig 满足 ModelConfigProvider 可选接口（modelConfigCapable 时）。
func (f *fakeEndpointModelsProvider) ModelConfig(choice harnessprotocol.ModelChoice, fallback *harnessprotocol.ModelChoice) (harnessprotocol.JsonObject, error) {
	return harnessprotocol.JsonObject{}, nil
}

// fakeNoModelConfigProvider 只实现 HarnessProvider 的探测桩（无 ModelConfigProvider）。
type fakeNoModelConfigProvider struct{}

func (f *fakeNoModelConfigProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Capabilities: map[harnessprotocol.HarnessCapability]bool{
			harnessprotocol.HarnessCapabilityEndpointModels: true,
			harnessprotocol.HarnessCapabilityAuthFallback:   true,
		},
	}
}

// Create 满足 HarnessProvider 接口（探测桩不会真正创建）。
func (f *fakeNoModelConfigProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return nil, nil
}
