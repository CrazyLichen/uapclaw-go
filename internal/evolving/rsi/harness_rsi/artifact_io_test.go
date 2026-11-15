package harnessrsi

import (
	"os"
	"testing"
)

// writeFile 测试助手：写入文本文件（仅本包测试共用，全包只定义一次）
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestArtifactIO_ReadYAMLMapping_缺失文件返回空映射 缺失文件必须返回空映射且不为 nil
func TestArtifactIO_ReadYAMLMapping_缺失文件返回空映射(t *testing.T) {
	got := ReadYAMLMapping(t.TempDir() + "/missing.yaml")
	if got == nil {
		t.Fatal("expected non-nil empty mapping for missing file")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty mapping, got %v", got)
	}
}

// TestArtifactIO_ReadYAMLMapping_非映射内容返回空映射 顶层非 mapping 的 YAML 返回空映射
func TestArtifactIO_ReadYAMLMapping_非映射内容返回空映射(t *testing.T) {
	path := t.TempDir() + "/list.yaml"
	if err := writeFile(path, "- a\n- b\n"); err != nil {
		t.Fatal(err)
	}
	got := ReadYAMLMapping(path)
	if len(got) != 0 {
		t.Fatalf("expected empty mapping for non-mapping YAML, got %v", got)
	}
}

// TestArtifactIO_WriteYAMLMapping_往返与父目录创建 写入自动建父目录且往返相等
func TestArtifactIO_WriteYAMLMapping_往返与父目录创建(t *testing.T) {
	root := t.TempDir()
	path := root + "/deep/nested/data.yaml"
	data := map[string]any{"alpha": "一", "count": 3}
	returned := WriteYAMLMapping(path, data)
	if returned != path {
		t.Fatalf("expected resolved path %q, got %q", path, returned)
	}
	got := ReadYAMLMapping(path)
	if got["alpha"] != "一" || got["count"] != 3 {
		t.Fatalf("round-trip mismatch: %v", got)
	}
}

// TestArtifactIO_ReadJSONMapping_缺失与非映射 缺失文件与非 dict JSON 返回空映射
func TestArtifactIO_ReadJSONMapping_缺失与非映射(t *testing.T) {
	if got := ReadJSONMapping(t.TempDir() + "/missing.json"); len(got) != 0 {
		t.Fatalf("expected empty mapping for missing file, got %v", got)
	}
	path := t.TempDir() + "/arr.json"
	if err := writeFile(path, "[1,2]"); err != nil {
		t.Fatal(err)
	}
	if got := ReadJSONMapping(path); len(got) != 0 {
		t.Fatalf("expected empty mapping for non-dict JSON, got %v", got)
	}
}

// TestArtifactIO_WriteJSONMapping_往返 写入自动建父目录且往返相等
func TestArtifactIO_WriteJSONMapping_往返(t *testing.T) {
	path := t.TempDir() + "/nested/out.json"
	data := map[string]any{"name": "case", "score": 0.8}
	returned := WriteJSONMapping(path, data)
	if returned != path {
		t.Fatalf("expected resolved path %q, got %q", path, returned)
	}
	got := ReadJSONMapping(path)
	if got["name"] != "case" || got["score"] != 0.8 {
		t.Fatalf("round-trip mismatch: %v", got)
	}
}
