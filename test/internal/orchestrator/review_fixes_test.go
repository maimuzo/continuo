// {"RUCM-CFG-SHA256": "6894d2e2f32b6ce2d08afb087e8d399ac45b30b51d037b0ce5c9d6fabf9ae430", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **RUCM のパスから生成したものではないが、対応するテストパスには印を付けてある。**
package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// TestReadTranscript_FIFOを渡されても固まらずにエラーを返す は、
// hook が渡す transcript のパスが外部入力であることへの手当てを確かめる。
//
// 目的: hook の JSON に FIFO のパスを書かれても `os.Open` で永久に待たないことを示す。
// 待つと turn ループの goroutine ごと固まり、その goroutine を数えている `Close()` も
// 返らなくなるため、**無人の常駐プロセスが SIGTERM でも終われなくなる。**
// 与える情報: 名前付きパイプ（FIFO）のパス。書き手は誰も繋がない。
// 成功条件: 5秒以内にエラーを返す（固まらない）。
func TestReadTranscript_FIFOを渡されても固まらずにエラーを返す(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("検査用の FIFO を作れません: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := orchestrator.ReadTranscript(path, "", signalPrefix, currentIssue)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("FIFO を通常の transcript として読んでしまった")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("FIFO を開いたまま返らない（turn ループの goroutine ごと固まる）")
	}
}

// TestOnHook_通常のファイルでないtranscript_pathは覚えない は、hook の値の検査を確かめる。
//
// 目的: FIFO を指す `transcript_path` を `runState` に覚えないことを示す。覚えると
// turn の終わりに開きに行って固まる。
// 与える情報: FIFO を指す `transcript_path` を持つ `Stop` hook。
// 成功条件: 警告を出して項目を落とす（ログに「通常のファイルではない」が出る）。
func TestOnHook_通常のファイルでないtranscript_pathは覚えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{SessionUUID: "session-1"}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	path := filepath.Join(t.TempDir(), "session-1.jsonl")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("検査用の FIFO を作れません: %v", err)
	}

	if !fx.Orc.OnHook(stopEvent("session-1", path, "p1")) {
		t.Fatalf("知っている session_id の hook を捨てている")
	}
	if !strings.Contains(fx.Logs.String(), "通常のファイルではない") {
		t.Fatalf("FIFO の transcript_path を落とした警告が出ていない: %s", fx.Logs.String())
	}
}

