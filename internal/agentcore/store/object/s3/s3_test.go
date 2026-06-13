package s3

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	objectpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/store/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestS3Client 创建基于 httptest 的 S3Client 用于测试
func newTestS3Client(handler http.Handler) (*S3Client, *httptest.Server) {
	server := httptest.NewServer(handler)

	credProvider := credentials.NewStaticCredentialsProvider("test-ak", "test-sk", "")
	awsCfg, _ := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credProvider),
		config.WithRegion("us-east-1"),
	)

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true // 测试时使用路径风格
	})

	return &S3Client{client: client}, server
}

// mockS3Handler 模拟 S3 API 的 HTTP 处理器
type mockS3Handler struct {
	buckets map[string]bool
	objects map[string]map[string][]byte // bucket -> key -> content
	failOp  string                       // 设置此字段使指定操作返回错误
}

func newMockS3Handler() *mockS3Handler {
	return &mockS3Handler{
		buckets: make(map[string]bool),
		objects: make(map[string]map[string][]byte),
	}
}

func (h *mockS3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 如果设置了 failOp，对应操作返回 500 错误
	if h.failOp != "" {
		if h.failOp == "all" ||
			(h.failOp == "create-bucket" && r.Method == http.MethodPut && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) == 1) ||
			(h.failOp == "upload" && r.Method == http.MethodPut && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) > 1) ||
			(h.failOp == "download" && r.Method == http.MethodGet && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) > 1) ||
			(h.failOp == "delete" && r.Method == http.MethodDelete) ||
			(h.failOp == "list" && r.Method == http.MethodGet && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) == 1) ||
			(h.failOp == "delete-bucket" && r.Method == http.MethodDelete && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) == 1) {
			writeS3Error(w, "InternalError", "mock error")
			return
		}
	}

	switch r.Method {
	case http.MethodPut:
		// 创建桶或上传对象
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		if len(parts) == 1 || parts[1] == "" {
			// 创建桶
			h.buckets[bucket] = true
			h.objects[bucket] = make(map[string][]byte)
			w.WriteHeader(http.StatusOK)
		} else {
			// 上传对象
			key := parts[1]
			body, _ := io.ReadAll(r.Body)
			if h.objects[bucket] == nil {
				h.objects[bucket] = make(map[string][]byte)
			}
			h.objects[bucket][key] = body
			w.WriteHeader(http.StatusOK)
		}

	case http.MethodGet:
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		if len(parts) == 1 || parts[1] == "" {
			// ListObjects
			prefix := r.URL.Query().Get("prefix")
			result := listBucketResult{
				IsTruncated: false,
			}
			for key, content := range h.objects[bucket] {
				if prefix == "" || strings.HasPrefix(key, prefix) {
					result.Contents = append(result.Contents, objectItem{
						Key:          key,
						LastModified: time.Now().Format(time.RFC3339),
						Size:         len(content),
						ETag:         fmt.Sprintf(`"%x"`, len(content)),
					})
				}
			}
			w.Header().Set("Content-Type", "application/xml")
			xml.NewEncoder(w).Encode(result)
		} else {
			// GetObject
			key := parts[1]
			content, ok := h.objects[bucket][key]
			if !ok {
				writeS3Error(w, "NoSuchKey", "object not found")
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.Write(content)
		}

	case http.MethodDelete:
		parts := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		if len(parts) == 1 || parts[1] == "" {
			// 删除桶
			delete(h.buckets, bucket)
			delete(h.objects, bucket)
			w.WriteHeader(http.StatusNoContent)
		} else {
			// 删除对象
			key := parts[1]
			delete(h.objects[bucket], key)
			w.WriteHeader(http.StatusNoContent)
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// S3 XML 响应类型
type listBucketResult struct {
	XMLName     xml.Name     `xml:"ListBucketResult"`
	IsTruncated bool         `xml:"IsTruncated"`
	Contents    []objectItem `xml:"Contents"`
}

type objectItem struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	Size         int    `xml:"Size"`
	ETag         string `xml:"ETag"`
}

type errorResult struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func writeS3Error(w http.ResponseWriter, code, message string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(errorResult{Code: code, Message: message})
}

// ──────────────────────────── CreateBucket 测试 ────────────────────────────

func TestS3Client_CreateBucket(t *testing.T) {
	mock := newMockS3Handler()
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.CreateBucket(context.Background(), "test-bucket", "us-east-1")
	assert.NoError(t, err)
	assert.True(t, mock.buckets["test-bucket"])
}

func TestS3Client_CreateBucket_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "create-bucket"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.CreateBucket(context.Background(), "test-bucket", "us-east-1")
	assert.Error(t, err)
}

// ──────────────────────────── DeleteBucket 测试 ────────────────────────────

func TestS3Client_DeleteBucket(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteBucket(context.Background(), "test-bucket")
	assert.NoError(t, err)
	assert.False(t, mock.buckets["test-bucket"])
}

func TestS3Client_DeleteBucket_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "delete-bucket"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteBucket(context.Background(), "nonexistent-bucket")
	assert.Error(t, err)
}

