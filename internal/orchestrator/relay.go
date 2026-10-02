package orchestrator

// 人間が issue に書いたコメントを、次に Claude Code を起動したときの最初のメッセージに付けて渡す
// 機能（relay）である（設計 3-85。issue #246）。
//
// **何のためにあるか。**`auto` の判定役（classifier）は user メッセージにある人間の意図しか許可として
// 数えず、`gh` で読んだ issue のコメントは道具の結果として取り除く。**人間が issue のコメントで
// 「issue を作ってよい」と許しても、判定役には届かない。**そこで continuo が、そのコメントを
// 最初のメッセージの末尾に付けて渡す。
//
// **どのコメントを渡すかの境目は、continuo が Claude Code の pane を閉じるたびに書く
// 「閉じた記録」（`<!-- continuo:closed -->`）で決める。**記録を書いた時点で、その issue の
// Claude Code はもう動いていないので、記録より後に書かれたものは人間のものだけになる
// （AI が目印を付け忘れた場合を除く。人間が受け入れた残る心配）。
//
// **手元にファイルを置かない。**境目は issue のコメントだけで決めるので、チームの別の機械が
// 引き継いでも同じ規則で決まる（人間の決定）。

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
)

// relayFetchTimeout は、最初のメッセージの直前にコメントを読む処理全体の期限の既定である（設計 3-85）。
//
// **期限を過ぎたら、節を付けずに最初のメッセージだけを送る。**読めないことで着手を止めない。
const relayFetchTimeout = 60 * time.Second

// closedRecordWriteTimeout は、閉じた記録を1件書くときの期限である（設計 3-85）。
// **巡回が片付けを見送ったときのコメントを1件書くときの期限にも使う**（設計 3-9。`noticeDeferredOnPatrol`）。
//
// **pane を閉じる期限とは別に取る。**止められた ctx からは `context.WithoutCancel` で切り離す
// （後片付けの先例と同じ）。**やり直さない。**`addComment` は同じものを2回書くことがある。
const closedRecordWriteTimeout = 10 * time.Second

// relayMaxRunes は、最初のメッセージに付ける節の全体の長さの上限（rune 数）である（設計 3-85）。
const relayMaxRunes = 30000

// relayTrustedAssociations は、渡してよい投稿者の立場である（組み込みの指示書 4-1 の `trusted_comment` と同じ）。
var relayTrustedAssociations = []string{"OWNER", "MEMBER", "COLLABORATOR"}

// relayFixedAIMarkers は、AI が書いたとみなす本文の先頭の印である（組み込みの指示書 4-1 の jq の式と同じ）。
//
// **設定の `tracker.comments.marker` と `self_marker` は、空でないときだけ足す**（`relayAIMarkers`）。
var relayFixedAIMarkers = []string{
	"<!-- continuo:",
	"<!-- code-review-result -->",
	"<!-- design-review-result -->",
	"<!-- design-review-skipped -->",
}

// closedRecordMode は、pane を閉じたときに閉じた記録をどう扱うかである（設計 3-85）。
type closedRecordMode int

const (
	// closedRecordWrite は、書くべきなら書く（ふつうの閉じ方）。
	closedRecordWrite closedRecordMode = iota
	// closedRecordSkip は書かない。**担当が別の機械・別の人へ移ったときだけ使う**
	// （設計 3-77c・3-83h の「担当を外された機械は issue へ書かない」）。保留も捨てる。
	closedRecordSkip
	// closedRecordDefer は書かずに保留する。**報告の書かせ直しの段2 だけで使う**（`ensureAgentComment`）。
	//
	// 段2 で書くと、人間がその記録を見て書いた許可が、段8 のあとの2件目の記録より前になり、黙って落ちる。
	// 段2 のあとの道は、どれも最後に呼び出し側の `stopWorker` を通るので、そこで書く。
	closedRecordDefer
)

// closedRecordState は、run が持ち越している閉じた記録の状態である（設計 3-85）。
type closedRecordState int

