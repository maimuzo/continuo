package doctor_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/tracker"
)

// TestDoctor_接続先がgithub_comならスキーマを照会しない は、github.com の利用者に
// 何も増えないことを確かめる。
//
// 目的: 設計 3-86。スキーマの照会は「GitHub Enterprise を使っている場合」だけに行う
// （人間の決定。issue #86）。github.com の利用者のリクエストを1本も増やさない。
// 与える情報: `tracker.provider.host` が既定（github.com）の設定。
// 成功条件: `カンバン` が `✓` で、偽カンバンが受け取ったクエリに照会が1本も無いこと。
func TestDoctor_接続先がgithub_comならスキーマを照会しない(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	if slices.Contains(fx.GitHub.Queries(), "schema") {
		t.Fatalf("github.com なのにスキーマを照会した: %v", fx.GitHub.Queries())
	}
}

// TestDoctor_接続先がGHEで要素が揃っていればカンバンの検査へ進む は、照会が通ったあとの流れを確かめる。
//
// 目的: 設計 3-86。照会は `カンバン` の検査の最初の段で、通ればいままでどおり Bootstrap と
// 候補の取得へ進む。**新しい見出し語は立てない。**
// 与える情報: `tracker.provider.host` が `ghe.example.com` の設定と、4要素とも在ると答える偽カンバン。
// 成功条件: `カンバン` が `✓` で、クエリが「照会 → Bootstrap → 候補の取得」の順に届き、
// `gh の認証` の説明に検査したホストが出ること。
func TestDoctor_接続先がGHEで要素が揃っていればカンバンの検査へ進む(t *testing.T) {
	fx := newFixture(t)
	fx.SetHost(t, "ghe.example.com")

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	queries := fx.GitHub.Queries()
	if len(queries) < 3 || queries[0] != "schema" || queries[1] != "bootstrap" || queries[2] != "items" {
		t.Fatalf("クエリの順が「照会 → Bootstrap → 候補の取得」ではない: %v", queries)
	}
	gh := assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolOK)
	if !strings.Contains(gh.Detail, "ghe.example.com") {
		t.Fatalf("gh の認証の説明に、検査したホストが出ていない: %q", gh.Detail)
	}
}

// TestDoctor_GHESに要素が足りなければカンバンを足りないにして版を案内する は、
// GitHub Enterprise Server 3.19 以下の接続先を見分けることを確かめる。
//
// 目的: 設計 3-86。3.19 以下では Bootstrap が「Unknown argument」で落ちるが、その文面からは
// 版が古いことが原因だと読めない。照会で先に見分け、足りない要素と要る版を出す。
// **足りないと分かったら Bootstrap は叩かない。**
// 与える情報: `ghe.example.com` の設定と、3.19 と同じ3要素が無いと答える偽カンバン。
// 成功条件: `カンバン` が `✗`、説明に足りない要素の名前が3つとも出て、直し方に「3.20」が入り、
// Bootstrap のクエリが届いていないこと。
func TestDoctor_GHESに要素が足りなければカンバンを足りないにして版を案内する(t *testing.T) {
	fx := newFixture(t)
	fx.SetHost(t, "ghe.example.com")
	fx.GitHub.SetSchemaMissing(
		tracker.SchemaElementItemsQuery,
		tracker.SchemaElementStatusChangedEvent,
		tracker.SchemaElementStatusChangedEnum,
	)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	for _, want := range []string{
		tracker.SchemaElementItemsQuery,
		tracker.SchemaElementStatusChangedEvent,
		tracker.SchemaElementStatusChangedEnum,
		"ghe.example.com",
	} {
		if !strings.Contains(board.Detail, want) {
			t.Errorf("説明に %q が無い: %q", want, board.Detail)
		}
	}
	if strings.Contains(board.Detail, tracker.SchemaElementBlockedBy) {
		t.Errorf("在る要素（%s）まで足りないと出している: %q", tracker.SchemaElementBlockedBy, board.Detail)
	}
	if !strings.Contains(strings.Join(board.Remedies, "\n"), "3.20") {
		t.Errorf("直し方に要る版（3.20）が無い: %v", board.Remedies)
	}
	if slices.Contains(fx.GitHub.Queries(), "bootstrap") {
		t.Errorf("足りないと分かったのに Bootstrap を叩いた: %v", fx.GitHub.Queries())
	}
}

// TestDoctor_ghe_comの接続先では版に触れない は、版を持たない接続先への案内を確かめる。
//
// 目的: 設計 3-86。`<名前>.ghe.com` は GitHub が運営していて、利用者は版を上げられない。
// 「3.20 以上が要ります」と出すと、直せない指示を渡すことになる。
// 与える情報: `octocorp.ghe.com` の設定と、1要素が無いと答える偽カンバン。
// 成功条件: `カンバン` が `✗` で、直し方に「3.20」が入っていないこと。
func TestDoctor_ghe_comの接続先では版に触れない(t *testing.T) {
	fx := newFixture(t)
	fx.SetHost(t, "octocorp.ghe.com")
	fx.GitHub.SetSchemaMissing(tracker.SchemaElementItemsQuery)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if strings.Contains(strings.Join(board.Remedies, "\n"), "3.20") {
		t.Fatalf("版を持たない接続先に版を案内している: %v", board.Remedies)
	}
}

// TestDoctor_照会がトークンの失効で落ちたら足りないにする は、照会の失敗の振り分けを確かめる。
//
// 目的: 設計 3-86。照会の失敗を `!`（確かめられなかった）にすると、`✗` が1つも出ず、
// 繋がっていないのに doctor が終了コード 0 で終わる。**Bootstrap の失敗と同じ振り分けにする。**
// 与える情報: `ghe.example.com` の設定と、401 を返す偽カンバン。
// 成功条件: `カンバン` が `✗` で、直し方の `gh auth refresh` に接続先ホストが入っていること。
func TestDoctor_照会がトークンの失効で落ちたら足りないにする(t *testing.T) {
	fx := newFixture(t)
	fx.SetHost(t, "ghe.example.com")
	fx.GitHub.SetFailure(failureBadCredentials)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(board.Remedies, "\n"), "gh auth refresh -h ghe.example.com") {
		t.Fatalf("直し方に接続先ホストが入っていない: %v", board.Remedies)
	}
}

// TestDoctor_cloneの直し方は接続先ホストから取る形で出す は、clone が無いときの案内を確かめる。
//
// 目的: 設計 3-86。ホストを省いた `ghq get <owner>/<repo>` を案内すると、GitHub Enterprise の
// 利用者が案内どおりに叩いて github.com から取ってくる。
// 与える情報: `ghe.example.com` の設定と、clone が1つも無い状態。
// 成功条件: `clone` が `✗` で、直し方が `ghq get --vcs git https://ghe.example.com/<owner>/<repo>` であること。
func TestDoctor_cloneの直し方は接続先ホストから取る形で出す(t *testing.T) {
	fx := newFixture(t)
	fx.SetHost(t, "ghe.example.com")
	fx.GhqPaths = map[string]string{}

	report := fx.Run(t)

	clone := assertSymbol(t, report, doctor.LabelClone, doctor.SymbolMissing)
	want := "ghq get --vcs git https://ghe.example.com/octocat/hello-world"
	if !strings.Contains(strings.Join(clone.Remedies, "\n"), want) {
		t.Fatalf("直し方に %q が無い: %v", want, clone.Remedies)
	}
}
