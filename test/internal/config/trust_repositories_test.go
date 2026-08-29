// Package config_test のうち、このファイルは trust.repositories の検査を固定する。
//
// **ここに書かれた文字列は、`continuo trust` が `~/.claude.json` へ書き込む鍵の元になる**
// （設計 3-33）。形を検査せずに ghq と git へ渡すと、打ち間違いが「clone が無い」として
// 静かに握り潰され、人間は「登録したつもりなのに効かない」状態に置かれる。
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: trust.repositories に "owner/repo" として受け付けられない値を書くと、
// 起動が止まることを確認する。
// 与える情報: 形が違う値を1つずつ書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に "trust.repositories" と、
// 何番目の要素かが含まれること。
func TestLoad_trust_repositoriesの形が違うと落ちる(t *testing.T) {
	for _, bad := range []string{
		"continuo",           // owner が無い
		"octocat/",           // repo が空
		"/continuo",          // owner が空
		"octocat/con tinuo",  // 空白が混ざっている
		"octocat/../../etc",  // パスを遡ろうとしている
		"-octocat/continuo",  // owner がハイフンで始まっている
		"octocat/continuo/x", // スラッシュが2つある
	} {
		front := validFrontMatter + "trust:\n  repositories: [" + quote(bad) + "]\n"
		assertLoadFailsWith(t, front, "trust.repositories[0]")
	}
}

// 目的: 同じリポジトリを2回書くと起動が止まることを確認する。
//
// **この一覧は人間が手で削る前提のものである**（設計 3-33）。同じ行が2つあるのは
// 消し損ねか貼り付けの誤りであり、黙って読み飛ばすと「消したつもりの行」が残る。
//
// 与える情報: 同じ "owner/repo" を2回書いた front matter。
// 成功条件: エラー文に "trust.repositories[1]" と "重複" が含まれること。
func TestLoad_trust_repositoriesに同じものを2回書くと落ちる(t *testing.T) {
	front := validFrontMatter +
		"trust:\n  repositories: [\"octocat/hello-world\", \"octocat/hello-world\"]\n"
	path := writeWorkflow(t, front, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("重複しているのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "trust.repositories[1]") {
		t.Errorf("何番目が重複しているのかを名指ししていない: %v", err)
	}
	if !strings.Contains(err.Error(), "2回") {
		t.Errorf("重複であることを説明していない: %v", err)
	}
}

// 目的: 正しく書かれた trust.repositories が、書いた順のまま読み出せることを確認する。
//
// **並び順は人間が書いた順のままにする。**`continuo trust` の出力がこの順で並ぶので、
// 勝手に並べ替えると WORKFLOW.md と出力の対応が取れなくなる。
//
// 与える情報: 3件を書いた front matter。
// 成功条件: 書いた順のまま3件が読み出せること。
func TestLoad_trust_repositoriesは書いた順のまま読み出せる(t *testing.T) {
	front := validFrontMatter +
		"trust:\n  repositories:\n    - \"octocat/hello-world\"\n    - \"acme/anvil\"\n    - \"acme/tool_kit.v2\"\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("正しい設定なのに読み込めなかった: %v", err)
	}
	want := []string{"octocat/hello-world", "acme/anvil", "acme/tool_kit.v2"}
	got := loaded.Config.Trust.Repositories
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("読み出した値が想定と違う: got %v, want %v", got, want)
	}
}

// 目的: trust.repositories を書かなくても起動できることを確認する。
//
// **既定は空である。**何も書かなければ `continuo trust` は何も登録しない（設計 3-33）。
//
// 与える情報: trust ブロックを書いていない front matter。
// 成功条件: 読み込みが成功し、trust.repositories が空であること。
func TestLoad_trust_repositoriesを書かなくても起動できる(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("読み込めなかった: %v", err)
	}
	if len(loaded.Config.Trust.Repositories) != 0 {
		t.Errorf("書いていないのに値が入っている: %v", loaded.Config.Trust.Repositories)
	}
}

// quote は YAML のフロー形式に埋め込める形へ、値を二重引用符で囲む。
//
// s: 囲む値。
// 戻り値: 二重引用符で囲んだ文字列。
func quote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