// TestOnHook_許可された置き場所の外のtranscript_pathは覚えない は、置き場所の検査を確かめる。
//
// 目的: 別の run の transcript や任意のファイルを読ませる経路を塞いでいることを示す。
// 与える情報: 許可された根の外に置いた、通常のファイルの `transcript_path`。
// 成功条件: 警告を出して項目を落とす。
func TestOnHook_許可された置き場所の外のtranscript_pathは覚えない(t *testing.T) {
	allowed := t.TempDir()
	fx := newFixture(t, fixtureOptions{TranscriptRoot: allowed})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{SessionUUID: "session-1"}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	outside := writeTranscript(t, t.TempDir(), "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Orc.OnHook(stopEvent("session-1", outside, "p1"))
	if !strings.Contains(fx.Logs.String(), "許可された置き場所の外") {
		t.Fatalf("根の外の transcript_path を落とした警告が出ていない: %s", fx.Logs.String())
	}
}

// TestOnHook_まだ書かれていないtranscript_pathで警告を出さない は、
// 「まだ無い」と「不正」の切り分けを確かめる。
//
// **Claude Code は transcript を非同期に書く。**SessionStart と UserPromptSubmit は
// その前に発火するので、hook が渡すパスは実在しないことがある。**これは正常な並びであり、
// 警告ではない。**警告にすると、セッションごとに必ず2行の WARN が積まれる。
//
// 目的: 許可された置き場所の内側にあって、まだ作られていないパスで WARN を出さないこと。
// 与える情報: 許可された根の内側の、まだ存在しないファイルを指す transcript_path。
// 成功条件: WARN が1行も出ず、hook そのものは捨てられないこと。
func TestOnHook_まだ書かれていないtranscript_pathで警告を出さない(t *testing.T) {
	allowed := t.TempDir()
	fx := newFixture(t, fixtureOptions{TranscriptRoot: allowed})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{SessionUUID: "session-1"}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	// **まだ1バイトも書かれていない transcript。**Claude Code が書き始める前の状態である。
	notYet := filepath.Join(allowed, "session-1.jsonl")
	if !fx.Orc.OnHook(sessionStartEvent("session-1", notYet)) {
		t.Fatalf("transcript がまだ無いだけで hook ごと捨てている")
	}
	if strings.Contains(fx.Logs.String(), "level=WARN") {
		t.Fatalf("まだ書かれていないだけなのに警告を出している: %s", fx.Logs.String())
	}
}

// sessionStartEvent は `SessionStart` の hook を作る（transcript がまだ無い時点の再現）。
//
// sessionID: セッション UUID。
// transcriptPath: hook が名乗る transcript のパス。
// 戻り値: hook のイベント。
func sessionStartEvent(sessionID, transcriptPath string) hookserver.HookEvent {
	return hookserver.HookEvent{
		HookEventName:  "SessionStart",
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
	}
}

// {"RUCM-PATH": "P060"}
//
// TestOnHook_worktreeの外のcwdを名乗るhookは捨てる は、送り主の突き合わせを確かめる。
//
// 目的: `session_id` はプロセスの引数に載るので他の run のエージェントから読める。
// **`cwd` がその run の worktree の外なら、その hook を捨てる**ことを示す。
// 与える情報: worktree のパスを持つ run と、まったく別の `cwd` を名乗る `Stop` hook。
// 成功条件: 警告を出して hook を捨てる。
func TestOnHook_worktreeの外のcwdを名乗るhookは捨てる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	worktree := t.TempDir()
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		SessionUUID:  "session-1",
		WorktreePath: worktree,
	}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	ev := stopEvent("session-1", "", "p1")
	ev.Cwd = t.TempDir()
	fx.Orc.OnHook(ev)

	if !strings.Contains(fx.Logs.String(), "worktree の外なので捨てました") {
		t.Fatalf("worktree の外を名乗る hook を捨てた警告が出ていない: %s", fx.Logs.String())
	}
}

