// {"RUCM-CFG-SHA256": "50921736c45c1f15ae5fa06edfb7c899bec55f753d79007d8dff567a6e84b1a8", "SOURCE": "docs/spec/usecases/particular_case/指示書の文面を組み立てて送る.cfg.json"}
//
// **ユースケース記述「指示書の文面を組み立てて送る」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
)

// {"RUCM-PATH": "P001"}
//
// 目的: 最初のメッセージの末尾に、閉じた記録より後に人間が書いたコメントを付けることを固定する。
//
// **付けないと、人間が issue のコメントで出した許可が、auto の判定役に届かない**（判定役は `gh` で読んだ
// コメントを道具の結果として取り除く）。
//
// 与える情報: 閉じた記録と、そのあとに OWNER が書いた許可を持つ `Ready` の issue。既定の設定（relay は有効）。
// 成功条件: 最初に送った本文に節の見出しと許可の本文が入り、組み込みの本文（1回目の本文）も残っていること。
func Test_指示書の文面を組み立てて送る_P001_最初のメッセージに境目より後の人間のコメントを付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(sampleIssue(301, "Ready"))
	seedRelayComments(fx, "I_node301")

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

	got := prompts()[0]
	for _, want := range []string{firstPromptMarker, relaySectionHeading, relayHumanGrant, "#issuecomment-102"} {
		if !strings.Contains(got, want) {
			t.Errorf("最初のメッセージに %q が無い:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Claude Code を閉じました") {
		t.Errorf("閉じた記録そのものを渡している:\n%s", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// 目的: コメントを読めないときと、期限を過ぎたときは、節を付けずに最初のメッセージだけを送ることを固定する。
//
// **読めないことで着手を止めない。**エラーで返すと、テンプレートの誤りと同じく `failRun` へ落ち、
// WORKFLOW.md を直すよう人間へ知らせてしまう。
//
// 与える情報: 上と同じ issue。relay 専用の読み取りが失敗する / 期限まで返らない（期限は 200 ミリ秒に縮める）。
// 成功条件: どちらも最初のメッセージが送られ、節が無く、1回目の本文は残っていること。
func Test_指示書の文面を組み立てて送る_P003_読めないときと期限切れのときは付けずに送る(t *testing.T) {
	for _, name := range []string{"読めない", "期限切れ"} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog("人間のコメントを付けずに最初のメッセージを送ります")
			prompts := recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(303, "Ready"))
			seedRelayComments(fx, "I_node303")
			if name == "読めない" {
				fx.Tracker.SetRelayError(errors.New("GraphQL が落ちた"))
			} else {
				release := fx.Tracker.HoldRelay()
				t.Cleanup(release)
				fx.Orc.SetRelayTimeoutForTest(200 * time.Millisecond)
			}

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

			got := prompts()[0]
			if strings.Contains(got, relaySectionHeading) {
				t.Errorf("読めなかったのに節を付けている:\n%s", got)
			}
			if !strings.Contains(got, firstPromptMarker) {
				t.Errorf("1回目の本文が送られていない:\n%s", got)
			}
		})
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: relay が無効なら、コメントを読まず、節も付けないことを固定する。
//
// 与える情報: 上と同じ issue。`agent.relay_trusted_comments: false` と、`permission_mode: dontAsk` の2通り。
// 成功条件: どちらも最初のメッセージに節が無く、relay 専用の読み取りが1回も呼ばれないこと。
func Test_指示書の文面を組み立てて送る_P005_relayが無効なら読まずに付けない(t *testing.T) {
	for name, mutate := range map[string]func(cfg *config.Config){
		"設定が false": func(cfg *config.Config) { cfg.Agent.RelayTrustedComments = false },
		"dontAsk":   func(cfg *config.Config) { cfg.Claude.PermissionMode = config.ClaudePermissionModeDontAsk },
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{Mutate: mutate})
			prompts := recordPrompts(fx)
			fx.Tracker.AddIssue(sampleIssue(302, "Ready"))
			seedRelayComments(fx, "I_node302")

			fx.Orc.Tick(context.Background())
			waitFor(t, 10*time.Second, "最初のメッセージが送られる", func() bool { return len(prompts()) > 0 })

			if strings.Contains(prompts()[0], relaySectionHeading) {
				t.Errorf("relay が無効なのに節を付けている:\n%s", prompts()[0])
			}
			if n := fx.Tracker.RelayCalls(); n != 0 {
				t.Errorf("relay が無効なのにコメントを読んでいる: %d 回", n)
			}
		})
	}
}
