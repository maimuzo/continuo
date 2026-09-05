// **「いまの Status を書いたのは誰か」を読む検査である**（設計 2-6 / 3-54。issue #33）。
//
// **`wasAutomated` は組み込みの自動化でも false を返す**（設計 2-6 の実測）。
// 見分けに使うのは `actor.__typename` であり、**`Bot` なら自動化、`User` なら人間か
// continuo 自身**である。ここではその読み取りと、絞り込み（自分のカンバン・いまの Status・
// いちばん新しいイベント）が効いていることを見る。
package tracker_test

import (
	"strings"
	"testing"
)

// itemWithStatusEvents は、timeline のイベントを持つ item 1件の byIDs 応答を組み立てる。
//
// status: いまの Status。
// events: timelineItems の nodes（statusEventJSON で作る）。
func itemWithStatusEvents(status string, events []map[string]any) map[string]any {
	return asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: status, Owner: "octocat", Repo: "hello-world",
		Number: 10, Title: "自動化に動かされた issue", StatusEvents: events,
	}))
}

// TestStatusAuthor_誰が書いたかの読み取り は、設計 2-6 / 3-54 を確かめる。
//
// 目的: **`wasAutomated` だけを見ると、組み込みの自動化を人間と取り違える。**
// 実測では `Pull request linked to issue` が動かしたときも `false` だった。
// **`actor.__typename` が `Bot` かどうかを見る。**あわせて `wasAutomated` も OR で混ぜる
// （同じ応答に載っているので費用が増えず、GitHub が将来直せば自動で効く）。
//
// 与える情報: 表のとおりの timeline を持つ item を、ID 指定で取り直す。
// 成功条件: `StatusChangedByAutomation` が期待どおりであること。
func TestStatusAuthor_誰が書いたかの読み取り(t *testing.T) {
	tests := []struct {
		name string
		// events は timelineItems の nodes。
		events []map[string]any
		// wantAutomated は「自動化が書いた」と判定されること。
		wantAutomated bool
		// wantLogin は Issue.StatusChangedBy に入る値。
		wantLogin string
	}{
		{
			name: "組み込みの自動化はBot型でwasAutomatedが偽",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:32:35Z", "In Progress",
					"Bot", "github-project-automation", false, 3),
			},
			wantAutomated: true,
			wantLogin:     "github-project-automation",
		},
		{
			name: "人間が動かしたものはUser型",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:32:35Z", "In Progress",
					"User", "octocat", false, 3),
			},
			wantAutomated: false,
			wantLogin:     "octocat",
		},
		{
			name: "wasAutomatedが真ならUser型でも自動化とみなす",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:32:35Z", "In Progress",
					"User", "some-app", true, 3),
			},
			wantAutomated: true,
			wantLogin:     "some-app",
		},
		{
			name: "actorが取れなくてもwasAutomatedだけで判定できる",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:32:35Z", "In Progress", "", "", true, 3),
			},
			wantAutomated: true,
			wantLogin:     "",
		},
		{
			name:          "イベントが1件も無ければ自動化ではないほうに倒す",
			events:        []map[string]any{},
			wantAutomated: false,
			wantLogin:     "",
		},
		{
			name: "別のカンバンのイベントは数えない",
			events: []map[string]any{
				// **1つの issue が複数のカンバンに載っていると、両方のイベントが同じ配列で返る**
				// （設計 2-6 の実測）。project.number で絞れていなければ、ここで自動化と誤判定する。
				statusEventJSON("2026-08-26T12:32:35Z", "In Progress",
					"Bot", "github-project-automation", false, 11),
			},
			wantAutomated: false,
			wantLogin:     "",
		},
		{
			name: "いまのStatusと違うイベントは数えない",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:00:00Z", "Ready",
					"Bot", "github-project-automation", false, 3),
			},
			wantAutomated: false,
			wantLogin:     "",
		},
		{
			name: "同じStatusのイベントが並んだらいちばん新しいものを採る",
			events: []map[string]any{
				statusEventJSON("2026-08-26T12:00:00Z", "In Progress",
					"Bot", "github-project-automation", false, 3),
				// **あとから人間が同じ Status へ動かし直した。**
				// 古いほうを採ると、人間の操作を自動化と読み違える。
				statusEventJSON("2026-08-26T12:40:00Z", "In Progress",
					"User", "octocat", false, 3),
			},
			wantAutomated: false,
			wantLogin:     "octocat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeGraphQLServer(t, single(dataResponse(
				byIDsPayload([]any{itemWithStatusEvents("In Progress", tt.events)}))))
			a := newAdapterForFetch(t, fs)

			issues, err := a.FetchIssuesByIDs(t.Context(), []string{"item-1"})
			if err != nil {
				t.Fatalf("ID 指定の取り直しが失敗した: %v", err)
			}
			if len(issues) != 1 {
				t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
			}
			if got := issues[0].StatusChangedByAutomation; got != tt.wantAutomated {
				t.Errorf("自動化が書いたかの判定が違う: got %v, want %v", got, tt.wantAutomated)
			}
			if got := issues[0].StatusChangedBy; got != tt.wantLogin {
				t.Errorf("書いた主体のログイン名が違う: got %q, want %q", got, tt.wantLogin)
			}
		})
	}
}

