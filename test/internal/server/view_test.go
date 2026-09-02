package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// 目的: まだ hook を1件も受けていない run（dispatch した直後）を、時刻ゼロのまま
// 出さないことを確認する。`0001-01-01T00:00:00Z` を見せると「1年に見える経過」になる。
// 与える情報: LastHookAt / StartedAt / TokensAt がゼロ値の run の写し。
// 成功条件: JSON では null になり、HTML では「—」と「未集計」になること。
func TestNewSnapshot_時刻が無い項目はnullにする(t *testing.T) {
	snap := server.NewSnapshot([]orchestrator.RunView{{
		Identifier: "octocat/hello-world#1",
		Title:      "着手した直後",
		State:      "In Progress",
	}}, nil, testTime)

	if len(snap.Runs) != 1 {
		t.Fatalf("run の件数が違う: got %d, want 1", len(snap.Runs))
	}
	run := snap.Runs[0]
	if run.LastHookAt != nil || run.StartedAt != nil || run.TokensAt != nil || run.BackoffUntil != nil {
		t.Errorf("ゼロ値の時刻が残っている: %+v", run)
	}
	if run.LastHookAgo != "—" {
		t.Errorf("経過の表示が違う: got %q, want %q", run.LastHookAgo, "—")
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("JSON にできなかった: %v", err)
	}
	if !strings.Contains(string(b), `"last_hook_at":null`) {
		t.Errorf("JSON で null になっていない: %s", b)
	}

	s, _ := newTestServer(t, []orchestrator.RunView{{
		Identifier: "octocat/hello-world#1",
		Title:      "着手した直後",
		State:      "In Progress",
	}})
	_, body := get(t, s, "GET", "/")
	if !strings.Contains(body, "未集計") {
		t.Error("トークンを一度も集計していないことが表示されていない")
	}
	if !strings.Contains(body, "まだ1件も受けていない") {
		t.Error("hook を1件も受けていないことが表示されていない")
	}
	if strings.Contains(body, "0001-01-01") {
		t.Error("ゼロ値の時刻がそのまま表示されている")
	}
}

// 目的: 最後に hook を受けてからの経過を、人間が一目で判断できる形で出すことを確認する。
// 絶対時刻だけでは、止まっているかどうかを頭の中で引き算しなければ分からない。
// 与える情報: いまから 5 秒前・90 秒前・2 時間前・未来（時計が巻き戻った場合）の run。
// 成功条件: それぞれ「5秒前」「1分30秒前」「2時間0分前」「0秒前」になること。
func TestNewSnapshot_最後にhookを受けてからの経過を出す(t *testing.T) {
	views := []orchestrator.RunView{
		{Identifier: "a", LastHookAt: testTime.Add(-5 * time.Second)},
		{Identifier: "b", LastHookAt: testTime.Add(-90 * time.Second)},
		{Identifier: "c", LastHookAt: testTime.Add(-2 * time.Hour)},
		{Identifier: "d", LastHookAt: testTime.Add(3 * time.Second)},
	}
	want := map[string]string{"a": "5秒前", "b": "1分30秒前", "c": "2時間0分前", "d": "0秒前"}

	snap := server.NewSnapshot(views, nil, testTime)
	for _, run := range snap.Runs {
		if run.LastHookAgo != want[run.Identifier] {
			t.Errorf("%s の経過が違う: got %q, want %q", run.Identifier, run.LastHookAgo, want[run.Identifier])
		}
	}
}

// 目的: run の写しの順序が不定でも、表示の並びが決まることを確認する。
// 並べ替えないと、再読み込みのたびに行が入れ替わって読めない。
// 与える情報: identifier が降順に並んだ写し。
// 成功条件: identifier の昇順になること。
func TestNewSnapshot_identifierの昇順に並べる(t *testing.T) {
	snap := server.NewSnapshot([]orchestrator.RunView{
		{Identifier: "octocat/hello-world#9"},
		{Identifier: "octocat/hello-world#12"},
		{Identifier: "octocat/hello-world#1"},
	}, nil, testTime)

	got := make([]string, 0, len(snap.Runs))
	for _, run := range snap.Runs {
		got = append(got, run.Identifier)
	}
	want := []string{"octocat/hello-world#1", "octocat/hello-world#12", "octocat/hello-world#9"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("並びが違う: got %v, want %v", got, want)
		}
	}
}
