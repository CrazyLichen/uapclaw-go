package web

import (
	"errors"
	"io/fs"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSelectFile 替换原生选择器并记录入参，返回预设结果。
func stubSelectFile(t *testing.T, selected string, err error) *struct {
	dir   string
	title string
} {
	t.Helper()
	got := &struct {
		dir   string
		title string
	}{}
	original := selectFileNativeFn
	t.Cleanup(func() { selectFileNativeFn = original })
	selectFileNativeFn = func(initialDir, title string) (string, error) {
		got.dir = initialDir
		got.title = title
		return selected, err
	}
	return got
}

// TestPathSelectFile_注册 方法已注册。
func TestPathSelectFile_注册(t *testing.T) {
	d := RegisterWebHandlers(nil)
	_, ok := d.methodHandlers["path.select_file"]
	assert.True(t, ok)
}

// TestPathSelectFile_参数校验 三个可选键类型不符各自 BAD_REQUEST。
func TestPathSelectFile_参数校验(t *testing.T) {
	wc := newTestChannel()

	cases := []struct {
		params map[string]any
		msg    string
	}{
		{map[string]any{"initial_dir": 123}, "initial_dir must be string"},
		{map[string]any{"initial_path": []any{"x"}}, "initial_path must be string"},
		{map[string]any{"title": 1}, "title must be string"},
	}
	for _, tc := range cases {
		_, err := callHandler(pathSelectFile(wc), tc.params, "")
		require.Error(t, err)
		assert.Equal(t, WsErrBadRequest, errorCode(err))
		assert.Equal(t, tc.msg, err.Error())
	}
}

// TestPathSelectFile_选中文件 回 {path, cancelled:false}，并把起始目录/标题透传给选择器。
func TestPathSelectFile_选中文件(t *testing.T) {
	got := stubSelectFile(t, "/tmp/picked.txt", nil)

	payload, err := callHandler(pathSelectFile(newTestChannel()), map[string]any{
		"initial_path": "/tmp/dir/old.txt",
		"title":        "选个文件",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/picked.txt", payload["path"])
	assert.Equal(t, false, payload["cancelled"])
	assert.Equal(t, "/tmp/dir", got.dir, "initial_path 时用其父目录作起始目录")
	assert.Equal(t, "选个文件", got.title)
}

// TestPathSelectFile_取消 选择器返回空串 → {path:null, cancelled:true}。
func TestPathSelectFile_取消(t *testing.T) {
	stubSelectFile(t, "", nil)

	payload, err := callHandler(pathSelectFile(newTestChannel()), map[string]any{"initial_dir": " /tmp "}, "")
	require.NoError(t, err)
	assert.Nil(t, payload["path"])
	assert.Equal(t, true, payload["cancelled"])
}

// TestPathSelectFile_选择器不可用 回 UNSUPPORTED 并透传错误文案。
func TestPathSelectFile_选择器不可用(t *testing.T) {
	stubSelectFile(t, "", errors.New("native file picker is not available (zenity/kdialog not found)"))

	_, err := callHandler(pathSelectFile(newTestChannel()), map[string]any{}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrUnsupported, errorCode(err))
	assert.Contains(t, err.Error(), "not available")
}

// TestPathSelectFile_params为空 params 为 nil 时按空对象处理（不报参数错）。
func TestPathSelectFile_params为空(t *testing.T) {
	got := stubSelectFile(t, "", nil)

	payload, err := callHandler(pathSelectFile(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Equal(t, true, payload["cancelled"])
	assert.Equal(t, "", got.dir)
}

// TestEscapeAppleScript 双引号与反斜杠被转义。
func TestEscapeAppleScript(t *testing.T) {
	assert.Equal(t, `a\"b`, escapeAppleScript(`a"b`))
}

// TestRunPickerCommand_成功与取消 有输出返回输出；非 0 退出且无输出视为取消。
func TestRunPickerCommand_成功与取消(t *testing.T) {
	out, err := runPickerCommand("echo", "/tmp/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a.txt", out)

	out, err = runPickerCommand("false")
	require.NoError(t, err)
	assert.Equal(t, "", out, "非 0 退出无输出按取消处理")

	_, err = runPickerCommand("this-command-does-not-exist-xyz")
	require.Error(t, err, "命令不存在应报错（供上层回退到下一个对话框）")
}

// TestIsCommandMissing 命令不存在为真，其他错误为假。
func TestIsCommandMissing(t *testing.T) {
	assert.True(t, isCommandMissing(exec.ErrNotFound))
	assert.True(t, isCommandMissing(fs.ErrNotExist))
	assert.False(t, isCommandMissing(errors.New("boom")))
}

// TestPickerInitialPath 空目录回空串（由对话框决定），非空原样返回。
func TestPickerInitialPath(t *testing.T) {
	assert.Equal(t, "", pickerInitialPath(""))
	assert.Equal(t, "/tmp", pickerInitialPath("/tmp"))
}
