// `continuo setup` が、既にある WORKFLOW.md から owner とカンバンの番号を拾えるかの検査である。
//
// **これが無かったために、`continuo init --project 9` で埋めたのに
// `continuo setup` でもう一度 `--project 9` を指定させられていた**（2026-08-21、設計 6-2）。
package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// writeWorkflow は検査用の WORKFLOW.md を1つ置く。
//
// t: 呼び出し元のテスト。
// body: front matter の中身（`---` で挟む前の部分）。
// 戻り値: 置いたディレクトリ。
func writeWorkflow(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	// **`CheckUpdatable` は Status のキーが全部あることを先に見る。**
	// 尋ねる前にキーの有無を確かめる設計なので、雛形から作って必要な箇所だけ差し替える。
	tmpl := scaffold.Template()
	out := tmpl
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		key := strings.SplitN(strings.TrimSpace(line), ":", 2)[0]
		replaced := false
		var b strings.Builder
		for _, l := range strings.Split(out, "\n") {
			if !replaced && strings.HasPrefix(strings.TrimSpace(l), key+":") {
				b.WriteString(line + "\n")
				replaced = true
				continue
			}
			b.WriteString(l + "\n")
		}
		out = strings.TrimSuffix(b.String(), "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return dir
}

// TestCheckUpdatable_既に書かれている owner とカンバンの番号を返す は、setup が読む値を確かめる。
//
// 目的: front matter に書かれた `owner` と `project_number` を返すこと。
// 与える情報: 両方が埋まった WORKFLOW.md。
// 成功条件: Result.Owner と Result.ProjectNumber にその値が入ること。
func TestCheckUpdatable_既に書かれているownerとカンバンの番号を返す(t *testing.T) {
	dir := writeWorkflow(t, "    owner: octocat\n    project_number: 9")

	got, err := scaffold.CheckUpdatable(dir)
	if err != nil {
		t.Fatalf("CheckUpdatable が失敗した: %v", err)
	}
	if got.Owner != "octocat" {
		t.Errorf("owner を読めていない: %q", got.Owner)
	}
	if got.ProjectNumber != 9 {
		t.Errorf("カンバンの番号を読めていない: %d", got.ProjectNumber)
	}
}

// TestCheckUpdatable_プレースホルダのままなら空で返す は、雛形をそのまま置いた場合を確かめる。
//
// **`continuo init` が値を埋められなかったとき、雛形のプレースホルダが残る。**
// それを「書かれた値」として扱うと、`continuo setup` が存在しないカンバンを読みに行く。
//
// 目的: プレースホルダを値として採らないこと。
// 与える情報: `continuo init` が値を埋める前の雛形そのもの。
// 成功条件: Owner が空文字、ProjectNumber が 0 であること。
func TestCheckUpdatable_プレースホルダのままなら空で返す(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(scaffold.Template()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	got, err := scaffold.CheckUpdatable(dir)
	if err != nil {
		t.Fatalf("CheckUpdatable が失敗した: %v", err)
	}
	if got.Owner != "" {
		t.Errorf("プレースホルダを owner として採っている: %q", got.Owner)
	}
	if got.ProjectNumber != 0 {
		t.Errorf("プレースホルダをカンバンの番号として採っている: %d", got.ProjectNumber)
	}
}

// TestCheckUpdatable_ownerの形が不正なら採らない は、YAML を壊す値を弾くことを確かめる。
//
// **ここで採ってしまうと、引用符や改行が混ざった値をそのまま GitHub へ問い合わせに行く。**
//
// 目的: GitHub のアカウント名として成り立たない値を採らないこと。
// 与える情報: 空白・スラッシュ・末尾ハイフンを含む owner。
// 成功条件: すべて空文字で返ること。
func TestCheckUpdatable_ownerの形が不正なら採らない(t *testing.T) {
	for _, owner := range []string{"has_underscore", "a/b", "trailing-", strings.Repeat("x", 40)} {
		t.Run(owner, func(t *testing.T) {
			dir := writeWorkflow(t, "    owner: "+owner)
			got, err := scaffold.CheckUpdatable(dir)
			if err != nil {
				t.Fatalf("CheckUpdatable が失敗した: %v", err)
			}
			if got.Owner != "" {
				t.Errorf("owner=%q を採ってしまっている: %q", owner, got.Owner)
			}
		})
	}
}

// TestValidOwner_GitHubの規則に合っているか は、アカウント名の検査を確かめる。
//
// **GitHub の user / organization 名は英数字とハイフンだけで、39文字以内である。**
// **ハイフンで始めることも終わることもできない。**
//
// 目的: 受け付ける形と弾く形を言い分けること。
// 与える情報: 通るべき名前と、弾くべき名前。
// 成功条件: それぞれが期待どおりに判定されること。
func TestValidOwner_GitHubの規則に合っているか(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"a", true},
		{"octocat", true},
		{"oct-cat", true},
		{"a--b", true},
		{strings.Repeat("x", 39), true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"has space", false},
		{"has_underscore", false},
		{"a/b", false},
		{strings.Repeat("x", 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scaffold.ValidOwner(tc.name); got != tc.want {
				t.Errorf("ValidOwner(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestCheckUpdatable_WORKFLOWmdが無ければエラーを返す は、対象が無い場合を確かめる。
//
// 目的: 書き換える WORKFLOW.md が無いとき、パスつきのエラーを返すこと。
// 与える情報: 空のディレクトリ。
// 成功条件: エラーが返り、Result.Path にどこを見たかが入っていること。
func TestCheckUpdatable_WORKFLOWmdが無ければエラーを返す(t *testing.T) {
	got, err := scaffold.CheckUpdatable(t.TempDir())
	if err == nil {
		t.Fatal("WORKFLOW.md が無いのにエラーにならなかった")
	}
	if got.Path == "" {
		t.Error("どのパスを見たかを返していない")
	}
}
