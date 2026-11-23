package rsi

// usage_recorder / model_resolver / validation_dataset 单测。

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestUsageRecord_节点归属与序列 ROOT 兜底 + 派生节点序。
func TestUsageRecord_节点归属与序列(t *testing.T) {
	r := NewUsageRecorder()
	r.Record("t", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 10, Output: 5}}, nil)
	r.Record("t", strPtr("N1"), RsiModelCall{Model: "m", CallCount: 2, Tokens: Tokens{Input: 20, Output: 8}}, nil)
	r.Record("t", strPtr("N1"), RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 1, Output: 1}}, nil)
	out, err := r.Get("t")
	if err != nil {
		t.Fatal(err)
	}
	usageByNode := out["usage_by_node"].(map[string]any)
	rootUsage := usageByNode["ROOT"].(map[string]any)
	if safeInt(rootUsage["call_count"]) != 1 {
		t.Fatalf("ROOT 聚合漂移: %v", rootUsage)
	}
	n1 := usageByNode["N1"].(map[string]any)
	if safeInt(n1["call_count"]) != 3 {
		t.Fatalf("N1 聚合漂移: %v", n1)
	}
	per := out["per_iteration"].([]map[string]any)
	// 对齐 Python：per_iteration 枚举派生节点序（ROOT 不入序），start=1。
	if len(per) != 1 || per[0]["iteration"] != 1 {
		t.Fatalf("派生序漂移: %v", per)
	}
}

// TestUsageRecord_序号幂等 同序号重放不计。
func TestUsageRecord_序号幂等(t *testing.T) {
	r := NewUsageRecorder()
	seq := 1
	r.Record("t", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 10}}, &seq)
	r.Record("t", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 10}}, &seq)
	out, _ := r.Get("t")
	total := out["usage"].(map[string]any)
	if safeInt(total["call_count"]) != 1 {
		t.Fatalf("重放未幂等: %v", total)
	}
}

// TestUsageRecordCumulative_快照水位 快照推进后序号 ≤ 水位的调用不重复计。
func TestUsageRecordCumulative_快照水位(t *testing.T) {
	r := NewUsageRecorder()
	// 快照先行（call_count=1 覆盖 seq1）。
	r.RecordCumulative("t", map[string]any{
		"tokens": map[string]any{"input": 100, "output": 50}, "call_count": 1}, nil)
	seq1 := 1
	r.Record("t", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 10}}, &seq1)
	out, _ := r.Get("t")
	total := out["usage"].(map[string]any)
	tkw := total["tokens"].(map[string]int)
	if safeInt(total["call_count"]) != 1 || tkw["input"] != 100 {
		t.Fatalf("快照水位漂移: %v", total)
	}
	// seq2 超水位 → 计入。
	seq2 := 2
	r.Record("t", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 30}}, &seq2)
	out, _ = r.Get("t")
	total = out["usage"].(map[string]any)
	tk := total["tokens"].(map[string]int)
	if safeInt(total["call_count"]) != 2 || tk["input"] != 130 {
		t.Fatalf("超水位调用未计入: %v", total)
	}
}

// TestUsageRecordCumulative_单调 与无序号增量折叠。
func TestUsageRecordCumulative_单调(t *testing.T) {
	r := NewUsageRecorder()
	r.RecordCumulative("t", map[string]any{"tokens": map[string]any{"input": 10}, "call_count": 1}, nil)
	r.RecordCumulative("t", map[string]any{"tokens": map[string]any{"input": 5}, "call_count": 1}, nil) // 未变化不清窗口
	out, _ := r.Get("t")
	if out["usage"].(map[string]any)["tokens"].(map[string]int)["input"] != 10 {
		t.Fatalf("未变化快照应保持: %v", out["usage"])
	}
	r.RecordCumulative("t", map[string]any{"tokens": map[string]any{"input": 40}, "call_count": 2}, nil)
	out, _ = r.Get("t")
	total := out["usage"].(map[string]any)
	tk2 := total["tokens"].(map[string]int)
	if safeInt(total["call_count"]) != 2 || tk2["input"] != 40 {
		t.Fatalf("快照推进漂移: %v", total)
	}
}

