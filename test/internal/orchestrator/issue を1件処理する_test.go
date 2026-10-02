// {"RUCM-CFG-SHA256": "db9742e059aee54b28b4fd1adec1b6e894b95939964d0893843d7bf6651a7a90", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **ユースケース記述「issue を1件処理する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
)

// {"RUCM-PATH": "P017"}
//
// Test_issueを1件処理する_P017_turnを送れなかったときStopHookのせいにしない は、
// 送信の失敗と「Stop hook が届かない」を混ぜないことを確かめる。
//
// 目的: `agent.prompt` が `agent_not_found` などで断ると、**turn は1文字も届いていない。**
// それを `turnStalled` に混ぜると、issue には「herdr は agent が待機状態になったと答えたが
// **Stop hook から通知が届かなかった**」という**起きていないことを断定した文面**が残り、
// 人間は正常な設定ファイルを確かめに行かされる。
//
// 与える情報: `agent.prompt` が `agent_not_found` を返す（人間が pane を閉じた直後）。
// `agent.max_retries` は 0 なので、1回目の失敗でそのまま人間へ渡る。
// 成功条件: issue に残る理由が「送れませんでした」であり、Stop hook にも
// 設定ファイルにも言及しないこと。
func Test_issueを1件処理する_P017_turnを送れなかったときStopHookのせいにしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_not_found", Message: "agent は登録されていません"}
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **コメントの取り戻しも同じ台本で落ちる。**この検証は agent.prompt を全部
	// agent_not_found にするので、引き渡しの直前に走るコメントの取り戻しも届かない。
	// 走る順番は機械の速さで前後するため、許可しておかないと環境によって落ちる。
	fx.AllowLog("turn を送れませんでした", "リトライの回数を使い切りました",
		"turn を1回も送っていないので", "コメントを書かせるプロンプトを送れません")

	fx.Orc.Tick(context.Background())

	// **着手の記録（Status を動かした記録）とは別物である。**引き渡しの通知だけを待つ。
	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node188")) > 0
	})

	body := fx.Tracker.HandoffCommentsOf("I_node188")[0].Body
	if !strings.Contains(body, "herdr へ指示を送れませんでした") {
		t.Errorf("送れなかったことが issue に書かれていない:\n%s", body)
	}
	if strings.Contains(body, "Stop hook") {
		t.Errorf("送れていないのに Stop hook のせいにしている:\n%s", body)
	}
	if !strings.Contains(body, "agent_not_found") {
		t.Errorf("herdr が返した本当の原因が issue に書かれていない:\n%s", body)
	}
}

// {"RUCM-PATH": "P014"}
//
// Test_issueを1件処理する_P014_打ち切りのときissueに残る理由が本当の理由である は、
// 引き渡しの通知の投稿枠を、本当の理由が先に取ることを確かめる。
//
// 目的: 引き渡しの通知は1つの run につき1件しか投稿しない。**コメントの取り戻しの失敗が
// 先に枠を使うと、issue に残るのは「作業を終えたと表明したのに書き残さなかった」だけになる。**
// 実際にはエージェントは完了を表明しておらず、画面が止まって打ち切られている。
//
// 与える情報: `agent.max_retries` が 0 で stall する run。コメントは1件も書かれず、
// セッションの復元も通らない（`agent.start --resume` が断られる）。
// 成功条件: issue に残る本文が**打ち切った理由**（画面が止まった）であり、
// コメントの取り戻しの失敗の文面で置き換わっていないこと。
func Test_issueを1件処理する_P014_打ち切りのときissueに残る理由が本当の理由である(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 1000
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	blockFirstPrompt(t, fx)
	// セッションの復元を断らせる（コメントの取り戻しが失敗する経路に入れる）。
	var started sync.Once
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		if strings.Contains(joinAny(args), "--resume") {
			return nil, &rpcErr{Code: "agent_start_failed", Message: "No conversation found"}
		}
		started.Do(func() {})
		// **既定の台本と同じ形で返す。**`agent_status` を `working` にすると、
		// stall の判定が「進んでいる」と読んで打ち切りに入らない。
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle", "interactive_ready": true, "pane_id": params["pane_id"]},
		}, nil
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("リトライの回数を使い切りました", "セッションを復元できません",
		"turn が終わったことを検知できません", "画面が変わらないまま", "stall")

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	clock.Advance(5 * time.Second)
	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
		return len(fx.Tracker.HandoffCommentsOf("I_node188")) > 0
	})
	time.Sleep(500 * time.Millisecond)

	comments := fx.Tracker.HandoffCommentsOf("I_node188")
	if len(comments) != 1 {
		t.Fatalf("引き渡しの通知が1件ではない: %d 件", len(comments))
	}
	body := comments[0].Body
	if !strings.Contains(body, "止まったものと判断して打ち切りました") {
		t.Errorf("打ち切った本当の理由が issue に残っていない:\n%s", body)
	}
	if strings.Contains(body, "何をしたのかを issue に書き残しませんでした") {
		t.Errorf("コメントの取り戻しの失敗が投稿枠を先に取り、本当の理由を追い出している:\n%s", body)
	}
}

// {"RUCM-PATH": "P060"}
//
// Test_issueを1件処理する_P060_既に印を持っているissueは二重にdispatchしない は、印の役目を確かめる。
//
// 目的: 設計 3-10 の「『この issue は自分が取った』という印で防ぐ。状態の絞り込みでは防がない」を示す。
// 与える情報: `Ready` の issue を1件 dispatch したあと、もう一度巡回する。
// 成功条件: `agent.start` が1回しか呼ばれない。
func Test_issueを1件処理する_P060_既に印を持っているissueは二重にdispatchしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の dispatch が済む", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 1 {
		t.Fatalf("同じ issue に2つ目の Claude Code を立てている: agent.start が %d 回", got)
	}
}

// {"RUCM-PATH": "P019"}
//
// Test_issueを1件処理する_P019_テンプレートに一覧に無い変数を書いたらそのissueを失敗にする は、
// 変数展開の失敗の扱いを確かめる。
//
// 目的: 設計 3-8 の「`missingkey=error` を付ける。**未知の変数を書いたテンプレートは
// 変数展開に失敗し、その issue を失敗として扱う**（黙って空文字を埋めない）」を示す。
// 与える情報: 5-3 の一覧に無い変数（`.issue.body`）を書いたテンプレート。
// 成功条件: Status が `failure_state`（Blocked）へ落ち、印から外れる。
func Test_issueを1件処理する_P019_テンプレートに一覧に無い変数を書いたらそのissueを失敗にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{PromptTemplate: "{{.issue.identifier}} {{.issue.body}}"})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "issue が失敗として扱われる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	waitFor(t, 5*time.Second, "印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("変数展開に失敗したのにプロンプトを送っている: %v", fx.Herdr.Methods())
	}
}

// {"RUCM-PATH": "P022"}
//
// Test_issueを1件処理する_P022_起動直後にblockedならescを送ってから失敗にする は、段10 の安全弁を確かめる。
//
// 目的: 設計 3-11 の「`blocked` のまま次を投げると、保留中の権限要求が承認されて実行される
// （3/3 で再現）」を防ぐため、**次を投げる前に `agent.send_keys` で `["esc"]` を送る**ことを示す。
// 与える情報: `agent.get` が `blocked` を返す台本。
// 成功条件: `agent.send_keys` に `["esc"]` が送られ、プロンプトは1回も送られず、
// Status が `failure_state` へ落ちる。
func Test_issueを1件処理する_P022_起動直後にblockedならescを送ってから失敗にする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "esc が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentSendKeys) > 0
	})

	keysParams := fx.Herdr.ParamsOf(t, herdr.MethodAgentSendKeys)
	keys, _ := keysParams["keys"].([]any)
	if len(keys) != 1 || keys[0] != "esc" {
		t.Fatalf("送ったキーが想定と違う: got %v, want [esc]", keys)
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("blocked のままプロンプトを投げている（保留中の権限要求が承認される）: %v", fx.Herdr.Methods())
	}
	waitFor(t, 5*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
}

// {"RUCM-PATH": "P047"}
//
// Test_issueを1件処理する_P047_failure_stateのissueをrunning_stateへ上書きしない は、段2 の許可リストを確かめる。
//
// **候補の一覧は GitHub のサーバ側の検索結果である。**continuo が直前に書いた Status が
// 索引へ反映される前に取り直すと、failure_state へ落としたばかりの issue が
// そのまま候補として返る。段2 が取り直しをしないと、
// **人間が Blocked に置いた issue を continuo が In Progress へ上書きしてしまう。**
//
// 目的: カンバンの Status が failure_state にある issue へ running_state を書かないこと。
// また、書かなかったときに段3 へ進まず、印を静かに外すこと。
// 与える情報: カンバンでは Blocked にあるのに、候補の写しでは Ready を名乗る issue。
// 成功条件: Status が Blocked のままで、書き込みそのものを試みず、worktree も開かず、
// 印が残らないこと。
func Test_issueを1件処理する_P047_failure_stateのissueをrunning_stateへ上書きしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	// カンバンの実体は Blocked である（人間が置いた、あるいは直前に落とした）。
	fx.Tracker.AddIssue(sampleIssue(188, "Blocked"))
	// 候補の一覧にだけ、反映が追いついていない Ready の写しが載る。
	fx.Tracker.SetExtraCandidates(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	// **段2 の取り直しが走ったことを待ち合わせの目印にする。**許可リストに落ちる場合、
	// continuo は UpdateStatus を1回も呼ばないので、そちらでは待てない。
	// **timeline の有無で経路が分かれるので、両方を数える**（設計 3-61。
	// 段2 が使うのは記録を取らない側だが、待ち合わせの意味は「取り直しが1本走った」である）。
	waitFor(t, 10*time.Second, "着手の試みが終わる", func() bool {
		return fx.Tracker.CountIDRefreshes() > 0
	})
	time.Sleep(500 * time.Millisecond)

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Errorf("active_states に無い issue へ書き込みを試みている: UpdateStatus を %d 回呼んだ", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Blocked" {
		t.Errorf("failure_state の issue を上書きしている: got %q, want Blocked", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("Status を書かなかったのに段3 へ進んでいる: worktree.open を %d 回呼んだ", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が残っている: %d 件", got)
	}
	if got := len(fx.Tracker.CommentsOf("I_node188")); got != 0 {
		t.Errorf("何も起きていないのに issue へコメントしている: %d 件", got)
	}
}

// {"RUCM-PATH": "P057"}
//
// Test_issueを1件処理する_P057_同じ理由で失敗し続けるissueは上限を超えたら拾わない は、
// issue 単位の失敗の記録を確かめる。
//
// **印（run）は失敗のたびに消えるので、印の中のリトライの回数では止まらない。**
// 次の巡回が0回目として拾い直し、同じ失敗を30秒ごとに繰り返す。
//
// 目的: 同じ issue が `agent.max_retries` を超えて失敗したら、それ以上拾わないこと。
// 与える情報: カンバンへ1バイトも書けない状況（failure_state へも落とせないので、
// issue は Ready のまま候補に上がり続ける）と、`agent.max_retries: 1`。
// 成功条件: 3回目以降の巡回で着手を試みなくなり、そのことが人間へ1度だけ知らされること。
func Test_issueを1件処理する_P057_同じ理由で失敗し続けるissueは上限を超えたら拾わない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxRetries = 1
	}})
	fx.AllowLog("Status を落とせません", "着手に失敗しました", "これ以上は拾いません")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetUpdateError(errors.New("テストが起こしたカンバンへの書き込みの失敗"))

	tick := func() {
		fx.Orc.Tick(context.Background())
		fx.WaitRunsDrained(t, 15*time.Second)
	}
	tick()
	tick()
	tick()
	before := fx.Tracker.CountCall("UpdateStatus")
	tick()
	after := fx.Tracker.CountCall("UpdateStatus")

	if after != before {
		t.Errorf("上限を超えても着手をやり直している: UpdateStatus の回数が %d から %d へ増えた", before, after)
	}
	if !strings.Contains(fx.Logs.String(), "これ以上は拾いません") {
		t.Errorf("拾わなくなったことを人間へ知らせていない")
	}
}