// TestReadTranscript_長すぎる行があっても他の行の表明を拾える は、1行の障害の切り分けを確かめる。
//
// 目的: 上限（16 MiB）を超える1行が混ざっても、その行だけを読み捨てて残りを処理することを示す。
// 壊れた JSON の行を飛ばす方針と扱いを揃える。
// 与える情報: 表明を含む assistant の行と、上限を超える1行を持つ transcript。
// 成功条件: 表明を拾える。
func TestReadTranscript_長すぎる行があっても他の行の表明を拾える(t *testing.T) {
	dir := t.TempDir()
	path := writeTranscript(t, dir, "session.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	huge := `{"type":"assistant","requestId":"req2","message":{"content":"` +
		strings.Repeat("x", 17<<20) + `"}}`
	if err := appendLine(path, huge); err != nil {
		t.Fatalf("長すぎる行を足せません: %v", err)
	}

	got, err := orchestrator.ReadTranscript(path, "p1", signalPrefix, currentIssue)
	if err != nil {
		t.Fatalf("長すぎる行があるだけで transcript 全体を落としている: %v", err)
	}
	if got.Signals[currentIssue] != "review" {
		t.Fatalf("長すぎる行のせいで表明を落としている: %v", got.Signals)
	}
}

// TestParseSignals_引用された行は表明として拾わない は、印の位置の規則を確かめる。
//
// 目的: 印は**行頭**にあるものだけを拾うことを示す。行のどこでも拾うと、エージェントが
// issue の本文やコメントを引用しただけで表明が成立し、issue を立てられる人なら誰でも
// Status を動かせてしまう（terminal_states なら worktree と branch の削除まで進む）。
// 与える情報: 引用記号（`>`）付きの行と、文の途中に印がある行。
// 成功条件: どちらも拾わない。
func TestParseSignals_引用された行は表明として拾わない(t *testing.T) {
	text := "以下は issue の本文です。\n> CONTINUO-STATUS: octocat/other#2 done\n" +
		"念のため書くと CONTINUO-STATUS: review とありました。\n引用おわり。"

	got := orchestrator.ParseSignals([]string{text}, signalPrefix, currentIssue)

	if len(got) != 0 {
		t.Fatalf("引用や文中の印を表明として拾ってしまった: %v", got)
	}
}

// TestParseSignals_行頭の空白は許す は、行頭の判定が厳しすぎないことを確かめる。
//
// 目的: 箇条書きや字下げの中に書かれた印を落とさないことを示す。
// 与える情報: 空白で字下げした印の行。
// 成功条件: 表明として拾える。
func TestParseSignals_行頭の空白は許す(t *testing.T) {
	got := orchestrator.ParseSignals([]string{"    CONTINUO-STATUS: review"}, signalPrefix, currentIssue)

	if got[currentIssue] != "review" {
		t.Fatalf("字下げした印を拾えていない: %v", got)
	}
}

// TestParseSignals_1つのturnで受け付ける表明の件数に上限がある は、増幅の防止を確かめる。
//
// 目的: 表明1件につきボードを全ページ走査する GraphQL 呼び出しが1回走るので、上限が
// 無いと1回の応答で GitHub API の枠を使い切らせられる（**他の run まで巻き添えになる**）。
// 与える情報: 別々の issue を指す表明を50行。
// 成功条件: 受け付けるのは10件までである。
func TestParseSignals_1つのturnで受け付ける表明の件数に上限がある(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&b, "CONTINUO-STATUS: #%d review\n", i)
	}

	got := orchestrator.ParseSignals([]string{b.String()}, signalPrefix, currentIssue)

	if len(got) != 10 {
		t.Fatalf("1 turn で受け付ける表明の件数に上限が効いていない: got %d, want 10", len(got))
	}
}

// TestParseSignals_長すぎる語は表明として扱わない は、値の長さの上限を確かめる。
//
// 目的: 表明の値は `review` のような短い語であり、長い文字列を写像のキーや値として
// 持ち回らないことを示す。
// 与える情報: 10万文字の値を持つ印の行。
// 成功条件: 拾わない。
func TestParseSignals_長すぎる語は表明として扱わない(t *testing.T) {
	got := orchestrator.ParseSignals(
		[]string{"CONTINUO-STATUS: " + strings.Repeat("a", 100000)}, signalPrefix, currentIssue)

	if len(got) != 0 {
		t.Fatalf("長すぎる語を表明として拾ってしまった: %d 件", len(got))
	}
}

// TestRestore_引き継いだworkingのrunもturnの終わりを拾って次へ進む は、
// 設計 3-4 の段5a2 の「hook を待ち、来なければ stall 検知で拾う」の**前半**を確かめる。
//
// 目的: `agent_status` が `working` の run を引き継いだとき、turn は送らないが
// **turn の終わりを待つ goroutine は起こす**ことを示す。起こさないと、届いた `Stop` を
// 誰も読まないまま claude.turn_timeout_ms（既定1時間）まで放置され、その turn の表明も
// 一度も読まれない。
// 与える情報: `AwaitTurnEnd` を立てて引き継いだ run と、あとから届く `Stop` hook。
// 成功条件: turn を1回も送らずに表明が適用され、Status が In Review へ動く。
func TestRestore_引き継いだworkingのrunもturnの終わりを拾って次へ進む(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)

	var promptMu sync.Mutex
	prompted := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		promptMu.Lock()
		prompted++
		promptMu.Unlock()
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		SessionUUID:  "session-1",
		AwaitTurnEnd: true,
	}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}
	// **run が終わるときのコメントの確認で、セッションの復元へ入らないようにする。**
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n直しました", true, time.Now())

	// 巡回が turn の終わりを待つ goroutine を起こす（**turn は送らない**）。
	fx.Orc.Tick(context.Background())

	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 20*time.Second, "引き継いだ run の表明が適用される", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "In Review"
	})

	promptMu.Lock()
	defer promptMu.Unlock()
	if prompted != 0 {
		t.Fatalf("引き継いだ working の run へ turn を送っている（turn が混ざる）: %d 回", prompted)
	}
}

