package routing

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMap 会话映射：稳定身份（provider/chat/bot[/user]）→ 轮转 agent session_id。
// 持久化到 {workspace}/agent/.checkpoint/session_map.json。
// Python: jiuwenswarm/gateway/routing/session_map.py SessionMap
type SessionMap struct {
	// scope 键作用域（构造时确定，不热重载）
	scope SessionMapScope
	// storePath 持久化文件路径
	storePath string
	// mapping 身份键 → agent session_id
	mapping map[string]string
	// mu 并发保护（Python asyncio 单线程无锁，Go 补齐）
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionMapScope 会话映射键作用域枚举
// Python: SessionMapScope(str, Enum)
type SessionMapScope int

const (
	// SessionMapScopePerChatBot 同 (provider, chat, bot) 共享会话（默认）
	SessionMapScopePerChatBot SessionMapScope = iota
	// SessionMapScopePerChatBotUser 同 (provider, chat, bot, user) 一个会话
	SessionMapScopePerChatBotUser
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMapScopePerChatBotStr 配置值 "per_chat_bot"
	sessionMapScopePerChatBotStr = "per_chat_bot"
	// sessionMapScopePerChatBotUserStr 配置值 "per_chat_bot_user"
	sessionMapScopePerChatBotUserStr = "per_chat_bot_user"
	// sessionMapFileName 持久化文件名
	sessionMapFileName = "session_map.json"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadSessionMapScope 读 config gateway.session_map_scope（构造时读一次，不热重载）。
//
// Python: load_session_map_scope (L21-33)：
// 全局读通道（getGlobalConfig → cfg.Load() 重读盘，等价 Python get_config()）；
// 未知值 / 未注册 / 读失败 → Warn + 默认 PerChatBot。
func LoadSessionMapScope() SessionMapScope {
	raw := sessionMapScopePerChatBotStr
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		configBase, err := cfg.Load()
		if err == nil {
			if gateway, ok := configBase["gateway"].(map[string]any); ok {
				if v, ok := gateway["session_map_scope"].(string); ok && v != "" {
					raw = v
				}
			}
		} else {
			logger.Warn(logComponent).
				Err(err).
				Str("default", sessionMapScopePerChatBotStr).
				Msg("SessionMap scope load failed, using default")
			return SessionMapScopePerChatBot
		}
	}
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case sessionMapScopePerChatBotStr:
		return SessionMapScopePerChatBot
	case sessionMapScopePerChatBotUserStr:
		return SessionMapScopePerChatBotUser
	default:
		logger.Warn(logComponent).
			Str("value", raw).
			Str("default", sessionMapScopePerChatBotStr).
			Msg("Unknown gateway.session_map_scope, using default")
		return SessionMapScopePerChatBot
	}
}

// NewSessionMap 创建会话映射。
//
// scope 为 nil 时走 LoadSessionMapScope() 读 config。
// Python: SessionMap(scope=None) (L65-69)
func NewSessionMap(scope *SessionMapScope) *SessionMap {
	s := SessionMapScopePerChatBot
	if scope != nil {
		s = *scope
	} else {
		s = LoadSessionMapScope()
	}
	m := &SessionMap{
		scope:     s,
		storePath: filepath.Join(path.CheckpointDir(), sessionMapFileName),
		mapping:   make(map[string]string),
	}
	m.load()
	return m
}

// NewSessionMapAtForTest 指定存储路径构造（供跨包测试使用，生产代码勿用）。
func NewSessionMapAtForTest(storePath string, scope SessionMapScope) *SessionMap {
	return newSessionMapAt(storePath, scope)
}

// GetSessionID 获取（或生成）身份键对应的 agent session_id。
//
// rotate=false 且已有映射 → 返回既有值；否则生成新值写盘。
// Python: get_session_id (L90-109)。
func (m *SessionMap) GetSessionID(provider, chatID, botID, userID string, rotate bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(m.scope, provider, chatID, botID, userID)
	existing, ok := m.mapping[key]
	if ok && !rotate {
		return existing
	}

	sid := makeSessionID(m.scope, provider, chatID, botID, userID)
	if existing == sid {
		// Python L105-106：同毫秒+同随机的理论幂等分支，直接返回不写盘
		return sid
	}
	m.mapping[key] = sid
	m.save()
	return sid
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSessionMapAt 指定存储路径的构造（测试与 NewSessionMapAtForTest 共用）。
func newSessionMapAt(storePath string, scope SessionMapScope) *SessionMap {
	m := &SessionMap{
		scope:     scope,
		storePath: storePath,
		mapping:   make(map[string]string),
	}
	m.load()
	return m
}

// makeKey 生成身份键。
//
// Python: _make_key (L36-45)：
// PerChatBot: "provider::chatID::botID"；PerChatBotUser 追加 "::userID"
func makeKey(scope SessionMapScope, provider, chatID, botID, userID string) string {
	if scope == SessionMapScopePerChatBot {
		return fmt.Sprintf("%s::%s::%s", provider, chatID, botID)
	}
	return fmt.Sprintf("%s::%s::%s::%s", provider, chatID, botID, userID)
}

// makeSessionID 生成新的 agent session_id。
//
// Python: _make_session_id (L48-59)：key 结构 + "::" + hex(ms时间戳) + "::" + 6位随机hex
func makeSessionID(scope SessionMapScope, provider, chatID, botID, userID string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := generateRandomHex(3)
	if scope == SessionMapScopePerChatBot {
		return fmt.Sprintf("%s::%s::%s::%s::%s", provider, chatID, botID, ts, suffix)
	}
	return fmt.Sprintf("%s::%s::%s::%s::%s::%s", provider, chatID, botID, userID, ts, suffix)
}

// load 从盘加载映射（防御式，失败仅 Warn）。
//
// Python: _load (L71-80)：文件不存在静默；非 dict 跳过；
// 值必须为非空 string（Python isinstance(v, str) and v）
func (m *SessionMap) load() {
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).Err(err).Msg("SessionMap load failed")
		}
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap load failed")
		return
	}
	for k, v := range parsed {
		if s, ok := v.(string); ok && s != "" {
			m.mapping[k] = s
		}
	}
}

// save 将映射写盘（防御式，失败仅 Warn）。
//
// Python: _save (L82-88)：mkdir parents + json.dump(indent=2, ensure_ascii=False)
func (m *SessionMap) save() {
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
		return
	}
	data, err := json.MarshalIndent(m.mapping, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
		return
	}
	if err := os.WriteFile(m.storePath, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
	}
}

// generateRandomHex 生成 n 字节的随机 hex（2n 字符）。
// Python: secrets.token_hex(n)；crypto/rand 失败降级时间戳切片
func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}