// {"RUCM-PATH": "P058"}
//
// Test_issueを1件処理する_P058_絞り込みの食い違いが1件あっても他のissueのdispatchは続く は、
// 巡回全体を止めないことを確かめる。
//
// **1件の食い違いで巡回の dispatch を丸ごと止めると、無関係の issue まで着手されなくなる。**
// 食い違った item だけを候補から外して続けること。
//
// 目的: 頼んだ Status に無い候補が混ざっても、他の issue の着手が進むこと。
// 与える情報: Ready の issue が1件と、候補の一覧にだけ載る Blocked の写しが1件。
// 成功条件: Ready の issue に turn が送られ、Blocked の issue の Status は動かないこと。
func Test_issueを1件処理する_P058_絞り込みの食い違いが1件あっても他のissueのdispatchは続く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("頼んだ Status に無い候補が返ったので飛ばします")
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.AddIssue(sampleIssue(189, "Blocked"))
	// **候補の先頭に食い違いを置く。**先頭で巡回が止まると、後続が着手されない。
	fx.Tracker.SetExtraCandidates(sampleIssue(189, "Blocked"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "食い違っていない issue に turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item189"); got != "Blocked" {
		t.Errorf("頼んだ Status に無い候補を着手している: got %q, want Blocked", got)
	}
	ids := fx.Orc.RunningIdentifiers()
	if len(ids) != 1 || !strings.Contains(ids[0], "#188") {
		t.Errorf("着手した issue が想定と違う: %v", ids)
	}
}

// {"RUCM-PATH": "P024"}
//
// Test_issueを1件処理する_P024_unknownのまま期限を過ぎたら人間へ渡さず試し直す は、打ち切りの側を確かめる。
//
// 目的: `herdr.startup_timeout_ms` を過ぎても `unknown` のままなら諦めること。
// **ただし人間へは渡さない**（`ErrStartupRetryable` を包むので、バックオフして試し直す）。
// 与える情報: `agent.get` が常に `unknown` を返す台本。`agent.max_retries` は既定（3回）。
// 成功条件: worker は止まるが、**Status が `failure_state` へ落ちない**（リトライが残っている）。
func Test_issueを1件処理する_P024_unknownのまま期限を過ぎたら人間へ渡さず試し直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "unknown"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	// **worker を止めるところまでは進む**（バックオフに入るため。stopWorker は pane を閉じる）。
	waitFor(t, 5*time.Second, "worker が止まる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	// **ここが本題である。**リトライが残っているうちは人間へ渡さない。
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Fatalf("unknown を1回受けただけで人間へ渡している（設計 3-16 の段10 は「試し直す」）: state=%s", got)
	}
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Fatalf("unknown のままプロンプトを投げている: %v", fx.Herdr.Methods())
	}
}

// {"RUCM-PATH": "P033"}
//
// Test_issueを1件処理する_P033_paneのlabelを書けなければ着手しない は、段8 の失敗を確かめる。
//
// **pane の label は issue の URL である。**再起動時にどの pane がどの issue かを
// 見分ける手がかりなので、書けないまま進むと復元できない pane ができる。
//
// 目的: `pane.rename` が失敗したら着手を止めること。
// 与える情報: 常に失敗する `pane.rename`。
// 成功条件: agent を起動せず、turn も送らないこと。
func Test_issueを1件処理する_P033_paneのlabelを書けなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodPaneRename, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "label を書けません"}
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 0 {
		t.Errorf("label を書けないのに agent を起動している: %d 回", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got != 0 {
		t.Errorf("label を書けないのに turn を送っている: %d 回", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// TestExternalFailure_turnの終わりに issue が消えていたら手放す は、設計 3-10 を確かめる。
//
// **turn が終わってから issue を取り直したとき、カンバンから返ってこないことがある**
// （人間がカンバンから外した、archive した）。**continuo はその issue の面倒を見ない。**
//
// 目的: 取り直しで見つからない issue を、印から外して手放すこと。
// 与える情報: turn の途中でカンバンから消える issue。
// 成功条件: 印から外れ、**worktree は残る**こと（人間が成果を見られる）。
func Test_issueを1件処理する_P003_turnの終わりにissueが消えていたら手放す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	// **1回目の `agent.prompt` は、`Stop` を流すまで返させない**（`blockFirstPrompt`）。
	// 返った瞬間から `claude.settle_ms`（この fixture では 50ms）の時計が走り出し、
	// **遅い機械では準備が終わる前に run を諦めてしまう。**
	releasePrompt := blockFirstPrompt(t, fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **カンバンから消す。**取り直しは「見つからない」を返す。
	fx.Tracker.RemoveIssue("PVTI_item188")

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\n\nCONTINUO-STATUS: review", false),
	})
	fx.Orc.OnHook(stopEvent(fx.Sessions[0], path, "p1"))
	// **`Stop` を積んでから返す。**ここから turn の終わりの判定が始まる。
	releasePrompt()

	// **手放したことをログで確かめる。**手放した run は印に残したままバックオフへ入る
	// （設計 3-21）ので、**印から外れたかどうかでは確かめられない。**
	waitFor(t, 20*time.Second, "手放したことがログに出る", func() bool {
		return strings.Contains(fx.Logs.String(), "カンバンから見えなくなりました")
	})
}

// {"RUCM-PATH": "P034"}
//
// Test_issueを1件処理する_P034_paneを引けなければ着手しない は、段8 の pane の解決の失敗を確かめる。
//
// 目的: `pane.list` が失敗したら着手を止めること。
// 与える情報: 常に失敗する `pane.list`。
// 成功条件: agent を起動しないこと。
func Test_issueを1件処理する_P034_paneを引けなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodPaneList, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "pane の一覧を取れません"}
	})

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 0 {
		t.Errorf("pane を引けないのに agent を起動している: %d 回", got)
	}
}

// {"RUCM-PATH": "P035"}
//
// Test_issueを1件処理する_P035_before_runが失敗したら新しく開いたpaneを閉じる は、
// 着手の途中の失敗の後始末を確かめる。
//
// 目的: `worktree.open` が pane を新しく開いたあと、`agent.start` より前で着手が失敗したら、
// **その pane を閉じる**ことを示す。**閉じないと、Claude Code の居ないシェルの pane が herdr に残る。**
//
// 与える情報: 必ず失敗する `workspace_hooks.before_run`。新規の着手（worktree も workspace も無い）。
// 成功条件:
//   - Status が `failure_state` になる
//   - `pane.close` がちょうど1回呼ばれ、相手は `worktree.open` が返した pane である
//   - agent を起動していない
func Test_issueを1件処理する_P035_before_runが失敗したら新しく開いたpaneを閉じる(t *testing.T) {
	fail := "exit 1"
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.BeforeRun = &fail },
	})
	fx.AllowLog("着手に失敗しました", "before_run")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	fx.WaitRunsDrained(t, 10*time.Second)

	closed := closedPaneIDs(fx)
	if len(closed) != 1 {
		t.Fatalf("pane.close が1回ではない: %v", closed)
	}
	opened := fx.Herdr.ParamsOf(t, herdr.MethodWorktreeOpen)
	if path, _ := opened["path"].(string); path == "" {
		t.Fatalf("worktree.open を呼んでいない")
	}
	if !strings.HasSuffix(closed[0], ":p1") {
		t.Errorf("閉じた pane が worktree.open の返した pane ではない: %q", closed[0])
	}
	if got := fx.Herdr.CountMethod(herdr.MethodAgentStart); got != 0 {
		t.Errorf("before_run が失敗したのに agent を起動している: %d 回", got)
	}
}

// {"RUCM-PATH": "P035"}
//
// Test_issueを1件処理する_P035_既に開いていたworkspaceのpaneは着手に失敗しても閉じない は、
// 着手の途中の失敗の後始末が、人間の pane に手を出さないことを確かめる。
//
// 目的: `worktree.open` が「既に開いていた」と答えた workspace の pane は、**着手に失敗しても閉じない**
// ことを示す。**その pane は人間が開いたものでありうる。**
//
// 与える情報: 必ず失敗する `workspace_hooks.before_run`。`already_open` を真で返す `worktree.open`。
// 成功条件: Status が `failure_state` になり、`pane.close` を1回も呼ばないこと。
func Test_issueを1件処理する_P035_既に開いていたworkspaceのpaneは着手に失敗しても閉じない(t *testing.T) {
	fail := "exit 1"
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.BeforeRun = &fail },
	})
	fx.AllowLog("着手に失敗しました", "before_run")
	open := fx.Herdr.HandlerOf(herdr.MethodWorktreeOpen)
	fx.Herdr.Handle(herdr.MethodWorktreeOpen, func(params map[string]any) (any, *rpcErr) {
		res, rerr := open(params)
		if m, ok := res.(map[string]any); ok {
			m["already_open"] = true
		}
		return res, rerr
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	fx.WaitRunsDrained(t, 10*time.Second)

	if closed := closedPaneIDs(fx); len(closed) != 0 {
		t.Errorf("既に開いていた workspace の pane を閉じている: %v", closed)
	}
}

// {"RUCM-PATH": "P044"}
//
// TestExternalFailure_Statusを書けなくても着手を続ける は、段2 の失敗の扱いを確かめる。
//
// **Status を書けないことは、着手を諦める理由になる。**
// 書けないまま worktree を作ると、**カンバンからは Ready のままに見えるのに実体が動く。**
// 次の巡回で二重に着手される。
//
// 目的: `UpdateStatus` が失敗したら worktree を作らないこと。
// 与える情報: 常に失敗する `UpdateStatus`。
// 成功条件: worktree を開かないこと。
func Test_issueを1件処理する_P044_Statusを書けなければworktreeを作らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetUpdateError(errors.New("Status を書けません"))

	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("Status を書けないのに worktree を開いている: %d 回", got)
	}
}

