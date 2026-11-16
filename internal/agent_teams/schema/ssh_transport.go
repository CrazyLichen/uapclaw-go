package schema

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SshTransportConfig 一个可达 ssh 端点的连接信息（外部 CLI 成员远程启动用）。
//
// Python: SshTransportConfig (agent_teams/schema/ssh_transport.py:15)
type SshTransportConfig struct {
	// Host ssh 端点的主机名或 IP
	Host string `json:"host"`
	// Port ssh 端口
	Port int `json:"port,omitempty"`
	// Username 登录用户；空表示由 ssh 从配置/agent 中挑选
	Username string `json:"username,omitempty"`
	// KeyFile 私钥文件路径
	KeyFile string `json:"key_file,omitempty"`
	// Password 密码认证值
	Password string `json:"password,omitempty"`
	// Agent 使用本地 ssh-agent 提供密钥
	Agent bool `json:"agent,omitempty"`
	// KnownHosts known_hosts 文件路径（可选）；空时保留严格默认校验，
	// 除非 DisableHostKeyCheck 已开启
	KnownHosts string `json:"known_hosts,omitempty"`
	// DisableHostKeyCheck 完全跳过 host key 校验
	DisableHostKeyCheck bool `json:"disable_host_key_check,omitempty"`
	// ConnectTimeoutS 连接超时秒数
	ConnectTimeoutS float64 `json:"connect_timeout_s,omitempty"`
	// UseExec 以 exec 方式把远程 shell 替换为 Claude 命令
	UseExec bool `json:"use_exec,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 校验至少配置了一种认证方式。
// Python: SshTransportConfig._require_auth (ssh_transport.py:41)
func (s *SshTransportConfig) Validate() error {
	if s.KeyFile == "" && s.Password == "" && !s.Agent {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("SshTransportConfig requires at least one auth method (key_file / password set / agent=True)"))
	}
	return nil
}