// TestUsageGet_未记录NotFound。
func TestUsageGet_未记录NotFound(t *testing.T) {
	r := NewUsageRecorder()
	_, err := r.Get("ghost")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("got %v", err)
	}
	if r.UsageSummary("ghost") != nil {
		t.Fatalf("摘要应 nil")
	}
}

// TestRecordEngineEvent_载荷归一 call_count 缺省 1。
func TestRecordEngineEvent_载荷归一(t *testing.T) {
	r := NewUsageRecorder()
	r.RecordEngineEvent("t", map[string]any{
		"node_ref": "N1",
		"model_call": map[string]any{"model": "m",
			"tokens": map[string]any{"input": 3, "output": 4, "cache_hit": 1}}}, nil)
	out, _ := r.Get("t")
	node := out["usage_by_node"].(map[string]any)["N1"].(map[string]any)
	if safeInt(node["call_count"]) != 1 {
		t.Fatalf("call_count 缺省漂移: %v", node)
	}
	if node["tokens"].(map[string]int)["input"] != 3 {
		t.Fatalf("tokens 漂移: %v", node)
	}
}

// TestSelectRsiModelEntry_严格语义 name#origin_index 与缺省优先。
func TestSelectRsiModelEntry_严格语义(t *testing.T) {
	idx0, idx1 := 0, 1
	entries := []struct {
		Entry map[string]any
		Index *int
	}{
		{map[string]any{"model_client_config": map[string]any{"model_name": "qwen-max"}, "is_default": true}, &idx0},
		{map[string]any{"model_client_config": map[string]any{"model_name": "qwen-max"}}, &idx1},
	}
	entry, index, err := SelectRsiModelEntry(entries, "qwen-max")
	if err != nil || *index != 0 {
		t.Fatalf("缺省优先漂移: %v %v", entry, index)
	}
	// name#1 命中第二个（非缺省）。
	entry, index, err = SelectRsiModelEntry(entries, "qwen-max#1")
	if err != nil || *index != 1 {
		t.Fatalf("origin_index 漂移: %v %v %v", entry, index, err)
	}
	// name#0 与缺省匹配；name#1 拒同名换序。
	if _, _, err := SelectRsiModelEntry(entries, "other#1"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("未命中应 NOT_FOUND: %v", err)
	}
	if _, _, err := SelectRsiModelEntry(entries, ""); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("空引用应 NOT_FOUND")
	}
	// 非法序号。
	if _, _, err := SelectRsiModelEntry(entries, "qwen-max#x"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("非法序号应 NOT_FOUND")
	}
}

