// **利用者へ配られる文言に、実在の GitHub アカウント名が入ることを禁じる。**
//
// **実際に配られた**（issue #81）。`continuo init` が書き出す `WORKFLOW.md` の雛形に
//
//	owner: __FILL_ME__   # ここを埋めること。例: https://github.com/<作者のアカウント名> なら <作者のアカウント名>
//
// と書いてあり、**利用者が自分の手元に作る設定ファイルへ、作者のアカウント名が残った。**
// 同じものが穴埋めの案内・エラーの文言・GoDoc の例にもあった。
//
// 例に使う名前は、GitHub が説明用に用意している `octocat` と `hello-world` にそろえる。
//
// **禁じる名前をこのファイルに書かない。**`go.mod` の module のパスから引く。
// 書くと、伏せたはずの名前がテストの側に残る。
//
// 人が気をつけるだけでは止まらないので、機械で弾く。
package testdesign_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shippedDirs は、利用者の手元へ届く文言を持つディレクトリである。
//
// **`internal` と `cmd` だけを見る。**README・LICENSE・SECURITY.md・install.sh は
// 本物のアカウント名でなければ壊れる（配布 URL と報告先である）ので、対象にしない。
var shippedDirs = []string{"internal", "cmd"}

// TestDesign_配る文言に実在のアカウント名が入っていない は、issue #81 の形を機械で弾く。
//
// 目的: 実行ファイルに焼き込まれる文言に、`go.mod` の owner が現れないこと。
// 与える情報: `internal/` と `cmd/` の全 `.go`。
// 成功条件: 1件も見つからないこと。**走査したファイルが0件でないこと**も確かめる。
func TestDesign_配る文言に実在のアカウント名が入っていない(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	modulePath := modulePathOf(t, root)
	owner := ownerOf(t, modulePath)

	checked := 0
	for _, dir := range shippedDirs {
		target := filepath.Join(root, dir)
		err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			checked++

			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(body), "\n") {
				// **module のパスは取り除いてから見る。**import と、自分のパッケージを
				// 指す GoDoc の参照は、owner を含んでいて当たり前である。
				stripped := strings.ReplaceAll(line, modulePath, "")
				if !strings.Contains(strings.ToLower(stripped), strings.ToLower(owner)) {
					continue
				}
				t.Errorf("%s:%d に実在のアカウント名が入っています。\n  %s\n"+
					"  **ここに書いたものは、利用者の手元の設定ファイルや画面にそのまま出ます**（issue #81）。\n"+
					"  例には octocat / hello-world を使ってください。",
					path, i+1, strings.TrimSpace(line))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s を走査できません: %v", target, err)
		}
	}
	if checked == 0 {
		t.Fatalf("走査したファイルが0件です（対象: %v）。パスを確かめてください", shippedDirs)
	}
	t.Logf("走査したファイル: %d 件", checked)
}

// modulePathOf は go.mod の module のパスを返す。
//
// root: リポジトリの root へのパス。
// 戻り値: `github.com/<owner>/<repo>` の形の module のパス。
func modulePathOf(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "go.mod")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("%s に module の行がありません", path)
	return ""
}

// ownerOf は module のパスから owner を取り出す。
//
// modulePath: `github.com/<owner>/<repo>` の形の module のパス。
// 戻り値: 真ん中の owner。
func ownerOf(t *testing.T, modulePath string) string {
	t.Helper()
	parts := strings.Split(modulePath, "/")
	if len(parts) < 3 {
		t.Fatalf("module のパスから owner を取り出せません: %q", modulePath)
	}
	return parts[1]
}