// {"RUCM-PATH": "P061"}
//
// Test_issueを1件処理する_P061_カンバンを読めなくても巡回は止まらない は、候補の取得の失敗を確かめる。
//
// 目的: `FetchIssuesByStates` が失敗しても、continuo が落ちないこと。
// 与える情報: 常に失敗する候補の取得。
// 成功条件: 巡回が返り、**worktree も pane も作らない**こと。
func Test_issueを1件処理する_P061_カンバンを読めなくても巡回は止まらない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.SetStatesError(errors.New("GitHub へ繋がりません"))

	// **落ちないことを確かめる。**panic すればここで止まる。
	fx.Orc.Tick(context.Background())

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("カンバンを読めないのに worktree を開いている: %d 回", got)
	}
}

// {"RUCM-PATH": "P061"}
//
// Test_issueを1件処理する_P061_Statusの選択肢が食い違ったら着手しない は、起動時検査の失敗を確かめる。
//
// **人間がカンバンの Status の選択肢を改名することがある。**
// **設定と食い違ったまま着手すると、continuo は存在しない選択肢へ書こうとして毎回失敗する。**
//
// 目的: 選択肢の照合に失敗したら、その巡回では着手しないこと。
// 与える情報: 常に失敗する `VerifyStatusOptions`。
// 成功条件: worktree を開かず、Status も動かさないこと。
func Test_issueを1件処理する_P061_Statusの選択肢が食い違ったら着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetVerifyError(errors.New("Status の選択肢名が設定と一致しません"))

	fx.Orc.Tick(context.Background())
	time.Sleep(1500 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("選択肢が食い違っているのに worktree を開いている: %d 回", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("着手していないのに Status を動かしている: %s", got)
	}
}

// {"RUCM-PATH": "P056"}
//
// Test_issueを1件処理する_P056_未信頼なら着手せず承認を促すコメントを1件書く は、段0 の信頼の検査を確かめる。
//
// 目的: 信頼登録されていないリポジトリの issue に着手しないこと。
// **そのまま黙って飛ばすと、人間は「なぜ動かないのか」を知る手がかりを持たない**ので、
// issue へ直し方を1件だけ書く。
// 与える情報: 信頼登録していないリポジトリ（`Untrusted`）。
// 成功条件: Status が動かず、worktree も開かず、issue にコメントが1件だけ付くこと。
func Test_issueを1件処理する_P056_未信頼なら着手せず承認を促すコメントを1件書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "承認を促すコメントが付く", func() bool {
		return len(fx.Tracker.CommentsOf("I_node188")) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("着手していないのに Status を動かしている: %s", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("未信頼なのに worktree を開いている: %d 回", got)
	}
	var body strings.Builder
	for _, c := range fx.Tracker.CommentsOf("I_node188") {
		body.WriteString(c.Body)
	}
	if !strings.Contains(body.String(), "continuo trust") {
		t.Errorf("直し方（continuo trust）を書いていない: %s", body.String())
	}
}

// {"RUCM-PATH": "P051"}
//
// Test_issueを1件処理する_P051_登録の無い実体があるならStatusを1バイトも書かずに飛ばす は、
// 段0 の worktree の検査を確かめる。
//
// **この検査が段3（worktree の用意）にあると、必ず失敗する着手でも先に running_state を
// 書いてしまう。**running_state は active_states なので次の巡回でまた候補に上がり、
// running_state と failure_state の往復が永久に続く。
//
// 目的: 目的のパスに実体があるのに git の worktree として登録されていないとき、
// **Status を1バイトも書かずに** その issue を飛ばすこと。
// 与える情報: 目的のパスに、git に登録されていないディレクトリを先に置く。
// 成功条件: UpdateStatus が1回も呼ばれず、Status が Ready のままで、
// worktree も開かれず、印も残らないこと。
func Test_issueを1件処理する_P051_登録の無い実体があるならStatusを1バイトも書かずに飛ばす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **git の登録を持たない実体を、目的のパスへ先に置く。**
	// 人間が手で作ったディレクトリや、消し損ねた残骸がこの形になる。
	unregistered := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if err := os.MkdirAll(unregistered, 0o700); err != nil {
		t.Fatalf("登録の無い実体を置けません: %v", err)
	}
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.ResetCalls()

	fx.Orc.Tick(context.Background())

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Errorf("着手できないと分かっているのに Status を書いている: UpdateStatus を %d 回呼んだ", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("Status が動いている: got %q, want Ready", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("着手できないのに worktree を開いている: %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が残っている: %d 件", got)
	}
}

// {"RUCM-PATH": "P052"}
//
// Test_issueを1件処理する_P052_branchを別のworktreeが使っているならStatusを1バイトも書かずに飛ばす は、
// 段0 の branch の検査を確かめる。
//
// **目的のパスには何も無い。**それでも `git worktree add <目的のパス> <branch>` は
// `fatal: '<branch>' is already used by worktree at '<別のパス>'` で必ず落ちる。
// **実機で1件通して初めて出た経路である**（設計 3-16b）。目的のパスだけを見る検査では
// 拾えないので、running_state を書いてから着手が落ち、
// running_state と failure_state の往復が始まっていた。
//
// 目的: 目的の branch を別の場所の worktree が使っているとき、
// **UpdateStatus を1回も呼ばずに** その issue を飛ばすこと。
// 与える情報: 置き場所の外に、同じ branch を出す worktree を1つ作っておく。
// 成功条件: UpdateStatus が1回も呼ばれず、Status が Ready のままで、
// 目的のパスも作られず、印も残らないこと。
func Test_issueを1件処理する_P052_branchを別のworktreeが使っているならStatusを1バイトも書かずに飛ばす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **置き場所の外**に、同じ branch を出す worktree を作る。
	// 前の run の worktree が別の場所に残っている状態や、人間が手で切った状態がこれである。
	elsewhere := filepath.Join(t.TempDir(), "別の場所")
	runGit(t, fx.Repo.Dir, "worktree", "add", "-b", "continuo/octocat/hello-world/188",
		elsewhere, fx.Repo.Base)

	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.ResetCalls()

	fx.Orc.Tick(context.Background())

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Errorf("着手できないと分かっているのに Status を書いている: UpdateStatus を %d 回呼んだ", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("Status が動いている: got %q, want Ready", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("着手できないのに worktree を開いている: %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が残っている: %d 件", got)
	}
	target := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("飛ばしたはずなのに目的のパスを作っている: %v", err)
	}
}

