package cron

import "github.com/uapclaw/uapclaw-go/internal/common/etcd"

// ──────────────────────────── 类型别名 ────────────────────────────
//
// 上游 etcd_client.py 是对 jiuwenswarm.common.etcd.client 的兼容导入
// （`from jiuwenswarm.common.etcd.client import (...)`），实体在 common 层。
// Go 侧实体在 internal/common/etcd，此处保持同名转发，使 cron 包内引用
// 路径与上游一致。

// EtcdError etcd 请求失败（别名 → etcd.ErrEtcd）。
// Python: etcd_client.py:7 EtcdError
var EtcdError = etcd.ErrEtcd

// EtcdCasError 比较并交换失败（别名 → etcd.ErrEtcdCas）。
// Python: etcd_client.py:7 EtcdCasError
var EtcdCasError = etcd.ErrEtcdCas

// EtcdKv etcd 键值记录（别名 → etcd.EtcdKv）。
// Python: etcd_client.py:8 EtcdKv
type EtcdKv = etcd.EtcdKv

// EtcdRangeResult 区间读取结果（别名 → etcd.EtcdRangeResult）。
// Python: etcd_client.py:9 EtcdRangeResult
type EtcdRangeResult = etcd.EtcdRangeResult

// EtcdJsonClient etcd JSON 客户端（别名 → etcd.EtcdJsonClient）。
// Python: etcd_client.py:8 EtcdJsonClient
type EtcdJsonClient = etcd.EtcdJsonClient

// PrefixRangeEnd 前缀扫描的 range_end（转发 → etcd.PrefixRangeEnd）。
// Python: etcd_client.py:10 prefix_range_end
func PrefixRangeEnd(prefix []byte) []byte {
	return etcd.PrefixRangeEnd(prefix)
}
