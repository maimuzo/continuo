package tracker

import (
	"strings"

	"github.com/maimuzo/continuo/internal/config"
)

// このファイルは、投稿する本文へ「機械が書いた」の印を差し込む処理である（設計 3-82）。
//
// **`internal/config` には置かない。**あちらは「利用者が何を設定したか」を持つ package であり、
// **投稿する本文を組み立てる場所ではない。**唯一の呼び出し元が `ComposeCommentBody` なので、ここへ置く。
// **本文を組み立てる処理を、この package へ集約しているわけではない。**
// `handoff.FormatBid` も `buildStatusMoveComment` も、投稿の直前に絶対パスを縮める
// `redact.Paths` も、この package の外にある。
// **印の綴り（`config.AIMarker`）だけは `config` に残す。**
// `ProgressMarker` / `PlanMarker` と並べておかないと、印の一覧が2箇所に割れる。

// commentOpen と commentClose は HTML のコメントの囲みである。
//
// **行頭ちょうどの `<!--` だけを印の行とみなす。**字下げした行は本文である。
// **[internal/handoff/assess.go](../handoff/assess.go) の `StartsAsProgressReport` とは、
// そこまでが同じで、1つ違う。**あちらは `<!--` で始まれば印の行と数えるが、
// **こちらは同じ行に `-->` があることも見る**（`isMarkerLine`）。
// **複数行の HTML コメントの開きで、2つの判定が分かれる。**
//
// **1行目だけは例外になる。**`withAIMarker` は1行目の空白を落としてから印かどうかを見る。
// **`handoff.IsMarked` も `FetchComments` も `TrimSpace(body)` してから先頭を見るので、
// そちらでも字下げした1行目の印は通る。**ここで通さないと、その2つと判定がずれる。
// **空白そのものは落とさない。**落とすと、4桁字下げのコード片で始まる本文の1行目だけが崩れる。
//
// **同じ前提に立つ定数が、他に3つある**（`StartsAsProgressReport` の説明にある一覧）。
// **印の形を変えるときは4つとも動かすこと。**
const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// withAIMarker は、本文の先頭の印の直後へ config.AIMarker を1行足す（設計 3-82）。
//
// **先頭の印を動かさない。**先頭へ割り込ませてはならない。
// `FetchComments`（[adapter.go](adapter.go)）も `handoff.IsMarked` も
// **本文の先頭が特定の印で始まっているか**で判定しており、
// **前へ1行入れると、その判定が全部外れる。**
//
// **見るのは、先頭の空行を飛ばした最初の1行だけである。**印が2本以上並ぶ本文は、
// **投稿の経路には1件も無い**（2026-09-08、実装レビュー6周目のあとの突き合わせで、
// `postComment` / `postOwnMarkedComment` の12箇所を1つずつ開いて数えた。
// **散文が7件、1行で閉じた印が1本だけのものが5件**。8周目に数え直した）。
// **関門の案内だけは印が2行並ぶが、2行目の `self_marker` は
// `ComposeCommentBody` があとから前へ足すので、ここへ来る本文は1行目が印である。**
//
// **来ない形のために走査を持たない。**以前は先頭に並ぶ印を辿る走査と、空行の扱いと、
// 末尾の改行の扱いと、先頭の空白の扱いを持っていた。**実装レビューの6周で、
// Critical 4件とその多くの High が、そこから出た。**本番の入力では1度も通らない枝だった。
//
// 例。
//
//	"<!-- continuo:self -->\n本文"        → "<!-- continuo:self -->\n<!-- continuo:ai -->\n本文"
//	"<!-- continuo:bid -->\n{…}\n\n散文"  → "<!-- continuo:bid -->\n<!-- continuo:ai -->\n{…}\n\n散文"
//	"本文だけ"                            → "<!-- continuo:ai -->\n本文だけ"
//
// **入札のコメントを壊さない。**`payloadAfterMarker`
// （[internal/handoff/handoff.go](../handoff/handoff.go)）は印の後ろの最初の `{` から
// 最後の `}` までを取る。**config.AIMarker は `{` も `}` も持たない。**
//
// **改行は `\n` である。**CRLF は扱わない。本文を作る12箇所は全部 Go の `"\n"` で組み立てており、
// **外から来る文字列は git と gh の出力だけで、continuo が動く場所（darwin と linux）では LF である。**
//
// body: 印を足す前の本文。**空文字は渡らない**（`ComposeCommentBody` が先に返す）。
// 戻り値: 印を1行足した本文。**もとの本文は1文字も書き換えない。**
func withAIMarker(body string) string {
	// **空行を飛ばしてから1行目を取る。**読む側は `TrimSpace(body)` してから先頭を見るので、
	// **先頭に空行がある本文でも、あちらは次の行を先頭として読む。**
	// 飛ばさないと、`"\n<!-- continuo:bid -->…"` のような本文で印が本物の印の前へ入り、
	// **`IsMarked` も `FetchComments` も同時に外れる。**
	head := 0
	for head < len(body) {
		i := strings.IndexByte(body[head:], '\n')
		if i < 0 || strings.TrimSpace(body[head:head+i]) != "" {
			break
		}
		head += i + 1
	}
	line, rest := body[head:], ""
	if i := strings.IndexByte(body[head:], '\n'); i >= 0 {
		line, rest = body[head:head+i+1], body[head+i+1:]
	}
	// **空白を落としてから見る。**読む側（`IsMarked` も `FetchComments` も）は
	// `TrimSpace(body)` してから先頭を見るので、**字下げした1行目の印は、あちらでは印として通る。**
	// **ここで通さないと、印がその前へ入り、あちらの先頭一致が全部外れる。**
	// **空白そのものは落とさない。**落とすと、4桁字下げのコード片で始まる本文の1行目だけが崩れる。
	if !isMarkerLine(strings.TrimSpace(line)) {
		return config.AIMarker + "\n" + body
	}
	if rest == "" && !strings.HasSuffix(line, "\n") {
		// 改行の無い、印1行だけの本文。**その後ろへ足す。**前へ入れると先頭一致が外れる。
		return body + "\n" + config.AIMarker
	}
	return body[:head] + line + config.AIMarker + "\n" + rest
}

