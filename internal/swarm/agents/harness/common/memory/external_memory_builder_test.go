package memory

import (
	"os"
	"strings"
	"testing"

	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// TestBuildExternalMemoryRail_provider为空 对齐 Python L41-43：无 provider 返回 None
func TestBuildExternalMemoryRail_provider为空(t *testing.T) {
	cfg := map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{},
	}}
	if rail := BuildExternalMemoryRail(cfg, "."); rail != nil {
		t.Errorf("provider 空时应返回 nil, got %T", rail)
	}
}

// TestBuildExternalMemoryRail_未知provider 对齐 Python L54 + L133-142：
// 未知 provider 走插件分支，plugin_discovery 不可用 → warn + None
// 回归偏差 #8：agentarts 已移除，落未知分支返回 nil
func TestBuildExternalMemoryRail_未知provider(t *testing.T) {
	for _, name := range []string{"agentarts", "unknown-prov"} {
		cfg := map[string]any{"memory": map[string]any{
			"engine": "both",
			"external": map[string]any{
				"provider": name,
				"mem0":     map[string]any{"api_key": "k"},
			},
		}}
		if rail := BuildExternalMemoryRail(cfg, "."); rail != nil {
			t.Errorf("provider=%s 应返回 nil（插件机制不可用）, got %T", name, rail)
		}
	}
}

// TestBuildExternalMemoryRail_mem0可用 对齐 _build_mem0_provider (L89-107)
func TestBuildExternalMemoryRail_mem0可用(t *testing.T) {
	cfg := map[string]any{"memory": map[string]any{
		"engine": "both",
		"external": map[string]any{
			"provider": "mem0",
			"mem0":     map[string]any{"api_key": "sk-test"},
		},
	}}
	rail := BuildExternalMemoryRail(cfg, ".")
	if rail == nil {
		t.Fatal("mem0 配置完整时应构建成功")
	}
	if _, ok := rail.(*memrail.ExternalMemoryRail); !ok {
		t.Fatalf("类型 = %T, want *memrail.ExternalMemoryRail", rail)
	}
}

// TestBuildMem0Provider_默认值与环境回退 对齐 L89-107（含品牌映射）
func TestBuildMem0Provider_默认值与环境回退(t *testing.T) {
	// 纯 env 场景（无 mem0 配置段）
	t.Setenv("MEM0_API_KEY", "env-key")
	t.Setenv("MEM0_USER_ID", "env-user")
	t.Setenv("MEM0_AGENT_ID", "env-agent")

	p := buildMem0Provider(ExternalMemoryConfig{})
	if p == nil {
		t.Fatal("env 提供时应构建成功")
	}
	if !p.IsAvailable() {
		t.Error("有 api_key 应可用")
	}
	// SystemPromptBlock 含 user_id，env-user 应出现（env 优先于品牌默认）
	if !strings.Contains(p.SystemPromptBlock(), "env-user") {
		t.Errorf("SystemPromptBlock = %q, 应含 env-user（env 优先于品牌默认）", p.SystemPromptBlock())
	}
}

// TestBuildMem0Provider_无key返回nil 对齐 L104-106：is_available false → None
func TestBuildMem0Provider_无key返回nil(t *testing.T) {
	t.Setenv("MEM0_API_KEY", "") // 防 CI 环境泄漏
	if p := buildMem0Provider(ExternalMemoryConfig{}); p != nil {
		t.Error("无 api_key 应返回 nil")
	}
}

// TestBuildMem0Provider_品牌默认值 对齐 L94-95：
// jiuwenswarm-user/jiuwenswarm → uapclawswarm-user/uapclawswarm（品牌映射）
func TestBuildMem0Provider_品牌默认值(t *testing.T) {
	t.Setenv("MEM0_API_KEY", "k")
	t.Setenv("MEM0_USER_ID", "")
	t.Setenv("MEM0_AGENT_ID", "")

	p := buildMem0Provider(ExternalMemoryConfig{
		Mem0: map[string]any{"api_key": "k"},
	})
	if p == nil {
		t.Fatal("有 api_key 应构建成功")
	}
	// SystemPromptBlock 格式 "Active. User: {user_id}." 断言品牌默认 user
	if !strings.Contains(p.SystemPromptBlock(), "uapclawswarm-user") {
		t.Errorf("SystemPromptBlock = %q, 应含品牌默认 user uapclawswarm-user", p.SystemPromptBlock())
	}
}