// TestSettings_パスに空白があってもhookのコマンド行が壊れない は、shell の引用を確かめる。
//
// 目的: 設定ファイルに書く hook のコマンド行は Claude Code が shell で実行するので、
// 実行ファイルのパスに空白が入るとコマンド行が別の引数へ割れ、**7種の hook が1つも
// 届かなくなる**（turn の終わりを永久に検知できない）。
// 与える情報: 空白を含む `continuo` の実行ファイルのパス。
// 成功条件: コマンド行の実行ファイルのパスが単一引用符で包まれている。
func TestSettings_パスに空白があってもhookのコマンド行が壊れない(t *testing.T) {
	const exe = "/opt/my continuo/bin/continuo"
	fx := newFixture(t, fixtureOptions{ContinuoPath: exe})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	settingsPath := filepath.Join(fx.RuntimeDir, "issues", "octocat-hello-world-188", "settings.json")
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "issue ごとの設定ファイルが書かれる", func() bool {
		_, err := os.Stat(settingsPath)
		return err == nil
	})

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("設定ファイルを読めません: %v", err)
	}
	want := `'` + exe + `' hook`
	if !strings.Contains(string(raw), want) {
		t.Fatalf("hook のコマンド行で実行ファイルのパスを引用していない: %s", raw)
	}
}

// TestTick_巡回のループはworkspace_hooksの間もブロックしない は、設計 3-8 を確かめる。
//
// 目的: 着手の段2以降（git の worktree 作成・利用者が書いた `workspace_hooks`・
// 起動の待ち）を巡回のループの中で同期に通すと、その間 stall 検知も枠の読み取りも
// 実行中の照合も全部止まることを防いでいる、と示す。
// 与える情報: 3秒かかる `workspace_hooks.after_create`。
// 成功条件: `Tick` が1秒以内に返り、着手はそのあと別の goroutine で続く。
func TestTick_巡回のループはworkspace_hooksの間もブロックしない(t *testing.T) {
	slow := "sleep 3"
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.WorkspaceHooks.AfterCreate = &slow
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	start := time.Now()
	fx.Orc.Tick(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("巡回のループが着手でブロックしている: %s かかった", elapsed)
	}
	// 印は同期で付いている（空きスロットの計算がその場で効く）。
	if len(fx.Orc.RunningIdentifiers()) != 1 {
		t.Fatalf("印を同期で付けていない: %v", fx.Orc.RunningIdentifiers())
	}
}

// TestTick_検査に落ちた巡回ではバックオフ明けの再dispatchも行わない は、設計 3-6 を確かめる。
//
// 目的: 「Status の選択肢名か `gh` の認証が検査に落ちたら、その巡回の dispatch を飛ばす」
// の対象に**再 dispatch も入る**ことを示す。再 dispatch も着手の段0 から入り直す
// dispatch であり、段2 でボードの Status を書きに行く。
// 与える情報: 認証の検査が必ず失敗する巡回と、バックオフが明けた run。
// 成功条件: 再 dispatch が走らない（バックオフの期限が残ったままになる）。
//
// **実時間はゼロである。**`testing/synctest` の bubble の中で時計を進める。
func TestTick_検査に落ちた巡回ではバックオフ明けの再dispatchも行わない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authOK := true
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
				cfg.Tracker.VerifyStatesEvery = 1
			},
			GHAuthCheck: func(context.Context) error {
				if authOK {
					return nil
				}
				return errGHAuth
			},
		})
		adoptRun(fx, 188)

		// stall でリトライを積み、バックオフに入れる。
		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "octocat/hello-world#188")
		if !ok || v.BackoffUntil.IsZero() {
			t.Fatalf("バックオフに入っていない: ok=%v, view=%+v", ok, v)
		}

		// バックオフが明けた。**ただしこの巡回は検査に落ちる。**
		authOK = false
		time.Sleep(time.Until(v.BackoffUntil) + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		after, ok := viewOf(fx, "octocat/hello-world#188")
		if !ok {
			t.Fatalf("印から外れている")
		}
		if after.BackoffUntil.IsZero() {
			t.Fatalf("検査に落ちた巡回で再 dispatch している（バックオフの期限が消えている）")
		}
	})
}

