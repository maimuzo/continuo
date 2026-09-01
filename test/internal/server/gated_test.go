package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// sampleGates は、着手できずに止まっているものの写しを2件返す。
//
// 戻り値: 理由の違う2件（人間が付けた担当と、担当者が2人以上）。
func sampleGates() []orchestrator.GateView {
	return []orchestrator.GateView{
		{
			Identifier: "octocat/hello-world#42",
			Title:      "担当者が付いたままの issue",
			URL:        "https://github.com/octocat/hello-world/issues/42",
			Reason:     orchestrator.GateReasonHumanAssigned,
			Assignees:  []string{"octocat-human"},
			Since:      testTime.Add(-40 * time.Minute),
			Noticed:    true,
		},
		{
			Identifier: "octocat/hello-world#43",
			Title:      "担当者が2人いる issue",
			URL:        "https://github.com/octocat/hello-world/issues/43",
			Reason:     orchestrator.GateReasonManyAssignees,
			Assignees:  []string{"octocat-human", "octocat-human-2"},
			Since:      testTime.Add(-10 * time.Minute),
		},
	}
}

// 目的: 着手できずに止まっているものが、issue・理由・いつから・直し方の4列で出ることを確認する
// （#134（ダッシュボードに「着手できずに止まっているもの」を出す））。
//
// **これがこの変更の成果物そのものである。**いままで手がかりはログの1行だけだった。
//
// 与える情報: 理由の違う2件の写し。
// 成功条件: 200 が返り、識別子・題名・理由・担当者・直し方が本文に含まれること。
func TestIndex_着手できずに止まっているものを出せる(t *testing.T) {
	s, _ := newTestServerWithGates(t, nil, sampleGates())
	code, body := get(t, s, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}

	for _, want := range []string{
		"着手できずに止まっているもの",
		"octocat/hello-world#42",
		"担当者が付いたままの issue",
		"担当者が付いています（octocat-human）",
		"GitHub の画面でその担当者を外してください。",
		"octocat/hello-world#43",
		"担当者が2人以上います（octocat-human, octocat-human-2）",
		"GitHub の画面で担当者を1人も付いていない状態にしてください。",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("画面に %q が出ていない", want)
		}
	}
}

// 目的: 1件も止まっていないときに、その旨の1行だけを出すことを確認する
// （#134（ダッシュボードに「着手できずに止まっているもの」を出す））。
//
// **表そのものを消さない。**消すと、機能が動いていないのか止まっているものが無いのかを
// 画面から区別できなくなる。
//
// 与える情報: 写しが1件も無い状態。
// 成功条件: 「着手できずに止まっているものはありません。」が出ること。
func TestIndex_着手できずに止まっているものが無ければ1行だけ出す(t *testing.T) {
	s, _ := newTestServerWithGates(t, nil, nil)
	code, body := get(t, s, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "着手できずに止まっているものはありません。") {
		t.Error("1件も無いときの1行が出ていない")
	}
	if !strings.Contains(body, "着手できずに止まっているもの") {
		t.Error("表の見出しが消えている（機能が動いているかを画面から判別できない）")
	}
}

// 目的: `Since` が同じ2件が、identifier の昇順に並ぶことを確認する（設計 10）。
//
// **`Since` 1本で並べてはならない。**同じ巡回で2件以上が同時に止まると値が同じになり、
// `sort.Slice` は安定ではないので、10秒ごとの再読み込みで行が入れ替わる。
//
// 与える情報: `Since` が同じで identifier の順が逆になっている3件。
// 成功条件: identifier の昇順に並ぶこと。
func TestIndex_止まった時刻が同じなら識別子の昇順に並べる(t *testing.T) {
	same := testTime.Add(-5 * time.Minute)
	gates := []orchestrator.GateView{
		{Identifier: "octocat/hello-world#9", Reason: orchestrator.GateReasonHumanAssigned, Since: same},
		{Identifier: "octocat/hello-world#12", Reason: orchestrator.GateReasonHumanAssigned, Since: same},
		{Identifier: "octocat/hello-world#1", Reason: orchestrator.GateReasonHumanAssigned, Since: same},
	}
	s, _ := newTestServerWithGates(t, nil, gates)

	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap struct {
		Gated []struct {
			Identifier string `json:"identifier"`
		} `json:"gated"`
	}
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を読めなかった: %v\n%s", err, body)
	}
	got := make([]string, 0, len(snap.Gated))
	for _, g := range snap.Gated {
		got = append(got, g.Identifier)
	}
	want := []string{"octocat/hello-world#1", "octocat/hello-world#12", "octocat/hello-world#9"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("並び順が違う: got %v, want %v", got, want)
	}
}