// {"RUCM-PATH": "P055"}
//
// Test_issueを1件処理する_P055_1つでも欠けたら着手しない は、絞り込みが効くことを確かめる。
//
// 目的: `required_labels` に並べたラベルを**全部**持っている issue だけに着手すること。
// 与える情報: 必須2つのうち1つしか持たない issue。
// 成功条件: **worktree も pane も作らない**（herdr を1回も叩かない）。
func Test_issueを1件処理する_P055_1つでも欠けたら着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.RequiredLabels = []string{"bug", "ready-for-ai"}
		},
	})
	fx.Tracker.AddIssue(issueWithLabels(188, "bug"))

	fx.Orc.Tick(context.Background())

	// **「起きない」ことを確かめるので、起きるだけの時間を与えてから見る。**
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("必須ラベルが欠けているのに worktree を開いている: %d 回", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("Status を動かしている: %s", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する は、設計 3-3b の「再着手」側を
// 確かめる。
//
// 目的: 「**既存の worktree を再利用していて、その身元ファイルに `SessionUUID` が入って
// いるなら、それを `--resume` に渡す。新しい UUID を採番しない。身元ファイルの
// `session_uuid` も変えない**」を示す。
//
// **送る本文は1回目の本文（5-3）のままである。**`In Review` から `In Progress` へ
// 戻される場面では人間が PR にレビューを書いており、**「issue を読むこと」「紐づく PR も
// 読むこと」が入っているのは1回目の本文だけだからである。**
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `In Progress` の issue 1件。エージェントは1回目の turn で `CONTINUO-STATUS: review` を
// 書いてコメントを残し、turn を終える。
// 成功条件:
//   - `agent.start` の起動フラグが `--resume sess-188` であり、`--session-id` が無い
//   - 身元ファイルの `session_uuid` が `sess-188` のまま変わっていない
//   - 送られた本文が1回目の本文（5-3）である
//   - 基本フローの事後条件（Status・コメント・pane・印・worktree と branch）が揃う
func Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	prompts := finishRunOnPrompt(t, fx, issue, "sess-188")
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "再着手で turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	if resumes[0] != "sess-188" {
		t.Fatalf("前回のセッションへ復帰していない: --resume=%q, want %q", resumes[0], "sess-188")
	}
	if starts[0] != "" {
		t.Fatalf("復帰するのに新しい UUID を採番している: --session-id=%q", starts[0])
	}
	if got := identitySessionUUID(t, fx, 188); got != "sess-188" {
		t.Fatalf("身元ファイルの session_uuid が書き換わっている: got %q, want %q", got, "sess-188")
	}

	// **復帰しても1回目の本文（5-3）を送る。**
	got := prompts()[0]
	if !strings.Contains(got, "gh issue view") || !strings.Contains(got, "octocat/hello-world#188") {
		t.Fatalf("復帰した run に1回目の本文（5-3）を送っていない: %q", got)
	}
	if strings.Contains(got, "続けてください") {
		t.Fatalf("復帰した run に継続の指示（5-4）だけを送っている（新しいレビューを読ませられない）: %q", got)
	}

	waitFor(t, 10*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
	assertBasicFlowPostcondition(t, fx, issue)
}

// {"RUCM-PATH": "P029"}
//
// Test_issueを1件処理する_P029_復帰に失敗したら新しいセッションで始め直す は、代替フロー `復帰の失敗` を確かめる。
//
// 目的: 「**`--resume` に渡した UUID のセッションが、もう存在しないことがある**
// （`~/.claude/projects/` の中身は利用者が消せる）。そのとき新しいセッションで
// 起動し直す」を示す。**ここで諦めると、利用者が履歴を消しただけで issue が
// `failure_state` へ落ちる。**
//
// **実測（2026-08-26）。**`claude --resume <無い UUID>` は終了コード 1 で、標準エラーに
// `No conversation found with session ID: <UUID>` を出して終わる。herdr 経由だと
// `agent.start` が `timeout: timed out waiting for agent startup` を返し、pane は
// シェルのプロンプトへ戻る（**同じ pane で、そのまま起動し直せる**）。台本はこれを再現する。
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `--resume` つきの `agent.start` だけが `timeout` を返す herdr。
// 成功条件（代替フロー `復帰の失敗` の事後条件をそのまま見る）:
//   - 1回目の起動が `--resume sess-188` で、`--session-id` が1つも入っていない
//   - 立て直しの起動は別の `agent.start` で、`--session-id` が新しい UUID であり、
//     **`--resume` が1つも残っていない**（残ると捨てたはずの会話へまた戻る）
//   - 身元ファイルの `session_uuid` が新しい UUID へ書き直されている
//     （書き直さないと、次の再着手も同じ死んだ UUID へ復帰しにいく）
//   - hook の引き当ての索引が張り替わっていて、前回のセッション UUID を名乗る hook は
//     どの run のものでもないとして捨てられる
//   - herdr の pane は開いたままである（閉じると立て直しの起動先が無くなる）
//   - issue の Status は running_state のままである（`failure_state` へ落とさない）
//   - 印は残っている
//   - worktree は残っている
func Test_issueを1件処理する_P029_復帰に失敗したら新しいセッションで始め直す(t *testing.T) {
	// **記録の根は、このテスト専用にする**（`sessionTranscriptDir` の説明）。
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	fx.AllowLog("前回のセッションへ復帰できなかったので")
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	wt := prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	// **記録を置いてから走らせる。**置かないと着手の段5b が「会話の記録が無い」と判定し、
	// **`--resume` を1回も渡さないので、この代替フローの分岐元そのものが起きない**（設計 3-3c）。
	// **ここで再現したいのは「記録はあるのに復帰できない」場合である**
	// （pane がまだシェルを起動しきっていない、など）。
	seeded := seedSessionTranscript(t, fx, "sess-188", []any{
		typedUserLine("p0", "前回の1行"),
		assistantLine("req0", "作業中です", false),
	})
	fx.Tracker.AddIssue(issue)

	var mu sync.Mutex
	resumeAttempts := 0
	base := fx.Herdr.HandlerOf(herdr.MethodAgentStart)
	if base == nil {
		t.Fatalf("agent.start の既定の台本が入っていない")
	}
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		args, _ := params["args"].([]any)
		resuming := false
		for _, item := range args {
			if s, _ := item.(string); s == "--resume" {
				resuming = true
				break
			}
		}
		if resuming {
			mu.Lock()
			resumeAttempts++
			mu.Unlock()
			return nil, &rpcErr{Code: herdr.ErrCodeTimeout, Message: "timed out waiting for agent startup"}
		}
		return base(params)
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "始め直したセッションで turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	mu.Lock()
	attempts := resumeAttempts
	mu.Unlock()
	if attempts == 0 {
		t.Fatalf("そもそも --resume を試していない")
	}

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) < 2 {
		t.Fatalf("agent.start が2回に届いていない（復帰と立て直しで2回のはず）: %v", starts)
	}
	// **1回目の起動が、代替フロー `復帰の失敗` の分岐元である。**
	// 死んだ UUID へ復帰しにいったことを、ここで見る。
	if resumes[0] != "sess-188" {
		t.Fatalf("1回目の起動が身元ファイルの UUID へ復帰していない: --resume=%q, want %q", resumes[0], "sess-188")
	}
	if starts[0] != "" {
		t.Fatalf("復帰の起動に --session-id が混ざっている: %q", starts[0])
	}

	// **立て直しの起動は、1回目とは別の `agent.start` である**
	// （事後条件「立て直しの起動はまだ1回も呼んでいない」の裏返しである）。
	// **`--resume` と `--session-id` は排他なので**（`internal/orchestrator/settings.go` の
	// `claudeStartArgs`）、`--session-id` が入っていること自体が
	// 「会話履歴を1文字も読まない起動である」ことを意味する。
	fresh := ""
	freshAt := -1
	for i, id := range starts {
		if id != "" {
			fresh = id
			freshAt = i
			break
		}
	}
	if fresh == "" {
		t.Fatalf("復帰に失敗したあと --session-id つきで起動し直していない: %v", starts)
	}
	// **立て直しの起動には `--resume` が1つも残っていない。**
	// **残ったままだと、`復帰の失敗` が捨てたはずの会話へまた戻りにいく。**
	if resumes[freshAt] != "" {
		t.Fatalf("立て直しの起動に --resume が残っている: --resume=%q (starts=%v, resumes=%v)",
			resumes[freshAt], starts, resumes)
	}
	if fresh == "sess-188" {
		t.Fatalf("死んだセッションの UUID を --session-id に使い回している: %q", fresh)
	}
	if got := identitySessionUUID(t, fx, 188); got != fresh {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の再着手もまた復帰を試みる）: got %q, want %q",
			got, fresh)
	}

	// 代替フロー `復帰の失敗` の残りの事後条件。
	//
	// **hook の引き当ての索引の張り替えを見る。**張り替えていないと、pane に残った前の
	// Claude Code が前回のセッション UUID を名乗って Stop hook を送り、
	// **立て直した run の turn が別の会話の transcript で終わる。**
	//
	// **同じ名前の記録を2つ作らない。**根の下に `sess-188.jsonl` が2つあると、
	// 着手の段5b がどちらを見つけるかは `os.ReadDir` の並び順（ディレクトリ名）で決まり、
	// **`os.MkdirTemp` の付ける接尾辞は実行のたびに変わる。**上で置いた1つを書き直す。
	stale := writeTranscript(t, filepath.Dir(seeded), "sess-188.jsonl", []any{
		typedUserLine("p-stale", "前のセッションの1行"),
		assistantLine("req-stale", "CONTINUO-STATUS: review", false),
	})
	if fx.Orc.OnHook(stopEvent("sess-188", stale, "p-stale")) {
		t.Errorf("前回のセッション UUID を名乗る hook を、まだこの run のものとして受けている")
	}
	// **pane は開いたままである。**閉じてしまうと、立て直しの起動先そのものが無くなる。
	if paneID := paneIDOf(t, fx, issue); closedPane(fx, paneID) {
		t.Errorf("立て直しの前に pane を閉じている: pane_id=%s", paneID)
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "In Progress" {
		t.Errorf("Status が running_state のままになっていない: got %q, want %q", got, "In Progress")
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("印が残っていない: %d 件（1 件のはず）", got)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Errorf("worktree が残っていない: %s (err=%v)", wt.Path, err)
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_issueを1件処理する_P015_worktreeの外のcwdを名乗るhookは捨てる は、送り主の突き合わせを確かめる。
//
// 目的: `session_id` はプロセスの引数に載るので他の run のエージェントから読める。
// **`cwd` がその run の worktree の外なら、その hook を捨てる**ことを示す。
// 与える情報: worktree のパスを持つ run と、まったく別の `cwd` を名乗る `Stop` hook。
// 成功条件: 警告を出して hook を捨てる。
func Test_issueを1件処理する_P015_worktreeの外のcwdを名乗るhookは捨てる(t *testing.T) {
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

// {"RUCM-PATH": "P021"}
//
// Test_issueを1件処理する_P021_turnループを起こせなかったらNeedsPromptを立て直す は、設計 3-8 を確かめる。
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
func Test_issueを1件処理する_P021_turnループを起こせなかったらNeedsPromptを立て直す(t *testing.T) {
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

// {"RUCM-PATH": "P030"}
//
// Test_issueを1件処理する_P030_paneがまだ使えないなら待ち直す は、「pane が起動を受け付ける」の検査から出る
// 代替フロー「paneがまだ使えない」を検査する。
//
// **`worktree.open` が作った pane は、シェルの起動が終わるまでコマンドを受け取れない。**
// herdr はそれを `agent_pane_busy`（`is not an available shell`）で返す。
//
// 目的: `agent_pane_busy` を受けても諦めず、pane が受け付けるまで待ち直すこと。
// 与える情報: 最初の2回だけ `agent_pane_busy` を返し、3回目から成功する `agent.start` の台本。
// 成功条件（RUCM の POSTCONDITION）: pane が起動を受け付けるまで待ち続けている。
// **ここでは「着手が最後まで進むこと」で確かめる**（待ち続けた結果、turn が送られる）。
func Test_issueを1件処理する_P030_paneがまだ使えないなら待ち直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	var starts atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		if starts.Add(1) <= 2 {
			return nil, &rpcErr{Code: "agent_pane_busy", Message: "agent target pane is not an available shell"}
		}
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": params["name"], "agent_status": "idle",
				"interactive_ready": true, "pane_id": params["pane_id"],
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "待ち直した末に turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := starts.Load(); got < 3 {
		t.Errorf("agent_pane_busy を受けて待ち直していない: agent.start の回数 %d", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Errorf("待てば通るのに人間へ渡している: state=%s", got)
	}
}

// {"RUCM-PATH": "P031"}
//
// Test_issueを1件処理する_P031_paneが使えないまま期限を過ぎたら人間へ渡す は、代替フロー「paneの断念」を検査する。
//
// 目的: pane が最後まで使えなければ、`failure_state` へ落として理由を書くこと。
// 与える情報: `agent.start` が常に `agent_pane_busy` を返す台本と、粘りの上限を短くした設定。
// 成功条件（RUCM の POSTCONDITION）: issue の Status は `failure_state` の選択肢である。
// Claude Code は起動していない。worktree は残っている。
func Test_issueを1件処理する_P031_paneが使えないまま期限を過ぎたら人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		// **リトライを1回で使い切らせる。**既定（3回）のままだと、バックオフの合計が
		// テストの待ち時間を超える。
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxRetries = 0 },
	})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Herdr.Handle(herdr.MethodAgentStart, func(_ map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "agent_pane_busy", Message: "agent target pane is not an available shell"}
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 60*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Errorf("起動していないのに turn を送っている: %v", fx.Herdr.Methods())
	}
	worktreePath := worktreePathOf(t, fx, issue)
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("起動に失敗しても worktree は残すこと: %s (err=%v)", worktreePath, err)
	}
}