// TestStatusAuthor_timelineを要求するのはID指定の取り直しだけ は、設計 3-54 を確かめる。
//
// 目的: **timeline は1件ずつにしか意味が無い。**候補の取得は100件返るので、そこへ足すと
// ネストした connection が1本増え、**リクエストの点数だけが増える**（設計 3-31）。
//
// 与える情報: 候補の取得と ID 指定の取り直しを1回ずつ呼ぶ。
// 成功条件: ID 指定の取り直しのクエリにだけ `timelineItems` が入っていること。
func TestStatusAuthor_timelineを要求するのはID指定の取り直しだけ(t *testing.T) {
	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		if n == 1 {
			return dataResponse(candidateItemsPayload(nil, false, ""))
		}
		return dataResponse(byIDsPayload(nil))
	})
	a := newAdapterForFetch(t, fs)

	if _, err := a.FetchIssuesByStates(t.Context(), []string{"Ready"}); err != nil {
		t.Fatalf("候補の取得が失敗した: %v", err)
	}
	if _, err := a.FetchIssuesByIDs(t.Context(), []string{"item-1"}); err != nil {
		t.Fatalf("ID 指定の取り直しが失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) != 2 {
		t.Fatalf("リクエストの件数が想定と違う: got %d, want 2", len(reqs))
	}
	if strings.Contains(reqs[0].Query, "timelineItems") {
		t.Errorf("候補の取得のクエリに timelineItems が入っている（100件返る側なので足してはならない）:\n%s", reqs[0].Query)
	}
	if !strings.Contains(reqs[1].Query, "timelineItems") {
		t.Errorf("ID 指定の取り直しのクエリに timelineItems が入っていない:\n%s", reqs[1].Query)
	}
	for _, want := range []string{"wasAutomated", "__typename", "project"} {
		if !strings.Contains(reqs[1].Query, want) {
			t.Errorf("ID 指定の取り直しのクエリに %q が入っていない:\n%s", want, reqs[1].Query)
		}
	}
}

// TestStatusAuthor_記録を読まない取り直しはtimelineを要求しない は、設計 3-61 を確かめる。
//
// 目的: **ID 指定の取り直しは2本ある。**「いまの Status を書いたのは誰か」を読む
// 呼び出し元は6つのうち2つだけであり（実行中の run の照合と turn の終わりの取り直し）、
// **残る4つは `FetchIssuesByIDsWithoutTimeline` を通る。**
// **そこへ timeline をぶら下げたままにすると、使わない50件のイベントを、
// 着手のたび・巡回のたび・起動のたびに読むことになる**（設計 3-31）。
//
// 与える情報: 記録を取らない側で1件取り直す。
// 成功条件: 送ったクエリに `timelineItems` が入っておらず、それでも Status と識別子は
// ふつうに読めていて、記録の2つのフィールドだけがゼロ値であること。
func TestStatusAuthor_記録を読まない取り直しはtimelineを要求しない(t *testing.T) {
	// **timeline を持たない応答である。**要求していないものは返ってこない。
	item := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "In Progress", Owner: "octocat", Repo: "hello-world",
		Number: 10, Title: "記録を読まない取り直しの相手",
	}))
	fs := newFakeGraphQLServer(t, single(dataResponse(byIDsPayload([]any{item}))))
	a := newAdapterForFetch(t, fs)

	issues, err := a.FetchIssuesByIDsWithoutTimeline(t.Context(), []string{"item-1"})
	if err != nil {
		t.Fatalf("記録を取らない取り直しが失敗した: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("件数が想定と違う: got %d, want 1", len(issues))
	}

	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストの件数が想定と違う: got %d, want 1", len(reqs))
	}
	if strings.Contains(reqs[0].Query, "timelineItems") {
		t.Errorf("記録を読まない取り直しのクエリに timelineItems が入っている:\n%s", reqs[0].Query)
	}

	// **省くのは記録の2つだけである。**他が落ちていないことも同時に見る
	// （クエリを丸ごと軽くして、取り直しそのものが壊れていないか）。
	if got, want := issues[0].State, "In Progress"; got != want {
		t.Errorf("Status が読めていない: got %q, want %q", got, want)
	}
	if got, want := issues[0].Identifier, "octocat/hello-world#10"; got != want {
		t.Errorf("識別子が読めていない: got %q, want %q", got, want)
	}
	if issues[0].StatusChangedByAutomation {
		t.Errorf("記録を取らないはずなのに「自動化が書いた」が立っている")
	}
	if got := issues[0].StatusChangedBy; got != "" {
		t.Errorf("記録を取らないはずなのに書いた主体が入っている: got %q", got)
	}
}

