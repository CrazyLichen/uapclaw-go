// 单次 send_file_to_user 调用的任务级授权。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/generated_artifact_delivery.py（175 行）
//
// 语义：把「本次要发送的精确路径 + 目标渠道」发布为任务级租约（lease），
// 由 send_file 执行点**一次性消费**并校验。租约带 terminal 终态，
// 被复制的 async 上下文共享同一终态，因此消费后所有分支都读不到。
//
// 语言差异：Python 用 ContextVar + threading.Lock 实现跨复制上下文共享终态；
// Go 用 ctx 派生值承载 *sendFileExecutionLease（含 sync.Mutex + terminal）。

package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SendFileAuthorizationItem 当前 send 调用授权的一个规范路径。
//
// Python: @dataclass(frozen=True, slots=True) SendFileAuthorizationItem
type SendFileAuthorizationItem struct {
	// ResolvedPath 规范化绝对路径
	ResolvedPath string
}

// SendFileExecutionGrant 只发布给当前工具任务的精确路径/目标决策。
//
// Python: @dataclass(frozen=True, slots=True) SendFileExecutionGrant
type SendFileExecutionGrant struct {
	// Items 授权项
	Items []SendFileAuthorizationItem
	// TargetChannels 目标渠道
	TargetChannels []string
}

// sendFileExecutionLease 被复制的 async 上下文共享的终态租约。
//
// Python: @dataclass(slots=True) _SendFileExecutionLease
type sendFileExecutionLease struct {
	// grant 授权
	grant SendFileExecutionGrant
	// mu 保护 terminal
	mu sync.Mutex
	// terminal 是否已终结（消费或清除）
	terminal bool
}

// sendFileExecutionGrantKey 租约的 context key 类型。
type sendFileExecutionGrantKey struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// errSendFileAuthorizationMissingPath 授权缺少路径
	errSendFileAuthorizationMissingPath = "send_file_authorization_missing_path"
	// errSendFileExecutionGrantMissing 租约缺失
	errSendFileExecutionGrantMissing = "send_file_execution_grant_missing"
	// errSendFileExecutionGrantMismatch 租约与请求不匹配
	errSendFileExecutionGrantMismatch = "send_file_execution_grant_mismatch"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSendFileTargetChannels 返回 send 调用的规范投递目标集合（排序去重）。
//
// Python: normalize_send_file_target_channels(value)
func NormalizeSendFileTargetChannels(value any) []string {
	candidates := normalizeSendFileCandidates(value)
	if len(candidates) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			set[trimmed] = true
		}
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