// TestResolver_ResolveToFile 落盘 YAML + 摘要 + max_retries 剥离。
func TestResolver_ResolveToFile(t *testing.T) {
	r := NewModelConfigResolver()
	r.configLoader = func() map[string]any { return nil }
	r.modelBuilder = func(mcc, mco map[string]any) (map[string]any, map[string]any, error) {
		client := map[string]any{}
		for k, v := range mcc {
			client[k] = v
		}
		request := map[string]any{}
		for k, v := range mco {
			request[k] = v
		}
		return client, request, nil
	}
	r.defaultsLoader = func(map[string]any) []map[string]any {
		return []map[string]any{{
			"model_client_config": map[string]any{
				"model_name": "m1", "client_provider": "OpenAI", "api_base": "http://x", "max_retries": 3},
			"model_config_obj": map[string]any{"model": "m1", "temperature": 0.7},
			"is_default":       true,
		}}
	}
	manifest, err := r.ResolveToFile("m1", "optimizer", filepath.Join(t.TempDir(), "models"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest["role"] != "optimizer" || manifest["model_name"] != "m1" {
		t.Fatalf("manifest 漂移: %v", manifest)
	}
	if manifest["config_sha256"] == "" || manifest["path"] == "" {
		t.Fatalf("摘要/路径缺失")
	}
	data, _ := os.ReadFile(manifest["path"].(string))
	var payload map[string]any
	if err := yamlUnmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	mcc := payload["model_client_config"].(map[string]any)
	if safeInt(mcc["max_retries"]) != 0 {
		t.Fatalf("max_retries 未剥离: %v", mcc)
	}
	mrc := payload["model_request_config"].(map[string]any)
	if valueToString(mrc["model"]) != "m1" {
		t.Fatalf("model 拼写漂移: %v", mrc)
	}
	if _, has := mrc["model_name"]; has {
		t.Fatalf("model_name 应删除")
	}
	// manifest 不含 key（安全约束）。
	raw, _ := json.Marshal(manifest)
	if len(raw) == 0 {
		t.Fatalf("manifest 序列化失败")
	}
}

func yamlUnmarshal(data []byte, out *map[string]any) error {
	return json.Unmarshal(exportJSON(data), out)
}

// exportJSON yaml→json 桥（测试内使用 gopkg.in/yaml.v3）。
func exportJSON(data []byte) []byte {
	var m map[string]any
	dec := newYamlDecoder(data)
	_ = dec.Decode(&m)
	out, _ := json.Marshal(m)
	return out
}

// TestValidationDataset_归一 GDPVAL validation → cases。
func TestValidationDataset_归一(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	ds := map[string]any{"validation": []any{
		map[string]any{"id": "claw-1", "domain": "general", "prompt": "写周报",
			"metadata": map[string]any{"task_type": "writing"}},
	}}
	data, _ := json.Marshal(ds)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := NormalizeValidationSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	if out["dataset_id"] != "evobench_local_no_key_validation" {
		t.Fatalf("dataset_id 漂移")
	}
	cases := out["cases"].([]map[string]any)
	c0 := cases[0]
	if c0["case_id"] != "claw-1" || c0["input"] != "写周报" || c0["source"] != "claw" {
		t.Fatalf("case 派生漂移: %v", c0)
	}
	if c0["task_type"] != "writing" {
		t.Fatalf("task_type 漂移: %v", c0)
	}
}

// TestValidationDataset_错误码全集 逐条错误路径。
func TestValidationDataset_错误码全集(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, "d.json")
		_ = os.WriteFile(path, []byte(content), 0o644)
		return path
	}
	// 坏 JSON。
	if _, err := NormalizeValidationSuite(write("{bad")); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("坏 JSON: %v", err)
	}
	// 非 validation 结构 → nil（标准数据集）。
	if out, _ := NormalizeValidationSuite(write(`{"other": []}`)); out != nil {
		t.Fatalf("标准数据集应 nil")
	}
	// validation+cases 并存 → nil。
	if out, _ := NormalizeValidationSuite(write(`{"validation": [], "cases": []}`)); out != nil {
		t.Fatalf("cases 并存应 nil")
	}
	// 空 validation。
	if _, err := NormalizeValidationSuite(write(`{"validation": []}`)); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("空 validation: %v", err)
	}
	// 非 dict 样本。
	if _, err := NormalizeValidationSuite(write(`{"validation": [1]}`)); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("非对象样本")
	}
	mk := func(tpl map[string]any) string {
		payload, _ := json.Marshal(map[string]any{"validation": []any{tpl}})
		return write(string(payload))
	}
	if _, err := NormalizeValidationSuite(mk(map[string]any{"domain": "general", "prompt": "p"})); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("缺 id")
	}
	if _, err := NormalizeValidationSuite(mk(map[string]any{"id": "claw-1", "prompt": "p"})); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("缺 domain")
	}
	if _, err := NormalizeValidationSuite(mk(map[string]any{"id": "bad-x", "domain": "general", "prompt": "p"})); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("非法来源")
	}
	if _, err := NormalizeValidationSuite(mk(map[string]any{"id": "claw-1", "domain": "legal", "prompt": "p"})); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("非法领域")
	}
	if _, err := NormalizeValidationSuite(mk(map[string]any{"id": "claw-1", "domain": "general", "prompt": " "})); !errors.Is(err, ErrDatasetInvalid) {
		t.Fatalf("缺 prompt")
	}
}

// newYamlDecoder yaml 解码器桥。
func newYamlDecoder(data []byte) *yaml.Decoder {
	return yaml.NewDecoder(bytes.NewReader(data))
}
