// 権限で止まったときの【対処】の文面の検査である（設計 3-11。issue #259）。
//
// **公開・非公開で文面は変わらない。**分けていたのは「公開の場所へ『ここへ許可を書けば通る』と
// 書くと、それを読んだ第三者が同じ文を書ける」ためだったが、**そもそも誰が書いても届かない。**
// 判定役への要求から道具の結果は取り除かれ、issue のコメントは `gh` の出力として届く
// （公式の permission modes のページ。2026-09-18 に取得。同じ日に実測でも確かめた）。
//
// **公開・非公開の3通りで本文が変わらないことは、引き渡しの経路で見る**
// （handoff_remedy_paths_test.go）。**そちらが実際に投稿される本文を組み立てる。**
package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// commentGrantGuidance は、issue のコメントで許可を出す案内である。
// **どのモードの引き渡しにも、これが1文字も入ってはならない。**
const commentGrantGuidance = "コメントに「その操作を許可します」と書いてください"

// allowGuidance は、許可の一覧に足す案内である。**どのモードでも入る。**
const allowGuidance = "`claude.permissions.allow` に"

// restartGuidance は、設定を読み直させる案内である。
// **`claude.permissions` は走行中に読み直さない**（config.Reloadable に入っていない）。
// **これが落ちると、利用者は直したのに同じところでまた止まる。**
const restartGuidance = "continuo を再起動してください"

// thirdPartyGuidance は、公開リポジトリの第三者への注意である。**どのモードでも入る。**
// 守っているのは判定役ではなく、許可を広げる人間である。
const thirdPartyGuidance = "書いた人を確かめてください"

// 目的: auto の対処が、狭い規則を allow へ足させ、再起動まで案内することを固定する。
//
// **広い規則は auto に入るときに落とされる**（公式の permission modes のページ）。
// **「allow に足してください」だけだと、`Bash` のような広い規則を足して、また止まる。**
//
// 与える情報: permission_mode が auto。
// 成功条件: コメントで許可を出す案内が入らず、狭い規則と再起動が案内されること。
func TestBlockedHandoff_autoは狭い規則と再起動を案内する(t *testing.T) {
	got := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeAuto)

	if strings.Contains(got, commentGrantGuidance) {
		t.Errorf("コメントで許可を出す案内が入っている。判定役はそれを読まない:\n%s", got)
	}
	for _, want := range []string{
		allowGuidance,
		"狭い規則",
		restartGuidance,
		// **広い規則が落とされることを言う。**言わないと、`Bash` を足してまた止まる
		"落とされます",
		// **別の原因の但し書き。**agent teams で止まったときに、許可の一覧を疑い続けないため
		"権限の拒否とは限りません",
		thirdPartyGuidance,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("auto の対処に %q がありません:\n%s", want, got)
		}
	}
}

// 目的: dontAsk の対処が、allow を案内し、再起動まで書くことを固定する。
//
// **dontAsk は許可の一覧だけで決める。**だからコメントで許可を出す経路がそもそも無い。
// **対処の中身は変えていないが、再起動の案内はこちらにも要る。**
// 設定を読み直さないのはモードと関係が無いためである。
//
// 与える情報: permission_mode が dontAsk。
// 成功条件: allow と再起動を案内し、会話で許可を出す案内は入らないこと。
func TestBlockedHandoff_dontAskもallowと再起動を案内する(t *testing.T) {
	got := orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeDontAsk)

	if strings.Contains(got, commentGrantGuidance) {
		t.Errorf("dontAsk なのに、会話で許可を出す案内が入っている:\n%s", got)
	}
	// **dontAsk では狭く書かせない。**このモードでは、引数まで絞ると書き込み系の操作が拒否される（設計 3-11）。
	if strings.Contains(got, "狭い規則") {
		t.Errorf("dontAsk の対処が狭い規則を案内している:\n%s", got)
	}
	for _, want := range []string{allowGuidance, restartGuidance, thirdPartyGuidance} {
		if !strings.Contains(got, want) {
			t.Errorf("dontAsk の対処に %q がありません:\n%s", want, got)
		}
	}
}

// 目的: 2つのモードの文面が、見出しで見分けられることを固定する。
//
// **見出しはモード名から作る。**決め打ちにすると、受け付ける値が増えたときに
// 別のモードを auto と名乗ってしまう。
//
// 与える情報: 2つのモード。
// 成功条件: それぞれの文面が、自分のモード名だけを見出しに持つこと。
func TestBlockedHandoff_見出しはモード名から作る(t *testing.T) {
	for _, mode := range []string{config.ClaudePermissionModeAuto, config.ClaudePermissionModeDontAsk} {
		got := orchestrator.PermissionRemedyTextForTest(mode)
		if !strings.Contains(got, "【"+mode+" について】") {
			t.Errorf("%s の文面に、そのモード名の見出しがありません:\n%s", mode, got)
		}
		for _, other := range []string{config.ClaudePermissionModeAuto, config.ClaudePermissionModeDontAsk} {
			if other == mode {
				continue
			}
			if strings.Contains(got, "【"+other+" について】") {
				t.Errorf("%s の文面に、別のモード %s の見出しが入っています:\n%s", mode, other, got)
			}
		}
	}
}
