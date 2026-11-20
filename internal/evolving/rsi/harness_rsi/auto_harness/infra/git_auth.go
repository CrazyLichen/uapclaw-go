package infra

import (
	"encoding/base64"
	"fmt"
	"os"
)

// BuildGitAuthEnv 构造非交互 GitCode 认证的子进程环境。
//
// 环境规避用户全局 credential helper，并只为 https://gitcode.com 请求
// 注入 Authorization 头（对齐 build_git_auth_env）。
func BuildGitAuthEnv(username string, token string) map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				env[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	env["GIT_TERMINAL_PROMPT"] = "0"
	env["GCM_INTERACTIVE"] = "never"

	if username == "" || token == "" {
		return env
	}

	basic := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, token)))
	env["GIT_CONFIG_COUNT"] = "6"
	env["GIT_CONFIG_KEY_0"] = "credential.helper"
	env["GIT_CONFIG_VALUE_0"] = ""
	env["GIT_CONFIG_KEY_1"] = "credential.interactive"
	env["GIT_CONFIG_VALUE_1"] = "never"
	env["GIT_CONFIG_KEY_2"] = "http.https://gitcode.com/.extraheader"
	env["GIT_CONFIG_VALUE_2"] = fmt.Sprintf("AUTHORIZATION: basic %s", basic)
	env["GIT_CONFIG_KEY_3"] = "http.version"
	env["GIT_CONFIG_VALUE_3"] = "HTTP/1.1"
	env["GIT_CONFIG_KEY_4"] = "http.lowSpeedLimit"
	env["GIT_CONFIG_VALUE_4"] = "1"
	env["GIT_CONFIG_KEY_5"] = "http.lowSpeedTime"
	env["GIT_CONFIG_VALUE_5"] = "60"
	return env
}

// envMapToSlice map 转环境切片（k=v 形式，供 CommandRunner 使用）
func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