const (
	// closedRecordNone は、持ち越しが無い。
	closedRecordNone closedRecordState = iota
	// closedRecordPending は、書かせ直しの段2 で記録を保留している。
	closedRecordPending
	// closedRecordCloseFailed は、この run で pane を閉じ損ね、その pane がまだ残っているかもしれない。
	// **そのあいだこの run は記録を書かない。**閉じ損ねた Claude Code が生きたまま記録を付けると、
	// その Claude Code が後で書いたものが人間のコメントとして渡りうる。
	// **次に pane を閉じるときに、閉じ損ねた pane をもう一度閉じてみる**（`closeFailedPanes`）。
	// 全部無くなったら下ろして記録を書く。run が終わるまで残ったら、巡回の `closeOrphanPane` が閉じるときに書く。
	closedRecordCloseFailed
)

// relayEnabled は、relay が有効かを返す（設計 3-85）。
//
// **記録を書く側・読む側・案内の文面の3か所が、これ1つで決める。**
//
//	claude.permission_mode が auto   … 判定役がいるのは auto だけである
//	agent.relay_trusted_comments      … 利用者が止められる
//	tracker.comments.self_marker が空でない … 空だと continuo 自身の「Status を動かしました」に目印が付かず、
//	                                     人間のコメントとして渡るため
//
// cfg: 設定。
// 戻り値: 3つとも満たせば true。
func relayEnabled(cfg config.Config) bool {
	return cfg.Claude.PermissionMode == config.ClaudePermissionModeAuto &&
		cfg.Agent.RelayTrustedComments &&
		strings.TrimSpace(cfg.Tracker.Comments.SelfMarker) != ""
}

// relayRequestedWithoutSelfMarker は、relay を選んでいるのに self_marker が空で効かないかを返す
// （起動時の WARN に使う。設計 3-85）。
//
// cfg: 設定。
// 戻り値: auto で relay_trusted_comments が真なのに self_marker が空なら true。
func relayRequestedWithoutSelfMarker(cfg config.Config) bool {
	return cfg.Claude.PermissionMode == config.ClaudePermissionModeAuto &&
		cfg.Agent.RelayTrustedComments &&
		strings.TrimSpace(cfg.Tracker.Comments.SelfMarker) == ""
}

// relayAIMarkers は、AI が書いたとみなす本文の先頭の印を全部返す（設計 3-85）。
//
// cfg: 設定。
// 戻り値: 固定の4つに、空でない `tracker.comments.marker` と `self_marker` を足したもの。
func relayAIMarkers(cfg config.Config) []string {
	out := append([]string{}, relayFixedAIMarkers...)
	for _, m := range []string{cfg.Tracker.Comments.Marker, cfg.Tracker.Comments.SelfMarker} {
		if strings.TrimSpace(m) != "" {
			out = append(out, m)
		}
	}
	return out
}

// relayAgentMarkers は、「前の回が閉じられたことを確かめられない」の判定に使う印を返す（設計 3-85）。
//
// **`<!-- continuo:agent -->` と、空でない `tracker.comments.marker` である。**
// 空の前方一致は全部のコメントに当たるので、空なら足さない。
//
// cfg: 設定。
// 戻り値: 印の一覧。
func relayAgentMarkers(cfg config.Config) []string {
	out := []string{"<!-- continuo:agent -->"}
	if m := cfg.Tracker.Comments.Marker; strings.TrimSpace(m) != "" && m != out[0] {
		out = append(out, m)
	}
	return out
}

// startsWithMarker は、本文の1行目が印で始まるかを返す（行頭の照合。設計 3-85）。
//
// **飛ばすのは前の空白・タブ・改行（`[ \t\r\n]*`）だけである**（組み込みの指示書 4-1 の jq の式と同じ）。
// 全角空白などは飛ばさない。jq と違う判定にすると、エージェントが自分で読んだ結果と食い違う。
//
// body: 本文。
// markers: 印の一覧。
// 戻り値: どれかで始まれば true。
func startsWithMarker(body string, markers []string) bool {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	for _, m := range markers {
		if m != "" && strings.HasPrefix(trimmed, m) {
			return true
		}
	}
	return false
}