// TestBuildOpenVikingProvider_无endpoint返回nil 对齐 L127-129
func TestBuildOpenVikingProvider_无endpoint返回nil(t *testing.T) {
	t.Setenv("OPENVIKING_ENDPOINT", "") // 防 CI 环境泄漏
	if p := buildOpenVikingProvider(ExternalMemoryConfig{}); p != nil {
		t.Error("无 endpoint 应返回 nil")
	}
}

// TestBuildOpenVikingProvider_构建 对齐 L110-130：
// OPENVIKING_* env 回退由 provider 内部完成（agentcore 既有测试覆盖内部默认值），
// builder 侧验证构建成功边界
func TestBuildOpenVikingProvider_构建(t *testing.T) {
	p := buildOpenVikingProvider(ExternalMemoryConfig{
		OpenViking: map[string]any{"endpoint": "http://vk"},
	})
	if p == nil {
		t.Fatal("有 endpoint 应构建成功")
	}
	if p.Name() != "openviking" {
		t.Errorf("Name = %q, want openviking", p.Name())
	}
}

// TestBuildOpenJiuwenProvider_构建 对齐 L81-86：BuildOpenJiuwenProviderConfig 组装
func TestBuildOpenJiuwenProvider_构建(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	t.Setenv("EMBED_MODEL", "m1")
	p := buildOpenJiuwenProvider(ExternalMemoryConfig{})
	if p == nil {
		t.Fatal("openjiuwen 分支应构建成功（provider 自身 is_available 宽松）")
	}
	if p.Name() != "openjiuwen" {
		t.Errorf("Name = %q, want openjiuwen", p.Name())
	}
}

// TestBuildOpenJiuwenProvider_mkdir失败 对齐 Python 异常传播：
// _resolve_ltm_dir mkdir 失败 → 整链返回 nil
func TestBuildOpenJiuwenProvider_mkdir失败(t *testing.T) {
	// UAPCLAW_DATA_DIR 指向「文件/子目录」路径 → MkdirAll 失败（父路径是文件）
	blocker := t.TempDir()
	blockFile := blocker + "/blocker"
	if err := writeFile(blockFile, ""); err != nil {
		t.Fatalf("准备阻挡文件失败: %v", err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", blockFile+"/sub")
	// WorkspaceDir 有 sync.Once 缓存，Setenv 后必须重置才能生效
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())

	p := buildOpenJiuwenProvider(ExternalMemoryConfig{})
	if p != nil {
		t.Error("mkdir 失败应沿链返回 nil（对齐 Python 异常传播）")
	}
}

// TestBuildExternalMemoryRail_成功路径 对齐 L66-75：rail 构造使用 ext_cfg 默认值
func TestBuildExternalMemoryRail_成功路径(t *testing.T) {
	t.Setenv("MEM0_API_KEY", "k")
	cfg := map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": "mem0"},
	}}
	rail := BuildExternalMemoryRail(cfg, ".")
	if rail == nil {
		t.Fatal("应构建成功")
	}
	if _, ok := rail.(*memrail.ExternalMemoryRail); !ok {
		t.Fatalf("类型 = %T", rail)
	}
	// user_id/scope_id 默认值填充已在 TestGetExternalMemoryConfig_默认值填充 覆盖，
	// 此处断言 rail 类型 + 可用 provider 即可
}

// TestLoadPluginProvider 对齐 L133-142：插件机制不可用恒 nil
func TestLoadPluginProvider(t *testing.T) {
	if p := loadPluginProvider("any", nil); p != nil {
		t.Error("插件机制不可用应返回 nil")
	}
}

// writeFile 写测试文件辅助
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
