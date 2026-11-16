package externalcli

import (
	"slices"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

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
