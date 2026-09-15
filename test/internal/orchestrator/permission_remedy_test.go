// 権限で止まったときの【対処】の文面が、リポジトリの公開・非公開で変わることと、
// **どの分岐でも continuo 自身が許可の文そのものを書かないこと**の検査である
// （設計 3-11。issue #259）。
//
// **なぜ書かないか。**この文面は issue のコメントとして投稿される。
// `auto` の判定役は会話の流れを読み、その会話には issue のコメントが載る。
// **つまり continuo が「その操作を許可します」と書くと、その1文が判定役の読む会話に載る。**
// **第三者を1人も必要とせずに、continuo 自身のコメントが許可の表明として読まれうる。**
// **判定役が書いた人の立場を見るかどうかは測っていない**ので、通らない前提を置かない。
//
// **公開かどうかの分岐は、これとは別の守りである。**公開リポジトリでは、
// コメントで許可を出せること自体を案内しない。**読んだ第三者が同じことをできるためである。**
package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// grantSentence は、判定役が許可の表明として読みうる文そのものである。
// **どの分岐の文面にも、1文字も入ってはならない。**
//
// **この検査が保証するのは「前の文面へ戻っていないこと」だけである。**
// 判定役は文字列ではなく意味を読むので、**言い換えた許可の表明は、この一致では見つからない。**
// 文面を直すときは、許可の表明と読める言い回し（「〜を許してよい」「〜を許可する」など）を
// 人間の目で確かめること。**文字列の一致は、意味の性質を証明しない。**
const grantSentence = "その操作を許可します"

// commentGrantGuidance は、issue のコメントで許可を出せることの案内である。
// **公開リポジトリの引き渡しには、これが1文字も入ってはならない。**
//
// **handoff_remedy_paths_test.go もこの定数を使う。**同じ package の中で同じ文字列を
// 2つ持つと、片方だけ文面に合わせて直したときに、もう片方が「存在しない文字列が入っていない」を
// 検査し続け、**必ず通る検査になる。**
const commentGrantGuidance = "この issue のコメントで、あなたの言葉で許可を出してください"

// publicRefusal は、公開リポジトリでだけ出す断りである。
const publicRefusal = "このリポジトリは公開なので、issue のコメントで許可を出す方法は案内しません"

// 目的: どのモード・どの公開状態でも、許可の文そのものを書かないことを固定する。
//
// **これが守りの本体である。**continuo が自分のコメントへその1文を書くと、
// **人間が1文字も許可を書いていないのに、判定役の読む会話に許可の表明が載る。**
// **通ったかどうかは continuo の側に記録が残らない**（turn.go の
// `blockedHandoffReason` の説明にあるとおり、何が確認の画面を出したかは残らない）。
//
// 与える情報: permission_mode が auto と dontAsk、RepoIsPrivate が true / false / nil。
// 成功条件: 6通りのどれにも `その操作を許可します` が入らないこと。
func TestBlockedHandoff_どの分岐でも許可の文そのものを書かない(t *testing.T) {
	private, public := true, false
	for _, mode := range []string{config.ClaudePermissionModeAuto, config.ClaudePermissionModeDontAsk} {
		for name, repoIsPrivate := range map[string]*bool{
			"非公開":    &private,
			"公開":     &public,
			"取れなかった": nil,
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				got := orchestrator.PermissionRemedyTextForTest(mode, repoIsPrivate)
				if strings.Contains(got, grantSentence) {
					t.Errorf("continuo 自身が許可の文を書いている（%s / %s）:\n%s", mode, name, got)
				}
			})
		}
	}
}

// 目的: 非公開リポジトリでは、コメントで許可を出せることを案内するのを固定する。
//
// **非公開なら、その issue へ書けるのは招かれた人だけである。**
// `auto` でいちばん効く対処なので、そこでは案内する。
// **ただし案内するだけで、許可の文そのものは人間に書かせる**（上の検査）。
//
// 与える情報: permission_mode が auto、RepoIsPrivate が true。
// 成功条件: コメントで許可を出す案内が入っていること。
func TestBlockedHandoff_非公開なら会話で許可を出す案内を書く(t *testing.T) {
	private := true
	got := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeAuto, &private)
	if !strings.Contains(got, commentGrantGuidance) {
		t.Fatalf("非公開なのに、会話で許可を出す案内が無い:\n%s", got)
	}
}

// 目的: 公開リポジトリでは、その案内を出さないことを固定する。
//
// **案内を出すと、continuo 自身が公開の場所へ「ここへ許可を書けば通る」と書き込むことになる。**
//
// 与える情報: permission_mode が auto、RepoIsPrivate が false と nil の2通り。
// 成功条件: どちらでも案内が入らず、代わりに permissions.allow を案内すること。
func TestBlockedHandoff_公開なら会話で許可を出す案内を書かない(t *testing.T) {
	public := false
	for name, repoIsPrivate := range map[string]*bool{
		"公開":     &public,
		"取れなかった": nil,
	} {
		t.Run(name, func(t *testing.T) {
			got := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeAuto, repoIsPrivate)
			if strings.Contains(got, commentGrantGuidance) {
				t.Errorf("公開なのに、会話で許可を出す案内が入っている:\n%s", got)
			}
			if !strings.Contains(got, "`claude.permissions.allow` に足してください") {
				t.Errorf("代わりの対処（allow に足す）が案内されていない:\n%s", got)
			}
		})
	}
}

// 目的: dontAsk では、公開・非公開に関わらず allow を案内することを固定する。
//
// **dontAsk の判定は会話を読まない。**だからコメントで許可を出す経路がそもそも無く、
// 公開かどうかで文面を変える理由も無い。
//
// 与える情報: permission_mode が dontAsk、RepoIsPrivate が true と false。
// 成功条件: どちらでも allow を案内し、会話で許可を出す案内は入らないこと。
func TestBlockedHandoff_dontAskは公開かどうかで変わらない(t *testing.T) {
	for name, v := range map[string]bool{"非公開": true, "公開": false} {
		t.Run(name, func(t *testing.T) {
			repoIsPrivate := v
			got := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeDontAsk, &repoIsPrivate)
			if strings.Contains(got, commentGrantGuidance) {
				t.Errorf("dontAsk なのに、会話で許可を出す案内が入っている:\n%s", got)
			}
			if !strings.Contains(got, "`claude.permissions.allow` に足してください") {
				t.Errorf("allow を案内していない:\n%s", got)
			}
		})
	}
}
