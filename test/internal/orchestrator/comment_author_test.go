// **エージェントの印を投稿者と併せて見ることの検査である**（設計 3-65）。
//
// **印（`tracker.comments.marker` / `self_marker`）は本文の先頭に置くただの文字列であり、
// issue にコメントできる人なら誰でも書ける。**投稿者を照合しないと、外部の第三者の
// コメントを「エージェントが成果を書いた」と読み違える。
//
// **投稿者で絞る処理そのものは internal/tracker の FetchComments にある**
// （test/internal/tracker/comments_test.go）。ここで見るのは
// **orchestrator が「gh の持ち主」を取り、それを FetchComments へ渡していること**と、
// **取れなくても run が止まらないこと**の2点である。
package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// runUntilCommentCheck は「エージェントがコメントを書かないまま turn を終える run」を
// 1件通し、コメントの確認（hasRunComment）まで到達させる。
//
// **コメントの確認は run の終わりにしか走らない**（設計 3-25）。そこまで通さないと、
// FetchComments に何が渡ったかを見られない。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
func runUntilCommentCheck(t *testing.T, fx *fixture) {
	t.Helper()
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			// **エージェントはコメントを書かない。**確認の経路を必ず通す。
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	fx.AllowLog("コメントを書かせる", "セッションを復元できません", "Status を落とせません")

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)
}

// 目的: continuo が「自分が使う gh の持ち主」を取り、コメントの取得へ渡していることを
// 確認する（設計 3-65）。
//
// **渡していなければ、判定は印だけに戻る。**第三者が同じ印でコメントを書くと、
// 成果が何も残っていない run が「書いた」と見なされる。
//
// 与える情報: 持ち主として octocat を返す関数と、エージェントがコメントを書かない run。
// 成功条件: FetchComments が受け取った持ち主が octocat であること。
func TestComment_持ち主をコメントの取得へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	runUntilCommentCheck(t, fx)

	if got := fx.Tracker.CommentsSelfLogin(); got != testGHLogin {
		t.Fatalf("コメントの取得へ渡した持ち主が違う: got %q, want %q（印だけで判定している）",
			got, testGHLogin)
	}
}

// 目的: 「gh の持ち主」を取れなくても、continuo が止まらず印だけの判定へ落ちることを
// 確認する（設計 3-65）。
//
// **取れないことで起動や run を止めてはならない。**`gh api` に一時的に届かないだけで
// continuo が動かなくなる。
//
// 与える情報: 必ず失敗する取得関数と、エージェントがコメントを書かない run。
// 成功条件: run が最後まで進み（引き渡しの通知が出る）、FetchComments へ渡った持ち主が
// 空文字であること。
func TestComment_持ち主を取れなくても止まらない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		GHLogin: func(context.Context) (string, error) {
			return "", errors.New("gh api user に届きません")
		},
	})
	fx.AllowLog("gh の持ち主を取れませんでした")

	runUntilCommentCheck(t, fx)

	if got := fx.Tracker.CommentsSelfLogin(); got != "" {
		t.Fatalf("持ち主を取れていないのに %q を渡している", got)
	}
	handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(handoffs) != 1 {
		t.Fatalf("持ち主を取れないことで run が止まっている: 引き渡しの通知が %d 件（%+v）",
			len(handoffs), fx.Tracker.CommentsOf("I_node188"))
	}
}