// TestStatusAuthor_識別子での照合はtimelineを要求しない は、設計 3-54 を確かめる。
//
// 目的: 識別子での照合（`FetchIssueByIdentifier`）は**カンバンを丸ごと読む**（設計 3-25）。
// **候補の取得と同じ理由で、ここにも timeline を足してはならない。**
//
// 与える情報: 識別子で1件引く。
// 成功条件: 送ったクエリに `timelineItems` が入っていないこと。
func TestStatusAuthor_識別子での照合はtimelineを要求しない(t *testing.T) {
	fs := newFakeGraphQLServer(t, single(dataResponse(candidateItemsPayload(nil, false, ""))))
	a := newAdapterForFetch(t, fs)

	if _, _, err := a.FetchIssueByIdentifier(t.Context(), "octocat/hello-world#10"); err != nil {
		t.Fatalf("識別子での照合が失敗した: %v", err)
	}
	reqs := fs.Requests()
	if len(reqs) != 1 {
		t.Fatalf("リクエストの件数が想定と違う: got %d, want 1", len(reqs))
	}
	if strings.Contains(reqs[0].Query, "timelineItems") {
		t.Errorf("識別子での照合のクエリに timelineItems が入っている（カンバンを丸ごと読む側である）:\n%s", reqs[0].Query)
	}
}

// TestStatusAuthor_Statusを書く前の取り直しはtimelineを要求しない は、設計 3-54 を確かめる。
//
// 目的: **書き込みの経路は timeline を1バイトも読まない。**見るのは取り直した Status だけで、
// それを blockedStates と突き合わせるためである。**Status は turn ごと・巡回ごとに書くので、
// この経路がいちばん多く呼ばれる。**ネストした connection をぶら下げたままにすると、
// 使わない50件のイベントを書き込みのたびに読むことになる（設計 3-31）。
//
// 与える情報: Bootstrap のあと `UpdateStatus` を1回呼ぶ。
// 成功条件: 書く前の取り直しのクエリに `timelineItems` が入っていないこと。
func TestStatusAuthor_Statusを書く前の取り直しはtimelineを要求しない(t *testing.T) {
	refetched := asProjectV2ItemNode(issueItemJSON(testIssueItemOpts{
		ItemID: "item-1", Status: "In Progress", Owner: "octocat", Repo: "hello-world", Number: 1, Title: "t",
	}))
	fs := newFakeGraphQLServer(t, func(n int, _ capturedRequest) fakeGraphQLResponse {
		switch n {
		case 1:
			return dataResponse(bootstrapProjectPayload(testStatusOptions))
		case 2:
			return dataResponse(byIDsPayload([]any{refetched}))
		default:
			return dataResponse(map[string]any{
				"updateProjectV2ItemFieldValue": map[string]any{
					"projectV2Item": map[string]any{"id": "item-1"},
				},
			})
		}
	})
	a := newBootstrappedAdapter(t, fs)

	if _, err := a.UpdateStatus(t.Context(), "item-1", "In Review", []string{"Done"}); err != nil {
		t.Fatalf("UpdateStatus が失敗した: %v", err)
	}

	reqs := fs.Requests()
	if len(reqs) < 2 {
		t.Fatalf("リクエストの件数が想定と違う: got %d, want 2 以上", len(reqs))
	}
	if !strings.Contains(reqs[1].Query, "nodes(ids:") {
		t.Fatalf("2回目のリクエストが ID 指定の取り直しになっていない:\n%s", reqs[1].Query)
	}
	if strings.Contains(reqs[1].Query, "timelineItems") {
		t.Errorf("Status を書く前の取り直しに timelineItems が入っている（書き込みの経路は読まない）:\n%s", reqs[1].Query)
	}
}
