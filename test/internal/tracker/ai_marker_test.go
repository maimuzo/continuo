package tracker_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: PostComment が「人間ではなく機械が書いた」の印を、
// 先頭の印の後ろへ足すことを固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する。
// **投稿者でも `author_association` でも見分けられない。**
// **先頭へ割り込ませてはならない。**`FetchComments` は本文の先頭が `self_marker` かどうかで
// **次の turn の入力から外すかを決めている。**外れると、continuo 自身の通知が
// エージェントへの入力に混ざる。
//
// 与える情報: 素の本文と self_marker。
// 成功条件: 投稿した本文が `self_marker` → 機械の印 → 素の本文 の順であること。
func TestPostComment_機械の印をselfMarkerの次の行に足す(t *testing.T) {
	const selfMarker = "<!-- continuo:self -->"
	const rawBody = "作業内容の要約"
	want := selfMarker + "\n" + config.AIMarker + "\n" + rawBody

	var got string
	fs := newFakeGraphQLServer(t, func(_ int, req capturedRequest) fakeGraphQLResponse {
		got, _ = req.Variables["body"].(string)
		return dataResponse(map[string]any{
			"addComment": map[string]any{
				"commentEdge": map[string]any{
					"node": map[string]any{
						"id": "c-new", "url": "https://example.com/c-new", "body": got,
						"createdAt": "2026-09-06T00:00:00Z",
						"author":    map[string]any{"login": "continuo-bot"},
					},
				},
			},
		})
	})
	a := newAdapterForFetch(t, fs)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", rawBody, selfMarker); err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if got != want {
		t.Fatalf("投稿した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
	if !strings.HasPrefix(got, selfMarker) {
		t.Errorf("self_marker が本文の先頭から外れています。"+
			"FetchComments が continuo 自身のコメントを外せなくなります: %q", got)
	}
}

// 目的: 本文が自分で印を持つコメント（持ち回りの入札・hold・released）でも、
// 先頭の印を動かさずに機械の印を足すことを固定する（設計 3-82）。
//
// **持ち回りのコメントは `selfMarker` を空で渡してくる**
// （internal/orchestrator の `postOwnMarkedComment`）。
// **本文の先頭が `<!-- continuo:bid -->` などでなくなると、別の機械がその入札を読めなくなる。**
//
// 与える情報: 先頭に持ち回りの印を持つ本文と、空の self_marker。
// 成功条件: 投稿した本文が 持ち回りの印 → 機械の印 → JSON の順であること。
func TestPostComment_持ち回りの印は先頭のまま動かさない(t *testing.T) {
	body := config.HandoffBidMarker + "\n{\"score\":190}\n\n立候補しています。\n"
	want := config.HandoffBidMarker + "\n" + config.AIMarker + "\n{\"score\":190}\n\n立候補しています。\n"

	var got string
	fs := newFakeGraphQLServer(t, func(_ int, req capturedRequest) fakeGraphQLResponse {
		got, _ = req.Variables["body"].(string)
		return dataResponse(map[string]any{
			"addComment": map[string]any{
				"commentEdge": map[string]any{
					"node": map[string]any{
						"id": "c-new", "url": "https://example.com/c-new", "body": got,
						"createdAt": "2026-09-06T00:00:00Z",
						"author":    map[string]any{"login": "continuo-bot"},
					},
				},
			},
		})
	})
	a := newAdapterForFetch(t, fs)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", body, ""); err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if got != want {
		t.Fatalf("投稿した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
}

// 目的: 機械の印を足しても、エージェントが書いたコメントの判別が変わらないことを固定する
// （設計 3-82 / 3-65）。
//
// **`FetchComments` は本文の先頭が `marker` かどうかで `IsAgent` を決めている。**
// **印がその前に入ると、成果を書いた run が「書いていない」と判定され、人間へ渡る。**
//
// 与える情報: 印を足したエージェントのコメントと、印を足した continuo 自身のコメント。
// 成功条件: 前者が IsAgent=true で残り、後者が結果から外れること。
func TestFetchComments_機械の印を足しても判別が変わらない(t *testing.T) {
	cfg := testTrackerConfig()
	markers := cfg.Comments

	agentBody := tracker.ComposeCommentBody(markers.Marker+"\n実装しました", "")
	selfBody := tracker.ComposeCommentBody(markers.SelfMarker+"\nStatus を動かしました", "")

	agent := map[string]any{
		"id": "c1", "url": "https://example.com/c1", "body": agentBody,
		"createdAt": "2026-09-06T00:00:01Z", "author": map[string]any{"login": "human-user"},
	}
	self := map[string]any{
		"id": "c2", "url": "https://example.com/c2", "body": selfBody,
		"createdAt": "2026-09-06T00:00:02Z", "author": map[string]any{"login": "human-user"},
	}

	fs := newFakeGraphQLServer(t, single(dataResponse(map[string]any{
		"node": map[string]any{
			"__typename": "Issue",
			"comments":   map[string]any{"nodes": []map[string]any{self, agent}},
		},
	})))
	a := newAdapterForFetch(t, fs)

	got, err := a.FetchComments(t.Context(), "ISSUENODE_1", cfg.Provider.Comments, markers, "human-user")
	if err != nil {
		t.Fatalf("FetchComments が失敗した: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("continuo 自身のコメントが外れていません: %d 件 %+v", len(got), got)
	}
	if !got[0].IsAgent {
		t.Errorf("印を足したら、エージェントが書いたコメントとして数えられなくなりました: %q", got[0].Body)
	}
	if !strings.Contains(got[0].Body, config.AIMarker) {
		t.Errorf("本文から機械の印が消えています: %q", got[0].Body)
	}
}

// 目的: `self_marker` が HTML のコメントでなくても、先頭に来ることを固定する（設計 3-82）。
//
// **`self_marker` は利用者が設定で決める文字列であり、形を縛る検査が無い**
// （`tracker.comments.self_marker`）。`[continuo-self]` のような値にできる。
// **`withAIMarker` は `<!--` で始まる行だけを印の行とみなす**ので、
// **`self_marker` を先に足してから通すと、印がその行より前へ入る。**
//
// **そうなると `FetchComments` の先頭一致が外れる。**
// continuo 自身の通知が次の turn の入力から外れなくなり、
// **人間が書いたコメントとして、毎 turn エージェントへ渡り続ける。**
//
// 与える情報: HTML のコメントではない self_marker。
// 成功条件: 本文が self_marker で始まり、その次の行が機械の印であること。
func TestComposeCommentBody_HTMLのコメントでないselfMarkerでも先頭に来る(t *testing.T) {
	const selfMarker = "[continuo-self]"
	got := tracker.ComposeCommentBody("Status を動かしました", selfMarker)
	want := selfMarker + "\n" + config.AIMarker + "\nStatus を動かしました"
	if got != want {
		t.Fatalf("self_marker が先頭から外れました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 持ち回りのコメント（self_marker が空）でも、印が先頭に来ないことを固定する（設計 3-82）。
//
// 与える情報: 先頭に持ち回りの印を持つ本文と、空の self_marker。
// 成功条件: 持ち回りの印 → 機械の印 → JSON の順であること。
func TestComposeCommentBody_持ち回りの印は先頭のまま(t *testing.T) {
	body := config.HandoffBidMarker + "\n{\"score\":190}\n"
	got := tracker.ComposeCommentBody(body, "")
	want := config.HandoffBidMarker + "\n" + config.AIMarker + "\n{\"score\":190}\n"
	if got != want {
		t.Fatalf("持ち回りの印が先頭から外れました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 印を足す位置が「先頭に並ぶ印のいちばん後ろ」であることを固定する（設計 3-82）。
//
// **先頭へ割り込ませてはならない。**`FetchComments` の先頭一致（`internal/tracker`）、
// `handoff.IsMarked`、CI の3本の正規表現が、どれも本文の先頭を見ている。
//
// 与える情報: continuo が実際に投稿する4つの形。
// 成功条件: 先頭の印が1つも動かず、印が2行目（先頭の印が無ければ1行目）に入ること。
func TestWithAIMarker_先頭の印を動かさずに足す(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			"continuo 自身のコメント",
			"<!-- continuo:self -->\nStatus を動かしました",
			"<!-- continuo:self -->\n" + config.AIMarker + "\nStatus を動かしました",
		},
		{
			"入札のコメント（本文が自分で印を持つ）",
			"<!-- continuo:bid -->\n{\"score\":190}\n\n立候補しています。\n",
			"<!-- continuo:bid -->\n" + config.AIMarker + "\n{\"score\":190}\n\n立候補しています。\n",
		},
		{
			"関門の案内（印が2つ並ぶ）",
			"<!-- continuo:self -->\n<!-- continuo:gated:human_assigned -->\n担当者が付いています",
			"<!-- continuo:self -->\n<!-- continuo:gated:human_assigned -->\n" +
				config.AIMarker + "\n担当者が付いています",
		},
		{
			"印が1つも無い本文",
			"素の本文",
			config.AIMarker + "\n素の本文",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tracker.ComposeCommentBody(tc.body, ""); got != tc.want {
				t.Errorf("印を足した本文が想定と違います:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// 目的: 二重に付けないことを固定する（設計 3-82）。
//
// **付けてしまうと、同じ行が巡回のたびに1行ずつ増える形の間違いを、誰も止められない。**
//
// 与える情報: 既に印を持つ本文。
// 成功条件: 1文字も変わらないこと。
func TestWithAIMarker_既に印があれば足さない(t *testing.T) {
	body := "<!-- continuo:self -->\n" + config.AIMarker + "\n本文"
	if got := tracker.ComposeCommentBody(body, ""); got != body {
		t.Fatalf("印が2つになりました:\n got %q\nwant %q", got, body)
	}
	// **先頭に印だけがある形でも足さない。**
	only := config.AIMarker + "\n本文"
	if got := tracker.ComposeCommentBody(only, ""); got != only {
		t.Fatalf("印が2つになりました:\n got %q\nwant %q", got, only)
	}
}

// 目的: 先頭に空白があっても、印を本物の印より前へ置かないことを固定する（設計 3-82）。
//
// **読む側は全部 `strings.TrimSpace(body)` してから先頭を見る**
// （`handoff.IsMarked` / `payloadAfterMarker` / `FetchComments`）。
// **ここで落とさないと、先頭に空行が1つあるだけで印が本物の印より前に入る。**
// そのとき `IsMarked` が偽になり、**continuo は自分が書いた入札も hold も読み戻せない。**
// **担当を人間のものと読み、その issue を二度と拾わない。**
//
// 与える情報: 先頭に空行や空白がある本文。
// 成功条件: 印が持ち回りの印の後ろに入り、TrimSpace したあとの先頭が持ち回りの印のままであること。
func TestWithAIMarker_先頭に空白があっても印を前へ置かない(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"先頭に空行", "\n" + config.HandoffBidMarker + "\n{\"score\":190}\n"},
		{"先頭に空白", "  " + config.HandoffBidMarker + "\n{\"score\":190}\n"},
		{"先頭に全角空白", "　" + config.HandoffBidMarker + "\n{\"score\":190}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tracker.ComposeCommentBody(tc.body, "")
			if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
				t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
			}
			if !strings.Contains(got, config.AIMarker) {
				t.Fatalf("印が入っていません:\n%q", got)
			}
		})
	}
}

// 目的: 末尾に改行が無い本文でも、印が前の行に繋がらないことを固定する（設計 3-82）。
//
// **繋がると、1行目が印そのものでなくなる。**
// `FetchComments` の先頭一致も `handoff.IsMarked` も、そこで外れる。
//
// 与える情報: 印だけで、末尾に改行が無い本文。
// 成功条件: 2行になり、1行目が元の印、2行目が機械の印であること。
func TestWithAIMarker_末尾に改行が無くても行を分ける(t *testing.T) {
	const self = "<!-- continuo:self -->"
	want := self + "\n" + config.AIMarker
	if got := tracker.ComposeCommentBody(self, ""); got != want {
		t.Fatalf("印が前の行に繋がりました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 印と印のあいだに空行があっても、いちばん後ろの印の直後へ足すことを固定する（設計 3-82）。
//
// **`handoff.StartsAsProgressReport` は空行では止めない。**そちらと決まりを揃える。
// **ただし、後ろの空行までは越えない。**越えると、本文の1行目が印になる。
//
// 与える情報: 印のあいだに空行がある本文と、印のあとに空行がある本文。
// 成功条件: どちらも、最後の印の行の直後に入ること。
func TestWithAIMarker_印のあいだの空行では止めない(t *testing.T) {
	const self = "<!-- continuo:self -->"
	const gated = "<!-- continuo:gated:human_assigned -->"

	got := tracker.ComposeCommentBody(self+"\n\n"+gated+"\n担当者が付いています", "")
	want := self + "\n\n" + gated + "\n" + config.AIMarker + "\n担当者が付いています"
	if got != want {
		t.Errorf("印のあいだの空行で止まりました:\n got %q\nwant %q", got, want)
	}

	// **印のあとに空行が続くときは、その空行を越えない。**
	got = tracker.ComposeCommentBody(self+"\n\n本文", "")
	want = self + "\n" + config.AIMarker + "\n\n本文"
	if got != want {
		t.Errorf("印のあとの空行を越えました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 2行目以降の字下げした行を、印の並びとみなさないことを固定する（設計 3-82）。
//
// **`handoff.StartsAsProgressReport` と同じ決まりに揃える。**
// あちらは本文全体の先頭の空白だけを落とし、**そのあとは行頭ちょうどの `<!--` だけを見る。**
// **だから1行目の字下げは落ち、2行目以降の字下げは落ちない。**
// 読む側（`handoff.IsMarked` / `FetchComments`）も `TrimSpace(body)` してから
// 先頭を見るので、**1行目の字下げは、その3つで同じように落ちる。**
//
// **2行目以降を印とみなすと、印について説明する成果の報告の途中へ、印が差し込まれる。**
//
// 与える情報: 1行目だけが印で、2行目が字下げした HTML コメントである本文。
// 成功条件: 印が1行目の直後に入り、字下げした2行目は本文として扱われること。
func TestWithAIMarker_2行目以降の字下げは印とみなさない(t *testing.T) {
	body := "<!-- continuo:agent -->\n    <!-- continuo:progress -->\nこれは見本です"
	want := "<!-- continuo:agent -->\n" + config.AIMarker +
		"\n    <!-- continuo:progress -->\nこれは見本です"
	if got := tracker.ComposeCommentBody(body, ""); got != want {
		t.Fatalf("字下げした2行目を印として数えています:\n got %q\nwant %q", got, want)
	}
}

// 目的: 1行目の字下げは、読む側と同じように落ちることを固定する（設計 3-82）。
//
// **`handoff.IsMarked` も `FetchComments` も `TrimSpace(body)` してから先頭を見る。**
// **つまり、字下げした1行目の印は、その2つでは印として通る。**
// **ここだけ通さないと、印がその行より前に入り、2つの判定が同時に外れる。**
//
// 与える情報: 1行目が4桁字下げの持ち回りの印である本文。
// 成功条件: 印がその行の後ろに入り、TrimSpace したあとの先頭が持ち回りの印のままであること。
func TestWithAIMarker_1行目の字下げは読む側と同じように落ちる(t *testing.T) {
	body := "    " + config.HandoffBidMarker + "\n{\"score\":190}\n"
	got := tracker.ComposeCommentBody(body, "")
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
	}
}

// 目的: どんな本文でも「TrimSpace したあとの先頭の1行」が変わらないことを固定する（設計 3-82）。
//
// **これがこの印のいちばん大事な約束である。**
// 読む側（`handoff.IsMarked` / `payloadAfterMarker` / `FetchComments`）は
// **どれも `TrimSpace(body)` してから先頭を見る。**
// **その1行が変われば、その全部が同時に外れる。**
//
// **1件ずつの検査では、この約束を守り切れない。**空行・空白・字下げ・改行の有無の
// 組み合わせは、思いつくたびに増える。**組み合わせて全部に当てる。**
//
// 与える情報: 印の並び・空白・改行の有無を組み合わせた本文。
// 成功条件: 元の本文が印で始まっていたなら、印を足したあとも同じ印で始まっていること。
func TestWithAIMarker_先頭の1行を変えない(t *testing.T) {
	heads := []string{
		"<!-- continuo:self -->",
		config.HandoffBidMarker,
		config.HandoffHoldMarker,
		config.HandoffReleasedMarker,
		"<!-- continuo:agent -->",
	}
	prefixes := []string{"", " ", "  ", "\n", "\n\n", "　", "    ", "\t"}
	bodies := []string{"本文", "{\"score\":190}\n\n散文。\n", "", "本文\n"}

	for _, head := range heads {
		for _, prefix := range prefixes {
			for _, body := range bodies {
				src := prefix + head
				if body != "" {
					src += "\n" + body
				}
				got := tracker.ComposeCommentBody(src, "")
				if !strings.HasPrefix(strings.TrimSpace(got), head) {
					t.Errorf("先頭の1行が変わりました。読む側の先頭一致が全部外れます\n"+
						" 入力 %q\n 出力 %q", src, got)
				}
				if !strings.Contains(got, config.AIMarker) {
					t.Errorf("印が入っていません\n 入力 %q\n 出力 %q", src, got)
				}
				// **二度通しても増えない。**
				if again := tracker.ComposeCommentBody(got, ""); again != got {
					t.Errorf("二度通すと印が増えました\n 1回目 %q\n 2回目 %q", got, again)
				}
			}
		}
	}
}

// 目的: 印を足しても、入札の JSON を切り出す側が壊れないことを固定する（設計 3-82）。
//
// **`payloadAfterMarker`（internal/handoff）は、印の後ろの最初の `{` から最後の `}` を取る。**
// **印そのものが `{` か `}` を持つと、その切り出しが壊れる。**
// ここは handoff を通さずに、印の綴りだけで確かめる（依存を増やさないため）。
//
// 与える情報: config.AIMarker。
// 成功条件: `{` も `}` も含まないこと。
func TestAIMarker_中括弧を含まない(t *testing.T) {
	if strings.ContainsAny(config.AIMarker, "{}") {
		t.Fatalf("印が中括弧を含んでいます（%q）。"+
			"入札のコメントから JSON を切り出す処理が壊れます", config.AIMarker)
	}
}

// 目的: 閉じの後ろに本文が続く1行の、前へ印を入れないことを固定する（設計 3-82）。
//
// **読む側は `TrimSpace(body)` してから先頭を見る。**
// `<!-- continuo:bid --> 立候補` のような1行を先頭に持つ本文は、
// **`handoff.IsMarked` からは持ち回りのコメントに見える。**
// **そこへ印を先に入れると、先頭一致が全部外れる。**
//
// 与える情報: 閉じの後ろに本文が続く1行で始まる本文。
// 成功条件: TrimSpace したあとの先頭が、元の印のままであること。
func TestWithAIMarker_閉じの後ろに本文が続く行の前へ入れない(t *testing.T) {
	body := config.HandoffBidMarker + " 立候補しています。\n{\"score\":190}\n"
	got := tracker.ComposeCommentBody(body, "")
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("印が持ち回りの印より前に入りました:\n%q", got)
	}
}

// 目的: 先頭の空白だけの行を作らないことを固定する（設計 3-82）。
//
// **`元の本文は1文字も書き換えない` という約束のうち、行の増やし方に当たる。**
// 空白だけの1行目が残ると、issue の画面で空行から始まるコメントになる。
//
// 与える情報: 先頭に空白があり、印を1つも持たない本文。
// 成功条件: 1行目が印であること。
func TestWithAIMarker_空白だけの行を残さない(t *testing.T) {
	got := tracker.ComposeCommentBody("  素の本文", "")
	want := config.AIMarker + "\n素の本文"
	if got != want {
		t.Fatalf("空白だけの行が残りました:\n got %q\nwant %q", got, want)
	}
}

// 目的: CRLF の本文へ足す行も CRLF で終わることを固定する（設計 3-82）。
//
// **git の失敗をそのまま貼る経路があるので、CRLF が混じりうる。**
// **1行だけ LF になると、投稿した本文の改行が揃わない。**
//
// 与える情報: CRLF の本文。
// 成功条件: 印の行が CRLF で終わり、先頭の1行が変わらないこと。
func TestWithAIMarker_CRLFの本文にはCRLFで足す(t *testing.T) {
	body := config.HandoffBidMarker + "\r\n{\"score\":190}\r\n"
	want := config.HandoffBidMarker + "\r\n" + config.AIMarker + "\r\n{\"score\":190}\r\n"
	if got := tracker.ComposeCommentBody(body, ""); got != want {
		t.Fatalf("改行の綴りが揃いません:\n got %q\nwant %q", got, want)
	}
}

// 目的: 複数行の HTML コメントの中へ印を差し込まないことを固定する（設計 3-82）。
//
// **`<!--` で始まり、同じ行に `-->` が無い行は、複数行のコメントの開きである。**
// 印の行として数えると、**その中へ印を入れてしまう。**
// **issue の画面には出ず、本文は `<!--` で始まったまま残る。**
//
// 与える情報: 複数行の HTML コメントで始まる本文。
// 成功条件: 印がコメントの中へ入らず、本文の先頭に来ること。
func TestWithAIMarker_複数行のコメントの中へ入れない(t *testing.T) {
	body := "<!--\n覚え書き\n-->\n本文"
	want := config.AIMarker + "\n" + body
	if got := tracker.ComposeCommentBody(body, ""); got != want {
		t.Fatalf("複数行のコメントの中へ印を入れました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 字下げして引用した印は、名乗りとして数えないことを固定する（設計 3-82）。
//
// **組み込みの指示書は、印を4桁字下げのコード片として見せている。**
// **それを引用した報告に名乗りの印が付かないと、その報告だけ人間が書いたものと見分けが付かない。**
// **だから、字下げした印は「既に付いている」と数えない。**
//
// 与える情報: 2行目に字下げした印がある本文。
// 成功条件: 名乗りの印が1行目の直後に入り、字下げした行はそのまま残ること。
func TestWithAIMarker_字下げした印は名乗りに数えない(t *testing.T) {
	body := "<!-- continuo:self -->\n  " + config.AIMarker + "\n本文"
	want := "<!-- continuo:self -->\n" + config.AIMarker + "\n  " + config.AIMarker + "\n本文"
	if got := tracker.ComposeCommentBody(body, ""); got != want {
		t.Fatalf("字下げした引用を名乗りとして数えました:\n got %q\nwant %q", got, want)
	}
}

// 目的: 改行が CRLF でも、先頭の1行が変わらないことを固定する（設計 3-82）。
//
// **git の失敗をそのまま貼る経路があるので、CRLF が混じりうる**
// （`internal/orchestrator` の `postComment`）。
// **先頭の1行さえ変わらなければ、読む側の先頭一致は全部通る。**
//
// 与える情報: CRLF の本文。
// 成功条件: TrimSpace したあとの先頭が、元の印のままであること。
func TestWithAIMarker_CRLFでも先頭の1行を変えない(t *testing.T) {
	body := config.HandoffBidMarker + "\r\n{\"score\":190}\r\n"
	got := tracker.ComposeCommentBody(body, "")
	if !strings.HasPrefix(strings.TrimSpace(got), config.HandoffBidMarker) {
		t.Fatalf("先頭の1行が変わりました:\n%q", got)
	}
	if !strings.Contains(got, config.AIMarker) {
		t.Fatalf("印が入っていません:\n%q", got)
	}
}

// 目的: 先頭に印が1つも無い CRLF の本文でも、足す行が CRLF で終わることを固定する（設計 3-82）。
//
// **git の失敗をそのまま貼る経路があるので、印を持たない CRLF の本文が来うる。**
// **直前の行だけを見ると、差し込む位置が先頭のときに LF が混ざる。**
//
// 与える情報: 印を1つも持たない CRLF の本文。
// 成功条件: 1行目が印で、その行が CRLF で終わること。
func TestComposeCommentBody_印の無いCRLFの本文にもCRLFで足す(t *testing.T) {
	body := "git push が失敗しました\r\nfatal: 何か\r\n"
	want := config.AIMarker + "\r\n" + body
	if got := tracker.ComposeCommentBody(body, ""); got != want {
		t.Fatalf("改行の綴りが揃いません:\n got %q\nwant %q", got, want)
	}
}

// 目的: 印の後ろに書き足した行があっても、二重に付けないことを固定する（設計 3-82）。
//
// **`isMarkerLine` は、閉じの後ろに本文が続く行も印の行として数える。**
// **完全一致で二重を見ると、`<!-- continuo:ai --> 補足` のような行を見落として印が2つ並ぶ。**
//
// 与える情報: 印の後ろに文が付いた行を持つ本文。
// 成功条件: 1文字も変わらないこと。
func TestComposeCommentBody_印の後ろに文が付いていても二重に付けない(t *testing.T) {
	body := "<!-- continuo:self -->\n" + config.AIMarker + " 機械が書きました\n本文"
	if got := tracker.ComposeCommentBody(body, ""); got != body {
		t.Fatalf("印が2つになりました:\n got %q\nwant %q", got, body)
	}
}
