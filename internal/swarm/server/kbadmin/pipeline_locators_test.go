package kbadmin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// ──────────────────────────── 30.3 Task7：locators 落库链测试 ────────────────────────────

// TestChunkStep_locators落库 pdf 文档走 parseStep→chunkStep 真链，
// kb_chunk_meta.locators_json 非空且结构合法（真实文件禁替身）。
func TestChunkStep_locators落库(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kbadmin.db")
	st, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	env := &pipelineEnv{store: st}
	job := &ParseJob{
		TaskID:   "task-loc-1",
		DocID:    "doc-loc-1",
		KBID:     "kb-loc-1",
		KBType:   "document",
		FilePath: "../../../openuapclaw/core/retrieval/indexing/processor/parser/testdata/fixture_pdf_multipage.pdf",
		Config:   KBConfig{Chunker: ChunkerConfig{ChunkSize: 20, ChunkOverlap: 0}},
	}

	// 本测试只验证 parse→chunk 两步的 locators 落库；embedding 需 kbFactory
	// 装配（向量库 provider 依赖），属 Task8 集成测试全链覆盖范围
	steps := pipelinePlans["document"][:2]
	if err := runPipeline(context.Background(), job, steps, env); err != nil {
		t.Fatalf("pipeline 不应失败: %v", err)
	}

	metas, err := st.Chunk().ListByDoc("doc-loc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) == 0 {
		t.Fatal("应产出 chunk 台账行")
	}
	var withLocators int
	for _, m := range metas {
		if m.LocatorsJSON == "" {
			continue
		}
		withLocators++
		var locs []common.SourceLocator
		if err := json.Unmarshal([]byte(m.LocatorsJSON), &locs); err != nil {
			t.Fatalf("locators_json 反序列化失败: %v", err)
		}
		var hasText bool
		for _, l := range locs {
			if l.Type == common.LocatorTypeText && l.PageNo >= 1 && len(l.LineBBoxes) > 0 {
				hasText = true
				for _, b := range l.LineBBoxes {
					if b[2] <= b[0] || b[3] <= b[1] {
						t.Fatalf("bbox 非法: %v", b)
					}
				}
			}
		}
		if !hasText {
			t.Fatalf("chunk %s 应含 text 锚: %s", m.ID, m.LocatorsJSON)
		}
	}
	if withLocators == 0 {
		t.Fatal("应有 chunk 携带 locators_json")
	}
}

// TestResolveVisionLLM_无配置软失败 默认模型未配置时返回 nil 不报错
// （captioner 禁用态，spec §6）。
func TestResolveVisionLLM_无配置软失败(t *testing.T) {
	m := resolveVisionLLM()
	// 环境无默认模型配置时为 nil；有配置时非 nil 且可直接调用装配面
	if m == nil {
		t.Log("环境无默认模型配置，caption 禁用态（符合软失败语义）")
	}
}

// TestMarshalLocators 空/非空两态。
func TestMarshalLocators(t *testing.T) {
	if got := marshalLocators(nil); got != "" {
		t.Fatalf("空 locators 应空串，实际 %q", got)
	}
	got := marshalLocators([]common.SourceLocator{
		{Type: common.LocatorTypeParagraph, Paragraph: 3, Quote: "q"},
	})
	if got == "" {
		t.Fatal("非空 locators 应序列化")
	}
}