// {"RUCM-PATH": "P023"}
//
// Test_issueを1件処理する_P023_入力を受け付けられるまで待ち直す は、「agent_status が idle か done で
// interactive_ready が真」の検査から出る代替フロー「起動の待ち直し」を検査する。
//
// **`agent.start` は起動が終わるのを待たずに返る。**返った直後の `agent_status` は
// `unknown` で、`idle` になったあとも数秒は `interactive_ready` が偽である
// （2026-08-21 に実測。設計 6-2 の表）。**この間に指示を送ると `agent_not_ready` で弾かれる。**
//
// 目的: `interactive_ready` が真になるまで待ってから turn を送ること。
// 与える情報: `unknown` → `idle`（ready=false）→ `idle`（ready=true）と変わる `agent.get` の台本。
// 成功条件（RUCM の POSTCONDITION）: 入力を受け付けられるようになるまで待ち続け、
// **そのあいだ turn の本文を送らない。**
func Test_issueを1件処理する_P023_入力を受け付けられるまで待ち直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	var gets atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		n := gets.Add(1)
		status, ready := "idle", true
		switch {
		case n == 1:
			status, ready = "unknown", false
		case n <= 3:
			// **`idle` でも `interactive_ready` が偽の時間がある。**ここが本題である。
			ready = false
		}
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": status, "interactive_ready": ready,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "ready になってから turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	if got := gets.Load(); got < 4 {
		t.Errorf("interactive_ready が偽のうちに進んでいる: agent.get の回数 %d", got)
	}
}

// {"RUCM-PATH": "P024"}
//
// Test_issueを1件処理する_P024_入力を受け付けないまま期限を過ぎたら人間へ渡す は、代替フロー「起動の断念」を検査する。
//
// 目的: `herdr.startup_timeout_ms` を過ぎても入力を受け付けなければ、
// `agent.max_retries` まで試したうえで `failure_state` へ落とすこと。
// 与える情報: `agent.get` が常に `idle` かつ `interactive_ready: false` を返す台本と、
// 短い `startup_timeout_ms`、リトライ 0 回の設定。
// 成功条件（RUCM の POSTCONDITION）: issue の Status は `failure_state` の選択肢である。
// **turn の本文は Claude Code に届いていない。**worktree は残っている。
func Test_issueを1件処理する_P024_入力を受け付けないまま期限を過ぎたら人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Herdr.StartupTimeoutMs = 1500
			cfg.Agent.MaxRetries = 0
		},
	})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{
				"name": params["target"], "agent_status": "idle", "interactive_ready": false,
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 60*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	if fx.Herdr.CountMethod(herdr.MethodAgentPrompt) != 0 {
		t.Errorf("入力を受け付けないのに turn を送っている: %v", fx.Herdr.Methods())
	}
	worktreePath := worktreePathOf(t, fx, issue)
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("起動に失敗しても worktree は残すこと: %s (err=%v)", worktreePath, err)
	}
}

// {"RUCM-PATH": "P054"}
//
// Test_issueを1件処理する_P054_上限まで着手したらそれ以上着手しない は、全体の上限を確かめる。
//
// 目的: `max_concurrent_agents` を超えて dispatch しないこと。
// 与える情報: 上限 2 の設定と、Ready の issue 3件。
// 成功条件: **2件だけが着手される**（3件目は Ready のまま）。
func Test_issueを1件処理する_P054_上限まで着手したらそれ以上着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Agent.MaxConcurrentAgents = 2 },
	})
	for _, n := range []int{188, 189, 190} {
		fx.Tracker.AddIssue(sampleIssue(n, "Ready"))
	}
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "2件が着手される", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) >= 2
	})
	// **3件目が着手されないことを確かめるので、着手しうる時間を与えてから見る。**
	time.Sleep(2 * time.Second)
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got > 2 {
		t.Errorf("上限 2 を越えて着手している: turn を %d 回送った", got)
	}
	running := 0
	for _, n := range []int{188, 189, 190} {
		if fx.Tracker.StateOf(fmt.Sprintf("PVTI_item%d", n)) == "In Progress" {
			running++
		}
	}
	if running != 2 {
		t.Errorf("In Progress の件数が上限と合わない: %d 件", running)
	}
}

// {"RUCM-PATH": "P012"}
//
// Test_issueを1件処理する_P012_止まったまま閾値を超えたら打ち切る は、打ち切りの条件を確かめる。
//
// 目的: **`agent_status` が `working` でないまま `claude.turn_timeout_ms` 経ったら打ち切る**
// （issue #173。[docs/spec/turn_end_detect_mechanizm.md](../../../docs/spec/turn_end_detect_mechanizm.md) の 4-1）。
//
// **`working` を打ち切ってはならない。**この検査は 2026-09-08 に前提を入れ替えた。
// **それまでは「`working` でも `revision`（画面の版）が増えなければ打ち切る」を固定していた。**
// **その版は画面を1バイトも見ておらず、continuo の pane では永久に動かない**
// （実測で、働いている3つの pane が2分間ずっと `revision: 1` だった）。
// **つまり、あの検査は「長いツール呼び出しの run を必ず殺す」ことを固定していた。**
//
// 与える情報: `agent_status` が `idle` のまま動かない run。
// 成功条件: 最初に閾値をまたいだ巡回でリトライが1つ積まれ、pane が閉じられる。
//
// **実時間はゼロである。**
func Test_issueを1件処理する_P012_止まったまま閾値を超えたら打ち切る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusIdle,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 閾値の手前では打ち切らない。
		time.Sleep(stallTimeout - time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "octocat/hello-world#188")
		if !ok {
			t.Fatalf("閾値の手前で run を印から外している")
		}
		if v.RetryCount != 0 {
			t.Fatalf("閾値の手前で打ち切っている: retry_count = %d", v.RetryCount)
		}

		// 閾値をまたいだら打ち切る。**猶予を1回与えて待ち直してはならない。**
		time.Sleep(2 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok = viewOf(fx, "octocat/hello-world#188")
		if !ok {
			t.Fatalf("バックオフ中の run を印から外している（30秒後の巡回で即座に拾い直される）")
		}
		if v.RetryCount != 1 {
			t.Fatalf("止まったまま閾値を超えたのにリトライを積んでいない: retry_count = %d", v.RetryCount)
		}
		if v.BackoffUntil.IsZero() {
			t.Fatalf("バックオフの期限を入れていない")
		}
		if len(fx.Herdr.ClosedPanes()) == 0 {
			t.Fatalf("stall で止めたのに pane を閉じていない（pane.close が唯一の手段である）")
		}
	})
}

// {"RUCM-PATH": "P009"}
//
// Test_issueを1件処理する_P009_background_tasksが空のStopだけでturnの終わりと判定しない は、
// turn の終わりの判定の要を確かめる。
//
// 目的: 設計 1-3 の実測「空の `Stop` 20件のうち4件が turn の途中だった」を踏まえ、
// 設計 3-2 の「`background_tasks` が空配列 → `settle_ms` のあいだ待ち、
// `<task-notification>` が来なければ turn の終わりとする。**来たら turn は続いている**」を
// 守っていることを示す。
//
// 与える情報: 1回目の `agent.prompt` のあとに、空の `Stop` → `<task-notification>` の順で
// hook が届く。**しばらく2つ目の `Stop` は来ない。**
// 成功条件: 1つ目の空の `Stop` では run が終わらず、2つ目の空の `Stop`（`<task-notification>`
// が続かない）を受けて初めて終わる。
func Test_issueを1件処理する_P009_background_tasksが空のStopだけでturnの終わりと判定しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// 途中の `Stop`（空配列）と、その直後の `<task-notification>`（実測で 0.033〜0.037 秒後）。
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		fx.Orc.OnHook(taskNotificationEvent("session-1", "t1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// settle_ms（50ms）の何倍も待っても、まだ終わっていないこと。
	time.Sleep(500 * time.Millisecond)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("空の Stop だけで turn の終わりと判定してしまった（設計 1-3 の実測 4/20 に反する）")
	}

	// 2つ目の `Stop`（今度は `<task-notification>` が続かない）。
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "2つ目の Stop で turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// {"RUCM-PATH": "P020"}
//
// Test_issueを1件処理する_P020_max_dispatch_turnsに達したらfailure_stateへ落とす は、打ち切りを確かめる。
//
// 目的: 設計 3-8 の「打ち切り: max_dispatch_turns に達したら failure_state へ落とす」と、
// 設計 3-14 の「turn は continuo が送った回数だけで数える」を守っていることを示す。
// 与える情報: `max_dispatch_turns` が1。1回目の turn で表明を書かず、Status は `In Progress` のまま。
// 成功条件: 2回目の turn を送らずに Status が `Blocked` へ落ちる。
func Test_issueを1件処理する_P020_max_dispatch_turnsに達したらfailure_stateへ落とす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "作業を進めています。", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)
	// **pane.close は Status の書き込みと同時ではない。**finishRunClaimed は
	// Status を書いたあと、引き渡しのコメント・エージェントのコメントの確認・after_run を
	// 通してから stopWorker を呼ぶ（設計 3-25 の「worker を止める前にコメントを確かめる」）。
	// Status だけを待って直後に検査すると、負荷が高いときに間に合わずに落ちる。
	waitFor(t, 20*time.Second, "worker が止まる（pane.close）", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})
	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got > 2 {
		// 段7（コメントを書かせ直す）で1回だけ余分に送ることがあるが、
		// **turn として2回目を送ってはならない。**
		t.Fatalf("max_dispatch_turns を超えて turn を送っている: agent.prompt が %d 回", got)
	}
}

