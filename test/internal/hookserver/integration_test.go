package hookserver_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/hookclient"
	"github.com/maimuzo/continuo/internal/hookserver"
)

// 目的: `continuo hook` の実体（internal/hookclient）が書いたものを、hook の受け口
// （internal/hookserver）がそのまま受け取れることを確認する。socket の形式（改行区切り
// JSON・1コネクション1メッセージ・応答なし）が両側で一致していることの検査である。
// 与える情報: listen 中の hookserver の socket のパスと、Stop hook の JSON。
// 成功条件: 転送の結果が sent になり、同じ session_id と background_tasks が OnHook へ届くこと。
func TestHookClientとHookServer_socket経由で1件が届く(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	input := `{"hook_event_name":"Stop","session_id":"s1","background_tasks":[{"id":"bmr1ksf9i","type":"shell","status":"running","command":"sleep 45"}]}`
	result := hookclient.Forward(hookclient.Config{
		SocketPath: ts.socketPath,
		PendingDir: ts.pendingDir(t, "octocat-hello-world-188"),
		Stdin:      strings.NewReader(input),
	})
	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("socket へ転送できたはずが %s でした: %v", result.Outcome, result.Err)
	}

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "s1" {
		t.Fatalf("届いた hook が想定と違います: %+v", got[0])
	}
	if got[0].BackgroundTasks == nil || len(*got[0].BackgroundTasks) != 1 {
		t.Fatalf("background_tasks が届いていません: %+v", got[0].BackgroundTasks)
	}
	if (*got[0].BackgroundTasks)[0].Command != "sleep 45" {
		t.Fatalf("background_tasks の中身が想定と違います: %+v", (*got[0].BackgroundTasks)[0])
	}
}

// 目的: continuo が落ちている間に `continuo hook` が逃がし先へ書いたものを、起動した
// hookserver が読み戻せることを確認する（設計 3-19。書く側のファイル名と、読む側の
// 走査の規則が一致していることの検査である）。
// 与える情報: 誰も listen していない socket のパスへ転送を試みた hookclient と、
// そのあとに同じ実行時ディレクトリで立ち上げた hookserver。
// 成功条件: hookclient が逃がし先へ書き、ReplayPending がそれを読み戻して OnHook へ渡し、
// 読み終えたファイルが消えていること。
func TestHookClientとHookServer_逃がし先に書いたものを読み戻せる(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	pendingDir := ts.pendingDir(t, "octocat-hello-world-188")

	// この時点では hookserver はまだ listen していない（continuo が落ちている状態）。
	input := `{"hook_event_name":"Stop","session_id":"s1","background_tasks":[]}`
	result := hookclient.Forward(hookclient.Config{
		SocketPath: ts.socketPath,
		PendingDir: pendingDir,
		Stdin:      strings.NewReader(input),
	})
	if result.Outcome != hookclient.OutcomeSpilled {
		t.Fatalf("逃がし先へ書いたはずが %s でした: %v", result.Outcome, result.Err)
	}

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "s1" || got[0].HookEventName != "Stop" {
		t.Fatalf("読み戻した hook が想定と違います: %+v", got[0])
	}
	if names := pendingEntryNames(t, pendingDir); len(names) != 0 {
		t.Fatalf("読み終えた逃がし先のファイルが残っています: %v", names)
	}
}

// 目的: 標準入力が上限を超えた hook でも捨てずに届くこと、そして中身が欠けていることが
// 受け取る側で分かることを確認する（受け入れの基準「どのイベントも捨てずに
// HookSink.OnHook へ渡す」）。
// 与える情報: 上限を 100 バイトに縮めた `continuo hook` と、tool_response が上限を大きく
// 超える PostToolUse の JSON。
// 成功条件: 転送の結果が sent かつ Truncated が true になり、受け口には
// session_id・cwd・hook_event_name が届き、continuo_truncated が true であること。
func TestHookClientとHookServer_上限を超えた入力でも捨てずに届く(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	const limit = 100
	input := `{"session_id":"s1","cwd":"/tmp/wt","hook_event_name":"PostToolUse","tool_response":"` +
		strings.Repeat("あ", 4096) + `"}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath:    ts.socketPath,
		PendingDir:    ts.pendingDir(t, "octocat-hello-world-188"),
		Stdin:         strings.NewReader(input),
		MaxInputBytes: limit,
	})
	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("上限を超えた入力も転送されるはずが %s でした: %v", result.Outcome, result.Err)
	}
	if !result.Truncated {
		t.Fatalf("上限を超えたことが Result に出ていません: %+v", result)
	}
	if result.EventName != "PostToolUse" {
		t.Fatalf("上限を超えた入力から hook_event_name を拾えていません: %q", result.EventName)
	}

	got := sink.waitFor(t, 1)
	if got[0].HookEventName != "PostToolUse" || got[0].SessionID != "s1" || got[0].Cwd != "/tmp/wt" {
		t.Fatalf("上限を超えた hook の共通項目が届いていません: %+v", got[0])
	}
	if !got[0].Truncated {
		t.Fatalf("中身が欠けていることが受け取る側に伝わっていません: %+v", got[0])
	}
	if got[0].TruncatedLimitBytes != limit {
		t.Fatalf("超えた上限のバイト数が伝わっていません: %d", got[0].TruncatedLimitBytes)
	}
}

// 目的: 受け口が受け付ける1行の上限が、書く側の標準入力の上限より大きいことを確認する。
// 同じ値だと、書く側が「送れる」と判断した上限ちょうどの1行を受け口の bufio.Scanner が
// 捨ててしまい、hook がどこにも残らずに消える。
// 与える情報: 両方のパッケージの既定値。
// 成功条件: hookserver.DefaultMaxMessageBytes が hookclient.DefaultMaxInputBytes より
// 大きいこと（等しくてもいけない。1行には末尾の改行が付く）。
func TestHookClientとHookServer_受け口の上限は書く側より大きい(t *testing.T) {
	if int64(hookserver.DefaultMaxMessageBytes) <= int64(hookclient.DefaultMaxInputBytes) {
		t.Fatalf("受け口の上限（%d）が書く側の上限（%d）以下です。上限ちょうどの hook が消えます",
			hookserver.DefaultMaxMessageBytes, hookclient.DefaultMaxInputBytes)
	}
}