// 目的: 案内の状態ごとに、行へ添える印が変わることを確認する（設計 11）。
//
// **書き終えている行には印を出さない。**それが正常な状態だからである。
//
// 与える情報: 「書き終えた」「まだ書いていない」「設定で切ってある」
// 「コメントが多すぎる」「切り分けられない」の5件。
// 成功条件: 書き終えた1件には印が付かず、残る4件にそれぞれの印が付くこと。
func TestIndex_案内の状態ごとに印を変える(t *testing.T) {
	gates := []orchestrator.GateView{
		{Identifier: "octocat/hello-world#1", Reason: orchestrator.GateReasonHumanAssigned, Noticed: true},
		{Identifier: "octocat/hello-world#2", Reason: orchestrator.GateReasonHumanAssigned},
		{
			Identifier: "octocat/hello-world#3", Reason: orchestrator.GateReasonHumanAssigned,
			NoticeSkip: orchestrator.GateNoticeOffByConfig,
		},
		{
			Identifier: "octocat/hello-world#4", Reason: orchestrator.GateReasonHumanAssigned,
			NoticeSkip: orchestrator.GateNoticeTooManyComments,
		},
		{
			Identifier: "octocat/hello-world#5", Reason: orchestrator.GateReasonManyAssigneesWithSelf,
			NoticeSkip: orchestrator.GateNoticeUnclearOwner,
		},
	}
	s, _ := newTestServerWithGates(t, nil, gates)

	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap struct {
		Gated []struct {
			Identifier  string `json:"identifier"`
			NoticeBadge string `json:"notice_badge"`
		} `json:"gated"`
	}
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を読めなかった: %v\n%s", err, body)
	}
	want := map[string]string{
		"octocat/hello-world#1": "",
		"octocat/hello-world#2": "issue へは未通知",
		"octocat/hello-world#3": "issue へは書かない設定です",
		"octocat/hello-world#4": "コメントが多すぎて確かめられません",
		"octocat/hello-world#5": "別の機械の担当かどうかを切り分けられません",
	}
	for _, g := range snap.Gated {
		if got := g.NoticeBadge; got != want[g.Identifier] {
			t.Errorf("%s の印が違う: got %q, want %q", g.Identifier, got, want[g.Identifier])
		}
	}
}

// 目的: 知らない理由が来たら、機械が読む値をそのまま画面へ出すことを確認する（設計 11）。
//
// **`dashboard.none` へ落とさない。**落とすと `—` になり、
// 知らない理由が来たことが画面から読めなくなる。
//
// 与える情報: 対応表に無い理由の写し1件。
// 成功条件: 理由の文字列そのものが本文に出て、直し方が空であること。
func TestIndex_知らない理由はそのまま出す(t *testing.T) {
	gates := []orchestrator.GateView{{
		Identifier: "octocat/hello-world#7",
		Reason:     orchestrator.GateReason("まだ実装していない理由"),
		Since:      testTime,
	}}
	s, _ := newTestServerWithGates(t, nil, gates)

	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap struct {
		Gated []struct {
			ReasonText string `json:"reason_text"`
			Remedy     string `json:"remedy"`
		} `json:"gated"`
	}
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を読めなかった: %v\n%s", err, body)
	}
	if len(snap.Gated) != 1 {
		t.Fatalf("件数が違う: got %d, want 1", len(snap.Gated))
	}
	if snap.Gated[0].ReasonText != "まだ実装していない理由" {
		t.Errorf("知らない理由を握り潰している: got %q", snap.Gated[0].ReasonText)
	}
	if snap.Gated[0].Remedy != "" {
		t.Errorf("知らない理由に直し方を付けている: got %q", snap.Gated[0].Remedy)
	}
}
