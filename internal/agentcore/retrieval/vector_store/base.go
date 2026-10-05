package vector_store

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// VectorStore 检索层向量存储抽象接口。
//
// 提供向量搜索、稀疏搜索、混合搜索等检索能力。
// 与 foundation 层 BaseVectorStore（CRUD+迁移）不同，本接口面向检索场景。
//
// Python: retrieval/vector_store/base.py (VectorStore)
type VectorStore interface {
	// CreateClient 创建向量数据库客户端，对齐 Python create_client
	CreateClient(databaseName, pathOrURI string, token string, opts ...StoreOption) (any, error)

	// CheckVectorField 校验向量字段配置是否和实际数据库一致
	// 对齐 Python check_vector_field / _check_configs_matching
	CheckVectorField() error

	// Add 添加向量数据，对齐 Python add
	Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error

	// Search 向量搜索，对齐 Python search
	Search(ctx context.Context, queryVector []float64, topK int, filters any, opts ...StoreOption) ([]common.SearchResult, error)

	// SparseSearch 稀疏搜索（文本匹配），对齐 Python sparse_search
	SparseSearch(ctx context.Context, queryText string, topK int, filters any, opts ...StoreOption) ([]common.SearchResult, error)

	// HybridSearch 混合搜索（向量+文本 RRF 融合），对齐 Python hybrid_search
	HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters any, opts ...StoreOption) ([]common.SearchResult, error)

	// Delete 删除向量，对齐 Python delete
	Delete(ctx context.Context, ids []string, filterExpr any) (bool, error)

	// TableExists 检查集合是否存在，对齐 Python table_exists
	TableExists(ctx context.Context, tableName string) (bool, error)

	// DeleteTable 删除集合，对齐 Python delete_table
	DeleteTable(ctx context.Context, tableName string) error

	// Close 关闭存储
	Close()
}

// ──────────────────────────── 结构体 ────────────────────────────

// StoreOptions 向量存储操作选项。
type StoreOptions struct {
	// BatchSize 批大小，0 表示使用默认值
	BatchSize int
}

// StoreOption 向量存储操作的函数选项。
type StoreOption func(*StoreOptions)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStoreOptions 从可变参数构建 StoreOptions。
func NewStoreOptions(opts ...StoreOption) StoreOptions {
	o := StoreOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithStoreBatchSize 设置批大小的 StoreOption。
func WithStoreBatchSize(n int) StoreOption {
	return func(o *StoreOptions) {
		o.BatchSize = n
	}
}

// CheckConfigsMatching 校验配置字典是否和实际配置一致。
//
// 对齐 Python VectorStore._check_configs_matching。
//   - 忽略 efSearchFactor 键
//   - 字符串比较不区分大小写
//   - 数值类型使用 isclose 近似比较（rel_tol=1e-2, abs_tol=1e-3）
//   - 不匹配时抛 RETRIEVAL_KB_DATABASE_CONFIG_INVALID
func CheckConfigsMatching(configured, actual map[string]any) error {
	matches := make(map[string]any)
	mismatches := make(map[string]map[string]string)

	for attr, val := range configured {
		// 对齐 Python: if attr in ["efSearchFactor"]: continue
		if attr == "efSearchFactor" {
			continue
		}

		valStr := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", val)))
		actualVal := actual[attr]
		actualValStr := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", actualVal)))

		// 对齐 Python: is_valid = actual_val_str == val_str
		isValid := actualValStr == valStr

		// 对齐 Python: if not is_valid and isinstance(val, (int, float)) and actual_val_str.replace(".", "").isnumeric()
		if !isValid {
			if isNumeric(val) && isNumericString(actualValStr) {
				// 对齐 Python: isclose(float(actual_val_str), float(val), rel_tol=1e-2, abs_tol=1e-3)
				valFloat := toFloat(val)
				actualFloat := toFloat(actualVal)
				if !math.IsNaN(valFloat) && !math.IsNaN(actualFloat) {
					isValid = math.Abs(actualFloat-valFloat) <= 1e-2*math.Abs(valFloat)+1e-3
				}
			}
		}

		if isValid {
			matches[attr] = val
		} else {
			mismatches[attr] = map[string]string{
				"settings": valStr,
				"actual":   actualValStr,
			}
		}
	}

	if len(mismatches) > 0 {
		return exception.BuildError(
			exception.StatusRetrievalKbDatabaseConfigInvalid,
			exception.WithParam("error_msg",
				fmt.Sprintf("database actual config differs from current knowledge base, matches=%v, mismatches=%v", matches, mismatches)),
		)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isNumeric 判断值是否为数值类型
func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// isNumericString 判断字符串是否为数值（含小数点）
func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	dotCount := 0
	for _, c := range s {
		if c == '.' {
			dotCount++
			if dotCount > 1 {
				return false
			}
		} else if c < '0' || c > '9' {
			// 允许负号开头
			if c == '-' && len(s) > 1 {
				continue
			}
			return false
		}
	}
	return true
}

// toFloat 将任意数值转为 float64
func toFloat(v any) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float32:
		return float64(val)
	case float64:
		return val
	default:
		return math.NaN()
	}
}