// {"RUCM-PATH": "P016"}
//
// Test_issueを1件処理する_P016_blockedが返ったらescを送ってから人間へ渡す は、安全に関わる分岐を確かめる。
//
// 目的: 設計 3-11 の「`blocked` が返ったとき、そのまま次のプロンプトを投げると、保留中の
// 権限要求が承認されて実行される（3/3 で再現）」を防ぐことを示す。
// 与える情報: 1回目の `agent.prompt` が `blocked` を返す台本。
// 成功条件: **次のプロンプトを投げる前に `agent.send_keys` で `["esc"]` が送られ、
// さらに `pane.close` で worker が止まっている**（そのあとコメントを書かせ直すために
// セッションを復元して送るのは、保留中の要求が消えたあとなので安全である。設計 3-25 の9段）。
// Status は `failure_state` へ落ちる。
func Test_issueを1件処理する_P016_blockedが返ったらescを送ってから人間へ渡す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "Status が failure_state へ落ちる", func() bool {
		return fx.Tracker.StateOf("PVTI_item188") == "Blocked"
	})
	// **後始末まで待つ。**Status は worker を止める前に書かれる（helpers_test.go の WaitRunsDrained）。
	fx.WaitRunsDrained(t, 10*time.Second)

	methods := fx.Herdr.Methods()
	escIdx, closeIdx, firstPrompt, secondPrompt := -1, -1, -1, -1
	for i, m := range methods {
		switch m {
		case herdr.MethodAgentSendKeys:
			if escIdx < 0 {
				escIdx = i
			}
		case herdr.MethodPaneClose:
			if closeIdx < 0 {
				closeIdx = i
			}
		case herdr.MethodAgentPrompt:
			if firstPrompt < 0 {
				firstPrompt = i
			} else if secondPrompt < 0 {
				secondPrompt = i
			}
		}
	}
	if escIdx < 0 {
		t.Fatalf("blocked なのに esc を送っていない: %v", methods)
	}
	if closeIdx < 0 {
		t.Fatalf("人間へ渡すときに worker を止めていない: %v", methods)
	}
	if escIdx < firstPrompt {
		t.Fatalf("esc を送った順番が想定と違う: %v", methods)
	}
	if secondPrompt >= 0 && !(escIdx < secondPrompt && closeIdx < secondPrompt) {
		t.Fatalf("esc と pane.close より前に次のプロンプトを投げている"+
			"（保留中の権限要求が承認されて実行される）: %v", methods)
	}
	keysParams := fx.Herdr.ParamsOf(t, herdr.MethodAgentSendKeys)
	keys, _ := keysParams["keys"].([]any)
	if len(keys) != 1 || keys[0] != "esc" {
		t.Fatalf("送ったキーが想定と違う: got %v, want [esc]", keys)
	}
}

// {"RUCM-PATH": "P018"}
//
// Test_issueを1件処理する_P018_一時的な送信の失敗ではpaneを閉じない は、
// **一時的な失敗と、送信そのものを断られたときとで、後始末が正反対である**ことを確かめる。
//
// 目的: 設計 3-48。`送信の失敗` は pane を閉じてリトライを積むが、`一時的な送信の失敗` は
// **何も閉じず、何も積まない**（RUCM「issue を1件処理する」の事後条件
// 「印は残っている。リトライの回数は増えていない。herdr の pane は閉じていない」）。
// **pane を閉じると、その中で動いている Claude Code が turn の途中で消える。**
// herdr が一瞬落ちただけなのに、エージェントの作業がそこで失われる。
//
// 与える情報: `agent.prompt` を受けたところで応答を書かずに接続を切るテスト用herdr mock
// （herdr の再起動そのものである）。リトライは 0 回。
// 成功条件: `pane.close` が1度も呼ばれず、run が印に残り、Status が `In Progress` のまま
// であること。
func Test_issueを1件処理する_P018_一時的な送信の失敗ではpaneを閉じない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Agent.MaxRetries = 0
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	fx.Herdr.DropConnection(herdr.MethodAgentPrompt)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.AllowLog("herdr へ届かなかったので", "herdr との通信が一時的に失敗した")

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "turn の送信が herdr へ届く", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
	// **run を捨てる実装なら、ここで pane を閉じるところまで走り切る。**走り切らせてから見る。
	time.Sleep(2 * time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodPaneClose); got != 0 {
		t.Errorf("herdr が一瞬落ちただけで pane を閉じた: pane.close が %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("印から run が外れた: %d 件（1 件のはず）", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("Status を動かした: got %q, want In Progress", got)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_issueを1件処理する_P013_待ち受けが返ってもStopHookが来なければ打ち切る は、
// 代替フロー「turnの終わりの取りこぼし」を検査する。
//
// 目的: 設計 3-2 / 3-40 の「**待ち受けが返ったあとに Stop hook が来なかったこと**だけが
// 『Stop hook が届かなかった』と言ってよい場所である」を示す。
// **巡回の停滞の検知（`claude.turn_timeout_ms` の沈黙）とは別の経路である。**
// あちらは hook の無音と `agent_status` で測るが、こちらは待ち受けが返った直後の `settle_ms` だけを見る。
//
// 与える情報: `agent.prompt` は `idle` で返るのに、Stop hook が1件も届かない。
// 成功条件: 「turn が終わったことを検知できませんでした」を理由にリトライを1つ積み、
// **Status は running_state のままで、印にも残る**（`failure_state` へは落とさない）。
func Test_issueを1件処理する_P013_待ち受けが返ってもStopHookが来なければ打ち切る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 20*time.Second, "Stop hook が来ないまま打ち切られる", func() bool {
		return strings.Contains(fx.Logs.String(), "run を諦めてリトライを積みました")
	})

	if !strings.Contains(fx.Logs.String(), "turn が終わったことを検知できませんでした") {
		t.Fatalf("Stop hook が届かなかったことを理由にしていない:\n%s", fx.Logs.String())
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("リトライが残っているのに Status を動かした: got %q, want In Progress", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 1 {
		t.Errorf("バックオフ中も印には残すはずが外れている: %d 件（1 件のはず）", got)
	}
}

// {"RUCM-PATH": "P008"}
//
// Test_issueを1件処理する_P008_差し戻して書き直している間はturnの終わりとしない は、#166 の欠陥そのものを押さえる。
//
// 目的: 設計 3-79 の「空の `Stop` は『turn が終わった』ではなく『止まってよいか hook に
// 尋ねた』である」を守っていることを示す。**`Stop` hook が `{"decision":"block"}` を返すと、
// Claude Code は turn を終わらせずに応答を書き直すが、その差し戻しは continuo に届かない。**
//
// **1つ上の TestTurn_空のStopのあとに来た走行中のStopも捨てない と同じ形の欠陥である。**
// あちらは Claude Code 自身が「まだ動いています」と申告してくる場合で、こちらは
// **誰も申告してこない**場合である。だから並べて置いてある。
//
// 与える情報: 空の `Stop` が1件届く。**`settle_ms` のあいだ、それ以上は何も届かない。**
// そのとき herdr は `working` を返す（差し戻された応答を書き直している最中である）。
// transcript には差し戻された側の応答A（`CONTINUO-STATUS: review`）だけが在る。
// 成功条件: `settle_ms` を何倍も過ぎても run が生きており、Status が `In Review` へ
// 動いておらず、次の指示も送られていないこと。書き直しが終わったあとに応答Bの表明
// （`CONTINUO-STATUS: working` ＝ Status を動かさない）が採られること。
func Test_issueを1件処理する_P008_差し戻して書き直している間はturnの終わりとしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		// **settle_ms を広げるのは、テストを通すためではない。**既定の 50ms では
		// 「窓が閉じる前に裏取りした」のか「たまたま間に合った」のかを区別できない。
		cfg.Claude.SettleMs = 300
		// **poll_wait_ms も一緒に広げる。**fixture の既定は 200ms で、settle_ms のほうが
		// 長くなる。**その大小関係は internal/config/validate.go の
		// 「claude.settle_ms は claude.poll_wait_ms 以下にすること」が起動時に弾くので、
		// 利用者の手元では絶対に起きない。**弾かれる設定でテストを走らせると、
		// **待ち直しを settle_ms で刻んでいるのか poll_wait_ms で刻んでいるのかも測れない。**
		cfg.Claude.PollWaitMs = 5000
	}})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	// 応答A。**差し戻される側である。**ここで Status を動かすと pane を閉じてしまう。
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	// **着手の段では `idle` を返させる。**起動の落ち着きを待つところ（`herdr.startup_timeout_ms`）
	// も同じ `agent.get` を読むので、最初から `working` にすると着手そのものが失敗する。
	script := &agentStatusScript{status: "idle"}
	script.Install(fx)

	var mu sync.Mutex
	var prompts int
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		first := prompts == 1
		mu.Unlock()
		if first {
			// 1回目の応答が差し戻され、書き直している最中である。
			script.Set("working")
		}
		// **hook から見えるのは「空の Stop」だけである。**差し戻しは hook の戻り値なので
		// continuo には飛んでこない。
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "空の Stop のあとに herdr へ裏取りして待ち直す", func() bool {
		return strings.Contains(fx.Logs.String(),
			"空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します")
	})

	// settle_ms（300ms）と poll_wait_ms（200ms）の何倍も待っても、まだ生きていること。
	time.Sleep(1 * time.Second)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("書き直している最中に turn を終わらせて pane を閉じた:\n%s", fx.Logs.String())
	}
	if state := fx.Tracker.StateOf("PVTI_item188"); state == "In Review" {
		t.Fatalf("差し戻された側の応答Aで Status を動かした: %q", state)
	}
	mu.Lock()
	sent := prompts
	mu.Unlock()
	if sent != 1 {
		t.Fatalf("書き直している最中に次の指示を送った: %d 回", sent)
	}

	// 書き直しが終わった。応答Bが transcript へ足され、herdr は idle に戻り、
	// 2本目の空の `Stop` が届く。
	writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
		assistantLine("req2", "書き直しました。まだ続けます。\nCONTINUO-STATUS: working", false),
	})
	script.Set("idle")
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "書き直しが終わったので次の指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return prompts >= 2
	})
	if state := fx.Tracker.StateOf("PVTI_item188"); state == "In Review" {
		t.Fatalf("応答Bの表明ではなく応答Aの表明が採られた: %q", state)
	}
}

