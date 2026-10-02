// {"RUCM-CFG-SHA256": "88af2dd88bb56cbb1372300dd45f176349eb61d5316484d2dab41642846f5d83", "SOURCE": "docs/spec/usecases/scenario/夜に機械を落として翌朝に担当を続ける.cfg.json"}
//
// **ユースケース記述「夜に機械を落として翌朝に担当を続ける」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// {"RUCM-PATH": "P002"}
//
// Test_夜に機械を落として翌朝に担当を続ける_P002_会話の記録が無いセッションには復帰しない は、設計 3-3c の
// 「記録が無ければ新しいセッションで始める」を確かめる。
//
// 目的: 「**身元ファイルにセッション UUID が入っていても、その会話の記録が
// 記録の置き場所に1件も無ければ `--resume` を渡さない**」を示す。
//
// **なぜ要るか。**着手が段6 より先で落ちると、身元ファイルには**会話が1度も作られていない
// UUID** が残る（`restartWithNewSession` が採り直した UUID を書いたあとで、立て直しの
// `agent.start` も失敗した場合である）。**そのまま復帰しにいくと、
// `confirmStartupWithRestart` が `herdr.startup_timeout_ms` を使い切るまで
// `agent.start` をやり直し続ける**（利用者の実測で18回・約60秒）。
//
// **`Test_issueを1件処理する_P029_復帰に失敗したら新しいセッションで始め直す` との違い。**あちらは
// **記録はあるのに起動できなかった**場合で、`--resume` を1回投げてから立て直す。
// こちらは**投げる前に決める**ので、`agent.start` は最初から `--session-id` で1回だけである。
//
// **この検査には経路の印を付けない**（設計 6-18e）。通る経路は
// `Test_issueを1件処理する_P001_既存のworktreeがあれば前回のセッションに復帰する` と同じ P001 である。
// 起動フラグを決める段は条件ステップではないので CFG に枝が無く、
// **同じ印を2本に付けると、片方を消しても集計は満たされたままになる。**
//
// 与える情報: セッション UUID `sess-188` を書いた身元ファイルつきの worktree と、
// `In Progress` の issue 1件。**その UUID の会話の記録は1件も置かない。**
// 成功条件:
//   - `agent.start` の起動フラグに `--resume` が1つも入らない
//   - `--session-id` に新しい UUID が渡る（`sess-188` を使い回さない）
//   - **`agent.start` が1回で済んでいる**（空回りしていない）
//   - 身元ファイルの `session_uuid` が、その新しい UUID へ書き直されている
func Test_夜に機械を落として翌朝に担当を続ける_P002_会話の記録が無いセッションには復帰しない(t *testing.T) {
	// **記録の根は、このテスト専用にする。**既定のままだと、前の実行が残した
	// `sess-188.jsonl` を拾って「記録がある」と判定し、**この検査が黙って通る。**
	fx := newFixture(t, fixtureOptions{TranscriptRoot: t.TempDir()})
	prompts := recordPrompts(fx)

	issue := sampleIssue(188, "In Progress")
	prepareWorktree(t, fx, issue, identityOverride{SessionUUID: "sess-188"})
	// **記録は置かない。**着手が途中で落ちたあとの身元ファイルを再現している。
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "新しいセッションで turn が送られる", func() bool {
		return len(prompts()) > 0
	})

	starts := startSessionIDs(fx)
	resumes := startResumeUUIDs(fx)
	if len(starts) == 0 {
		t.Fatalf("agent.start が1度も呼ばれていない")
	}
	for i, uuid := range resumes {
		if uuid != "" {
			t.Fatalf("会話の記録が無いのに %d 回目の起動へ --resume を渡している: %q", i+1, uuid)
		}
	}
	if starts[0] == "" {
		t.Fatalf("新しい UUID を --session-id で渡していない: %v", starts)
	}
	if starts[0] == "sess-188" {
		t.Fatalf("記録の無いセッションの UUID を --session-id に使い回している: %q", starts[0])
	}
	// **空回りしていないことを、`agent.start` の回数で見る。**死んだ UUID へ復帰しにいくと、
	// `herdr.startup_timeout_ms` の中でやり直しが積み上がる。
	if len(starts) != 1 {
		t.Fatalf("agent.start が1回で済んでいない（空回りしている）: %d 回, %v", len(starts), starts)
	}
	if got := identitySessionUUID(t, fx, 188); got != starts[0] {
		t.Fatalf("身元ファイルの session_uuid を書き直していない（次の着手もまた復帰を試みる）: got %q, want %q",
			got, starts[0])
	}

	// **ログの3通り目を見る**（設計 3-3c）。この行と `記録の置き場所` の項目が、
	// **検査が黙って無効になったこと**（Claude Code が置き場所の形を変えた・
	// 根が別の場所を指している）**に気づける唯一の手がかりである。**
	// **落とすと、この再着手が「新しいセッションを立てて着手します」と名乗り、
	// 運用者から見て新規の着手と見分けが付かなくなる。**
	logs := fx.Logs.String()
	if !strings.Contains(logs, "身元ファイルのセッションへ復帰しないで、新しいセッションで始めます") {
		t.Errorf("復帰しなかったことを名乗る行が出ていない")
	}
	if !strings.Contains(logs, "記録の置き場所") {
		t.Errorf("探した場所がログに載っていない（検査が無効になったことに気づけない）")
	}
	if !strings.Contains(logs, "sess-188") {
		t.Errorf("復帰しなかったセッションの UUID がログに載っていない（改竄された値の証拠が残らない）")
	}
}
