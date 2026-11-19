// Python 源：external_providers 注入点用例（域注册/未注册/覆盖/诊断）

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalProviders(t *testing.T) {
	t.Run("未注册域返回Unregistered错误", func(t *testing.T) {
		_, err := Provider("no_such_domain")
		require.Error(t, err)
		var unregistered *ExternalProviderUnregisteredError
		require.True(t, errors.As(err, &unregistered))
		assert.Equal(t, "no_such_domain", unregistered.Domain)
		assert.Contains(t, err.Error(), "is not registered in this process")
	})
	t.Run("注册后可取用且重复注册以后者为准", func(t *testing.T) {
		Register("test_domain_x", "first")
		v, err := Provider("test_domain_x")
		require.NoError(t, err)
		assert.Equal(t, "first", v)
		Register("test_domain_x", "second") // 供测试覆盖
		v, err = Provider("test_domain_x")
		require.NoError(t, err)
		assert.Equal(t, "second", v)
	})
	t.Run("诊断列表已排序", func(t *testing.T) {
		domains := RegisteredDomains()
		require.NotEmpty(t, domains)
		for i := 1; i < len(domains); i++ {
			assert.Less(t, domains[i-1], domains[i])
		}
	})
}
