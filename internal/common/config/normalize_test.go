package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestParseCustomHeaders(t *testing.T) {
	t.Run("nil输入返回nil", func(t *testing.T) {
		assert.Nil(t, ParseCustomHeaders(nil))
	})

	t.Run("空字符串返回nil", func(t *testing.T) {
		assert.Nil(t, ParseCustomHeaders(""))
	})

	t.Run("已经是map直接返回", func(t *testing.T) {
		input := map[string]any{"X-Custom": "value"}
		result := ParseCustomHeaders(input)
		assert.Equal(t, input, result)
	})

	t.Run("JSON字符串解析为map", func(t *testing.T) {
		input := `{"X-Trace-Id": "abc", "X-Request": "req"}`
		result := ParseCustomHeaders(input)
		m, ok := result.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "abc", m["X-Trace-Id"])
		assert.Equal(t, "req", m["X-Request"])
	})

	t.Run("无效JSON返回nil", func(t *testing.T) {
		assert.Nil(t, ParseCustomHeaders("not-json"))
	})

	t.Run("非字符串非map类型返回nil", func(t *testing.T) {
		assert.Nil(t, ParseCustomHeaders(123))
	})

	t.Run("JSON数组返回nil", func(t *testing.T) {
		assert.Nil(t, ParseCustomHeaders(`[1,2,3]`))
	})
}

func TestNormalizeConfig(t *testing.T) {
	t.Run("nil数据不panic", func(t *testing.T) {
		NormalizeConfig(nil) // 不 panic 即通过
	})

	t.Run("空数据不panic", func(t *testing.T) {
		NormalizeConfig(map[string]any{}) // 不 panic 即通过
	})

	t.Run("models中custom_headers从JSON字符串解析为map", func(t *testing.T) {
		data := map[string]any{
			"models": map[string]any{
				"qwen": map[string]any{
					"model_client_config": map[string]any{
						"custom_headers": `{"X-Trace-Id": "abc"}`,
					},
				},
			},
		}
		NormalizeConfig(data)

		mcc := data["models"].(map[string]any)["qwen"].(map[string]any)["model_client_config"].(map[string]any)
		headers := mcc["custom_headers"].(map[string]any)
		assert.Equal(t, "abc", headers["X-Trace-Id"])
	})

	t.Run("react中custom_headers从JSON字符串解析为map", func(t *testing.T) {
		data := map[string]any{
			"react": map[string]any{
				"model_client_config": map[string]any{
					"custom_headers": `{"X-Custom": "val"}`,
				},
			},
		}
		NormalizeConfig(data)

		mcc := data["react"].(map[string]any)["model_client_config"].(map[string]any)
		headers := mcc["custom_headers"].(map[string]any)
		assert.Equal(t, "val", headers["X-Custom"])
	})

	t.Run("channels.web.send_file_allowed设置默认值true", func(t *testing.T) {
		data := map[string]any{
			"channels": map[string]any{
				"web": map[string]any{},
			},
		}
		NormalizeConfig(data)

		web := data["channels"].(map[string]any)["web"].(map[string]any)
		assert.Equal(t, true, web["send_file_allowed"])
	})

	t.Run("channels.web.send_file_allowed已存在不覆盖", func(t *testing.T) {
		data := map[string]any{
			"channels": map[string]any{
				"web": map[string]any{"send_file_allowed": false},
			},
		}
		NormalizeConfig(data)

		web := data["channels"].(map[string]any)["web"].(map[string]any)
		assert.Equal(t, false, web["send_file_allowed"])
	})

	t.Run("channels无web段时创建web默认值", func(t *testing.T) {
		data := map[string]any{
			"channels": map[string]any{},
		}
		NormalizeConfig(data)

		web := data["channels"].(map[string]any)["web"].(map[string]any)
		assert.Equal(t, true, web["send_file_allowed"])
	})

	t.Run("custom_headers已经是map不做转换", func(t *testing.T) {
		originalHeaders := map[string]any{"X-Existing": "value"}
		data := map[string]any{
			"models": map[string]any{
				"qwen": map[string]any{
					"model_client_config": map[string]any{
						"custom_headers": originalHeaders,
					},
				},
			},
		}
		NormalizeConfig(data)

		mcc := data["models"].(map[string]any)["qwen"].(map[string]any)["model_client_config"].(map[string]any)
		assert.Equal(t, originalHeaders, mcc["custom_headers"])
	})
}

func TestNormalizeConfig_与Load集成(t *testing.T) {
	t.Run("WithNormalize注入后Load自动调用", func(t *testing.T) {
		// 创建临时 config.yaml
		dir := t.TempDir()
		configPath := dir + "/config.yaml"
		yamlContent := `logging:
  level: info
models:
  qwen:
    model_client_config:
      custom_headers: '{"X-Trace-Id": "from-yaml"}'
channels:
  web: {}
`
		require.NoError(t, os.WriteFile(configPath, []byte(yamlContent), 0o644))

		cfg, err := New(configPath, WithNormalize(NormalizeConfig))
		require.NoError(t, err)

		data, err := cfg.Load()
		require.NoError(t, err)

		// 验证 custom_headers 已从 JSON 字符串解析为 map
		models := data["models"].(map[string]any)
		qwen := models["qwen"].(map[string]any)
		mcc := qwen["model_client_config"].(map[string]any)
		headers := mcc["custom_headers"].(map[string]any)
		assert.Equal(t, "from-yaml", headers["X-Trace-Id"])

		// 验证 send_file_allowed 默认值
		channels := data["channels"].(map[string]any)
		web := channels["web"].(map[string]any)
		assert.Equal(t, true, web["send_file_allowed"])
	})
}
