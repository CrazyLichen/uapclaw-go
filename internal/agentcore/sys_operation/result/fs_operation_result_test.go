package result

import (
	"encoding/json"
	"testing"
)

// TestReadFileResult_构造 测试 ReadFileResult 构造与 JSON 序列化
func TestReadFileResult_构造(t *testing.T) {
	r := ReadFileResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &ReadFileData{
			Path:    "/tmp/test.txt",
			Content: "hello world",
			Mode:    "text",
		},
	}
	if !r.IsSuccess() {
		t.Error("应为成功")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ReadFileResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Data.Path != "/tmp/test.txt" {
		t.Errorf("期望 Path='/tmp/test.txt'，实际 %q", decoded.Data.Path)
	}
}

// TestWriteFileResult_构造 测试 WriteFileResult 构造
func TestWriteFileResult_构造(t *testing.T) {
	r := WriteFileResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &WriteFileData{
			Path: "/tmp/test.txt",
			Size: 11,
			Mode: "text",
		},
	}
	if r.Data.Size != 11 {
		t.Errorf("期望 Size=11，实际 %d", r.Data.Size)
	}
}

// TestFileSystemItem_序列化 测试 FileSystemItem JSON 序列化
func TestFileSystemItem_序列化(t *testing.T) {
	fileType := ".txt"
	item := FileSystemItem{
		Name:         "test.txt",
		Path:         "/tmp/test.txt",
		Size:         100,
		ModifiedTime: "2026-01-01 00:00:00",
		IsDirectory:  false,
		Type:         &fileType,
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded FileSystemItem
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Name != "test.txt" {
		t.Errorf("期望 Name='test.txt'，实际 %q", decoded.Name)
	}
	if decoded.Type == nil || *decoded.Type != ".txt" {
		t.Error("期望 Type='.txt'")
	}
}

// TestFileSystemData_构造 测试 FileSystemData 构造
func TestFileSystemData_构造(t *testing.T) {
	data := FileSystemData{
		TotalCount: 2,
		ListItems: []FileSystemItem{
			{Name: "a.txt", Path: "/tmp/a.txt", Size: 10, ModifiedTime: "2026-01-01", IsDirectory: false},
			{Name: "b", Path: "/tmp/b", Size: 4096, ModifiedTime: "2026-01-01", IsDirectory: true},
		},
		RootPath:  "/tmp",
		Recursive: false,
		MaxDepth:  nil,
	}
	if data.TotalCount != 2 {
		t.Errorf("期望 TotalCount=2，实际 %d", data.TotalCount)
	}
}

// TestSearchFilesData_构造 测试 SearchFilesData 构造
func TestSearchFilesData_构造(t *testing.T) {
	data := SearchFilesData{
		TotalMatches:   1,
		MatchingFiles:  []FileSystemItem{{Name: "found.txt", Path: "/tmp/found.txt"}},
		SearchPath:     "/tmp",
		SearchPattern:  "*.txt",
		ExcludePatterns: []string{"*.log"},
	}
	if data.TotalMatches != 1 {
		t.Errorf("期望 TotalMatches=1，实际 %d", data.TotalMatches)
	}
}

// TestUploadFileResult_构造 测试 UploadFileResult 构造
func TestUploadFileResult_构造(t *testing.T) {
	r := UploadFileResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &UploadFileData{
			LocalPath:  "/tmp/local.txt",
			TargetPath: "/tmp/target.txt",
			Size:       100,
		},
	}
	if r.Data.LocalPath != "/tmp/local.txt" {
		t.Errorf("期望 LocalPath='/tmp/local.txt'，实际 %q", r.Data.LocalPath)
	}
}

// TestDownloadFileResult_构造 测试 DownloadFileResult 构造
func TestDownloadFileResult_构造(t *testing.T) {
	r := DownloadFileResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &DownloadFileData{
			SourcePath: "/tmp/source.txt",
			LocalPath:  "/tmp/dest.txt",
			Size:       100,
		},
	}
	if r.Data.SourcePath != "/tmp/source.txt" {
		t.Errorf("期望 SourcePath='/tmp/source.txt'，实际 %q", r.Data.SourcePath)
	}
}

// TestReadFileChunkData_序列化 测试流式读取块数据序列化
func TestReadFileChunkData_序列化(t *testing.T) {
	chunk := ReadFileChunkData{
		Path:         "/tmp/test.txt",
		ChunkContent: "line 1\n",
		Mode:         "text",
		ChunkSize:    7,
		ChunkIndex:   0,
		IsLastChunk:  false,
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ReadFileChunkData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if !decoded.IsLastChunk {
		// 正确：不是最后一块
	} else {
		t.Error("IsLastChunk 应为 false")
	}
}
