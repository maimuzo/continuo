// 権限で止まったときの【対処】の文面を、日本語と英語の両方で検査する（設計 3-11。issue #259）。
//
// **なぜ両方か。**`i18n.DefaultLang` は `LangEN` である。**`language: ja` を書いていない利用者には英語が届く。**
// ところが orchestrator の検査は package ごと日本語に固定されている
// （test/internal/orchestrator/lang_test.go の testlang.Run）。
// **そこだけでは、英語の文面へ許可の文が入っても、公開の断りが消えても、1本も落ちない。**
package i18n_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// remedyWording は、1つの言語で【対処】の文面が持つべき／持ってはならない言い回しである。
type remedyWording struct {
	grant         string // 許可の表明として読まれうる文そのもの。どの分岐にも入ってはならない
	ownWords      string // 非公開でだけ出す「あなたの言葉で許可を出す」案内
	publicRefusal string // 公開でだけ出す「コメントで許可を出す方法は案内しない」断り
}

// remedyWordings は、言語ごとの言い回しである。
//
// **grant が保証するのは「前の文面へ戻っていないこと」だけである。**
// 判定役は意味を読むので、言い換えた許可の表明はこの一致では見つからない。
var remedyWordings = map[i18n.Lang]remedyWording{
	i18n.LangJA: {
		grant:         "その操作を許可します",
		ownWords:      "この issue のコメントで、あなたの言葉で許可を出してください",
		publicRefusal: "このリポジトリは公開なので、issue のコメントで許可を出す方法は案内しません",
	},
	i18n.LangEN: {
		grant:         "I allow that operation",
		ownWords:      "grant it in your own words in a comment on this issue",
		publicRefusal: "This repository is public, so we do not tell you how to grant permission in a comment",
	},
}

// remedyKeys は、検査する3つの分岐のキーである。
var remedyKeys = map[string]i18n.Key{
	"dontAsk":  i18n.KeyOrchestratorPermissionRemedyDontAsk,
	"auto/非公開": i18n.KeyOrchestratorPermissionRemedyAutoPrivate,
	"auto/公開":  i18n.KeyOrchestratorPermissionRemedyAutoPublic,
}

// 目的: どの言語・どの分岐にも、許可の文そのものが入らないことを固定する。
//
// **入っていると、continuo 自身のコメントが判定役の読む会話へ許可の表明を載せる**（自己注入）。
// **両方の言語の grant を、両方の言語の文面で探す。**英語の文面へ日本語の許可の文が混ざる事故も拾う。
//
// 与える情報: 日本語と英語それぞれの、3つの分岐の文面。
// 成功条件: 6通りのどれにも、日本語と英語の許可の文が入っていないこと。
func TestPermissionRemedy_どの言語どの分岐にも許可の文が入らない(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)
		for branch, key := range remedyKeys {
			got := i18n.T(key, "auto", "auto")
			for _, w := range remedyWordings {
				if strings.Contains(got, w.grant) {
					t.Errorf("%v / %s: 文面に許可の文 %q が入っています。"+
						"continuo 自身のコメントが、判定役の読む会話へ許可の表明を載せます:\n%s",
						lang, branch, w.grant, got)
				}
			}
		}
	}
}

// 目的: 非公開の分岐だけが「あなたの言葉で許可を出す」案内を持ち、公開の分岐だけが断りを持つことを、両方の言語で固定する。
//
// **英語で公開の断りが消えると、英語の利用者の公開リポジトリへ、コメントで許可を出せることが案内される。**
//
// 与える情報: 日本語と英語それぞれの、3つの分岐の文面。
// 成功条件: 案内は非公開にだけ、断りは公開にだけ入っていること。
func TestPermissionRemedy_案内は非公開にだけ断りは公開にだけ出る(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)
		w := remedyWordings[lang]
		for branch, key := range remedyKeys {
			got := i18n.T(key, "auto", "auto")
			wantGuidance := branch == "auto/非公開"
			wantRefusal := branch == "auto/公開"
			if strings.Contains(got, w.ownWords) != wantGuidance {
				t.Errorf("%v / %s: 「あなたの言葉で許可を出す」案内の有無が違います（期待 %v）:\n%s",
					lang, branch, wantGuidance, got)
			}
			if strings.Contains(got, w.publicRefusal) != wantRefusal {
				t.Errorf("%v / %s: 公開の断りの有無が違います（期待 %v）:\n%s",
					lang, branch, wantRefusal, got)
			}
		}
	}
}
