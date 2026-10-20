package team

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// subprocessTimeoutSec 单条外部命令（pg_isready/systemctl 等）的墙钟超时（秒）。
	// 对齐 Python: _SUBPROCESS_TIMEOUT_SEC = 120.0 (team_manager.py L78)
	subprocessTimeoutSec = 120.0
	// pgPostStartReadyMaxSec 启动命令返回 OK 后等待就绪的最大秒数。
	// 对齐 Python: _PG_POST_START_READY_MAX_SEC = 30.0
	pgPostStartReadyMaxSec = 30.0
	// pgPostStartReadyInitSleep 就绪轮询初始休眠（秒）。
	// 对齐 Python: _PG_POST_START_READY_INIT_SLEEP = 0.4
	pgPostStartReadyInitSleep = 0.4
	// pgPostStartReadyMaxSleep 就绪轮询单次最大休眠（秒）。
	// 对齐 Python: _PG_POST_START_READY_MAX_SLEEP = 2.0
	pgPostStartReadyMaxSleep = 2.0
	// pgPostStartReadyBackoff 就绪轮询退避系数。
	// 对齐 Python: _PG_POST_START_READY_BACKOFF = 1.45
	pgPostStartReadyBackoff = 1.45
	// pgPostStartLogEverySec 就绪等待期间的日志间隔（秒）。
	// 对齐 Python: _PG_POST_START_LOG_EVERY_SEC = 5.0
	pgPostStartLogEverySec = 5.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsDistributedMode 判断 config 是否为分布式模式。
// 对齐 Python: is_distributed_mode (distributed_runtime.py L20-28)
//
// team.runtime.mode=="distributed" 或 team.transport.type=="pyzmq"（均忽略大小写与空白）。
func IsDistributedMode(configBase map[string]any) bool {
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	if strings.EqualFold(strings.TrimSpace(anyToStr(runtimeCfg["mode"])), "distributed") {
		return true
	}
	transportCfg, _ := teamCfg["transport"].(map[string]any)
	if transportCfg == nil {
		transportCfg = map[string]any{}
	}
	return strings.EqualFold(strings.TrimSpace(anyToStr(transportCfg["type"])), "pyzmq")
}

// RuntimeRole 解析运行时角色。
// 对齐 Python: runtime_role (L31-35)
//
// 默认 leader，仅接受 leader/teammate（忽略大小写），其他值回退 leader。
func RuntimeRole(configBase map[string]any) string {
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	role := strings.ToLower(strings.TrimSpace(anyToStr(runtimeCfg["role"])))
	if role == "teammate" {
		return "teammate"
	}
	return "leader"
}

// RuntimeMemberName 解析运行时成员名。
// 对齐 Python: runtime_member_name (L38-50)
//
// team.runtime.member_name → predefined_members[0].member_name 回退，均无返回空串。
func RuntimeMemberName(configBase map[string]any, teamCfg map[string]any) string {
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	configured := strings.TrimSpace(anyToStr(runtimeCfg["member_name"]))
	if configured != "" {
		return configured
	}
	predefined, _ := teamCfg["predefined_members"].([]any)
	for _, item := range predefined {
		member, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if memberName := strings.TrimSpace(anyToStr(member["member_name"])); memberName != "" {
			return memberName
		}
	}
	return ""
}