// ──────────────────────────── UploadFile 测试 ────────────────────────────

func TestS3Client_UploadFile(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	// 创建临时测试文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	err = client.UploadFile(context.Background(), "test-bucket", "test.txt", tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello world"), mock.objects["test-bucket"]["test.txt"])
}

func TestS3Client_UploadFile_文件不存在(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.UploadFile(context.Background(), "test-bucket", "test.txt", "/nonexistent/file.txt")
	assert.Error(t, err)
}

func TestS3Client_UploadFile_服务端错误(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "upload"
	client, server := newTestS3Client(mock)
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello"), 0644)
	require.NoError(t, err)

	err = client.UploadFile(context.Background(), "test-bucket", "test.txt", tmpFile)
	assert.Error(t, err)
}

// ──────────────────────────── DownloadFile 测试 ────────────────────────────

func TestS3Client_DownloadFile(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"test.txt": []byte("download content"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "downloaded.txt")

	err := client.DownloadFile(context.Background(), "test-bucket", "test.txt", tmpFile)
	assert.NoError(t, err)

	content, err := os.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "download content", string(content))
}

func TestS3Client_DownloadFile_文件不存在(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "downloaded.txt")

	err := client.DownloadFile(context.Background(), "test-bucket", "nonexistent.txt", tmpFile)
	assert.Error(t, err)
}

// ──────────────────────────── DeleteObject 测试 ────────────────────────────

func TestS3Client_DeleteObject(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"test.txt": []byte("content"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteObject(context.Background(), "test-bucket", "test.txt")
	assert.NoError(t, err)
	_, exists := mock.objects["test-bucket"]["test.txt"]
	assert.False(t, exists)
}

func TestS3Client_DeleteObject_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	mock.failOp = "delete"
	client, server := newTestS3Client(mock)
	defer server.Close()

	err := client.DeleteObject(context.Background(), "test-bucket", "test.txt")
	assert.Error(t, err)
}

// ──────────────────────────── ListObjects 测试 ────────────────────────────

func TestS3Client_ListObjects(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"prefix/a.txt": []byte("a"),
		"prefix/b.txt": []byte("bb"),
		"other/c.txt":  []byte("ccc"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	objects, err := client.ListObjects(context.Background(), "test-bucket", "prefix/")
	assert.NoError(t, err)
	assert.Len(t, objects, 2)
}

func TestS3Client_ListObjects_空桶(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = make(map[string][]byte)
	client, server := newTestS3Client(mock)
	defer server.Close()

	objects, err := client.ListObjects(context.Background(), "test-bucket", "")
	assert.NoError(t, err)
	assert.Empty(t, objects)
}

func TestS3Client_ListObjects_WithMaxObjects(t *testing.T) {
	mock := newMockS3Handler()
	mock.buckets["test-bucket"] = true
	mock.objects["test-bucket"] = map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
		"c.txt": []byte("c"),
	}
	client, server := newTestS3Client(mock)
	defer server.Close()

	// mock 服务端不做 MaxKeys 截断，所以返回全部对象
	// 这里验证 WithMaxObjects 选项能正确传递且不报错
	objects, err := client.ListObjects(context.Background(), "test-bucket", "",
		objectpkg.WithMaxObjects(2))
	assert.NoError(t, err)
	assert.NotEmpty(t, objects)
}

func TestS3Client_ListObjects_失败(t *testing.T) {
	mock := newMockS3Handler()
	mock.failOp = "list"
	client, server := newTestS3Client(mock)
	defer server.Close()

	_, err := client.ListObjects(context.Background(), "nonexistent-bucket", "")
	assert.Error(t, err)
}