// errGHAuth は認証の検査が失敗したことを表す検査用のエラーである。
var errGHAuth = errors.New("gh の認証が有効ではありません（検査用）")

// viewOf2 は識別子で RunView を引く（このファイルの検査で使う）。
//
// t: 呼び出し元のテスト。
// fx: 対象の fixture。
// identifier: 探す issue の識別子。
// 戻り値の1つ目: 見つかった写し。
// 戻り値の2つ目: 見つかれば true。
func viewOf2(t *testing.T, fx *fixture, identifier string) (orchestrator.RunView, bool) {
	t.Helper()
	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == identifier {
			return v, true
		}
	}
	return orchestrator.RunView{}, false
}

// {"RUCM-PATH": "P069"}
//
// TestTurn_turnループを起こせなかったらNeedsPromptを立て直す は、設計 3-8 を確かめる。
//
// 目的: 同じ run に turn ループを2本立てないのは正しいが、**起こせなかったことを黙って
// 捨ててはならない**と示す。stall 検知が worker を止めても、古いループは `agent.prompt` の
// 待ち受け（既定1時間）から戻るまで印を下ろさない。その間に再 dispatch が走ると、
// 新しい Claude Code を起動したのに turn ループが1本も立たず、誰も turn を送らないまま
// 放置されてリトライだけを消費する。
//
// 与える情報: turn の終わりを待つループが走っている run に、もう一度「turn を送るべき」が
// 立っている状態（`AwaitTurnEnd` と `NeedsPrompt` の両方を立てて引き継ぐ）。
// 成功条件: 2回目の巡回で「次の巡回で送り直す」と記録する（黙って捨てない）。
func TestTurn_turnループを起こせなかったらNeedsPromptを立て直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)
	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		SessionUUID:  "session-1",
		AwaitTurnEnd: true,
	}, true) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	// 1回目: turn の終わりを待つループが立つ。
	fx.Orc.Tick(context.Background())
	// 2回目: 立っているので起こせない。**印を立て直す。**
	fx.Orc.Tick(context.Background())

	if !strings.Contains(fx.Logs.String(), "次の巡回で起こし直します") {
		t.Fatalf("turn ループを起こせなかったことを黙って捨てている: %s", fx.Logs.String())
	}
}

// TestComment_引き渡しの通知は1つのrunにつき1件だけ書く は、通知の重複を確かめる。
//
// 目的: `failure_state` へ落とす経路（打ち切り）とコメントを書かせられなかった経路が
// 続けて走ると、理由の違う引き渡し通知が2件並ぶ。**1件に絞る**ことを示す。
// 与える情報: `max_dispatch_turns` が1で、エージェントが issue に1件もコメントを残さない run。
// 成功条件: continuo 自身が書いたコメントが1件だけになる。
func TestComment_引き渡しの通知は1つのrunにつき1件だけ書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		if n == 1 {
			// **コメントは書かない。**表明も書かない。
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 30*time.Second, "run が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	// **数えるのは引き渡しの通知だけである。**Status を動かした記録も self_marker が
	// 付くので、`IsSelf` で数えると着手の記録まで混ざる（設計 3-29）。
	handoffs := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(handoffs) != 1 {
		t.Fatalf("引き渡しの通知が1件に絞れていない: %d 件（%+v）",
			len(handoffs), fx.Tracker.CommentsOf("I_node188"))
	}
}