// {"RUCM-PATH": "P010"}
//
// Test_issueを1件処理する_P010_まだ動いていると名乗ったStopを捨てない は、issue #77 の欠陥を塞ぐ。
//
// 目的: 設計 3-2 の「`background_tasks` が空でない → **まだ動いている。turn の終わりとしては
// 扱わない**」と、設計 1-7 の「待っていれば `background_tasks` が空の `Stop` が来る」を
// 守っていることを示す。
//
// **欠陥はこうだった。**空でない `Stop` は「空の `Stop`」ではないので読み捨てられ、
// `settle_ms`（既定2000ミリ秒）が過ぎたところで turn の終わりを検知できなかったとして
// pane を閉じていた。**「まだ動いています」と名乗ってきた2秒後に殺していた。**
//
// 与える情報: 1回目の `agent.prompt` のあとに、バックグラウンドの shell を1件載せた
// `Stop` が届く。**しばらく空の `Stop` は来ない。**
// 成功条件: run が生き続け、リトライも積まれないこと。そのあと空の `Stop` を受けて初めて
// turn が終わること。
func Test_issueを1件処理する_P010_まだ動いていると名乗ったStopを捨てない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **Claude Code 自身の「まだ動いています」という申告。**
		fx.Orc.OnHook(runningShellStopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// settle_ms（50ms）と poll_wait_ms（200ms）の何倍も待っても、まだ生きていること。
	time.Sleep(1 * time.Second)
	if len(fx.Orc.RunningIdentifiers()) == 0 {
		t.Fatalf("まだ動いていると名乗った Stop を受けたのに run を終わらせた:\n%s", fx.Logs.String())
	}
	if strings.Contains(fx.Logs.String(), "run を諦めてリトライを積みました") {
		t.Fatalf("まだ動いていると名乗った Stop を捨てて run を諦めた:\n%s", fx.Logs.String())
	}

	// バックグラウンド処理が終わり、空の `Stop` が来る。
	fx.Tracker.SetState("PVTI_item188", "Done")
	fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
	fx.Orc.OnHook(stopEvent("session-1", path, "p1"))

	waitFor(t, 10*time.Second, "空の Stop で turn が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})
}

// {"RUCM-PATH": "P004"}
//
// Test_issueを1件処理する_P004_既に同じ値なら記録を残さない は、書き込みが起きない経路を確かめる。
//
// 目的: 取り直した値が既に遷移先と同じなら、`UpdateStatus` は書き込みの mutation を
// 送らない。**カンバンは何も動いていないので、記録を書いてはならない。**
//
// 与える情報: 表明を反映する直前に、カンバンを遷移先と同じ `In Review` にしておく。
// 成功条件: Status を動かした記録が1件（着手のぶん）だけである。
func Test_issueを1件処理する_P004_既に同じ値なら記録を残さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		// エージェントが自分で gh を叩いて先に In Review へ動かしていた状況。
		fx.Tracker.SetState(issue.ID, "In Review")
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 30*time.Second)

	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 1 {
		t.Fatalf("書き込みを省いたのに記録を書いている: %d 件（着手の1件だけのはず）\n%+v",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
	if !strings.Contains(moves[0].Body, "Ready → In Progress") {
		t.Errorf("残っているのが着手の記録ではない:\n%s", moves[0].Body)
	}
}

// {"RUCM-PATH": "P002"}
//
// Test_issueを1件処理する_P002_20turn回しても記録は着手と終わりの2件だけ は、記録が増えすぎないことを確かめる。
//
// 目的: **turn ごとに Status が動くわけではない。**作業中の turn でエージェントが出す
// `working` は `status_signal_map` で null に対応づいており、書き込みが1回も起きない。
// **書き込みが起きなければ記録も出ない。**
//
// 与える情報: 19回 `working` を書き、20回目に `review` を書く transcript
// （`max_dispatch_turns` は既定の 20）。
// 成功条件: Status を動かした記録が2件だけである（着手の1件と終わりの1件）。
func Test_issueを1件処理する_P002_20turn回しても記録は着手と終わりの2件だけ(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	working := writeTranscript(t, transcriptDir, "working.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ続きがあります。\nCONTINUO-STATUS: working", false),
	})
	done := writeTranscript(t, transcriptDir, "done.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
	})

	const lastTurn = 20
	var mu sync.Mutex
	prompts := 0
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		prompts++
		n := prompts
		mu.Unlock()
		path := working
		if n >= lastTurn {
			fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
			path = done
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	fx.WaitRunsDrained(t, 60*time.Second)

	if got := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); got < lastTurn {
		t.Fatalf("turn が %d 回まで回っていない: %d 回", lastTurn, got)
	}
	moves := fx.Tracker.StatusMoveCommentsOf("I_node188")
	if len(moves) != 2 {
		t.Fatalf("Status を動かした記録が2件ではない: %d 件（%+v）",
			len(moves), fx.Tracker.CommentsOf("I_node188"))
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_issueを1件処理する_P007_担当が移っていたらturnの終わりで止める は、設計 3-77c を確かめる。
//
// 目的: 「1時間ごとに、走っている最中も issue の担当者を読み直す。担当が移っていれば、
// その turn の終わりで止まる」。
// 与える情報: 着手して走っている run と、その最中に担当者を別の機械へ書き換えたカンバン。
// 確かめ直す間隔は 1ms にしてある。
// 成功条件: turn の終わりで run が印から外れること。**Status は動かさないこと**
// （動かすと、新しい担当の機械が着手しようとしているカンバンを外された機械が書き換える）。
func Test_issueを1件処理する_P007_担当が移っていたらturnの終わりで止める(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **1回の turn のあいだに確かめ直す時刻が来るようにする。**
			cfg.Tracker.Provider.Handoff.RecheckIntervalMs = 1
		},
	})
	fx.AllowLog("担当が移ったので")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "続けます。\nCONTINUO-STATUS: working", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		// **turn が終わる前に、別の機械が担当を取り上げた状態にする。**
		fx.Tracker.SetAssignees("PVTI_item188", rivalLogin)
		fx.Tracker.AddCommentBy(issueNode(188), rivalLogin, handoff.FormatHold(handoff.Hold{
			Assignee: rivalLogin,
			Branch:   "continuo/octocat/hello-world/188", At: time.Now(),
		}), time.Now())
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "担当が移った run が止まる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	// **Status は動かさない。**着手のときに書いた `In Progress` のままであるべきである。
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("担当を外された機械がカンバンを書き換えている: Status が %q（In Progress のままであるべき）", got)
	}
}

// {"RUCM-PATH": "P026"}
//
// Test_issueを1件処理する_P026_hookが届いていれば起動していると扱う は、設計 3-80 の中心を検査する。
//
// 目的: `agent.get` が `agent_not_found` を返しても、**その run のセッションから
// 作業中の hook が届いていれば、continuo が pane を閉じず、
// 走っている turn へ1回目の指示も投げないこと。**
// 与える情報: `agent_not_found` を返しながら、**呼ばれるたびに**この run の
// `PreToolUse` を1件流す `agent.get` の台本。
// **流すのは `agent.start` より後である**（そこが証拠になる線である）。
// 成功条件（3つ）: issue が `failure_state`（`Blocked`）へ落ちず、`pane.close` が
// 1回も呼ばれず、**その時点で `agent.prompt` が1回も呼ばれていないこと。**
// **そのうえで、走っていた turn が終わったら1回目の指示が送られること。**
//
// **この3つは、run が終わる前の時点を見ている。**そのあと run は成果を書かせられずに
// 人間へ渡るので、末尾の `WaitRunsDrained` を過ぎた時点では
// `Status=Blocked` / `pane.close=2` / `agent.start=2` になる。
// **末尾へ `pane.close == 0` のような検査を足すと落ちる。**実装の欠陥ではない。
func Test_issueを1件処理する_P026_hookが届いていれば起動していると扱う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **これは想定して起こしている失敗である。**1回目の指示のあと `blocked` にして、
	// run を確実に終わらせている。
	fx.AllowLog("権限の確認で止まりました", "run を終えます")
	// **herdr がまだ登録していないので、turn の終わりの裏取り（設計 3-79）は答えを得られない。**
	// **それでよい。**あの裏取りは「読めなかったら従来どおり進む」と決めてあり、
	// **turn の終わりの判定そのものは hook（`Stop`）だけで足りている。**
	fx.AllowLog("turn の終わりの裏取りができませんでした")
	// **run の終わりで、コメントを書かせる復元が必ず ErrStartupBusy になる。**
	// 下の `agent.get` の handler は呼ばれるたびに hook を注ぎ込むので、
	// **復元が起動を確かめる `confirmStartup` から見ても、常に `since` より新しい hook がある。**
	// **これは設計どおりの動きで、このテストが確かめたいこと（hook が届いていれば起動と扱う）とは別である。**
	fx.AllowLog("復元した Claude Code が走っているので、コメントを書かせる指示は送れません")
	fx.Tracker.AddIssue(sampleIssue(235, "Ready"))

	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})

	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		// **作業中の Claude Code が hook を送ってくる場面である。**
		// **`agent.start` より後に届いたものだけが証拠になる。**
		fx.Orc.OnHook(toolHook("session-1", "PreToolUse"))
		return nil, agentNotFoundErr()
	})
	// **1回目の指示が送られたことを確かめたら、そこで run を終わらせる。**
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "起動の確認が「Claude Code は走っている」で終わる", func() bool {
		return strings.Contains(fx.Logs.String(), "1回目の指示を送らずに turn の終わりを待ちます")
	})

	// **ここまでで、走っている turn へ何もしていないことを確かめる。**
	if n := fx.Herdr.CountMethod(herdr.MethodAgentPrompt); n != 0 {
		t.Errorf("走っている turn へ1回目の指示を投げた: agent.prompt の回数 %d", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodPaneClose); n != 0 {
		t.Errorf("生きている Claude Code の pane を閉じた: pane.close の回数 %d", n)
	}
	if n := fx.Herdr.CountMethod(herdr.MethodAgentStart); n != 1 {
		t.Errorf("走っている Claude Code へ agent.start をやり直した: agent.start の回数 %d", n)
	}
	if got := fx.Tracker.StateOf("PVTI_item235"); got == "Blocked" {
		t.Errorf("hook が届いているのに人間へ渡している: state=%s", got)
	}

	// **走っていた turn が終わる。**turn ループは hook だけでこれを見分ける。
	fx.Orc.Tick(context.Background())
	fx.Orc.OnHook(stopEvent("session-1", parent, "p1"))
	waitFor(t, 20*time.Second, "turn の終わりのあとに1回目の指示が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **run が終わりきるのを待つ。**待たずに返すと、run が
	// `finishRunClaimed` の途中にいるまま `t.Cleanup` が context を切る。
	// **そこまで進めたかどうかが機械の速さで変わるので、出るログも変わる。**
	fx.WaitRunsDrained(t, 20*time.Second)
}

// {"RUCM-PATH": "P046"}
//
// Test_issueを1件処理する_P046_候補の写しでは自分が担当でも取り直して担当者にいなければ着手しない は、設計 3-27 を確かめる。
//
// 目的: 担当を手放した直後の issue に、同じ機械がもう1度着手しないこと。
// **着手すると、GitHub の上では担当者がいないので別の機械が入札して拾い、同じ branch で2台が動く。**
//
// 与える情報: カンバンの実体は担当者が0人なのに、候補の一覧には「担当はこの機械」という
// 古い写しが載っている issue（手放しが候補の取得のあとで担当者を外した状況）。
// 成功条件: 着手の直前の取り直しが走り、Status を書かず、run の登録が残らないこと。
func Test_issueを1件処理する_P046_候補の写しでは自分が担当でも取り直して担当者にいなければ着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	// **実体は `In Progress` で担当者0人。**手放しは Status を動かさない。
	fx.Tracker.AddIssue(sampleIssue(4201, "In Progress"))
	stale := sampleIssue(4201, "In Progress")
	stale.Assignees = []tracker.Assignee{{ID: "U_" + fakeViewerLogin, Login: fakeViewerLogin}}
	stale.AssigneeCount = 1
	fx.Tracker.SetExtraCandidates(stale)
	fx.AllowLog("この機械が担当者にいないので着手しません", "着手を取りやめました")

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "着手の直前の取り直しが走る", func() bool {
		return fx.Tracker.CountIDRefreshes() > 0
	})
	fx.WaitRunsDrained(t, 10*time.Second)

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Fatalf("担当者にいない issue の Status を書いた: UpdateStatus = %d 回, want 0", got)
	}
	if got := fx.Tracker.CountCall("AddAssignees"); got != 0 {
		t.Fatalf("この巡回で担当者を書いた: AddAssignees = %d 回, want 0（次の巡回で、いまの担当者で判定し直す）", got)
	}
	// **issue へも何も書かない**（実装レビュー2周目の LOW）。**担当は既に自分に無いので、
	// 後始末で担当者を外したり、`released` のコメントを出したりしてはならない。**
	// 別の機械が拾ったあとの issue へ「手放した」と書くことになる。
	if got := fx.Tracker.CountCall("RemoveAssignees"); got != 0 {
		t.Fatalf("担当が自分に無い issue の担当者を外しに行った: RemoveAssignees = %d 回, want 0", got)
	}
	if got := fx.Tracker.CommentsOf(stale.ID); len(got) != 0 {
		t.Fatalf("担当が自分に無い issue へコメントを書いた: %v", got)
	}
	if got := fx.Tracker.MarkedHandoffCommentsOf(stale.ID, config.HandoffReleasedMarker); len(got) != 0 {
		t.Fatalf("担当が自分に無い issue へ released を書いた: %v", got)
	}
}

