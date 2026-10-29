package knowledgebase

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// storageAttrProvider 校验流水线的属性消费接口（非导出，消费侧接口惯例）。
//
// ChromaVectorStore/MilvusVectorStore/ChromaIndexer/MilvusIndexer 已回填
// 7 getter（13.26）。未实现本接口的对象其属性按 nil 处理——对齐 Python
// getattr(obj, attr, None) 缺属性语义。
type storageAttrProvider interface {
	DatabaseName() string
	DistanceMetric() string
	TextField() string
	VectorField() any
	SparseVectorField() string
	MetadataField() string
	DocIDField() string
}

// ensureLoader 可选加载钩子（对齐 Python getattr(vs, "_ensure_loaded", None)）。
// MilvusVectorStore 实现本接口；ChromaVectorStore 无此方法 → 断言失败跳过。
type ensureLoader interface {
	EnsureLoaded(ctx context.Context) error
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// validateAttrOrder 7 属性固定比对顺序（Python 字面：
// ["database_name", "distance_metric"] + data_fields）。
var validateAttrOrder = []string{
	"database_name", "distance_metric", "text_field", "vector_field",
	"sparse_vector_field", "metadata_field", "doc_id_field",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateIndex 校验 vector store 与 index manager 的 7 项配置一致性。
//
// 任一属性不等抛 155508（错误消息含属性名+双方值+类型名）；
// strict 且 vectorStore 非 nil 时执行 CheckVectorField 并传播错误。
//
// Python: knowledge_base.py validate_index
func (b *BaseKnowledgeBase) ValidateIndex(ctx context.Context) error {
	for _, attr := range validateAttrOrder {
		vsVal := storageAttr(b.vectorStore, attr)
		imVal := storageAttr(b.indexManager, attr)
		if !attrEqual(vsVal, imVal) {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithMsg(fmt.Sprintf(
					"Vector store and index manager have incompatible %s configs:\n"+
						"- Vector Store (%s) is using \"%s\"\n"+
						"- Index manager (%s) is using \"%s\"",
					attr, typeName(b.vectorStore), renderAttrVal(vsVal),
					typeName(b.indexManager), renderAttrVal(imVal),
				)),
			)
		}
	}
	if b.strictValidation && b.vectorStore != nil {
		return b.vectorStore.CheckVectorField()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// postAssignValidation 赋值期三段式校验（Python __setattr__ 的显式对应物）。
//
// ① ValidateIndex（7 属性比对 + CheckVectorField）
// ② Chroma 本地限制拦截（类型名前缀探测）
// ③ ensureLoader 钩子（Milvus 构造期 load collection）
func (b *BaseKnowledgeBase) postAssignValidation(ctx context.Context) error {
	if err := b.ValidateIndex(ctx); err != nil {
		return err
	}
	if isChromaType(b.vectorStore) || isChromaType(b.indexManager) {
		if b.strictValidation && b.config.IndexType != common.IndexTypeVector {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithMsg("Chroma database does not support sparse embedding & hybrid search in local mode yet"),
			)
		}
	}
	if el, ok := b.vectorStore.(ensureLoader); ok {
		return el.EnsureLoaded(ctx)
	}
	return nil
}

// storageAttr 按属性名读取组件属性（getattr(obj, attr, None) 同构）。
func storageAttr(obj any, attr string) any {
	p, ok := obj.(storageAttrProvider)
	if !ok {
		return nil
	}
	switch attr {
	case "database_name":
		return p.DatabaseName()
	case "distance_metric":
		return p.DistanceMetric()
	case "text_field":
		return p.TextField()
	case "vector_field":
		return p.VectorField()
	case "sparse_vector_field":
		return p.SparseVectorField()
	case "metadata_field":
		return p.MetadataField()
	case "doc_id_field":
		return p.DocIDField()
	}
	return nil
}

// attrEqual 属性相等比较（V1 拍板：any 槽位 + DeepEqual）。
//
// 双 nil → true；单 nil → false；DeepEqual 首步即类型比较——
// Chroma 配置 vs Milvus 配置跨源天然 false → 155508（混配拦截）。
func attrEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.DeepEqual(a, b)
}

// isChromaType 类型名 "chroma" 前缀探测（R1 拍板）。
//
// 对齐 Python type(...).__name__.casefold().startswith("chroma")：
// 命名约定而非行为契约，第三方 Chroma 命名类型同样被拦。
func isChromaType(obj any) bool {
	t := reflect.TypeOf(obj)
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return strings.HasPrefix(strings.ToLower(t.Name()), "chroma")
}

// typeName 反射取类型名（错误消息用）。
func typeName(obj any) string {
	if obj == nil {
		return "NoneType"
	}
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

// renderAttrVal 渲染属性值（对齐 Python str() 形态：None 原样，其余 %v）。
func renderAttrVal(v any) string {
	if v == nil {
		return "None"
	}
	return fmt.Sprintf("%v", v)
}