// ParsePort 解析并验证端口值。
// 对齐 Python: parse_port (L53-71)
//
// M-04: 恢复 Python raise ValueError 语义——非法端口显式报错传播到调用方
// （Python 调用链 normalize_distributed_transport_fields/_load_team_spec 无 except，
// 配置错误显式暴露）。原降级为 warn+default 的注释"上层吞掉后使用默认值"与 Python
// 事实不符，已修正。value 为 nil/空串时返回 default 不报错（对齐 Python None 守卫）。
// 范围 1..65535，越界或解析失败返回 error。
func ParsePort(value any, defaultPort int, fieldName string) (int, error) {
	if value == nil {
		return defaultPort, nil
	}
	raw := value
	if s, ok := value.(string); ok {
		stripped := strings.TrimSpace(s)
		if stripped == "" {
			return defaultPort, nil
		}
		raw = stripped
	}
	var port int
	switch v := raw.(type) {
	case int:
		port = v
	case int32:
		port = int(v)
	case int64:
		port = int(v)
	case float64:
		port = int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return defaultPort, fmt.Errorf("invalid %s: %v, expected an integer in range 1..65535", fieldName, value)
		}
		port = n
	default:
		return defaultPort, fmt.Errorf("invalid %s: %v, expected an integer in range 1..65535", fieldName, value)
	}
	if port < 1 || port > 65535 {
		return defaultPort, fmt.Errorf("invalid %s: %d, expected an integer in range 1..65535", fieldName, port)
	}
	return port, nil
}

