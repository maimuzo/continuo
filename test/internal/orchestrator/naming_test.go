package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// TestBuildAgentName_repoと番号からcontinuo接頭辞の名前を作る は、agent 名の1〜2段を確かめる。
//
// 目的: 設計 3-3 の「`continuo-<repo>-<番号>` を作り、小文字にし、英数字とハイフン以外を
// ハイフンに置き換え、連続するハイフンを1つにまとめる」を守っていることを示す。
// 与える情報: 大文字とアンダースコアを含む repo 名と issue 番号。
// 成功条件: 小文字のハイフン区切りになり、herdr のパターンを満たす。
func TestBuildAgentName_repoと番号からcontinuo接頭辞の名前を作る(t *testing.T) {
	got := orchestrator.BuildAgentName("", "Hello_World", 188)

	if got != "continuo-hello-world-188" {
		t.Fatalf("agent 名が想定と違う: got %q, want %q", got, "continuo-hello-world-188")
	}
	if err := herdr.ValidateAgentName(normalize.SafeName(got)); err != nil {
		t.Fatalf("作った agent 名が herdr のパターンを満たさない: %v", err)
	}
}

// TestBuildAgentName_idを付けたら名前に混ぜる は、`--id` を agent 名へ混ぜることを確かめる
// （設計 3-17b）。
//
// 目的: **別のボードの同じ番号の issue が、同じ agent 名になってはならない。**
// agent 名は「人間が端末で見分けるためのもの」なので（設計 3-3）、
// **見分けられないなら役割を果たしていない。**
// 与える情報: `--id e2e` と、`--id` を付けていない場合。
// 成功条件: 名前が分かれ、`e2e` が入り、herdr のパターンを満たすこと。
func TestBuildAgentName_idを付けたら名前に混ぜる(t *testing.T) {
	withID := orchestrator.BuildAgentName("e2e", "hello-world", 188)
	def := orchestrator.BuildAgentName("", "hello-world", 188)

	if withID == def {
		t.Fatalf("--id を付けても名前が分かれていない: %q", withID)
	}
	if withID != "continuo-e2e-hello-world-188" {
		t.Fatalf("agent 名が想定と違う: got %q, want %q", withID, "continuo-e2e-hello-world-188")
	}
	if err := herdr.ValidateAgentName(normalize.SafeName(withID)); err != nil {
		t.Fatalf("作った agent 名が herdr のパターンを満たさない: %v", err)
	}
}

// TestBuildAgentName_idが長くても32文字に収める は、herdr の上限を守ることを確かめる
// （設計 3-3 / 3-17b）。
//
// 目的: `--id` は32文字まで書ける（設計 3-17d）ので、**repo を全部削っても収まらないこと
// がある。****そのときは `--id` の名前も削る。**一意性は段4（`agent.list` を見て `-2`,
// `-3` を足す）が担うので、**この関数が守るのは上限だけである。**
// 与える情報: 32文字の `--id` と長い repo 名。
// 成功条件: 32文字以内に収まり、番号が残り、herdr のパターンを満たすこと。
func TestBuildAgentName_idが長くても32文字に収める(t *testing.T) {
	got := orchestrator.BuildAgentName(strings.Repeat("a", 32), "very-long-repository-name", 188)

	if len(got) > 32 {
		t.Fatalf("agent 名が32文字を超えている: got %q (%d 文字)", got, len(got))
	}
	if !strings.HasSuffix(got, "-188") {
		t.Fatalf("番号が削られている: got %q", got)
	}
	if err := herdr.ValidateAgentName(normalize.SafeName(got)); err != nil {
		t.Fatalf("作った agent 名が herdr のパターンを満たさない: %v", err)
	}
}

// TestBuildAgentName_32文字を超えるときは番号を残してrepoを削る は、agent 名の段3 を確かめる。
//
// 目的: 設計 3-3 の「32文字を超えていたら、repo の部分を後ろから1文字ずつ削って収める
// （**番号は削らない。**番号が消えると別の issue と同じ名前になりうる）」を示す。
// 与える情報: 32文字に収まらないほど長い repo 名。
// 成功条件: 32文字以内に収まり、末尾の `-188` が残っている。
func TestBuildAgentName_32文字を超えるときは番号を残してrepoを削る(t *testing.T) {
	got := orchestrator.BuildAgentName("", "very-long-repository-name-that-never-fits", 188)

	if len(got) > 32 {
		t.Fatalf("agent 名が32文字を超えている: got %q (%d 文字)", got, len(got))
	}
	if !strings.HasSuffix(got, "-188") {
		t.Fatalf("番号が削られている: got %q", got)
	}
	if !strings.HasPrefix(got, "continuo-") {
		t.Fatalf("接頭辞が落ちている: got %q", got)
	}
	if err := herdr.ValidateAgentName(normalize.SafeName(got)); err != nil {
		t.Fatalf("作った agent 名が herdr のパターンを満たさない: %v", err)
	}
}

