// 権限で止まったときの【対処】の文面が、リポジトリの公開・非公開で変わることの検査である
// （設計 3-11。issue #259）。
//
// **なぜ分けるか。**この文面は issue のコメントとして投稿される。
// `auto` の判定役は会話の流れを読み、その会話には issue のコメントが載る。
// **だから「ここへ許可を書けば通る」を公開の場所へ書くと、それを読んだ第三者が同じ文を書ける。**
// **判定役が書いた人の立場を見るかどうかは測っていない**ので、通らない前提を置かない。
package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// commentGrantGuidance は、issue のコメントで許可を出す案内である。
// **公開リポジトリの引き渡しには、これが1文字も入ってはならない。**
const commentGrantGuidance = "コメントに「その操作を許可します」と書いてください"

// 目的: 非公開リポジトリでは、コメントで許可を出す案内を出すことを固定する。
//
// **非公開なら、その issue へ書けるのは招かれた人だけである。**
// `auto` でいちばん効く対処なので、そこでは案内する。
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
// **これが守りの本体である。**案内を出すと、continuo 自身が公開の場所へ
// 「ここへ許可を書けば通る」と書き込むことになる。
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