// NormalizeDistributedTransportFields 标准化分布式传输字段。
// 对齐 Python: normalize_distributed_transport_fields (L74-151)
//
// 仅 transport.type=="pyzmq" 时处理：leader 清 known_peers/bootstrap_peers/
// bootstrap_known_peers（即使 pubsub/direct_addr 已预填）；pubsub 地址未预填时按
// 默认端口推导（leader direct 18555/pub 18556/sub 18557、teammate direct 18600）；
// teammate 构造指向 leader 的 known_peers/bootstrap_peers。返回深拷贝，不修改入参。
// M-04: 端口非法时返回 error（对齐 Python parse_port raise 沿调用链传播）。
func NormalizeDistributedTransportFields(configBase map[string]any, teamCfg map[string]any) (map[string]any, error) {
	normalizedCfg := utils.DeepCopyMap(teamCfg)
	if normalizedCfg == nil {
		normalizedCfg = map[string]any{}
	}
	transportCfg, _ := normalizedCfg["transport"].(map[string]any)
	if transportCfg == nil {
		return normalizedCfg, nil
	}
	if !strings.EqualFold(strings.TrimSpace(anyToStr(transportCfg["type"])), "pyzmq") {
		return normalizedCfg, nil
	}
	params, _ := transportCfg["params"].(map[string]any)
	if params == nil {
		return normalizedCfg, nil
	}

	role := RuntimeRole(configBase)
	if role == "leader" {
		// 即使 YAML 模板已预填 pubsub/direct_addr 也应用（leader 清空 peers）
		delete(params, "known_peers")
		delete(params, "bootstrap_peers")
		delete(params, "bootstrap_known_peers")
	}

	pubAddr := strings.TrimSpace(anyToStr(params["pubsub_publish_addr"]))
	subAddr := strings.TrimSpace(anyToStr(params["pubsub_subscribe_addr"]))
	if pubAddr != "" && subAddr != "" {
		return normalizedCfg, nil
	}

	leaderCfg, _ := params["leader"].(map[string]any)
	if leaderCfg == nil {
		leaderCfg = map[string]any{}
	}
	teammateCfg, _ := params["teammate"].(map[string]any)
	if teammateCfg == nil {
		teammateCfg = map[string]any{}
	}
	leaderHost := strings.TrimSpace(anyToStr(leaderCfg["host"]))
	if leaderHost == "" {
		leaderHost = "127.0.0.1"
	}
	// M-04: 端口解析失败显式返回 error（对齐 Python raise ValueError）
	leaderDirectPort, err := ParsePort(leaderCfg["direct_port"], 18555, "team.transport.params.leader.direct_port")
	if err != nil {
		return normalizedCfg, err
	}
	leaderPubPort, err := ParsePort(leaderCfg["pub_port"], 18556, "team.transport.params.leader.pub_port")
	if err != nil {
		return normalizedCfg, err
	}
	leaderSubPort, err := ParsePort(leaderCfg["sub_port"], 18557, "team.transport.params.leader.sub_port")
	if err != nil {
		return normalizedCfg, err
	}

	localDirectAddr := ""
	pubsubBind := false
	if role == "leader" {
		localDirectAddr = "tcp://0.0.0.0:" + strconv.Itoa(leaderDirectPort)
		pubsubBind = true
	} else {
		leaderMemberName := "team_leader"
		if leaderIdentity, _ := normalizedCfg["leader"].(map[string]any); leaderIdentity != nil {
			if n := strings.TrimSpace(anyToStr(leaderIdentity["member_name"])); n != "" {
				leaderMemberName = n
			}
		}
		teammateHost := strings.TrimSpace(anyToStr(teammateCfg["host"]))
		if teammateHost == "" {
			teammateHost = "127.0.0.1"
		}
		teammateDirectPort, err := ParsePort(teammateCfg["direct_port"], 18600, "team.transport.params.teammate.direct_port")
		if err != nil {
			return normalizedCfg, err
		}
		localDirectAddr = "tcp://0.0.0.0:" + strconv.Itoa(teammateDirectPort)
		knownPeers := []any{
			map[string]any{
				"agent_id": leaderMemberName,
				"addrs":    []any{"tcp://" + leaderHost + ":" + strconv.Itoa(leaderDirectPort)},
			},
		}
		pubsubBind = false
		params["known_peers"] = knownPeers
		params["bootstrap_peers"] = knownPeers
	}

	params["direct_addr"] = localDirectAddr
	params["pubsub_publish_addr"] = "tcp://" + leaderHost + ":" + strconv.Itoa(leaderPubPort)
	params["pubsub_subscribe_addr"] = "tcp://" + leaderHost + ":" + strconv.Itoa(leaderSubPort)
	metadata, _ := params["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["pubsub_bind"] = pubsubBind
	params["metadata"] = metadata
	return normalizedCfg, nil
}

// IsPostgresqlStorage 判断是否为 PostgreSQL 存储。
// 对齐 Python: is_postgresql_storage (L154-157)
func IsPostgresqlStorage(teamCfg map[string]any) bool {
	storageCfg, _ := teamCfg["storage"].(map[string]any)
	if storageCfg == nil {
		return false
	}
	storageType := strings.ToLower(strings.TrimSpace(anyToStr(storageCfg["type"])))
	return storageType == "postgresql" || storageType == "postgres"
}

// MissingDistributedDependencies 返回分布式运行时缺失的依赖列表。
// 对齐 Python: missing_distributed_dependencies (L160-172)
//
// Go 差异：静态编译无运行时缺包概念（Python 检查 zmq/asyncpg import 存在性），
// 恒返回空列表，保留函数以对齐调用链结构与日志点。
func MissingDistributedDependencies(configBase map[string]any) []string {
	return []string{}
}

// FallbackDistributedToLocal 从分布式配置创建本地回退配置。
// 对齐 Python: fallback_distributed_to_local (L175-220)
//
// runtime.mode=local + role=leader；transport.type=inprocess（去 params）；
// postgres 存储→sqlite team.db；modes.team 逐项同样处理。返回深拷贝。
func FallbackDistributedToLocal(configBase map[string]any) map[string]any {
	normalized := utils.DeepCopyMap(configBase)
	if normalized == nil {
		normalized = map[string]any{}
	}

	teamCfg, _ := normalized["team"].(map[string]any)
	if teamCfg != nil {
		runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
		if runtimeCfg == nil {
			runtimeCfg = map[string]any{}
		}
		runtimeCfg["mode"] = "local"
		runtimeCfg["role"] = "leader"
		teamCfg["runtime"] = runtimeCfg

		transportCfg, _ := teamCfg["transport"].(map[string]any)
		if transportCfg == nil {
			transportCfg = map[string]any{}
		}
		transportCfg["type"] = "inprocess"
		delete(transportCfg, "params")
		teamCfg["transport"] = transportCfg

		if IsPostgresqlStorage(teamCfg) {
			teamCfg["storage"] = map[string]any{
				"type":   "sqlite",
				"params": map[string]any{"connection_string": "team.db"},
			}
		}
	}

	modesCfg, _ := normalized["modes"].(map[string]any)
	if modesCfg != nil {
		modeTeam, _ := modesCfg["team"].(map[string]any)
		for _, candidate := range modeTeam {
			cand, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			transportCfg, _ := cand["transport"].(map[string]any)
			if transportCfg == nil {
				transportCfg = map[string]any{}
			}
			transportCfg["type"] = "inprocess"
			delete(transportCfg, "params")
			cand["transport"] = transportCfg
			if IsPostgresqlStorage(cand) {
				cand["storage"] = map[string]any{
					"type":   "sqlite",
					"params": map[string]any{"connection_string": "team.db"},
				}
			}
		}
	}
	return normalized
}

// ExtractPgEndpoint 从存储配置解析 PG 主机与端口。
// 对齐 Python: extract_pg_endpoint (L223-235)
//
// 接受 sqlalchemy 风格 URL（postgresql+asyncpg://user:pass@host:5432/db），
// 前缀归一化后 urlparse；默认 127.0.0.1:5432。
func ExtractPgEndpoint(teamCfg map[string]any) (string, int) {
	storageCfg, _ := teamCfg["storage"].(map[string]any)
	if storageCfg == nil {
		return "127.0.0.1", 5432
	}
	params, _ := storageCfg["params"].(map[string]any)
	if params == nil {
		return "127.0.0.1", 5432
	}
	connectionString := strings.TrimSpace(anyToStr(params["connection_string"]))
	if connectionString == "" {
		return "127.0.0.1", 5432
	}
	normalized := connectionString
	if strings.HasPrefix(normalized, "postgresql+") {
		if idx := strings.Index(normalized, "://"); idx >= 0 {
			normalized = "postgresql://" + normalized[idx+3:]
		}
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "127.0.0.1", 5432
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := 5432
	if p := parsed.Port(); p != "" {
		if n, convErr := strconv.Atoi(p); convErr == nil {
			port = n
		}
	}
	return host, port
}

// RunCommand 执行外部命令并返回退出码与合并输出（stdout+stderr）。
// 对齐 Python: run_command (distributed_runtime.py L238-279)
//
// 退出码约定：spawn 失败 127 / 超时 124 / 成功或非零退出返回真实退出码。
// Python 的 communicate 失败 126 在 Go 中无直接对应（CombinedOutput 管道错误
// 极罕见），并入 127 路径。
func RunCommand(ctx context.Context, args []string, timeoutSec float64) (int, string) {
	if len(args) == 0 {
		return 127, ""
	}
	cmdStr := strings.Join(args, " ")
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	cmd := exec.CommandContext(cctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return cmd.ProcessState.ExitCode(), string(out)
	}
	// 超时（Python: 124）
	if cctx.Err() == context.DeadlineExceeded {
		logger.Warn(logComponent).Str("cmd", cmdStr).Float64("timeout_sec", timeoutSec).
			Msg("[TeamManager] subprocess timed out")
		return 124, ""
	}
	// 普通非零退出，返回真实退出码（对齐 Python 返回 returncode）
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	// spawn 失败（Python OSError → 127）
	logger.Warn(logComponent).Str("cmd", cmdStr).Err(err).
		Msg("[TeamManager] subprocess spawn failed")
	return 127, ""
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsPgAvailable 探测 PG 端点是否可接受连接。
// 对齐 Python: is_pg_available (L282-291)
//
// pg_isready -h host -p port，rc==0 视为可用。
func IsPgAvailable(ctx context.Context, host string, port int) bool {
	rc, _ := RunCommand(ctx, []string{"pg_isready", "-h", host, "-p", strconv.Itoa(port)}, subprocessTimeoutSec)
	return rc == 0
}

// TryStartPgCluster 尝试启动本机 PG 集群。
// 对齐 Python: try_start_pg_cluster (L294-316)
//
// pg_lsclusters --no-header 解析首行 → pg_ctlcluster <ver> <name> start → 启动成功返回 true。
func TryStartPgCluster(ctx context.Context) bool {
	rc, out := RunCommand(ctx, []string{"pg_lsclusters", "--no-header"}, subprocessTimeoutSec)
	if rc != 0 {
		return false
	}
	firstLine := ""
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	if firstLine == "" {
		return false
	}
	parts := strings.Fields(firstLine)
	if len(parts) < 2 {
		return false
	}
	version, cluster := parts[0], parts[1]
	startRc, _ := RunCommand(ctx, []string{"pg_ctlcluster", version, cluster, "start"}, subprocessTimeoutSec)
	return startRc == 0
}

// EnsurePostgresqlForLeader 确保 leader 的 PostgreSQL 可用。
// 对齐 Python: ensure_postgresql_for_leader (L319-413)
//
// 组合流程：非 leader 或非 pg 存储短路 → ExtractPgEndpoint → IsPgAvailable
// 失败则 TryStartPgCluster → 回退 systemctl/service start → 启动后指数退避等待
// 就绪（init 0.4s / max 2.0s / backoff 1.45 / max 30s / 每 5s 日志）。
func EnsurePostgresqlForLeader(ctx context.Context, configBase map[string]any) {
	if RuntimeRole(configBase) != "leader" {
		return
	}
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	if !IsPostgresqlStorage(teamCfg) {
		return
	}

	host, port := ExtractPgEndpoint(teamCfg)
	if IsPgAvailable(ctx, host, port) {
		return
	}

	logger.Warn(logComponent).Str("host", host).Int("port", port).
		Msg("[TeamManager] PostgreSQL not reachable, attempting auto-start as leader")

	started := TryStartPgCluster(ctx)
	if !started {
		for _, cmd := range [][]string{
			{"systemctl", "start", "postgresql"},
			{"service", "postgresql", "start"},
		} {
			if rc, _ := RunCommand(ctx, cmd, subprocessTimeoutSec); rc == 0 {
				started = true
				break
			}
		}
	}
	if !started {
		logger.Warn(logComponent).
			Msg("[TeamManager] PostgreSQL auto-start command failed, will continue and rely on DB connect error")
		return
	}

	logger.Info(logComponent).Str("host", host).Int("port", port).
		Float64("max_sec", pgPostStartReadyMaxSec).
		Msg("[TeamManager] start command returned OK; waiting for connections")

	deadline := time.Now().Add(time.Duration(pgPostStartReadyMaxSec * float64(time.Second)))
	sleepFor := pgPostStartReadyInitSleep
	waitStart := time.Now()
	lastLog := waitStart
	for time.Now().Before(deadline) {
		if IsPgAvailable(ctx, host, port) {
			logger.Info(logComponent).Str("host", host).Int("port", port).
				Float64("waited_sec", time.Since(waitStart).Seconds()).
				Msg("[TeamManager] PostgreSQL is ready")
			return
		}
		now := time.Now()
		if now.Sub(lastLog).Seconds() >= pgPostStartLogEverySec {
			logger.Info(logComponent).Str("host", host).Int("port", port).
				Float64("elapsed_sec", now.Sub(waitStart).Seconds()).
				Float64("max_sec", pgPostStartReadyMaxSec).
				Msg("[TeamManager] still waiting for PostgreSQL")
			lastLog = now
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := time.Duration(sleepFor * float64(time.Second))
		if sleep > remaining {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		sleepFor = math.Min(sleepFor*pgPostStartReadyBackoff, pgPostStartReadyMaxSleep)
	}

	logger.Warn(logComponent).Str("host", host).Int("port", port).
		Float64("max_sec", pgPostStartReadyMaxSec).
		Msg("[TeamManager] PostgreSQL start attempted but endpoint still not ready")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// anyToStr 从 any 提取字符串（非 string 返回空串）。
//
// Python str() 会把非字符串值转为不匹配 "distributed" 等的字面量，
// Go 返回空串同样不匹配，语义等价。
func anyToStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
