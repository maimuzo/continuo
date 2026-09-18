package githubapp_test

import (
	"io/fs"
	"os"
	"testing"
)

// readFile はファイルを文字列で読む。
func readFile(t *testing.T, path string) (string, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めない: %v", path, err)
	}
	return string(data), nil
}

// chmod は権限を変える（テストの意図を名前で示すためだけの薄い包み）。
func chmod(path string, perm fs.FileMode) error { return os.Chmod(path, perm) }
