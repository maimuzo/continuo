package tracker_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 目的: internal/tracker のソースコードのどこにも、Status の選択肢そのものを書き換える
// mutation（`updateProjectV2Field`）を呼ぶコードが無いことを確認する。
//
// **これは CLAUDE.md の絶対制約である。**選択肢の指定は全件置き換えとして扱われ、
// GitHub 側が全部の選択肢に新しい ID を採番し直す。その結果、item が参照していた古い ID が
// 無効になり、project #3 の104件中100件に設定済みの Status が全部 null に落ちる
// （設計 4-1。2026-08-10 に使い捨ての project で実測済み）。
//
// **`updateProjectV2ItemFieldValue`（1件の Status 値だけを書く、正しい mutation）は
// 対象外にする。**"updateProjectV2Field" という文字列は
// "updateProjectV2ItemFieldValue" の部分文字列ではない（"Item" が挟まっているため）ので、
// 単純な部分文字列検索で両者を区別できる。
//
// 対象パス: internal/tracker/*.go
// （このテストファイル自身を含む test/ 配下は対象にしない。コメントで言及することはあるため）。
// 検索パターン: "updateProjectV2Field"（部分文字列。大文字小文字を区別する）。
// 与える情報: internal/tracker ディレクトリ内の .go ファイルすべて。
// 成功条件: いずれのファイルにも "updateProjectV2Field" という部分文字列が
// 1バイトも出現しないこと。
func TestSource_updateProjectV2Fieldを呼ぶコードがどこにも無い(t *testing.T) {
	const forbidden = "updateProjectV2Field"
	const allowed = "updateProjectV2ItemFieldValue"

	dir := filepath.Join("..", "..", "..", "internal", "tracker")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("internal/tracker を読めません（対象パス: %s）: %v", dir, err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s を読めません: %v", path, err)
		}
		checked++

		text := string(content)
		idx := 0
		for {
			pos := strings.Index(text[idx:], forbidden)
			if pos < 0 {
				break
			}
			absPos := idx + pos
			// "updateProjectV2ItemFieldValue" にたまたま含まれているだけではないことを
			// 明示的に確かめる（本来ヒットしないはずだが、将来コードが変わっても
			// 誤検出しないようにするための二重チェック）。
			surrounding := text[max(0, absPos-4):min(len(text), absPos+len(allowed))]
			if strings.Contains(surrounding, allowed) {
				idx = absPos + len(forbidden)
				continue
			}
			t.Fatalf(
				"%s に禁止された mutation 名 %q が出現しています（Status の選択肢を全件置き換える"+
					"mutation を呼ぶコードを追加してはならない）: 周辺=%q",
				path, forbidden, surrounding,
			)
		}
	}

	if checked == 0 {
		t.Fatalf("internal/tracker 配下に .go ファイルが1つも見つかりませんでした（対象パス: %s）", dir)
	}
}
