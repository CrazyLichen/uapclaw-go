package milvus

import (
	"context"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 结构体 ────────────────────────────

// milvusClient Milvus 客户端接口，用于测试 mock 和生产适配。
// 使用 ...any 作为 callOptions 参数，避免在接口中导入 gRPC。
type milvusClient interface {
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error
	DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error)
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error)
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error)
	Upsert(ctx context.Context, option milvusclient.UpsertOption, callOptions ...any) (milvusclient.UpsertResult, error)
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error)
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error)
	ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error)
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error
	CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...any) error
	DropDatabase(ctx context.Context, option milvusclient.DropDatabaseOption, callOptions ...any) error
	Compact(ctx context.Context, option milvusclient.CompactOption, callOptions ...any) (int64, error)
	Close(ctx context.Context) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
