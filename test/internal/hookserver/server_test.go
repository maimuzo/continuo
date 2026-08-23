package hookserver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 目的: socket へ届いた hook が、そのまま HookSink.OnHook へ渡ることを確認する
// （受け入れの基準「どのイベントも捨てずに HookSink.OnHook へ渡す」）。
// 与える情報: listen 中の hookserver と、Stop hook の JSON 1件。
// 成功条件: 配送を始めたあと、同じ session_id と hook_event_name のイベントが1件届くこと。
func TestServer_socketに届いたhookがOnHookへ渡る(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	ts.send(t, stopEventJSON("session-1", "[]"))

	got := sink.waitFor(t, 1)
	if got[0].HookEventName != "Stop" || got[0].SessionID != "session-1" {
		t.Fatalf("届いた hook が想定と違います: %+v", got[0])
	}
}

// 目的: listen（段5d）と配送（段6b）が分かれていること、つまり StartDelivery を
// 呼ぶまでは溜めるだけで OnHook へ渡さないことを確認する。
// 与える情報: Start だけ呼んだ hookserver と、socket へ書き込んだ hook 2件。
// 成功条件: 2件がキューへ積まれた時点で1件も配送されておらず、StartDelivery のあとに
// 送信順で2件届くこと。
func TestServer_StartDeliveryを呼ぶまで配送しない(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)

	ts.send(t, stopEventJSON("session-1", "[]"))
	ts.send(t, stopEventJSON("session-2", "[]"))

	// 送った2件がキューへ積まれたことを見てから、配送件数を確かめる。
	// **一定時間眠って様子を見ない。**負荷の高い環境で偽陰性になる。
	waitForQueueLen(t, ts.server, 2)
	if n := len(sink.Events()); n != 0 {
		t.Fatalf("StartDelivery の前に %d 件が配送されました（溜めるだけのはずです）", n)
	}

	ts.server.StartDelivery()
	got := sink.waitFor(t, 2)
	if got[0].SessionID != "session-1" || got[1].SessionID != "session-2" {
		t.Fatalf("配送の順番が受信順ではありません: %q, %q", got[0].SessionID, got[1].SessionID)
	}
}

// 目的: 知らない session_id（OnHook が false を返すもの）が届いても、警告をログに出して
// 捨てるだけで落ちないことを確認する（受け入れの基準「知らない session_id が届いたら…落ちない」）。
// 与える情報: session_id が "unknown-session" のときだけ false を返す HookSink と、
// 知らない session_id の hook 1件、そのあとに知っている session_id の hook 1件。
// 成功条件: 2件とも OnHook へ渡り、ログに session_id と警告が残り、後続の1件も配送されること。
func TestServer_知らないsession_idはログに出して捨てる(t *testing.T) {
	sink := newRecordingSink("unknown-session")
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	ts.send(t, stopEventJSON("unknown-session", "[]"))
	ts.send(t, stopEventJSON("known-session", "[]"))

	got := sink.waitFor(t, 2)
	if got[1].SessionID != "known-session" {
		t.Fatalf("知らない session_id のあとの hook が配送されていません: %+v", got)
	}
	logs := ts.logs.String()
	if !strings.Contains(logs, "unknown-session") || !strings.Contains(logs, "level=WARN") {
		t.Fatalf("知らない session_id の警告がログに出ていません: %s", logs)
	}
}

// 目的: background_tasks の「欠けている」「空配列」「非空」の3つを区別できることを確認する
// （設計 3-2 の判定はこの3つを別々に扱うため、区別を潰してはならない）。
// 与える情報: background_tasks を持たない Stop、空配列の Stop、subagent と shell の
// 2件が入った Stop の計3件。
// 成功条件: 順に nil / 長さ0の非 nil / 長さ2 になり、要素の項目（id・type・agent_type・command）も
// 実測どおりに読めること。
func TestServer_background_tasksの欠落と空配列と非空を区別できる(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	nonEmpty := `[{"id":"a1f9f743842d397e1","type":"subagent","status":"running","description":"ディレクトリ調査","agent_type":"Explore"},` +
		`{"id":"bmr1ksf9i","type":"shell","status":"running","description":"45秒スリープをバックグラウンド実行","command":"sleep 45"}]`

	ts.send(t, stopEventJSON("missing", ""))
	ts.send(t, stopEventJSON("empty", "[]"))
	ts.send(t, stopEventJSON("nonempty", nonEmpty))

	got := sink.waitFor(t, 3)

	if got[0].BackgroundTasks != nil {
		t.Fatalf("background_tasks が欠けている hook で nil になっていません: %+v", got[0].BackgroundTasks)
	}
	if got[1].BackgroundTasks == nil {
		t.Fatalf("空配列の background_tasks が nil になりました（欠落と区別できません）")
	}
	if len(*got[1].BackgroundTasks) != 0 {
		t.Fatalf("空配列の background_tasks の長さが 0 ではありません: %d", len(*got[1].BackgroundTasks))
	}
	if got[2].BackgroundTasks == nil || len(*got[2].BackgroundTasks) != 2 {
		t.Fatalf("非空の background_tasks を読めていません: %+v", got[2].BackgroundTasks)
	}
	tasks := *got[2].BackgroundTasks
	if tasks[0].ID != "a1f9f743842d397e1" || tasks[0].Type != "subagent" || tasks[0].AgentType != "Explore" {
		t.Fatalf("subagent の background_task の項目が想定と違います: %+v", tasks[0])
	}
	if tasks[1].Type != "shell" || tasks[1].Command != "sleep 45" {
		t.Fatalf("shell の background_task の項目が想定と違います: %+v", tasks[1])
	}
}

