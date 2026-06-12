package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlDbStore 基于 BaseDbStore 的通用 SQL CRUD 封装。
//
// 本封装将 GORM 的常用操作抽象为通用方法，让上层组件
// （SqlMessageStore 等）通过统一接口操作数据库，
// 而非直接编写 GORM 查询。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_db_store.py (SqlDbStore)
type SqlDbStore struct {
	// dbStore 数据库存储抽象
	dbStore db.BaseDbStore
	// db GORM 数据库实例
	db *gorm.DB
	// tableCache 表列名缓存（表名 → 列名列表）
	tableCache sync.Map
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlDbStore 创建 SqlDbStore 实例。
// 从 BaseDbStore 获取 *gorm.DB，后续所有操作通过此实例执行。
//
// 对应 Python: SqlDbStore.__init__(db_store)
func NewSqlDbStore(dbStore db.BaseDbStore) *SqlDbStore {
	return &SqlDbStore{
		dbStore: dbStore,
		db:      dbStore.GetDB(context.Background()),
	}
}

// Write 插入一行数据到指定表。
// data 为列名到值的映射。
//
// 对应 Python: SqlDbStore.write(table, data)
func (s *SqlDbStore) Write(ctx context.Context, table string, data map[string]any) error {
	if err := s.db.Table(table).Create(data).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("table_name", table).
			Err(err).
			Msg("写入数据失败")
		return exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("write failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// ConditionGet 条件查询，支持 IN 子句。
// conditions 的值必须为切片类型（对应 Python 的 list），用于 IN 查询。
// columns 指定需要返回的列，为空时返回所有列。
//
// 对应 Python: SqlDbStore.condition_get(table, conditions, columns)
func (s *SqlDbStore) ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 选择指定列
	if len(columns) > 0 {
		query = query.Select(columns)
	}
	// 构建 IN 条件，校验 values 必须为切片类型
	for col, val := range conditions {
		switch val.(type) {
		case []string, []any, []int, []int64, []float64:
			query = query.Where(fmt.Sprintf("%s IN ?", col), val)
		default:
			return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("condition_get: conditions[%q] must be a slice, got %T", col, val)),
			)
		}
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("条件查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("condition_get failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// GetWithSort 过滤+排序+分页查询。
// filters 为等值过滤条件，sortBy 为排序字段，order 为 "ASC" 或 "DESC"，limit 为返回行数上限。
//
// 对应 Python: SqlDbStore.get_with_sort(table, filters, sort_by, order, limit)
func (s *SqlDbStore) GetWithSort(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int) ([]map[string]any, error) {
	// 排序列校验
	if sortBy != "" {
		colNames, err := s.GetTable(ctx, table)
		if err != nil {
			return nil, err
		}
		found := false
		for _, name := range colNames {
			if name == sortBy {
				found = true
				break
			}
		}
		if !found {
			return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("sort column '%s' does not exist in table '%s'", sortBy, table)),
			)
		}
	}

	query := s.db.Table(table)
	// 等值过滤
	for col, val := range filters {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	// 排序
	if sortBy != "" {
		dir := "ASC"
		if order == "DESC" || order == "desc" {
			dir = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sortBy, dir))
	}
	// 限制行数
	if limit > 0 {
		query = query.Limit(limit)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("排序查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_with_sort failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// Update 条件更新。
// conditions 为 WHERE 条件（支持 IN 子句，值为切片时使用 IN），data 为需要更新的列值。
//
// 对应 Python: SqlDbStore.update(table, conditions, data)
func (s *SqlDbStore) Update(ctx context.Context, table string, conditions map[string]any, data map[string]any) error {
	query := s.db.Table(table)
	for col, val := range conditions {
		switch v := val.(type) {
		case []any:
			query = query.Where(fmt.Sprintf("%s IN ?", col), v)
		default:
			query = query.Where(fmt.Sprintf("%s = ?", col), v)
		}
	}

	if err := query.Updates(data).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_UPDATE").
			Str("table_name", table).
			Err(err).
			Msg("更新数据失败")
		return exception.BuildError(exception.StatusStoreMessageUpdateExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("update failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// Delete 条件删除。
// conditions 为 WHERE 条件（支持 IN 子句，值为切片时使用 IN）。
//
// 对应 Python: SqlDbStore.delete(table, conditions)
func (s *SqlDbStore) Delete(ctx context.Context, table string, conditions map[string]any) error {
	query := s.db.Table(table)
	for col, val := range conditions {
		switch v := val.(type) {
		case []any:
			query = query.Where(fmt.Sprintf("%s IN ?", col), v)
		default:
			query = query.Where(fmt.Sprintf("%s = ?", col), v)
		}
	}

	if err := query.Delete(nil).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("table_name", table).
			Err(err).
			Msg("删除数据失败")
		return exception.BuildError(exception.StatusStoreMessageDeleteExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("delete failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// Exist 检查是否存在满足条件的记录。
//
// 对应 Python: SqlDbStore.exist(table, conditions)
func (s *SqlDbStore) Exist(ctx context.Context, table string, conditions map[string]any) (bool, error) {
	query := s.db.Table(table)
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}

	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("exist check failed for table %s: %s", table, err.Error())),
		)
	}
	return count > 0, nil
}

// Count 统计满足条件的记录数。
// 使用 SQL COUNT 聚合查询，而非取回全部数据后 len()。
//
// 对应 Python: Python 中无此方法（count_messages 用 get_with_sort + len 实现）
// Go 新增：替代 Python 的低效计数方式
func (s *SqlDbStore) Count(ctx context.Context, table string, conditions map[string]any) (int64, error) {
	query := s.db.Table(table)
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("计数查询失败")
		return 0, exception.BuildError(exception.StatusStoreMessageCountExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("count failed for table %s: %s", table, err.Error())),
		)
	}
	return count, nil
}

// GetWithSortAndTimeRange 过滤+排序+分页+时间范围查询。
// 在 GetWithSort 基础上增加 StartTime/EndTime 范围过滤。
// 修正 Python 缺陷：Python 定义了 start_time/end_time 但未实现。
//
// 对应 Python: SqlDbStore.get_with_sort（Go 扩展了时间范围查询）
func (s *SqlDbStore) GetWithSortAndTimeRange(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int, startTime *time.Time, endTime *time.Time) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 等值过滤
	for col, val := range filters {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	// 时间范围过滤
	if startTime != nil {
		query = query.Where("timestamp >= ?", startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		query = query.Where("timestamp <= ?", endTime.Format(time.RFC3339))
	}
	// 排序
	if sortBy != "" {
		dir := "ASC"
		if order == "DESC" || order == "desc" {
			dir = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sortBy, dir))
	}
	// 限制行数
	if limit > 0 {
		query = query.Limit(limit)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("时间范围排序查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_with_sort_and_time_range failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// CreateBatch 批量插入多行数据到指定表。
// 使用 GORM Create 一次性批量 INSERT，而非循环调用 Write。
// rows 为空时直接返回 nil。
//
// 对应 Python: Go 新增（Python 无对应方法，add_messages 循环调用 write）
func (s *SqlDbStore) CreateBatch(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if err := s.db.Table(table).Create(rows).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("table_name", table).
			Int("row_count", len(rows)).
			Err(err).
			Msg("批量写入数据失败")
		return exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("create_batch failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// GetTable 获取表的列名列表（带缓存）。
// 用于列存在性校验，避免重复查询数据库 schema。
//
// 对应 Python: SqlDbStore.get_table(table_name)
func (s *SqlDbStore) GetTable(ctx context.Context, tableName string) ([]string, error) {
	// 检查缓存
	if cached, ok := s.tableCache.Load(tableName); ok {
		return cached.([]string), nil
	}

	// 查询列信息
	columns, err := s.db.Table(tableName).Migrator().ColumnTypes(tableName)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_table failed for table %s: %s", tableName, err.Error())),
		)
	}

	colNames := make([]string, 0, len(columns))
	for _, col := range columns {
		colNames = append(colNames, col.Name())
	}

	// 写入缓存
	s.tableCache.Store(tableName, colNames)
	return colNames, nil
}

// InvalidateTableCache 清除指定表的列名缓存。
// 下次 GetTable 调用会重新查询数据库 schema。
//
// 对应 Python: SqlDbStore.invalidate_table_cache(table_name)
func (s *SqlDbStore) InvalidateTableCache(tableName string) {
	s.tableCache.Delete(tableName)
}

// BatchGet 多组 OR 条件查询。
// conditionsList 中每个 condition 之间用 OR 连接，
// 单个 condition 内部用 AND 连接。
//
// 对应 Python: SqlDbStore.batch_get(table, conditions_list)
func (s *SqlDbStore) BatchGet(ctx context.Context, table string, conditionsList []map[string]any) ([]map[string]any, error) {
	query := s.db.Table(table)

	if len(conditionsList) > 0 {
		var orConditions []string
		var orArgs []any
		for _, cond := range conditionsList {
			var andParts []string
			var andArgs []any
			for col, val := range cond {
				andParts = append(andParts, fmt.Sprintf("%s = ?", col))
				andArgs = append(andArgs, val)
			}
			if len(andParts) > 0 {
				orConditions = append(orConditions, fmt.Sprintf("(%s)", joinWithAnd(andParts)))
				orArgs = append(orArgs, andArgs...)
			}
		}
		if len(orConditions) > 0 {
			query = query.Where(fmt.Sprintf("(%s)", joinWithOr(orConditions)), orArgs...)
		}
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("批量条件查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("batch_get failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// Get 按条件查询单条记录（limit 1）。
// Python 硬编码 WHERE id = record_id，Go 改为通用 conditions 参数，
// 避免硬编码主键列名（不同表的主键不同）。
// columns 指定需要返回的列，为空时返回所有列。
//
// 对应 Python: SqlDbStore.get(table, record_id, columns)
func (s *SqlDbStore) Get(ctx context.Context, table string, conditions map[string]any, columns []string) (map[string]any, error) {
	query := s.db.Table(table)
	if len(columns) > 0 {
		query = query.Select(columns)
	}
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	query = query.Limit(1)

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("单条查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get failed for table %s: %s", table, err.Error())),
		)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// DeleteTable 删除整张表（DROP TABLE）。
//
// 对应 Python: SqlDbStore.delete_table(table_name)
func (s *SqlDbStore) DeleteTable(ctx context.Context, tableName string) error {
	if err := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("table_name", tableName).
			Err(err).
			Msg("删除表失败")
		return exception.BuildError(exception.StatusStoreMessageDeleteExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("delete_table failed for %s: %s", tableName, err.Error())),
		)
	}
	// 清除缓存
	s.InvalidateTableCache(tableName)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// joinWithAnd 用 AND 连接字符串切片
func joinWithAnd(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " AND " + parts[i]
	}
	return result
}

// joinWithOr 用 OR 连接字符串切片
func joinWithOr(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " OR " + parts[i]
	}
	return result
}