// {"RUCM-PATH": "P053"}
//
// Test_issueを1件処理する_P053_マージンが先に効いて止まり使用率と閾値が出る は、出す1行の中身を確かめる
// （設計 3-77j。issue #173）。
//
// 目的: **新規着手が止まる使用率は `100 − マージン` である。**
// マージン10なら **90% から**である（`rate_limit.pause_above_percent` は消えた。issue #173）。
// **観測した使用率と、枠ごとの閾値の両方を出さないと、どちらの枠が原因かを読めない。**
//
// 与える情報: 1週間の枠が 92%。担当者のいない `Ready` の issue が1件。
// 成功条件: dispatch されず、使用率と閾値が1行に出ること。
func Test_issueを1件処理する_P053_マージンが先に効いて止まり使用率と閾値が出る(t *testing.T) {
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 30, "resets_at": nil, "severity": "normal"},
		{"kind": "weekly_all", "percent": 92, "resets_at": nil, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_MARGIN")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Tracker.Provider.Handoff.FiveHourMarginPercent = 10
			cfg.Tracker.Provider.Handoff.WeeklyMarginPercent = 10
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(193, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#193" {
			t.Fatalf("余裕値がマイナスなのに着手している: %+v", v)
		}
	}
	got := fx.Logs.String()
	for _, want := range []string{
		"余裕値が0以下",
		"1週間の枠の使用率=92",
		"5時間の枠の使用率=30",
		`1週間の枠の閾値="90% に達したら止まります"`,
		`5時間の枠の閾値="90% に達したら止まります"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("1行に %q が入っていない:\n%s", want, got)
		}
	}
}

// {"RUCM-PATH": "P053"}
//
// Test_issueを1件処理する_P053_枠を使い切っているときはquotaJSONを消す手順まで出す は、100% の機械への案内を確かめる
// （issue #173。実装レビュー5周目の MEDIUM）。
//
// 目的: **使用率100 では、マージンをどう書いても動き出さない。**
// マージンは 0〜99 に制限されているので（`internal/config/validate.go` の `validateHandoff`）、
// **余裕値は `100 − 100 − マージン` で必ず0以下になる。**
// **それなのに「2つのマージンを見てください」とだけ案内すると、
// 利用者はマージンを触って、効かないまま原因を探し続ける。**
//
// **この案内は一度実際に失われている。**消した `rate_limit.pause_above_percent` の判定が
// 持っていたものを、`logNewWorkBlocked` へ移し忘れていた（2026-09-29 に戻した）。
// **検査が無いと、同じことがもう一度起きても誰も気づかない。**
//
// 与える情報: 5時間の枠が 100% で、リセットは2時間後。担当者のいない `Ready` の issue が1件。
// 成功条件: 着手しないこと。**「マージンを下げても動き出しません」と
// 「quota.json を消し」の両方が、同じ1行に出ること。**
func Test_issueを1件処理する_P053_枠を使い切っているときはquotaJSONを消す手順まで出す(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
		{"kind": "weekly_all", "percent": 10, "resets_at": resetsAt, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_FULL_RETAKE")

	fx := newStubFixture(t, stubFixtureOptions{
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Tracker.Provider.Handoff.FiveHourMarginPercent = 10
			cfg.Tracker.Provider.Handoff.WeeklyMarginPercent = 10
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(194, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#194" {
			t.Fatalf("枠を使い切っているのに着手している: %+v", v)
		}
	}
	got := fx.Logs.String()
	for _, want := range []string{
		"枠を使い切っているので",
		"マージンを下げても動き出しません",
		"quota.json を消し",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("案内に %q が出ていない:\n%s", want, got)
		}
	}
	// **`**` のような markdown の強調を混ぜない**（実装レビュー5周目の LOW）。
	// **ログは平文で出るので、そのまま画面に出る。**
	if strings.Contains(got, "**マージンを下げても") {
		t.Fatalf("ログの本文に markdown の強調が混ざっている:\n%s", got)
	}
}

// {"RUCM-PATH": "P053"}
//
// Test_issueを1件処理する_P053_枠を読めない機械は入札しない は、設計 3-77 の「投稿しない条件」を確かめる。
//
// 目的: **読めないと使用率0（＝いちばん暇）に見え、必ず勝ってしまう。**だから黙る。
// 与える情報: 使用率を読む設定（`statusline`。issue #284）だが、ステータスラインの行が
// 1行も届いていない状態。trust.repositories は空（statusline取得は「使える clone が無い」で終わる）。
// 成功条件: 入札のコメントが1件も増えず、着手もしないこと。
func Test_issueを1件処理する_P053_枠を読めない機械は入札しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **使用率を読む設定にする。**行は1行も入れないので、保管値は空のままになる
			// （＝読めなかった状態）。
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	// **値が無いので巡回の最後に statusline取得を開き、使える clone が無いので WARN を出す。**
	// その状況はこのテストが作っている（trust.repositories を空にしている）。
	fx.AllowLog("使える clone が無い")
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 0 {
		t.Errorf("枠を読めないのに入札している: %d 件", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("枠を読めないのに着手している: %d 件", got)
	}
}

// {"RUCM-PATH": "P053"}
//
// 目的: 使用率の値が古くなったら、そこから先は入札しないことを確認する（設計 3-77i。issue #284）。
//
// **古い値で入札させない。**止まったセッションや statusline取得が届かない機械は、最後に
// 届いた「使用率 5%」を持ち続ける。**それで入札すると、正直に読めている機械に必ず勝つ。**
// 勝った機械は着手できないので、**その issue は誰にも進まない。**
// 新しさは「`rate_limits` を持つ新しい応答の行を最後に受けた時刻」から `refresh_interval_ms`
// までである（計画の「値の保管と読み方」）。
//
// 与える情報: 手で進める時計。ステータスラインから 5% の新しい応答の行を受けたあと、
// 巡回1回目で issue 188 に入札させる。時計を `refresh_interval_ms` より進め、
// 2回目の巡回の前に issue 189 を足す。trust.repositories は空（statusline取得は
// 「使える clone が無い」で終わる）。
// 成功条件: issue 188 には入札があり、**issue 189 には入札が1件も無い**こと。
func Test_issueを1件処理する_P053_値が古くなったら入札を止める(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			// **締め切りを待たせる。**待たせないと1回目の巡回で担当者になり、
			// スロットが埋まって2回目の候補を見なくなる。
			cfg.Tracker.Provider.Handoff.BidWindowMs = 3600000
		},
	})
	holdPrompt(fx)
	// **値が古くなると statusline取得を開き、使える clone が無いので WARN を出す。**
	// その状況はこのテストが作っている（trust.repositories を空にしている）。
	fx.AllowLog("使える clone が無い")
	feedFreshQuota(fx.Orc, "pane-a", clock.Now(), 5, 10)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 1 {
		t.Fatalf("値が新しいのに入札していない: %d 件", got)
	}

	// **新しさの上限を跨がせてから、次の候補を足す。**
	clock.Advance(time.Duration(fx.Config.RateLimit.RefreshIntervalMs)*time.Millisecond + time.Second)
	fx.Tracker.AddIssue(sampleIssue(189, "Ready"))
	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(189), config.HandoffBidMarker)); got != 0 {
		t.Errorf("値が古くなったのに古い写しで入札している: %d 件", got)
	}
}

// {"RUCM-PATH": "P053"}
//
// **新しい issue を取るかどうかの門を確かめる。**門は `issue を1件処理する` の代替フロー
// `枠の余裕なし` に在る（`レートリミットで待って再開する` は、走っている run の話で、この門を持たない）。
//
// Test_issueを1件処理する_P053_枠を読めなければ入札の要るissueには着手しない は、2つの門を1つに揃えたことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないとき、入札は「黙る」、新規 dispatch は「止めない」で逆を向いていた。**
// 入札が先に効くので後ろは一度も効かず、**ボードが1件も進まないのに出るのは `Debug` の1行だけ**
// だった。**判定を1つに揃え、既定の水準で理由を出すことを示す。**
//
// 与える情報: usage API が 500 を返す（枠を読めない）。担当者のいない `Ready` の issue が1件。
// 成功条件: その issue が dispatch されず、`Info` で理由が出ること。
func Test_issueを1件処理する_P053_枠を読めなければ入札の要るissueには着手しない(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_UNREADABLE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#190" {
			t.Fatalf("枠を読めないのに入札の要る issue へ着手している: %+v", v)
		}
	}
	// **止まったことが人間に見えなければ、直したことにならない。**
	got := fx.Logs.String()
	// **同じ1行に INFO と文面の両方があることを見る**（実装レビュー2周目の LOW）。
	// 別々に探すと、ほかの行の `level=INFO` で通ってしまう。
	infoLine := false
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "level=INFO") && strings.Contains(line, "枠を読めないので") {
			infoLine = true
			break
		}
	}
	if !infoLine {
		t.Fatalf("止めた理由を INFO で出していない:\n%s", got)
	}
	// **直し方を取り違えさせない。**枠を読めないのは資格情報の話であって、
	// **マージンをいくら下げても動き出さない。**
	if !strings.Contains(got, "マージンを下げても動き出しません") {
		t.Fatalf("枠を読めないときに、マージンでは直らないと書いていない:\n%s", got)
	}
}