// 目的: hookserver がイベントの種類で選り分けないことを確認する
// （捨てるかどうかを決めるのは orchestrator である。受け入れの基準「どのイベントも捨てずに渡す」）。
// 与える情報: agent_type が空文字の SubagentStop・UserPromptSubmit・Notification・
// PreToolUse・SessionStart の5件。
// 成功条件: 5件すべてが送信順に OnHook へ渡り、agent_type や notification_type などの
// イベント固有の項目も読めること。
func TestServer_どのイベントも捨てずに渡す(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	lines := []string{
		`{"hook_event_name":"SubagentStop","session_id":"s","agent_id":"a1","agent_type":""}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"<task-notification><task-id>a1</task-id></task-notification>"}`,
		`{"hook_event_name":"Notification","session_id":"s","notification_type":"permission_prompt"}`,
		`{"hook_event_name":"PreToolUse","session_id":"s","prompt_id":"p1"}`,
		`{"hook_event_name":"SessionStart","session_id":"s","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/wt"}`,
	}
	for _, l := range lines {
		ts.send(t, l)
	}

	got := sink.waitFor(t, len(lines))
	want := []string{"SubagentStop", "UserPromptSubmit", "Notification", "PreToolUse", "SessionStart"}
	for i, name := range want {
		if got[i].HookEventName != name {
			t.Fatalf("%d 件目のイベント名が %q ではなく %q でした", i+1, name, got[i].HookEventName)
		}
	}
	if got[0].AgentID != "a1" || got[0].AgentType != "" {
		t.Fatalf("agent_type が空文字の SubagentStop の項目が想定と違います: %+v", got[0])
	}
	if !strings.Contains(got[1].Prompt, "<task-notification>") {
		t.Fatalf("UserPromptSubmit の prompt が読めていません: %q", got[1].Prompt)
	}
	if got[2].NotificationType != "permission_prompt" {
		t.Fatalf("Notification の notification_type が読めていません: %q", got[2].NotificationType)
	}
	if got[4].TranscriptPath != "/tmp/t.jsonl" || got[4].Cwd != "/tmp/wt" {
		t.Fatalf("SessionStart の共通項目が読めていません: %+v", got[4])
	}
}

// 目的: JSON として解釈できない行が届いても、hookserver が落ちず、後続の hook を配送し続ける
// ことを確認する。
// 与える情報: 壊れた行1件と、正しい Stop 1件。
// 成功条件: 正しい1件だけが配送され、壊れた行については警告がログに残ること。
func TestServer_壊れた行は捨てて配送を続ける(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	ts.send(t, `{"hook_event_name":"Stop"`)
	ts.send(t, stopEventJSON("session-1", "[]"))

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "session-1" {
		t.Fatalf("配送されたのが正しい1件ではありません: %+v", got[0])
	}
	if !strings.Contains(ts.logs.String(), "解釈できませんでした") {
		t.Fatalf("壊れた行の警告がログに出ていません: %s", ts.logs.String())
	}
}

// 目的: Close が listen を止め、socket ファイルを片付け、2回呼んでも落ちないことを確認する。
// 与える情報: listen 中の hookserver。
// 成功条件: Close 後に socket ファイルが消えており、2回目の Close もエラーを返さないこと。
func TestServer_Closeでsocketを片付ける(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)
	ts.start(t)
	ts.server.StartDelivery()

	if err := ts.server.Close(); err != nil {
		t.Fatalf("Close に失敗しました: %v", err)
	}
	if _, err := os.Lstat(ts.socketPath); !os.IsNotExist(err) {
		t.Fatalf("Close 後も socket ファイルが残っています: %s (err=%v)", ts.socketPath, err)
	}
	if err := ts.server.Close(); err != nil {
		t.Fatalf("2回目の Close がエラーになりました: %v", err)
	}
}

// 目的: 実行時ディレクトリが filepath.Dir(解決済みの socket のパス) であること、つまり
// socket と同じディレクトリの下の issues/*/pending を逃がし先として走査することを確認する
// （設計 3-23。自分で決め直さない）。
// 与える情報: socket と同じディレクトリの下に置いた逃がし先のファイル1件。
// 成功条件: RuntimeDir が socket のディレクトリと一致し、そこに置いた hook が読み戻されること。
func TestServer_実行時ディレクトリはsocketのディレクトリである(t *testing.T) {
	sink := newRecordingSink()
	ts := newTestServer(t, sink)

	if ts.server.RuntimeDir() != filepath.Dir(ts.socketPath) {
		t.Fatalf("実行時ディレクトリが socket のディレクトリと違います: %q と %q",
			ts.server.RuntimeDir(), filepath.Dir(ts.socketPath))
	}

	dir := ts.pendingDir(t, "octocat-hello-world-188")
	writePendingFile(t, dir, "1787057953362306-Stop.json", stopEventJSON("session-1", "[]"))

	ts.start(t)
	if err := ts.server.ReplayPending(); err != nil {
		t.Fatalf("逃がし先の読み戻しに失敗しました: %v", err)
	}
	ts.server.StartDelivery()

	got := sink.waitFor(t, 1)
	if got[0].SessionID != "session-1" {
		t.Fatalf("逃がし先から読み戻した hook が想定と違います: %+v", got[0])
	}
}