// TestIssueSlug_識別子から設定ファイルの置き場所のスラグを作る は、スラグの規則を確かめる。
//
// 目的: 設計 3-2 の「英数字とハイフン以外を全部ハイフンに置き換え、連続するハイフンを
// 1つにまとめ、小文字にする」を守っていることを示す。**設定ファイルの置き場所（3-12）と
// 逃がし先（3-19）の両方で使う同じ規則である。**
// 与える情報: `octocat/hello-world#188`。
// 成功条件: `octocat-hello-world-188` になる。
func TestIssueSlug_識別子から設定ファイルの置き場所のスラグを作る(t *testing.T) {
	got := orchestrator.IssueSlug("octocat/hello-world#188")

	if got != "octocat-hello-world-188" {
		t.Fatalf("スラグが想定と違う: got %q, want %q", got, "octocat-hello-world-188")
	}
}

// TestBuildContinuationPrompt_残り回数を必ず入れる は、2回目以降のプロンプトを確かめる。
//
// 目的: 設計 5-4 の「毎回『続けてください。この確認は n 回目です。あと m 回で打ち切ります』
// を入れる」「毎回（無条件）権限で拒否された操作があれば blocked を出すよう促す」を示す。
// 与える情報: 3回目の turn、上限20回、表明あり（前回は表明があった）。
// 成功条件: 回数と残り回数と blocked の促しが入り、**1回目の本文は入らない**。
func TestBuildContinuationPrompt_残り回数を必ず入れる(t *testing.T) {
	got := orchestrator.BuildContinuationPrompt(3, 20, false, "In Progress", signalPrefix)

	if !strings.Contains(got, "3 回目") {
		t.Fatalf("何回目かが入っていない: %q", got)
	}
	if !strings.Contains(got, "17 回で打ち切ります") {
		t.Fatalf("残り回数が入っていない: %q", got)
	}
	if !strings.Contains(got, "権限で拒否された操作") {
		t.Fatalf("権限の拒否を書かせる1文が入っていない: %q", got)
	}
	if strings.Contains(got, "gh issue view") {
		t.Fatalf("1回目の本文を送り直している（設計 5-4 / SPEC.md 7.1 に反する）: %q", got)
	}
}

// TestBuildContinuationPrompt_表明が無かった次のturnで促す は、第3層の差し込みを確かめる。
//
// 目的: 設計 3-25 の「表明せずに終わったら、次の turn で促す」（hook から差し戻す仕組みは
// 採らない）を守っていることを示す。
// 与える情報: 前回の turn に表明が無かった状態。
// 成功条件: 「Status がまだ In Progress のままです」と印の名前が入る。
func TestBuildContinuationPrompt_表明が無かった次のturnで促す(t *testing.T) {
	got := orchestrator.BuildContinuationPrompt(2, 20, true, "In Progress", signalPrefix)

	if !strings.Contains(got, "In Progress のままです") {
		t.Fatalf("表明を促す1文が入っていない: %q", got)
	}
	if !strings.Contains(got, signalPrefix) {
		t.Fatalf("表明の印の名前が入っていない: %q", got)
	}
}

// TestDispatch_agent名が重複したら末尾に連番を付ける は、agent 名の段4 を確かめる。
//
// 目的: 設計 3-3 の段4「herdr が名前の重複を拒否したら、末尾に `-2`、`-3` と付けて
// 空くまで試す（上限10回）」を示す。**起動を試みる前に `agent.list` で使用中の名前を
// 調べる**（起動してから拒否されると pane に半端な状態が残りうるため）。
//
// 与える情報: `agent.list` が `continuo-hello-world-188` を使用中として返す状態で、
// `octocat/hello-world#188` を dispatch する。
// 成功条件: `agent.start` に渡す名前が `continuo-hello-world-188-2` になる。
func TestDispatch_agent名が重複したら末尾に連番を付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	// 素の名前は既に別の agent が使っている。
	fx.Herdr.Handle(herdr.MethodAgentList, func(map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "agent_list",
			"agents": []any{
				map[string]any{"name": "continuo-hello-world-188", "agent_status": "working"},
			},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "agent が起動する", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentStart) > 0
	})

	params := fx.Herdr.ParamsOf(t, herdr.MethodAgentStart)
	if got, want := params["name"], "continuo-hello-world-188-2"; got != want {
		t.Fatalf("重複した agent 名に連番を付けていない: got %v, want %q", got, want)
	}
}