// NormalizeSendFilePaths 返回 send 调用的规范有序路径元组（保序、不去重）。
//
// Python: normalize_send_file_paths(value)
func NormalizeSendFilePaths(value any) []string {
	candidates := normalizeSendFileCandidates(value)
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// CreateSendFileExecutionGrant 构造当前 send sink 所需的最小授权。
//
// Python: create_send_file_execution_grant(requested_paths, *, target_channels=None)
// 路径为空时返回错误（对齐 Python 抛出的 ValueError）。
func CreateSendFileExecutionGrant(
	requestedPaths []string,
	targetChannels any,
) (SendFileExecutionGrant, error) {
	items := make([]SendFileAuthorizationItem, 0, len(requestedPaths))
	for _, path := range requestedPaths {
		items = append(items, SendFileAuthorizationItem{ResolvedPath: resolveSendFilePath(path)})
	}
	if len(items) == 0 {
		return SendFileExecutionGrant{}, fmt.Errorf("%s", errSendFileAuthorizationMissingPath)
	}
	return SendFileExecutionGrant{
		Items:          items,
		TargetChannels: NormalizeSendFileTargetChannels(targetChannels),
	}, nil
}

// PublishSendFileExecutionGrant 把精确授权发布到当前任务，返回派生 ctx。
//
// Python: publish_send_file_execution_grant(grant)
// 发布前先清除旧租约（对齐 Python 的 clear → set 顺序）。
func PublishSendFileExecutionGrant(ctx context.Context, grant SendFileExecutionGrant) context.Context {
	ctx = ClearSendFileExecutionGrant(ctx)
	return context.WithValue(ctx, sendFileExecutionGrantKey{}, &sendFileExecutionLease{grant: grant})
}

// CurrentSendFileExecutionGrant 返回当前任务未消费的授权。
//
// Python: current_send_file_execution_grant()
func CurrentSendFileExecutionGrant(ctx context.Context) *SendFileExecutionGrant {
	lease := leaseFromCtx(ctx)
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.terminal {
		return nil
	}
	grant := lease.grant
	return &grant
}

// ClearSendFileExecutionGrant 清除陈旧或已消费的当前任务授权，返回派生 ctx。
//
// Python: clear_send_file_execution_grant()
// 旧租约被标记 terminal，因此持有旧 ctx 的分支也读不到。
func ClearSendFileExecutionGrant(ctx context.Context) context.Context {
	lease := leaseFromCtx(ctx)
	ctx = context.WithValue(ctx, sendFileExecutionGrantKey{}, (*sendFileExecutionLease)(nil))
	if lease != nil {
		lease.mu.Lock()
		lease.terminal = true
		lease.mu.Unlock()
	}
	return ctx
}

// ConsumeSendFileExecutionGrant 一次性消费并校验精确路径与目标集合。
//
// Python: consume_send_file_execution_grant(requested_paths, *, target_channels=None)
// 返回 error 表达三种失败：租约缺失 / 已消费 / 不匹配。
func ConsumeSendFileExecutionGrant(
	ctx context.Context,
	requestedPaths []string,
	targetChannels any,
) ([]SendFileAuthorizationItem, error) {
	lease := leaseFromCtx(ctx)
	// Python 的 consume 只做 ContextVar.set(None) 清绑定，**不**调用
	// clear_send_file_execution_grant（后者会额外标记 terminal）。
	// 若这里误用 Clear，terminal 会被提前置真，导致下面的检查必然失败。
	//
	// Go 的 ctx 派生不可变，无法清父 ctx 的绑定；但把 lease.terminal 置真后，
	// CurrentSendFileExecutionGrant 对所有持有该 lease 的分支都返回 nil，
	// 与 Python「消费后读不到」的语义等价。
	if lease == nil {
		return nil, fmt.Errorf("%s", errSendFileExecutionGrantMissing)
	}
	lease.mu.Lock()
	if lease.terminal {
		lease.mu.Unlock()
		return nil, fmt.Errorf("%s", errSendFileExecutionGrantMissing)
	}
	lease.terminal = true
	grant := lease.grant
	lease.mu.Unlock()

	expected, err := CreateSendFileExecutionGrant(requestedPaths, targetChannels)
	if err != nil {
		return nil, err
	}
	if !sendFileGrantEqual(grant, expected) {
		return nil, fmt.Errorf("%s", errSendFileExecutionGrantMismatch)
	}
	return grant.Items, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeSendFileCandidates 把任意输入归一为字符串候选列表。
//
// Python: normalize_send_file_* 里的 candidates 推导（两函数共用逻辑）
// 字符串先 trim，再尝试 JSON 解析；解析失败则整体当单值。
func normalizeSendFileCandidates(value any) []string {
	parsed := value
	if s, ok := value.(string); ok {
		stripped := strings.TrimSpace(s)
		if stripped == "" {
			return nil
		}
		var decoded any
		if err := json.Unmarshal([]byte(stripped), &decoded); err == nil {
			parsed = decoded
		} else {
			parsed = stripped
		}
	}
	switch v := parsed.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = anyToStringValue(item)
		}
		return out
	case []string:
		return v
	default:
		return []string{anyToStringValue(parsed)}
	}
}

// anyToStringValue Python str(value) 等价。
func anyToStringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

// resolveSendFilePath Path(path).expanduser().resolve(strict=False) 等价。
func resolveSendFilePath(path string) string {
	expanded := expandHomeString(strings.TrimSpace(path))
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return filepath.ToSlash(expanded)
	}
	return filepath.ToSlash(filepath.Clean(abs))
}

// sendFileGrantEqual 授权相等比较（对齐 frozen dataclass 的 ==）。
func sendFileGrantEqual(a, b SendFileExecutionGrant) bool {
	if len(a.Items) != len(b.Items) || len(a.TargetChannels) != len(b.TargetChannels) {
		return false
	}
	for i := range a.Items {
		if a.Items[i].ResolvedPath != b.Items[i].ResolvedPath {
			return false
		}
	}
	for i := range a.TargetChannels {
		if a.TargetChannels[i] != b.TargetChannels[i] {
			return false
		}
	}
	return true
}

// leaseFromCtx 从 ctx 读取租约（未绑定或显式 nil 返回 nil）。
func leaseFromCtx(ctx context.Context) *sendFileExecutionLease {
	lease, ok := ctx.Value(sendFileExecutionGrantKey{}).(*sendFileExecutionLease)
	if !ok {
		return nil
	}
	return lease
}
