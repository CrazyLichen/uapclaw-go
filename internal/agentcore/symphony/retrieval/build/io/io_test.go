package io

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build"
)

// TestSortTreeNodes 段数优先排序
func TestSortTreeNodes(t *testing.T) {
	nodes := []any{
		map[string]any{"cid": "1.2.3", "type": "c"},
		map[string]any{"cid": "2", "type": "b"},
		map[string]any{"cid": "1.1", "type": "a"},
		"junk", // 非字典剔除
	}
	sorted := SortTreeNodes(nodes)
	if len(sorted) != 3 {
		t.Fatalf("非字典项应剔除: %d", len(sorted))
	}
	// Python key=(len(cid.split(".")), cid)：段数升序 → 2(1段) < 1.1(2段) < 1.2.3(3段)
	if strAny(sorted[0].(map[string]any), "cid") != "2" ||
		strAny(sorted[1].(map[string]any), "cid") != "1.1" ||
		strAny(sorted[2].(map[string]any), "cid") != "1.2.3" {
		t.Fatalf("排序不符: %v", sorted)
	}
}

// TestWriteTreePreset 写盘字段序与条件输出
func TestWriteTreePreset(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tree_index.yaml")
	payload := map[string]any{"nodes": []any{
		map[string]any{"cid": "1", "type": "skill", "description": "描述", "worker_id": " w "},
		map[string]any{"cid": "0", "type": "root"},
	}}
	if err := WriteTreePreset(payload, p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	text := string(data)
	if !strings.HasPrefix(text, "nodes:\n") {
		t.Fatalf("首行不符: %q", text)
	}
	// 排序后 0 在前
	if !strings.Contains(text, `  - cid: "0"`) || strings.Index(text, `"0"`) > strings.Index(text, `"1"`) {
		t.Fatalf("排序不符: %s", text)
	}
	// description/worker_id（strip 后）输出；select_when 等空字段不输出
	if !strings.Contains(text, "描述") || !strings.Contains(text, "worker_id") {
		t.Fatalf("字段输出不符: %s", text)
	}
	if strings.Contains(text, "select_when") {
		t.Fatal("空 select_when 不应输出")
	}
}

// TestLoadTreePreset 往返与回落
func TestLoadTreePreset(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tree_index.yaml")
	payload := map[string]any{"nodes": []any{
		map[string]any{"cid": "1.2", "type": "s", "description": "d"},
		map[string]any{"cid": "1", "type": "r"},
	}}
	if err := WriteTreePreset(payload, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTreePreset(p)
	if err != nil {
		t.Fatal(err)
	}
	nodes := loaded["nodes"].([]any)
	if strAny(nodes[0].(map[string]any), "cid") != "1" {
		t.Fatalf("读回应已排序: %v", nodes)
	}
	// 简单格式回落（非标准 YAML 无法通过 yaml.Unmarshal 时）
	p2 := filepath.Join(dir, "simple.yaml")
	_ = os.WriteFile(p2, []byte("nodes:\n  - cid: 2\n    type: s\n  - cid: 1\n    type: r\n"), 0o644)
	loaded2, err := LoadTreePreset(p2)
	if err != nil {
		t.Fatal(err)
	}
	if strAny(loaded2["nodes"].([]any)[0].(map[string]any), "cid") != "1" {
		t.Fatalf("简单解析应有序: %v", loaded2)
	}
	// 文件不存在 → error
	if _, err = LoadTreePreset(filepath.Join(dir, "nope.yaml")); err == nil {
		t.Fatal("不存在应报错")
	}
}

// TestParseSimpleNodesYAML 值类型分支
func TestParseSimpleNodesYAML(t *testing.T) {
	text := "nodes:\n  - cid: 1.1\n    count: 3\n    ratio: 0.5\n    flag: true\n    none: null\n    arr: [a, b]\n    plain: hello\n"
	out := ParseSimpleNodesYAML(text)
	nodes := out["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("节点数不符: %v", nodes)
	}
	n := nodes[0].(map[string]any)
	if n["count"] != json.Number("3") && n["count"] != float64(3) {
		t.Fatalf("数字解析不符: %v (%T)", n["count"], n["count"])
	}
	if n["flag"] != true || n["none"] != nil { // json null→nil 即期望
		t.Fatalf("布尔/null 解析不符: %v", n)
	}
	// Python :114-118：JSON 解析失败回落 strip('"') → 未引号元素数组保持字符串
	if n["arr"] != "[a, b]" {
		t.Fatalf("数组回落语义不符: %v (%T)", n["arr"], n["arr"])
	}
	if n["plain"] != "hello" {
		t.Fatalf("普通串解析不符: %v", n["plain"])
	}
}

// TestNormalizeItemPaths 规范化与去重
func TestNormalizeItemPaths(t *testing.T) {
	abs, _ := filepath.Abs(".")
	got := NormalizeItemPaths([]string{abs, abs, "", "  ", "jsonl://skill/x", "s3://bucket/p"})
	if len(got) != 3 {
		t.Fatalf("去重/空剔除不符: %v", got)
	}
	if got[0] != abs || got[1] != "jsonl://skill/x" || got[2] != "s3://bucket/p" {
		t.Fatalf("路径语义不符: %v", got)
	}
}

// TestIsPassthroughItemURI 判定
func TestIsPassthroughItemURI(t *testing.T) {
	cases := map[string]bool{
		"https://x/y":      true,
		"http://x":         true,
		"s3://b/k":         true,
		"JSONL://skill/1":  true,
		"/local/path":      false,
		"":                 false,
	}
	for in, want := range cases {
		if got := IsPassthroughItemURI(in); got != want {
			t.Fatalf("%q 应为 %v，得 %v", in, want, got)
		}
	}
}

// TestParseJSONLScannedItems 解析全分支
func TestParseJSONLScannedItems(t *testing.T) {
	content := "\ufeff" +
		`{"contentExtendParam": {"skillId": "a", "skillName": "A", "skillDesc": "da", "stars": 5, "isOfficial": true, "contentHash": "h1"}}` + "\n" +
		`{"contentExtendParam": {"skillId": "b"}}` + "\n" +              // 无 name 回落 skillId
		`{"nope": 1}` + "\n" +                                            // 缺 contentExtendParam → 剔除
		`{"contentExtendParam": {"skillId": ""}}` + "\n" +                // 空 skillId → 剔除
		`{"contentExtendParam": {"skillId": "a", "skillName": "dup"}}` + "\n" + // 重复 → 忽略
		`broken{json` + "\n" +                                            // 坏项 → 重同步
		`{"contentExtendParam": {"skillId": "c", "skillName": "C", "skillPath": "/p", "content": "body"}}`
	scanned, ordered := ParseJSONLScannedItems(content)
	if len(scanned) != 3 {
		t.Fatalf("应解析出 a/b/c 三项: %v", scanned)
	}
	a := scanned["a"]
	if a["name"] != "A" || a["description"] != "da" || a["stars"] != 5 || a["is_official"] != true || a["content_hash"] == "" {
		t.Fatalf("a 项字段不符: %v", a)
	}
	if scanned["b"]["name"] != "b" {
		t.Fatalf("b 项 name 应回落 skillId: %v", scanned["b"])
	}
	if scanned["c"]["path"] != "/p" || scanned["c"]["content"] != "body" {
		t.Fatalf("c 项字段不符: %v", scanned["c"])
	}
	if len(ordered) != 3 || ordered[0] != "jsonl://skill/a" || ordered[2] != "jsonl://skill/c" {
		t.Fatalf("有序路径不符: %v", ordered)
	}
}

// TestLoadItemsJSONLText 本地分支与错误
func TestLoadItemsJSONLText(t *testing.T) {
	if s, _ := LoadItemsJSONLText(""); s != "" {
		t.Fatal("空路径应返回空串")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "items.jsonl")
	_ = os.WriteFile(p, []byte("{}"), 0o644)
	if s, err := LoadItemsJSONLText(p); err != nil || s != "{}" {
		t.Fatalf("本地读取不符: %q %v", s, err)
	}
	if _, err := LoadItemsJSONLText(filepath.Join(dir, "nope.jsonl")); err == nil {
		t.Fatal("不存在应报错")
	}
	if _, err := LoadItemsJSONLText("s3://b/k"); err != ErrS3NotImplemented {
		t.Fatalf("S3 应返回 D-16-4-1 登记错误: %v", err)
	}
}

// TestManifestWriteLoad 往返
func TestManifestWriteLoad(t *testing.T) {
	dir := t.TempDir()
	abs, _ := filepath.Abs(dir)
	if err := WriteManifest(dir, []string{abs, "jsonl://x"}, []string{"w1", "w2"}, "full", "skill"); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["mode"] != "full" || m["count"] != float64(2) || m["item_type"] != "skill" {
		t.Fatalf("manifest 字段不符: %v", m)
	}
	paths := m["item_paths"].([]any)
	if paths[0] != abs || paths[1] != "jsonl://x" {
		t.Fatalf("路径序列化不符: %v", paths)
	}
	// 不存在 → 空表
	empty, err := LoadManifest(filepath.Join(dir, "sub"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("缺 manifest 应空表: %v %v", empty, err)
	}
}

// TestLoadCatalogRecords 逐行解析与双键回落
func TestLoadCatalogRecords(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, build.CatalogFilename)
	content := `{"worker_id": "w1", "cid": "1", "name": "n", "description": "d", "skill_path": "/s", "branch_path": ["a", "b"], "category": "c", "retrieval_text": "r", "metadata": {"k": "v"}}` + "\n" +
		`{"skill_id": "w2"}` + "\n" + "\n"
	_ = os.WriteFile(p, []byte(content), 0o644)
	records, err := LoadCatalogRecords(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("记录数不符: %d", len(records))
	}
	r0 := records[0]
	if r0.WorkerID != "w1" || r0.CID != "1" || len(r0.BranchPath) != 2 || r0.Metadata["k"] != "v" {
		t.Fatalf("记录字段不符: %+v", r0)
	}
	if records[1].WorkerID != "w2" {
		t.Fatalf("skill_id 回落不符: %+v", records[1])
	}
	// 文件不存在 → 空表
	empty, err := LoadCatalogRecords(filepath.Join(dir, "nope.jsonl"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("缺文件应空表: %v %v", empty, err)
	}
}

// TestReadConfigText 本地与 S3
func TestReadConfigText(t *testing.T) {
	if _, err := ReadConfigText("", "cfg"); err == nil {
		t.Fatal("空路径应报错")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	_ = os.WriteFile(p, []byte("a: 1"), 0o644)
	if s, err := ReadConfigText(p, "cfg"); err != nil || s != "a: 1" {
		t.Fatalf("本地读取不符: %q %v", s, err)
	}
	if _, err := ReadConfigText(filepath.Join(dir, "nope"), "cfg"); err == nil {
		t.Fatal("不存在应报错")
	}
	if _, err := ReadConfigText("s3://b/k", "cfg"); err != ErrS3NotImplemented {
		t.Fatalf("S3 应返回登记错误: %v", err)
	}
}

// TestParseJSONOrYaml 双解析
func TestParseJSONOrYaml(t *testing.T) {
	v, err := ParseJSONOrYaml(`{"a": 1}`, "s")
	if err != nil || v == nil {
		t.Fatalf("JSON 解析不符: %v %v", v, err)
	}
	v, err = ParseJSONOrYaml("a: 1\nb: x", "s")
	if err != nil || v == nil {
		t.Fatalf("YAML 解析不符: %v %v", v, err)
	}
	if _, err = ParseJSONOrYaml("{{{{", "src.yaml"); err == nil {
		t.Fatal("双失败应报错")
	}
}
