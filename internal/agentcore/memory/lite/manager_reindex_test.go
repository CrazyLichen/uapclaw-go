package lite

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeEmbedProvider 可配置 id/model/dims 的假 provider。
// 存在的意义：needsReindex 必须比对「实例值」（Python: self.provider.id），
// 而 settings 里的 provider 是恒定配置值；只有换掉实例才能构造出
// 「配置没变、实例变了」这条线上真实发生的场景（embed 缺 key → 降级 mock）。
type fakeEmbedProvider struct {
	// id provider 标识
	id string
	// model 模型名
	model string
	// dims 向量维度
	dims int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EmbedQuery 返回全零的定长向量（本文件不关心向量内容）
func (f *fakeEmbedProvider) EmbedQuery(context.Context, string) ([]float64, error) {
	return make([]float64, f.dims), nil
}

// EmbedDocuments 批量接口（未使用）
func (f *fakeEmbedProvider) EmbedDocuments(context.Context, []string) ([][]float64, error) {
	return nil, nil
}

// ID 返回 provider 标识
func (f *fakeEmbedProvider) ID() string { return f.id }

// Model 返回模型名
func (f *fakeEmbedProvider) Model() string { return f.model }

// Dims 返回向量维度
func (f *fakeEmbedProvider) Dims() int { return f.dims }

// ConfigFingerprint 返回配置指纹（测试替身固定值）
func (f *fakeEmbedProvider) ConfigFingerprint() string { return f.id + ":" + f.model }

// TestNeedsReindex_表驱动 覆盖重建判定的各条路径。
// 对齐 Python _should_full_reindex（manager.py:906）的判定项，并补齐
// providerKey / vectorDims 两项。
func TestNeedsReindex_表驱动(t *testing.T) {
	baseMeta := func(over map[string]any) map[string]any {
		meta := map[string]any{
			"provider": "mock",
			"model":    "mock",
			// providerKey 现为 config fingerprint；fake 替身返回 id:model
			"providerKey": "mock:mock",
			"chunkTokens": float64(256),
		}
		for k, v := range over {
			meta[k] = v
		}
		return meta
	}

	cases := []struct {
		// name 用例名
		name string
		// meta 要写入的 meta；nil 表示不写
		meta map[string]any
		// providerID 实例 provider 标识
		providerID string
		// providerMd 实例模型名
		providerMd string
		// want 期望是否需要重建
		want bool
		// wantReason 期望原因
		wantReason string
	}{
		{"meta 缺失", nil, "mock", "mock", true, "meta_missing"},
		{"全一致", baseMeta(nil), "mock", "mock", false, ""},
		{"provider 从 mock 换成真模型", baseMeta(nil), "openai_compatible", "text-embedding-v3", true, "provider_changed"},
		{"model 变化", baseMeta(nil), "mock", "other-model", true, "model_changed"},
		{"chunkTokens 变化", baseMeta(map[string]any{"chunkTokens": float64(128)}), "mock", "mock", true, "chunking_changed"},
		{"providerKey 变化（换端点或密钥）", baseMeta(map[string]any{"providerKey": "mock:mock:https://old:aaa"}), "mock", "mock", true, "provider_key_changed"},
		{"vectorDims 变化", baseMeta(map[string]any{"vectorDims": float64(128)}), "mock", "mock", true, "vector_dims_changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, cleanup := newReindexTestManager(t)
			defer cleanup()

			m.provider = &fakeEmbedProvider{id: tc.providerID, model: tc.providerMd, dims: 1024}
			// providerKey 必须与实例 fingerprint 同步：needsReindex 现在比对它，
			// 不同步的话每个用例都会被判成 provider_key_changed
			m.providerKey = m.provider.ConfigFingerprint()
			if tc.meta != nil {
				if err := m.writeMeta(tc.meta); err != nil {
					t.Fatalf("写 meta 失败: %v", err)
				}
			} else {
				// manager 初始化时已跑过 initial sync 并写入 meta，这里显式删掉以构造「meta 缺失」
				if _, err := m.db.Exec("DELETE FROM meta"); err != nil {
					t.Fatalf("删除 meta 失败: %v", err)
				}
			}

			got, reason := m.needsReindex()
			if got != tc.want || reason != tc.wantReason {
				t.Errorf("needsReindex() = (%v, %q), want (%v, %q)", got, reason, tc.want, tc.wantReason)
			}
		})
	}
}

// TestNeedsReindex_provider为nil 初始化失败时也必须要求重建（并打 Error）
func TestNeedsReindex_provider为nil(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.provider = nil
	got, reason := m.needsReindex()
	if !got || reason != "provider_unavailable" {
		t.Errorf("needsReindex() = (%v, %q), want (true, provider_unavailable)", got, reason)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newReindexTestManager 造一个带真实 sqlite（含 meta 表）的 manager。
func newReindexTestManager(t *testing.T) (*memoryIndexManager, func()) {
	t.Helper()
	ws := workspace.NewWorkspace(t.TempDir(), "cn")
	nodePath := ws.GetNodePath("memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 memory 节点")
	}
	settings := CreateMemorySettings(*nodePath, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0

	mgr, err := GetMemoryIndexManager(MemoryManagerParams{
		AgentID:   "unit_reindex",
		Workspace: ws,
		Settings:  settings,
		NodeName:  "memory",
	})
	if err != nil {
		t.Fatalf("初始化 manager 失败: %v", err)
	}
	m := mgr.(*memoryIndexManager)
	return m, func() {
		_ = m.Close()
		ClearMemoryManagerCache()
	}
}

// TestRunReindex_meta写实例值 回归：meta 必须记**实例**值（mock），而不是 settings 的
// openai_compatible。否则下次启动判定不出「provider 换了」，永远不重建索引。
// 对齐 Python _run_reindex（manager.py:611）：meta["provider"] = self.provider.id。
func TestRunReindex_meta写实例值(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.provider = &fakeEmbedProvider{id: "mock", model: "mock", dims: 128}
	if err := m.runReindex(context.Background(), "unit"); err != nil {
		t.Fatalf("runReindex 失败: %v", err)
	}

	meta, err := m.readMeta()
	if err != nil {
		t.Fatalf("读 meta 失败: %v", err)
	}
	if meta["provider"] != "mock" || meta["model"] != "mock" {
		t.Errorf("meta 应记实例值 mock/mock，实际 %v/%v", meta["provider"], meta["model"])
	}
}