// isMarkerLine は、その行が1行で閉じた HTML のコメント（＝印の行）かを返す。
//
// **行頭ちょうどの `<!--` だけを印の行とみなす。**字下げした行は本文である。
//
// **開きだけでは足りない。**`<!--` で始まり、同じ行に `-->` が無い行は、
// **複数行の HTML コメントの開きである。**印の行として数えると、
// **その中へ印を差し込むことになり、issue の画面では見えなくなる。**
//
// **閉じの後ろに本文が続く行は、印の行として数える。**
// `<!-- 方針 --> production へは push しないでください。` のような1行の書き方があり、
// **その形だと、印は本文の1行の下へ入る。**見た目は良くない。
// **それでも数えるほうを採る。**数えないと、その行より**前**へ印を入れることになり、
// **`IsMarked` も `FetchComments` も先頭一致が同時に外れる。**
//
// **同じ前提に立つ定数が、他に3つある**（`StartsAsProgressReport` の説明にある一覧）。
// **印の形を変えるときは4つとも動かすこと。**
//
// line: 見る行（行末の改行は含まない）。
// 戻り値: 行頭ちょうどの `<!--` で始まり、同じ行に `-->` があれば真。
func isMarkerLine(line string) bool {
	if !strings.HasPrefix(line, commentOpen) {
		return false
	}
	return strings.Contains(line[len(commentOpen):], commentClose)
}

