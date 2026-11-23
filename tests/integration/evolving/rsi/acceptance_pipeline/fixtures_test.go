//go:build integration

// 17.10 acceptance_pipeline 共享夹具：fake OpenAI 兼容端点 / 模型配置 /
// 数据集 / harness 引用，提取自 evaluator_scoring_pipeline 手法（spec
// 2026-12-12-rsi-17.10-acceptance-pipeline-design §3）。父目录 rsitest 包的
// 非导出符号跨包不可见，本包按需重建。
//
// 对应 Python 代码：tests/system_tests/rsi/（e2e 形态）+
// tests/unit_tests/rsi/（夹具语义基线）。

package rsi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// accFakeLLM 可编程 fake OpenAI 兼容端点：按请求序弹出预置回复，
// 记录请求体供断言；slowDur>0 时每次响应先 sleep（制造取消窗口）。
type accFakeLLM struct {
	// replies 预置回复序列（choices[0].message.content 文本）；超出序号回落 "REPLY"
	replies []string
	// slowDur 每次响应前的 sleep 毫秒数（0=不延迟）
	slowDur int

	mu     sync.Mutex
	calls  int
	bodies []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// server 启动 fake 端点（调用方负责 Close）。
func (f *accFakeLLM) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		i := f.calls
		f.calls++
		f.bodies = append(f.bodies, string(body))
		content := "REPLY"
		if i < len(f.replies) {
			content = f.replies[i]
		}
		slow := f.slowDur
		f.mu.Unlock()
		if slow > 0 {
			time.Sleep(time.Duration(slow) * time.Millisecond)
		}
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

// callsCount 已发生的请求数（并发安全快照）。
func (f *accFakeLLM) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// accWriteModelConfig 写模型配置 JSON（api_base 指向 fake 端点），
// 返回文件路径。形态复刻 ScoringPipelineSuite.SetupTest。
func accWriteModelConfig(t *testing.T, apiBase string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "model_config.yaml")
	raw, err := json.Marshal(map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{
				"client_provider": "OpenAI", "client_id": "acc-client",
				"api_key": "test-key", "api_base": apiBase,
				"verify_ssl": false, "timeout": 30.0,
			},
			"model_request_config": map[string]any{"model": "fake-model"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// accWriteDataset 写两 case 数据集（1 答对 + 1 答错，exact_match 型），
// 形态复刻 ScoringPipelineSuite.writeDataset。
func accWriteDataset(t *testing.T, dir string) harnessrsi.DatasetArtifact {
	t.Helper()
	casesFile := filepath.Join(dir, "cases.json")
	raw, err := json.Marshal([]map[string]any{
		{
			"case_id":    "case-pass",
			"task_input": map[string]any{"query": "answer with REPLY"},
			"reference":  map[string]any{"answer": "REPLY"},
		},
		{
			"case_id":    "case-fail",
			"task_input": map[string]any{"query": "answer with REPLY"},
			"reference":  map[string]any{"answer": "DIFFERENT"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return harnessrsi.DatasetArtifact{
		DatasetID:    "ds-acc",
		DatasetDir:   dir,
		DatasetFiles: []string{casesFile},
	}
}

// accWriteHarnessRefs 写最小 harness 引用（role → harness 目录路径 + 最小
// harness_config.yaml），形态复刻 ScoringPipelineSuite 全链用例。
func accWriteHarnessRefs(t *testing.T, root string) string {
	t.Helper()
	harnessDir := filepath.Join(root, "harness", "solver")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "harness_config.yaml"),
		[]byte("schema_version: harness_config.v0.1\nid: acc-solver\nname: acc-solver\nlanguage: en\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refsPath := filepath.Join(root, "harness_refs.yaml")
	if err := os.WriteFile(refsPath,
		[]byte("harness_refs:\n  solver: "+harnessDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return refsPath
}