// trustedAssociation は、投稿者の立場が信頼できるものかを返す（設計 3-85）。
//
// association: `authorAssociation` の値。
// 戻り値: OWNER / MEMBER / COLLABORATOR のどれかなら true。
func trustedAssociation(association string) bool {
	for _, a := range relayTrustedAssociations {
		if strings.EqualFold(strings.TrimSpace(association), a) {
			return true
		}
	}
	return false
}

// commentNumber は、コメントの URL の `#issuecomment-<番号>` から番号を読む（設計 3-85）。
//
// **同じ秒に書かれたコメントの前後を決めるためだけに使う。**GitHub の作成時刻は秒までしか無い。
//
// rawURL: コメントの URL。
// 戻り値の1つ目: 番号。
// 戻り値の2つ目: 読めたら true。
func commentNumber(rawURL string) (int64, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return 0, false
	}
	const prefix = "issuecomment-"
	if !strings.HasPrefix(u.Fragment, prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(u.Fragment, prefix), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// relayOrder は、あるコメントが境目より後かを返す（設計 3-85）。
//
// **作成時刻で決める。**同じ秒なら URL の番号を数として比べ、大きいほうを後とみなす。
//
// c: 比べるコメント。
// boundary: 境目。
// 戻り値の1つ目: 後なら true。
// 戻り値の2つ目: 決められたら true（同じ秒で、どちらかの番号を読めなければ false）。
func relayOrder(c, boundary tracker.Comment) (bool, bool) {
	if c.CreatedAt.After(boundary.CreatedAt) {
		return true, true
	}
	if c.CreatedAt.Before(boundary.CreatedAt) {
		return false, true
	}
	cn, ok1 := commentNumber(c.URL)
	bn, ok2 := commentNumber(boundary.URL)
	if !ok1 || !ok2 {
		return false, false
	}
	return cn > bn, true
}

// lastTouched は、作成時刻と更新時刻の新しいほうを返す。
//
// **更新時刻が取れなければゼロ値なので、作成時刻が残る。**
func lastTouched(c tracker.Comment) time.Time {
	if c.UpdatedAt.After(c.CreatedAt) {
		return c.UpdatedAt
	}
	return c.CreatedAt
}

// relayVerdict は、コメントを選んだ結果の種類である（設計 3-85）。
type relayVerdict int

const (
	// relayNoBoundary は、閉じた記録が1件も無い（初めての着手と同じ）。**何も渡さない。**
	relayNoBoundary relayVerdict = iota
	// relayUnverified は、境目より後に AI の報告があり、前の回が閉じられたことを確かめられない。
	relayUnverified
	// relayIncomplete は、ページ数の上限で古い側を読み切れず、境目より後を全部読めたと言えない。
	relayIncomplete
	// relayOK は、渡すコメントを選べた（0件のこともある）。
	relayOK
)

// relaySelection はコメントを選んだ結果である。
type relaySelection struct {
	// Verdict は結果の種類である。
	Verdict relayVerdict
	// Picked は渡すコメントである（**古い順**）。Verdict が relayOK のときだけ入る。
	Picked []tracker.Comment
	// Boundary は境目にした閉じた記録である（ログに出す）。
	Boundary tracker.Comment
}

// selectRelayComments は、渡すコメントを選ぶ（設計 3-85）。
//
//	境目       … 信頼できる立場が書いた閉じた記録のうち、作成時刻がいちばん新しいもの
//	渡すもの   … 境目より後に作られ、信頼できる立場で、AI の印が無く、隠されていないコメント
//	渡さない   … 境目より後に、信頼できる立場の AI の報告（`<!-- continuo:agent -->` か marker）がある
//	             （前の回が閉じられたことを確かめられない）
//
// **外部の人が書いた閉じた記録は境目にしない。**境目を後ろへずらされると、許可が渡らなくなる。
// **読み切れなかったときは、読めた中でいちばん古い更新時刻が、境目の作成時刻以下なら全部読めている**
// とみなす（コメントは更新日時の新しい順に読み、落ちるのは古い側だけである）。
//
// comments: 読んだコメント（並びは問わない）。
// truncated: ページ数の上限で古い側を読み切れなかったなら true。
// aiMarkers: AI が書いたとみなす印（`relayAIMarkers`）。
// agentMarkers: 前の回の報告とみなす印（`relayAgentMarkers`）。
// 戻り値: 選んだ結果。
func selectRelayComments(
	comments []tracker.Comment, truncated bool, aiMarkers, agentMarkers []string,
) relaySelection {
	sorted := append([]tracker.Comment{}, comments...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	var boundary tracker.Comment
	found := false
	for _, c := range sorted {
		if !trustedAssociation(c.AuthorAssociation) || !startsWithMarker(c.Body, []string{config.ClosedMarker}) {
			continue
		}
		if !found {
			boundary, found = c, true
			continue
		}
		// **同じ秒の記録は番号で比べる。**番号が読めなければ、並べ直した順（後ろのもの）を採る。
		if after, ok := relayOrder(c, boundary); after || !ok {
			boundary = c
		}
	}

	if truncated {
		oldest := time.Time{}
		for i, c := range sorted {
			t := lastTouched(c)
			if i == 0 || t.Before(oldest) {
				oldest = t
			}
		}
		if !found || oldest.After(boundary.CreatedAt) {
			return relaySelection{Verdict: relayIncomplete}
		}
	}
	if !found {
		return relaySelection{Verdict: relayNoBoundary}
	}

	var picked []tracker.Comment
	for _, c := range sorted {
		if c.ID != "" && c.ID == boundary.ID {
			continue
		}
		after, known := relayOrder(c, boundary)
		if !trustedAssociation(c.AuthorAssociation) {
			continue
		}
		// **前の回が閉じられたことを確かめる。**同じ秒で前後を決められないものも、後ろとして数える
		// （確かめられないほうへ倒す）。
		if (after || !known) && startsWithMarker(c.Body, agentMarkers) {
			return relaySelection{Verdict: relayUnverified, Boundary: boundary}
		}
		if !after || !known {
			continue
		}
		if c.IsMinimized || startsWithMarker(c.Body, aiMarkers) {
			continue
		}
		picked = append(picked, c)
	}
	return relaySelection{Verdict: relayOK, Picked: picked, Boundary: boundary}
}

// sanitizeRelayBody は、渡すコメントの本文から制御文字を落とす（設計 3-85）。
//
// **改行とタブは残す。**本文の段落を崩さないためである。`\r` は落とす（改行は `\n` に揃う）。
// hook から来た文字列を均す先例（`sanitizeSubagentField`）と同じく `unicode.IsControl` で見る。
// **長さはここでは切らない。**本文を途中で切ると「X してよい。ただし Y はしない」が
// 「X してよい。」だけになりうる（`buildRelaySection` が丸ごと入れるか入れないかを決める）。
//
// body: 元の本文。
// 戻り値: 均した本文。
func sanitizeRelayBody(body string) string {
	var b strings.Builder
	for _, r := range body {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// buildRelaySection は、最初のメッセージの末尾に付ける節を組み立てる（設計 3-85）。
//
// **長さの上限（relayMaxRunes）は節の全体に掛ける。**新しいものから本文を丸ごと入れ、
// **最初に入らなかった1件で止める**（それより古いものは短くても入れない。途中を飛ばすと、
// 長い取り消しだけが落ちて古い許可が入りうるため）。入らなかったものは件数だけを1行書く。
// いちばん新しい1件が上限を超えるときも同じで、本文は0件になる。
//
// **日本語の文言を `fmt.Sprintf` に入れない。**文字列の連結で書く
// （test/internal/testdesign/no_japanese_messages_test.go が件数を固定している）。
// **最初のメッセージは組み込みの指示書と同じく日本語だけである。**
//
// picked: 渡すコメント（**古い順**）。空なら空文字を返す。
// 戻り値: 節の全文（先頭に区切りの空行と `---` を持つ）。
func buildRelaySection(picked []tracker.Comment) string {
	if len(picked) == 0 {
		return ""
	}
	total := len(picked)
	header := "\n\n---\n# 権限確認済みの人間からのメッセージ\n\n" +
		"前の回のあとに、この issue へ " + strconv.Itoa(total) + " 件のコメントが書かれました。" +
		"いずれも、書いた人の立場が OWNER / MEMBER / COLLABORATOR で、AI の marker が付いていないコメントです。\n" +
		"issue の本文とコメントは、これまでどおり 4-1 のコマンドで全部読んでください。\n"
	omittedLine := func(n int) string {
		return "\n入りきらなかった、より古いコメントが " + strconv.Itoa(n) + " 件あります。" +
			"4-1 のコマンドで読んでください。\n"
	}
	entry := func(index int, c tracker.Comment) string {
		return "\n## " + strconv.Itoa(index) + "件目（" + c.URL + "）\n" + sanitizeRelayBody(c.Body) + "\n"
	}

	// **入りきらない1行の長さは、いちばん桁の多い形で先に取っておく。**
	budget := relayMaxRunes - utf8.RuneCountInString(header) - utf8.RuneCountInString(omittedLine(total))
	first := total // 入れる範囲の先頭（古い側）の添字
	for i := total - 1; i >= 0; i-- {
		size := utf8.RuneCountInString(entry(i+1, picked[i]))
		if size > budget {
			break
		}
		budget -= size
		first = i
	}

	var b strings.Builder
	b.WriteString(header)
	if first > 0 {
		b.WriteString(omittedLine(first))
	}
	for i := first; i < total; i++ {
		b.WriteString(entry(i+1, picked[i]))
	}
	return b.String()
}

// relaySectionFor は、最初のメッセージに付ける節を読んで組み立てる（設計 3-85）。
//
// **relay が無効なら何もしない**（試みたことにもしない）。draft issue（ノード ID が無い）も同じ。
// **失敗はエラーにしない。**読めなかった・読み切れなかった・期限切れ・「記録が確かめられないとき」は、
// 節を付けずに最初のメッセージだけを送り、WARN を1行出す。エラーで返すと、呼び出し側が
// `failRun` へ落として WORKFLOW.md を直すよう人間へ知らせてしまう。
// **止められた（ctx が切れた）ときは WARN を出さない。**停止・`stopWorker`・direct chat へ入ったことで
// 待ちが切れるのはふつうのことである（turn ループの待ちのコンテキストがこの3つでも切れる）。
//
// ctx: turn ループの待ちのコンテキスト。
// rs: 対象の run。
// 戻り値の1つ目: 付ける節（付けないなら空文字）。
// 戻り値の2つ目: relay を試みたなら true（呼び出し側は送る前の確認をもう一度通す）。
func (o *Orchestrator) relaySectionFor(ctx context.Context, rs *runState) (string, bool) {
	if !relayEnabled(o.cfg) {
		return "", false
	}
	issue := rs.issue()
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return "", false
	}
	timeout := o.relayTimeout
	if timeout <= 0 {
		timeout = relayFetchTimeout
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	comments, truncated, err := o.tracker.FetchRelayComments(fetchCtx, nodeID)
	if err != nil {
		if ctx.Err() == nil {
			o.logger.Warn("issue のコメントを読めないので、人間のコメントを付けずに最初のメッセージを送ります",
				"identifier", issue.Identifier, "error", err)
		}
		return "", true
	}
	sel := selectRelayComments(comments, truncated, relayAIMarkers(o.cfg), relayAgentMarkers(o.cfg))
	switch sel.Verdict {
	case relayNoBoundary:
		o.logger.Debug("閉じた記録がまだ無いので、人間のコメントは付けません", "identifier", issue.Identifier)
		return "", true
	case relayIncomplete:
		o.logger.Warn("コメントが多すぎて境目より後を読み切れたか分からないので、人間のコメントを付けずに最初のメッセージを送ります",
			"identifier", issue.Identifier, "読んだ件数", len(comments))
		return "", true
	case relayUnverified:
		o.logger.Warn("閉じた記録より後にエージェントの報告があり、前の回が閉じられたことを確かめられないので、"+
			"人間のコメントを付けずに最初のメッセージを送ります（direct chat の間に報告を書かせた場合と、"+
			"起動直後に前の会話の続きを走らせた Claude Code が報告を書いた場合も当たります）",
			"identifier", issue.Identifier, "境目", sel.Boundary.URL)
		return "", true
	}
	if ctx.Err() != nil {
		return "", true
	}
	if len(sel.Picked) == 0 {
		return "", true
	}
	o.logger.Info("閉じた記録より後に人間が書いたコメントを、最初のメッセージに付けて渡します",
		"identifier", issue.Identifier, "件数", len(sel.Picked), "境目", sel.Boundary.URL)
	return buildRelaySection(sel.Picked), true
}

// paneAlreadyGone は、`pane.close` の失敗が「その pane は無い」かを返す（設計 3-85）。
//
// **無いなら Claude Code は動いていないので、閉じたとみなす**（閉じた記録を書く）。
// herdr が返す形は 2026-09-29 に herdr 0.9.1 で測った（`herdr.ErrCodePaneNotFound`）。
//
// err: `pane.close` が返したエラー。
// 戻り値: その pane が無いという誤りなら true。
func paneAlreadyGone(err error) bool {
	return err != nil && herdr.IsCode(err, herdr.ErrCodePaneNotFound)
}

// settleClosedRecord は、pane を閉じようとしたあとで、閉じた記録を書くか・保留するか・捨てるかを決める
// （設計 3-85。issue #246）。
//
// **書くのは次のどちらかのときである。**
//
//	閉じた pane が、その run の `startedPaneID`（その pane で `agent.start` が成功した）と同じ
//	その run に保留が立っている（閉じる pane が無くても書く）
//
// **閉じ損ねたら書かない**（保留も捨て、以後この run は書かない）。Claude Code が生きたまま記録を
// 付けないためである。**`agent.start` に失敗した pane を閉じたときも書かない。**書くと、人間が
// 前の記録のあとに書いた許可が、まだ一度も渡らないまま境目より前へ押し出される。
// **何を送ったかは見ない。**何も送らなくても、前の会話の続きを走らせて起動した Claude Code は書ける。
//
// ctx: 呼び出しに適用するコンテキスト（書き込みは切り離して別の期限で行う）。
// rs: 対象の run。
// paneID: 閉じようとした pane の ID。空なら閉じる pane が無かった。
// closed: 閉じられた（か、その pane は既に無かった）なら true。
// mode: 書く・書かない・保留する。
func (o *Orchestrator) settleClosedRecord(
	ctx context.Context, rs *runState, paneID string, closed bool, mode closedRecordMode,
) {
	started, state := rs.takePaneClosed(paneID, closed)
	if mode == closedRecordSkip {
		rs.setClosedRecord(closedRecordNone)
		return
	}
	if state == closedRecordCloseFailed {
		if paneID != "" && !closed {
			// いま閉じ損ねた。
			return
		}
		if !o.closeFailedPanes(ctx, rs) {
			return
		}
		// **閉じ損ねていた pane で Claude Code が動いていたかもしれないので、書く。**
		started = true
	}
	if !started && state != closedRecordPending {
		return
	}
	if mode == closedRecordDefer {
		rs.setClosedRecord(closedRecordPending)
		return
	}
	rs.setClosedRecord(closedRecordNone)
	o.recordWorkerClosed(ctx, rs.issue())
}

// closeFailedPanes は、この run が閉じ損ねた pane をもう一度閉じてみる（設計 3-85d）。
//
// **pane の ID だけでは閉じない。**herdr は pane の ID を使い回しうるので、別の issue の pane を閉じないよう、
// `pane.list` で cwd がこの run の worktree（かその内側）にある pane だけを閉じる。
// 一覧に無い pane は、もう無いものとして外す。cwd が worktree の外にある pane は、使い回された別の pane として外す。
// **worktree か pane の cwd のパスを解決できないときは、どちらとも決められないので、控えたまま書かない。**
//
// ctx: 呼び出しに適用するコンテキスト（止められていたら切り離し、herdr の読み取りの期限を付ける）。
// rs: 対象の run。
// 戻り値: 閉じ損ねた pane が1枚も残っていなければ true。
func (o *Orchestrator) closeFailedPanes(ctx context.Context, rs *runState) bool {
	ids := rs.failedPanes()
	if len(ids) == 0 {
		return true
	}
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx),
			time.Duration(o.cfg.Herdr.ReadTimeoutMs)*time.Millisecond)
		defer cancel()
	}
	identifier := rs.issue().Identifier
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		o.logger.Warn("閉じ損ねた pane が残っているかを確かめられないので、Claude Code を閉じた記録は書きません",
			"identifier", identifier, "pane_id", ids, "error", err)
		return false
	}
	root, rootOK := resolvePath(rs.WorktreePath)
	if !rootOK {
		// **worktree の場所が分からないと、残っている pane がこの run のものかを決められない。**控えたまま書かない。
		o.logger.Warn("worktree のパスを解決できないので、閉じ損ねた pane を確かめられません（Claude Code を閉じた記録は書きません）",
			"identifier", identifier, "pane_id", ids, "path", rs.WorktreePath)
		return false
	}
	cwdOf := make(map[string]string, len(list.Panes))
	for _, p := range list.Panes {
		cwdOf[p.PaneID] = p.Cwd
	}
	empty := false
	for _, id := range ids {
		cwd, alive := cwdOf[id]
		if !alive {
			empty = rs.forgetFailedPane(id)
			continue
		}
		got, ok := resolvePath(cwd)
		if !ok {
			// **cwd が解決できない pane は、この run のものかを決められない。**控えたまま書かない。
			o.logger.Warn("閉じ損ねた pane の cwd を解決できないので、Claude Code を閉じた記録は書きません",
				"identifier", identifier, "pane_id", id, "cwd", cwd)
			return false
		}
		if got != root && !isUnder(root, got) {
			o.logger.Warn("閉じ損ねた pane の ID が別の場所の pane に使われているので、閉じません",
				"identifier", identifier, "pane_id", id, "cwd", cwd)
			empty = rs.forgetFailedPane(id)
			continue
		}
		if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: id}); err != nil && !paneAlreadyGone(err) {
			o.logger.Warn("閉じ損ねた pane をもう一度閉じられませんでした（Claude Code を閉じた記録は書きません）",
				"identifier", identifier, "pane_id", id, "error", err)
			return false
		}
		o.logger.Info("閉じ損ねていた pane を閉じました", "identifier", identifier, "pane_id", id)
		empty = rs.forgetFailedPane(id)
	}
	return empty
}