// ComposeCommentBody は、continuo が投稿する本文を組み立てる（設計 3-82）。
//
// **`PostComment` から切り出してある。**検査の偽の tracker
// （`test/internal/orchestrator` の `fakeTracker`）も、これを呼ぶ。
// **写して持つと、片方を直したときに黙ってずれる。**
// ずれても orchestrator の検査は通り続けるので、**誰も気づけない。**
//
// body: 素の本文。
// selfMarker: 本文の先頭に付ける印（`tracker.comments.self_marker`）。
// **空文字なら付けない。**持ち回りのコメント（入札・hold・released）は、
// 本文が自分で印を持っているので空文字で渡ってくる。
// 戻り値: 投稿する本文。**先頭の印の直後に `config.AIMarker` が入る。**
// **見るのは1行目だけである。**印が2行以上並ぶ本文を渡すと、**印はその2行のあいだへ入る。**
// **本番の呼び出し元は、そういう本文を作らない**（`withAIMarker` の説明にある12箇所の数え）。
// **`self_marker` の次とは限らない。**本文が自分で印を持っていれば、その後ろになる
// （関門の案内は `self_marker` → `continuo:gated:*` → 印 の3行になる）。
func ComposeCommentBody(body, selfMarker string) string {
	// **`self_marker` を足す前に印を足す。**順序を逆にしてはならない。
	//
	// **`self_marker` は利用者が設定で決める文字列であり、形を縛る検査が無い**
	// （`tracker.comments.self_marker`）。`[continuo-self]` のような値にできる。
	// **`withAIMarker` は `<!--` で始まる行だけを印の行とみなす**ので、
	// **そういう値を先に足すと、印がその行より前へ入る。**
	// そうなると `FetchComments` の先頭一致が外れ、
	// **continuo 自身の通知が、次の turn の入力から外れなくなる。**
	// 人間が書いたコメントとして、毎 turn エージェントへ渡り続ける。
	//
	// **先に印を足せば、`self_marker` の形を問わない。**
	// 持ち回りのコメント（入札・hold・released）は `selfMarker` が空で渡ってくるので、
	// **本文が自分で持っている印の後ろへ入る。**そちらは固定の `<!--` の印である。
	//
	// **`<!-- design-review-skipped -->` の断りを、ここで外すことはしない**（設計 3-82c）。
	// **この経路からは、その断りへ届かないためである。**`PostComment` が投稿する先は
	// カンバンに載った issue であり（pull request は `Gone` として捨てる。
	// [internal/tracker/query.go](query.go) の `classify`）、
	// **その例外が守っている CI は pull request のコメントを読む**
	// （[.github/workflows/review-gate.yml](../../.github/workflows/review-gate.yml) の
	// `repos/${REPO}/issues/${PR_NUMBER}/comments`）。
	// **断りを実際に書くのは人間かエージェントの `gh pr comment` で、この関数を1度も通らない。**
	// **例外を守っているのは組み込みの指示書の 5-6 と、その検査と、CI の案内文の3つである。**
	// **本当に空の本文には、何も足さない。**
	// **足すと空でなくなるので GitHub が受け付け、見えないコメントが公開されて消せない。**
	// 足さなければ GitHub が断り、**呼び出し側の欠陥がログに出る。**
	//
	// **空白だけの本文は止めない。**止めると `self_marker` も落ちるが、
	// **GitHub は空白だけの本文を空でないものとして受け付ける。**
	// そのコメントは `FetchComments` が外せず、
	// **continuo 自身の通知が毎 turn エージェントへ渡り続ける。**
	// **`TrimSpace` で止めると、そちらの事故を作ることになる。**
	//
	// **「既に `self_marker` が付いていれば外す」は入れて取り消した**
	// （2026-09-08、実装レビュー4周目）。`self_marker` が短い値（`<!--` など）のときに、
	// **関門の案内の1行目を壊す。**二度組み立て直す呼び出し元は、本番に1つも無い。
	if body == "" {
		return body
	}
	full := withAIMarker(body)
	if selfMarker != "" {
		// **改行は `\n` である。**CRLF は扱わない（理由は `withAIMarker` にある）。
		full = selfMarker + "\n" + full
	}
	return full
}
