package kbadmin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ErrCodeKBNotImplemented 方法待后续批次实现
const ErrCodeKBNotImplemented = "KB_NOT_IMPLEMENTED"

// ErrCodeKBNotFound 知识库不存在
const ErrCodeKBNotFound = "KB_NOT_FOUND"

// ErrCodeKBInternal 内部错误
const ErrCodeKBInternal = "KB_INTERNAL"

// ErrCodeKBInvalidParams 参数解析失败
const ErrCodeKBInvalidParams = "KB_CONFIG_INVALID"

// ──────────────────────────── 结构体 ────────────────────────────

// Domain kb_admin 域聚合（server 侧单点注入）
type Domain struct {
	// KB 知识库 CRUD 编排
	KB *KBService
	// Doc 文档管理编排（30.5/30.2 回填）
	Doc *DocService
	// Chunk 分块管理编排（30.6 回填）
	Chunk *ChunkService
	// Search 检索测试编排（30.6 回填）
	Search *SearchService
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDomain 组装 kb_admin 域
func NewDomain(store *Store, kbFactory KBFactory) (*Domain, error) {
	kbSvc, err := NewKBService(store, kbFactory)
	if err != nil {
		return nil, err
	}
	return &Domain{
		KB:     kbSvc,
		Doc:    NewDocService(store),
		Chunk:  NewChunkService(store),
		Search: NewSearchService(store),
	}, nil
}

// IsKBMethod 判定是否 kb.* 方法族
func IsKBMethod(method string) bool { return strings.HasPrefix(method, "kb.") }

// BootstrapErrorResponse 域装配失败的统一错误响应（server 薄适配层使用）
func BootstrapErrorResponse(request *schema.AgentRequest) *schema.AgentResponse {
	return errorResponse(request, "kb_admin 域装配失败", ErrCodeKBInternal)
}

// HandleKBRequest kb.* 方法族统一分发（30.1 注册全族；未实现返 KB_NOT_IMPLEMENTED）。
// 对齐 repositories.HandleProjectRequest 写法：返回 *schema.AgentResponse，
// 失败经 OK=false + payload.error_code 表达；由 server 侧薄适配写回。
func HandleKBRequest(domain *Domain, request *schema.AgentRequest) *schema.AgentResponse {
	notImplemented := func() *schema.AgentResponse {
		return errorResponse(request, "方法将在后续批次实现", ErrCodeKBNotImplemented)
	}
	switch request.ReqMethod {
	case schema.ReqMethodKBKBList:
		list, err := domain.KB.List()
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kbs": mustJSONAny(list)})
	case schema.ReqMethodKBKBGet:
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		kb, err := domain.KB.Get(p.ID)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBNotFound)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(kb)})
	case schema.ReqMethodKBKBCreate:
		var p struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			ConfigJSON string `json:"config_json"`
			FolderPath string `json:"folder_path"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if p.Type == "" {
			p.Type = "document"
		}
		kb, err := domain.KB.Create(context.Background(), p.ID, p.Name, p.Type, p.ConfigJSON, p.FolderPath)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(kb)})
	case schema.ReqMethodKBKBUpdate:
		var kb KBInfo
		if err := parseParams(request, &kb); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		updated, err := domain.KB.Update(&kb)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(updated)})
	case schema.ReqMethodKBKBDelete:
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if err := domain.KB.Delete(context.Background(), p.ID); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBNotFound)
		}
		return okResponse(request, map[string]any{"deleted": true})
	// 其余方法族：30.1 只注册不实现（spec D-30-1-2，30.2-30.6 各批摘除）
	case schema.ReqMethodKBKBPin, schema.ReqMethodKBKBMove,
		schema.ReqMethodKBKBSetConfig, schema.ReqMethodKBKBStatistics,
		schema.ReqMethodKBKBGenerateProfile,
		schema.ReqMethodKBDocUpload,
		schema.ReqMethodKBDocImportURL, schema.ReqMethodKBDocImportManual,
		schema.ReqMethodKBDocList, schema.ReqMethodKBDocGet, schema.ReqMethodKBDocUpdate,
		schema.ReqMethodKBDocDelete, schema.ReqMethodKBDocBatchDelete,
		schema.ReqMethodKBDocReparse, schema.ReqMethodKBDocBatchReparse,
		schema.ReqMethodKBDocCancelParse, schema.ReqMethodKBDocRegenerateSummary,
		schema.ReqMethodKBDocPreview, schema.ReqMethodKBDocDownload,
		schema.ReqMethodKBChunkList, schema.ReqMethodKBChunkUpdate,
		schema.ReqMethodKBChunkRevisions, schema.ReqMethodKBChunkRevert,
		schema.ReqMethodKBChunkDelete, schema.ReqMethodKBChunkGenerateQuestions,
		schema.ReqMethodKBChunkListQuestions, schema.ReqMethodKBChunkerPreview,
		schema.ReqMethodKBSearchTest, schema.ReqMethodKBTaskProgress,
		schema.ReqMethodKBTaskSubscribe:
		return notImplemented()
	default:
		return errorResponse(request, "未知的 kb 方法", ErrCodeKBInternal)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// okResponse 成功响应（对齐 repositories.buildErrorResponse 的工厂用法）
func okResponse(request *schema.AgentRequest, payload map[string]any) *schema.AgentResponse {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(payload),
	)
}

// errorResponse 失败响应（OK=false + payload.error_code）
func errorResponse(request *schema.AgentRequest, message, code string) *schema.AgentResponse {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": message, "error_code": code}),
	)
}

// parseParams 解析请求参数（AgentRequest.Params JSON）
func parseParams(request *schema.AgentRequest, target any) error {
	if len(request.Params) == 0 {
		return nil
	}
	return json.Unmarshal(request.Params, target)
}

// mustJSONAny 把模型转 map（单个对象）；json tag 已定义
func mustJSONValue(v any) map[string]any {
	out, _ := mustJSONAny(v).(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

// mustJSONAny 把任意模型经 JSON 往返转 payload 值（对象或数组）
func mustJSONAny(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}