// recordWorkerClosed は、issue へ閉じた記録を1件書く（設計 3-85。issue #246）。
//
// **書くのは relay が有効なときだけである。**draft issue にはコメントできないので書かない。
// **担当者が他人のアカウント1人のときは書かない**（担当が別の機械へ移ったあと。設計 3-77c・3-83h）。
// 判定は `judgeDirectChatAssignees` を使い、担当者が0人・2人以上・自分のログイン名が取れないときは書く
// （書かないと境目が前の run に残り、次の run が渡らなくなる）。
//
// **`self_marker` は付けない**（`postOwnMarkedComment`）。1行目が `<!-- continuo:closed -->` そのものでないと、
// 別の機械が境目として読めない。2行目の文は利用者の言語で引く。
//
// **書き込みは、止められた ctx から切り離し、10秒の期限で1回だけ行う。**失敗したら WARN を1行出す。
// やり直さない（`addComment` は同じものを2回書くことがある）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) recordWorkerClosed(ctx context.Context, issue tracker.Issue) {
	if !relayEnabled(o.cfg) {
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closedRecordWriteTimeout)
	defer cancel()
	if o.judgeDirectChatAssignees(writeCtx, issue) == assigneeOther {
		o.logger.Info("担当者が別のアカウントなので、Claude Code を閉じた記録は書きません",
			"identifier", issue.Identifier, "担当者", strings.Join(assigneeLogins(issue), ", "))
		return
	}
	body := config.ClosedMarker + "\n" + i18n.T(i18n.KeyOrchestratorRelayClosedRecord)
	if err := o.postOwnMarkedComment(writeCtx, nodeID, body); err != nil {
		o.logger.Warn("Claude Code を閉じた記録を issue へ書けませんでした"+
			"（次の起動では、この記録より前のコメントが境目になります）",
			"identifier", issue.Identifier, "error", err)
		return
	}
	o.logger.Info("Claude Code を閉じた記録を issue へ書きました", "identifier", issue.Identifier)
}
