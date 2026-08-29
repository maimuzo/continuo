// **エージェントの印を投稿者と併せて見ることの検査である**（設計 3-65）。
//
// **印（`tracker.comments.marker` / `self_marker`）は本文の先頭に置くただの文字列であり、
// issue にコメントできる人なら誰でも書ける。**投稿者を照合しないと、外部の第三者の
// コメントを「エージェントが成果を書いた」と読み違える。
//
// **投稿者で絞る処理そのものは internal/tracker の FetchComments にある**
// （test/internal/tracker/comments_test.go）。ここで見るのは orchestrator の側である。
//
//	持ち主を取り、FetchComments へ渡すこと
//	取れなくても run が止まらないこと
//	取れなかった持ち主を、コメントを確かめる前に取り直すこと
//	取れない状態が続いていることがログに出続けること
//	「印はあるが投稿者が違う」を名指しでログに出すこと
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
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
// beforeTurnEnd: **turn が終わる直前に呼ばれる**（nil なら何もしない）。
// **run が始まったあとにしか作れない状況を作るために使う**（時計を進める・
// 第三者のコメントを足す）。
func runUntilCommentCheck(t *testing.T, fx *fixture, beforeTurnEnd func()) {
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
			if beforeTurnEnd != nil {
				beforeTurnEnd()
			}
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

	runUntilCommentCheck(t, fx, nil)

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
	fx.AllowLog("gh の持ち主を取れません")

	runUntilCommentCheck(t, fx, nil)

	if got := fx.Tracker.CommentsSelfLogin(); got != "" {
		t.Fatalf("持ち主を取れていないのに %q を渡している", got)
	}
	handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(handoffs) != 1 {
		t.Fatalf("持ち主を取れないことで run が止まっている: 引き渡しの通知が %d 件（%+v）",
			len(handoffs), fx.Tracker.CommentsOf("I_node188"))
	}
}

// 目的: 一度取れなかった持ち主を、**コメントを確かめる前に取り直している**ことを確認する
// （設計 3-65）。
//
// **取り直さないと、`gh api` に一度届かなかっただけで、プロセスが生きているあいだ
// ずっと印だけの判定に戻る。**この検査は `hasRunComment` の中の取り直しを見ている。
// **その1行を消すと、巡回のときの1回目（失敗）だけで終わり、持ち主は空文字のままになる。**
//
// 与える情報: 1回目だけ失敗して2回目からは octocat を返す取得関数と、turn の終わりの直前に
// 取り直しの間隔（5分）を超えて進む時計。
// 成功条件: FetchComments が受け取った持ち主が octocat であること。
func TestComment_取れなかった持ち主をコメントの確認の前に取り直す(t *testing.T) {
	clock := newTestClock()
	var mu sync.Mutex
	attempts := 0
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		GHLogin: func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			if attempts == 1 {
				return "", errors.New("gh api user に1回目は届かない")
			}
			return testGHLogin, nil
		},
	})
	fx.AllowLog("gh の持ち主を取れません")

	// **turn が終わる直前に、取り直しの間隔を超えて時計を進める。**
	runUntilCommentCheck(t, fx, func() { clock.Advance(6 * time.Minute) })

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got < 2 {
		t.Fatalf("持ち主を取り直していない: 取得の呼び出しが %d 回", got)
	}
	if got := fx.Tracker.CommentsSelfLogin(); got != testGHLogin {
		t.Fatalf("取り直した持ち主をコメントの取得へ渡していない: got %q, want %q", got, testGHLogin)
	}
}

// 目的: 持ち主を取れない状態が続いていることが、**ログから読める**ことを確認する
// （設計 3-65）。
//
// **警告が起動直後の1回きりだと、常駐して何日も経ったあとには形跡が残らない。**
// そのあいだ continuo は印だけで判定し続けているので、第三者のコメントを
// エージェントのものと読み違えうる。
//
// 与える情報: 必ず失敗する取得関数と、手で進める時計。巡回を3回（1回目、間隔を空けずに
// 2回目、5分を超えて進めてから3回目）。
// 成功条件: 取得の試みが2回（間隔を空けない巡回では取り直さない）、警告も2回で、
// 2回目の警告に連続して失敗した回数が入っていること。
func TestComment_持ち主を取れない状態が続くと警告が出続ける(t *testing.T) {
	clock := newTestClock()
	var mu sync.Mutex
	attempts := 0
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		GHLogin: func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			return "", errors.New("gh api user に届きません")
		},
	})
	fx.AllowLog("gh の持ち主を取れません")

	ctx := context.Background()
	fx.Orc.Tick(ctx)
	// **間隔を空けない巡回では取り直さない**（外部プロセスを毎巡回で起こさない）。
	fx.Orc.Tick(ctx)
	clock.Advance(6 * time.Minute)
	fx.Orc.Tick(ctx)

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 2 {
		t.Fatalf("取り直しの回数が想定と違う: got %d, want 2（間隔を空けない巡回でも取り直している）", got)
	}
	logs := fx.Logs.String()
	if n := strings.Count(logs, "gh の持ち主を取れません"); n != 2 {
		t.Fatalf("警告の回数が想定と違う: got %d, want 2\n%s", n, logs)
	}
	if !strings.Contains(logs, "連続して失敗した回数=2") {
		t.Fatalf("失敗が続いていることがログから読めない（連続して失敗した回数が無い）:\n%s", logs)
	}
}

// 目的: **「印は付いているのに投稿者が持ち主と違う」を名指しでログに出す**ことを確認する
// （設計 3-65）。
//
// **これがいちばん切り分けの難しい状態である。**issue の画面には印の付いたコメントが
// 見えているのに、continuo は「書かれていない」と判定してセッションを復元しにいく。
// **名指しで出さないと、人間に見えるのは「この run のコメントが無いので…」の1行だけになる。**
//
// 与える情報: turn の終わりの直前に、第三者（outsider）が印付きで書いたコメント1件。
// 成功条件: 投稿者を名指しした警告がログに出て、なお「エージェントが書いた」とは
// 数えられていないこと（引き渡しの通知が出る）。
func TestComment_印はあるが投稿者が違うことをログに残す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("投稿者が gh の持ち主と違います")

	runUntilCommentCheck(t, fx, func() {
		fx.Tracker.AddSpoofedComment("I_node188",
			fx.Config.Tracker.Comments.Marker+"\n第三者が同じ印で書いた", "outsider", time.Now())
	})

	logs := fx.Logs.String()
	if !strings.Contains(logs, "投稿者が gh の持ち主と違います") {
		t.Fatalf("「印はあるが投稿者が違う」がログに出ていない:\n%s", logs)
	}
	if !strings.Contains(logs, "outsider") {
		t.Fatalf("警告に投稿者の名前が入っていない:\n%s", logs)
	}
	if len(fx.Tracker.HandoffCommentsOf("I_node188")) != 1 {
		t.Fatalf("第三者のコメントをエージェントのものと数えている（引き渡しの通知が出ていない）: %+v",
			fx.Tracker.CommentsOf("I_node188"))
	}
}
