// 雛形が書き出す値と、Go の既定値が一致することの検査である（設計 3-11 / 3-64。issue #259）。
//
// **既存の検査はキーの集合しか比べていない**（design_template_test.go）。
// **だから雛形の値だけが古いまま出荷されても、どのテストも落ちない。**
// `continuo init` を叩いた人には雛形の値が届き、既定値は届かないので、
// **ずれると「新しい既定になったはずの人」だけが古い挙動のまま走る。**
package scaffold_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 雛形の権限まわりの3つの値が、DefaultConfig() と一致することを固定する。
//
// **比べるのは `config.Load` を通したあとの値である。**雛形の文字列を正規表現で探すと、
// 引用符やコメントの書き方を変えただけで落ちる。**読み込んだ結果で比べれば、
// `mode: "off"` と `mode: off` のどちらでも同じ値になることまで含めて確かめられる。**
//
// 与える情報: プレースホルダを埋めた雛形。
// 成功条件: permission_mode / permissions.deny / permissions.allow / tool_gate.mode が
// DefaultConfig() と一致すること。
func TestTemplate_権限まわりの値が既定値と一致する(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	body := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("雛形を読み込めません: %v", err)
	}
	want := config.DefaultConfig()

	got := loaded.Config.Claude
	if got.PermissionMode != want.Claude.PermissionMode {
		t.Errorf("雛形の permission_mode が既定値と違う: got %q, want %q",
			got.PermissionMode, want.Claude.PermissionMode)
	}
	if got.ToolGate.Mode != want.Claude.ToolGate.Mode {
		t.Errorf("雛形の tool_gate.mode が既定値と違う: got %q, want %q",
			got.ToolGate.Mode, want.Claude.ToolGate.Mode)
	}
	assertSameStrings(t, "permissions.allow", got.Permissions.Allow, want.Claude.Permissions.Allow)
	assertSameStrings(t, "permissions.deny", got.Permissions.Deny, want.Claude.Permissions.Deny)
}

// assertSameStrings は2つの文字列の並びが同じであることを確かめる。
//
// t: 呼び出し元のテスト。
// label: 食い違ったときに出す設定キーの名前。
// got: 雛形から読んだ値。
// want: 既定値。
func assertSameStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("雛形の %s の件数が既定値と違う: got %v, want %v", label, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("雛形の %s が既定値と違う: got %v, want %v", label, got, want)
			return
		}
	}
}
