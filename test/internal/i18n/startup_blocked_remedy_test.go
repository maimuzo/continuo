// 起動直後に確認の画面で止まったときの文言が、issue のコメントに書く許可の文を持たないことの検査である
// （設計 3-11。issue #259）。
//
// **なぜ持たせないか。**この文言は、公開かどうかを見ずに issue のコメントとして投稿される。
// 以前は文言の中に「この issue のコメントに『その操作を許可します』と書いてください」を固定で持っており、
// 公開リポジトリの issue へもそのまま載っていた。**しかも何の確認だったかは continuo の側に残らないので、
// その案内が合っているかどうかも分からない。**
//
// **両方の言語を見る。**既定の言語は英語（`i18n.DefaultLang`）で、英語の文言にも同じ案内があった。
// プロセス全体の言語は切り替えず、`i18n.CatalogOf` から言語ごとに引く。
package i18n_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// startupGrantRecipes は、以前の起動直後の文言が持っていた、コメントに書く許可の文である。
// **日本語と英語のどちらの文言にも、どちらも入ってはならない。**
var startupGrantRecipes = []string{
	"その操作を許可します",
	"I allow that operation",
}

// 目的: 起動直後の文言が、コメントに書く許可の文を持たず、信頼の案内を持つことを、両方の言語で固定する。
//
// **許可の文が戻ると、公開リポジトリの issue へも投稿される。**
// **信頼の案内が消えると、よくある原因（フォルダが信頼登録されていない）の直し方が届かない。**
//
// 与える情報: 日本語と英語の `orchestrator.confirm_startup.blocked`。
// 成功条件: どちらにも許可の文が入っておらず、`continuo trust` が入っていること。
func TestStartupBlocked_許可の文を持たず信頼の案内を持つ(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		c, ok := i18n.CatalogOf(lang)
		if !ok {
			t.Fatalf("%v の資源を引けません", lang)
		}
		got := c.T(i18n.KeyOrchestratorConfirmStartupBlocked, "agent-1")
		for _, ng := range startupGrantRecipes {
			if strings.Contains(got, ng) {
				t.Errorf("%v: 起動直後の文言に %q が入っています。"+
					"この文言は公開かどうかを見ずに issue へ投稿されます:\n%s", lang, ng, got)
			}
		}
		if !strings.Contains(got, "continuo trust") {
			t.Errorf("%v: 起動直後の文言に `continuo trust` の案内がありません:\n%s", lang, got)
		}
		// **閉じた pane を見ろと案内しない**（設計 3-34b と同じ決まり）。continuo はこの文言を投稿したあとで
		// pane を閉じるので、人間が読むときには agent が消えている。
		// **原因を断定しない。**何が確認の画面を出したかは continuo の側に残らない。
		for _, ng := range []string{"herdr agent read", "許可されていないコマンド", "a command that is not allowed"} {
			if strings.Contains(got, ng) {
				t.Errorf("%v: 起動直後の文言に %q が入っています:\n%s", lang, ng, got)
			}
		}
	}
}
