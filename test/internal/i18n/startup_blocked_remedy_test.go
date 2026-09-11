// 起動直後に確認の画面で止まったときの文言が、【対処】を自分で持たないことの検査である
// （設計 3-11。issue #259）。
//
// **なぜ持たせないか。**この文面は issue のコメントとして投稿される。
// **公開リポジトリでは、コメントで許可を出す案内を書いてはならない。**
// `auto` の判定役は会話の流れを読み、その会話には issue のコメントが載るので、
// **「ここへ許可を書けば通る」を公開の場所へ書くと、それを読んだ第三者が同じ文を書ける。**
//
// **turn.go と restore.go は既に `permissionRemedyText` を通している。**
// **この経路だけが固定の文言を持っており、公開かどうかを1度も見ていなかった。**
package i18n_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// grantRecipeJA は、issue のコメントで許可を出す手順である（日本語）。
// **固定の文言に、これが1文字も入ってはならない。**
const grantRecipeJA = "その操作を許可します"

// grantRecipeEN は、同じ手順の英語である。
const grantRecipeEN = "I allow that operation"

// 目的: 起動直後に止まったときの文言が、許可の出し方を自分で書かないことを固定する。
//
// **入っていると、公開リポジトリの issue へ攻略法がそのまま投稿される。**
// 断られた第三者は、continuo が書いた1文をコピーするだけで済む。
//
// 与える情報: 両方の言語の `orchestrator.confirm_startup.blocked` の文言。
// 成功条件: 許可の出し方が1文字も入っていないこと。
func TestStartupBlocked_許可の出し方を文言に埋めない(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		got := i18n.T(i18n.KeyOrchestratorConfirmStartupBlocked, "agent-1", "agent-1", "")
		for _, ng := range []string{grantRecipeJA, grantRecipeEN} {
			if strings.Contains(got, ng) {
				t.Errorf("%v: 起動直後の文言に %q が入っています。"+
					"公開リポジトリの issue へ投稿されると、断られた第三者がそのままコピーできます。"+
					"【対処】は permissionRemedyText から渡してください", lang, ng)
			}
		}
	}
	i18n.Use(i18n.LangJA)
}

// 目的: 3つ目の引数（【対処】）が、文言の中で使われていることを固定する。
//
// **使われていないと、呼び出し側が渡した対処が黙って捨てられる。**
// 人間は「何をすればよいか」が書かれていない引き渡しを受け取る。
//
// 与える情報: 3つ目の引数に目印の文字列を渡したときの出力。
// 成功条件: その目印が出力に入っていること。
func TestStartupBlocked_渡した対処が文言に入る(t *testing.T) {
	const marker = "<<ここに対処が入る>>"

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		got := i18n.T(i18n.KeyOrchestratorConfirmStartupBlocked, "agent-1", "agent-1", marker)
		if !strings.Contains(got, marker) {
			t.Errorf("%v: 3つ目の引数が文言に入っていません。"+
				"呼び出し側が組み立てた【対処】が捨てられ、人間は次に何をすればよいか分かりません。"+
				"出力: %q", lang, got)
		}
	}
	i18n.Use(i18n.LangJA)
}
