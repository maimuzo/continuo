package orchestrator

import (
	"context"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/redact"
)

// 確認の画面で止まったときに、閉じる前に画面を1回読み、何で止まったかと直し方を
// 人間へ渡す（設計 3-11 の「確認の画面の文を引き渡しに載せる」。issue #347）。
//
// **読むだけで、Claude Code へは何も送らない。**入力を送らないので、保留中の要求は承認されない。
//
// **source は `detection` である。**`recent_unwrapped`（と `recent`）は `blocked` のあいだ
// `lines` を付けて読めない（herdr 0.9.1 が `agent_not_idle` を返す。2026-11-12 に実測）。
// `detection` は herdr が `blocked` の判定に使っている下部の平文で、確認の画面がそこに入る。
const (
	// blockedScreenReadLines は読む行数である。確認の画面は 20 行ほどに収まる（実測した4画面）。
	blockedScreenReadLines = 40
	// blockedScreenMaxLines は載せる行数の上限である。
	blockedScreenMaxLines = 20
	// blockedScreenHeadLines は、上限を超えたときに残す頭の行数である（見出しを残す）。
	blockedScreenHeadLines = 5
	// blockedScreenTailLines は、上限を超えたときに残す終わりの行数である（問いと選択肢を残す）。
	blockedScreenTailLines = 15
	// blockedScreenMaxRunes は載せる文字数（rune）の上限である。
	blockedScreenMaxRunes = 2000
	// blockedScreenErrRunes は、読めなかったときに載せる誤りの文の上限（rune）である。
	blockedScreenErrRunes = 200
	// blockedScreenRuleMin は「全幅の罫線だけの行」と見なす `─` の最小の個数である。
	blockedScreenRuleMin = 10
)

// blockedKind は、確認の画面の種類である。
type blockedKind int

const (
	// blockedKindUnknown は見分けられなかったことを表す（読めなかった・読み直して確認の画面でなかったも含む）。
	blockedKindUnknown blockedKind = iota
	// blockedKindToolPermission はツールを使ってよいかの確認である（`Do you want to` と `1. Yes`）。
	blockedKindToolPermission
	// blockedKindFolderTrust はフォルダを信頼するかの確認である（`Yes, I trust this folder` か `Accessing workspace:`）。
	blockedKindFolderTrust
	// blockedKindAskUserQuestion は AskUserQuestion の質問の画面である（`Enter to select` と選択肢）。
	blockedKindAskUserQuestion
)

// String はログに出す名前を返す。
//
// 戻り値: 種類の名前。
func (k blockedKind) String() string {
	switch k {
	case blockedKindToolPermission:
		return "tool_permission"
	case blockedKindFolderTrust:
		return "folder_trust"
	case blockedKindAskUserQuestion:
		return "ask_user_question"
	default:
		return "unknown"
	}
}

// blockedScreen は、確認の画面で止まったときに読んだ画面の解析結果である。
//
// **ここに入る文字列は、どれも無害化したあとのものである**（制御文字と ANSI を落とし、資格情報の形を伏せた）。
type blockedScreen struct {
	// ReadErr は読めなかったときの理由である（無害化して 200 字で切ったもの）。空なら読めた。
	ReadErr string
	// NotBlocked は、読み直したときに `blocked` ではなくなっていたことを表す（画面は使わない）。
	NotBlocked bool
	// Excerpt は載せる抜粋である（囲む前。上限と中略を当てたもの）。
	Excerpt string
	// Extracted は確認の画面を切り出せたなら true である。偽なら Excerpt は画面の下の行である。
	Extracted bool
	// Kind は見分けた種類である。
	Kind blockedKind
	// Heading はツールの権限の確認の見出し（`Bash command` など）である。使えないときは空である。
	Heading string
	// NeedsApproval は `don’t ask again for:` の後ろの文字列（許可が要る部分）である。無ければ空である。
	// **囲みの外には出さない。**ログとテストのためだけに持つ。
	NeedsApproval string
}

// hasScreen は、載せる画面の文を持っているかを返す。
//
// 戻り値: 読めて、読み直しでも確認の画面だったなら true。
func (s blockedScreen) hasScreen() bool {
	return s.ReadErr == "" && !s.NotBlocked
}

// escMayLookLikeRejection は、esc を送ったあとに会話へ残る文が「人間の拒否」に見えうるかを返す。
//
// **ツールの権限の確認・AskUserQuestion・見分けられなかったもの（読めなかったときを含む）**である。
// esc で閉じたツールの確認は会話の記録に「The user doesn't want to proceed with this tool use」を、
// AskUserQuestion は画面に「User declined to answer questions」を残す（2026-11-12 に実測）。
// **読み直したときに確認の画面ではなくなっていたなら出さない。**そのときの esc は確認の画面を閉じていない
// （走っている turn を中断しうるが、そのとき会話に残る文は測っていない。補足は測った2つの文を名指しするので、
// 測っていない場面で言い切らない）。
// フォルダの信頼は、ツールの呼び出しも質問も無いので出さない。
//
// 戻り値: 補足を出すべきなら true。
func (s blockedScreen) escMayLookLikeRejection() bool {
	if s.NotBlocked {
		return false
	}
	return s.Kind == blockedKindToolPermission || s.Kind == blockedKindAskUserQuestion || s.Kind == blockedKindUnknown
}

// wantsPermissionRemedy は、画面の部分のあとに許可の対処（`permissionRemedyText`）を続けるかを返す。
//
// **AskUserQuestion とフォルダの信頼だと見分けられたときは続けない。**画面の部分がその種類の直し方
// （deny を確かめる・`continuo trust`）を出しており、続けて「allow に狭い規則を足す」を出すと逆向きの直し方が並ぶ。
// 読めなかった・確認の画面でなくなっていた・見分けられなかったときは、汎用の対処として続ける。
//
// 戻り値: 続けるなら true。
func (s blockedScreen) wantsPermissionRemedy() bool {
	if !s.hasScreen() {
		return true
	}
	return s.Kind != blockedKindAskUserQuestion && s.Kind != blockedKindFolderTrust
}

// blockedStartupError は、起動直後に確認の画面で止まったことを表す誤りに、読んだ画面を持たせたものである。
//
// **`Error()` はもとの誤り（`KeyOrchestratorConfirmStartupBlocked` の文）をそのまま返す。**
// 画面を載せる出口（着手の引き渡し・コメントを書かせるための復元）は `errors.As` で取り出す。
// 取り出さない出口（復帰の作り直し・direct chat の用意）は、いままでどおりの文を使い、画面はログにだけ残る。
type blockedStartupError struct {
	// screen は esc を送る前に読んだ画面である。
	screen blockedScreen
	// err はもとの誤りである。
	err error
}

// Error はもとの誤りの文を返す。
//
// 戻り値: もとの誤りの文。
func (e *blockedStartupError) Error() string { return e.err.Error() }

// Unwrap はもとの誤りを返す。
//
// 戻り値: もとの誤り。
func (e *blockedStartupError) Unwrap() error { return e.err }

// readBlockedScreen は、確認の画面で止まった run の画面を1回読み、解析してログに書く。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// recheck: 真なら、読んだ直後に状態を1回確かめ、`blocked` でなければ画面を使わない
// （作業の途中の道だけ。subagent を待ったあとなので、そのあいだに画面が変わりうる）。
// 戻り値: 解析した結果。
func (o *Orchestrator) readBlockedScreen(ctx context.Context, rs *runState, recheck bool) blockedScreen {
	return o.readBlockedScreenOf(ctx, rs.issue().Identifier, rs.agentName(), recheck)
}

// readBlockedScreenOf は readBlockedScreen の本体である（runState を持たない再起動の引き継ぎからも呼ぶ）。
//
// **結果は、どの道でもログに WARN で1回書く。**コメントに載せられない道（復帰の作り直し・
// direct chat の用意・先に引き渡しを投稿した道）では、これが唯一の記録である。
//
// ctx: 呼び出しに適用するコンテキスト。
// identifier: ログに出す issue の識別子。
// agent: 読む agent の名前。空なら読まない。
// recheck: 真なら、読んだ直後に状態を1回確かめる。
// 戻り値: 解析した結果。
func (o *Orchestrator) readBlockedScreenOf(
	ctx context.Context, identifier string, agent normalize.SafeName, recheck bool,
) blockedScreen {
	ja := blockedScreenCatalog()
	if agent == "" {
		s := blockedScreen{ReadErr: ja.T(i18n.KeyOrchestratorBlockedScreenErrNoAgent)}
		o.logger.Warn("確認の画面で止まったときの画面を読めませんでした", "identifier", identifier, "error", s.ReadErr)
		return s
	}
	res, err := o.herdr.AgentRead(ctx, herdr.AgentReadParams{
		Target: agent,
		Source: herdr.ReadSourceDetection,
		Lines:  blockedScreenReadLines,
	})
	if err != nil {
		s := blockedScreen{ReadErr: blockedScreenErrText(err.Error())}
		// **context が終わっているなら、ログを下げる。**別の経路が run を終わらせている最中で、
		// 読めないのは当たり前である（このあとの終わらせる処理の確認で戻る）。
		if ctx.Err() != nil {
			o.logger.Debug("確認の画面で止まったときの画面を読めませんでした（止まる途中）", "identifier", identifier, "error", s.ReadErr)
			return s
		}
		o.logger.Warn("確認の画面で止まったときの画面を読めませんでした", "identifier", identifier, "error", s.ReadErr)
		return s
	}
	if res == nil || strings.TrimSpace(sanitizeScreenText(res.Read.Text)) == "" {
		s := blockedScreen{ReadErr: ja.T(i18n.KeyOrchestratorBlockedScreenErrEmpty)}
		o.logger.Warn("確認の画面で止まったときの画面を読めませんでした", "identifier", identifier, "error", s.ReadErr)
		return s
	}
	if recheck {
		got, gerr := o.herdr.AgentGet(ctx, herdr.AgentGetParams{Target: agent})
		if gerr == nil && got != nil && got.Agent.AgentStatus != herdr.AgentStatusBlocked {
			o.logger.Warn("確認の画面で止まったときの画面を読みましたが、読み直したときには確認の画面ではありませんでした",
				"identifier", identifier, "agent_status", string(got.Agent.AgentStatus))
			return blockedScreen{NotBlocked: true}
		}
		// **状態を取れなかったときは、画面を使う。**読んだ時点で確認の画面だったかは分からないが、
		// `blocked` を見てから数十秒しか経っていない。載せないより、載せて種類を見分けるほうが手がかりになる。
		if gerr != nil {
			o.logger.Debug("読んだ直後の状態を確かめられませんでした（読んだ画面を使います）",
				"identifier", identifier, "error", gerr)
		}
	}
	s := parseBlockedScreen(res.Read.Text)
	// **「引き渡しに載せる」とは書かない。**載せない道（復帰の作り直し・direct chat の用意・
	// 先に引き渡しを投稿した道）では、このログが唯一の記録である。
	o.logger.Warn("確認の画面で止まったときの画面を読みました",
		"identifier", identifier, "種類", s.Kind.String(), "切り出せたか", s.Extracted,
		"見出し", s.Heading, "許可が要る部分", s.NeedsApproval, "抜粋", s.Excerpt)
	return s
}

// blockedScreenCatalog は、画面の部分の文言を引く資源を返す。
//
// **日本語で引く。**引き渡しのコメントの外枠（`buildHandoffComment`・`runStartOrFail`）と
// `permissionRemedyText` が日本語で書いてあるので、合わせる（混ざると読みにくい）。
//
// 戻り値: 日本語の資源。無ければいまの言語の資源。
func blockedScreenCatalog() *i18n.Catalog {
	if c, ok := i18n.CatalogOf(i18n.LangJA); ok {
		return c
	}
	c, _ := i18n.CatalogOf(i18n.Current())
	return c
}

// blockedScreenErrText は、読めなかった理由を載せられる形にする（1行目だけ・無害化・200 字）。
//
// msg: 誤りの文。
// 戻り値: 載せる文。
func blockedScreenErrText(msg string) string {
	clean := sanitizeScreenText(msg)
	if i := strings.IndexByte(clean, '\n'); i >= 0 {
		clean = clean[:i]
	}
	clean = redactScreenSecrets(strings.TrimSpace(clean))
	// **囲みの外に出るので、backtick を落とし、`@` を全角にする**（設計 3-23。`sanitizeSubagentField` と同じ理由）。
	// 誤りの文は herdr の応答の本体（画面の文）を含みうる。落とさないと code span を抜けたり、
	// `@名前` が人への通知になったりする。
	clean = strings.ReplaceAll(clean, "`", "")
	clean = strings.ReplaceAll(clean, "@", "＠")
	return truncateRunes(clean, blockedScreenErrRunes)
}

// ansiEscape は ANSI のエスケープ列である（CSI・OSC・2文字のもの）。
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// sanitizeScreenText は、画面の文から ANSI のエスケープ列と制御文字を落とす。
//
// **改行とタブだけを残す。**C0（改行とタブ以外）・DEL・C1 を落とす。`\r` も落とす。
//
// text: 読んだ文。
// 戻り値: 落としたあとの文。
func sanitizeScreenText(text string) string {
	text = ansiEscape.ReplaceAllString(text, "")
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// 落とす。
		case r == utf8.RuneError:
			// 壊れたバイト列も落とす。
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// screenSecretRules は伏せ字の規則である。**網羅ではない。**知らない形の秘密は残りうる。
//
// **伏せるのは `value` 番目の組だけである**（0 なら一致の全体）。前置き（`Authorization:`・`token=`・
// `://`）と後ろの `@` は残す。`redactScreenSecrets`（1つの文）と `redactScreenLines`（行をまたぐ）の
// 両方が同じ規則を使い、結果の形を揃える。
var screenSecretRules = []struct {
	re    *regexp.Regexp
	value int
}{
	{regexp.MustCompile(`(?i)(authorization:[ \t]*)([^\s"'` + "`" + `]+(?:[ \t]+[^\s"'` + "`" + `]+)?)`), 2},
	{regexp.MustCompile(`(?i)(bearer[ \t]+)([^\s"'` + "`" + `]+)`), 2},
	{regexp.MustCompile(`(?:ghp_|gho_|ghs_|ghu_|ghr_|github_pat_)[A-Za-z0-9_]+`), 0},
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]+`), 0},
	{regexp.MustCompile(`xox[abprs]-[A-Za-z0-9\-]+`), 0},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), 0},
	// **語の境目を求めない。**`access_token=` のように前に語が付いた形も伏せる。
	{regexp.MustCompile(`(?i)((?:token|password|secret|api_key)=)([^\s&"'` + "`" + `]+)`), 2},
	{regexp.MustCompile(`(://)([^/\s:@]+:[^/\s@]+)@`), 2},
}

// screenSecretRanges は、文の中で伏せる範囲（バイトの位置）を返す。
//
// text: 対象の文。
// 戻り値: [開始, 終端) の並び。
func screenSecretRanges(text string) [][2]int {
	var out [][2]int
	for _, rule := range screenSecretRules {
		for _, m := range rule.re.FindAllStringSubmatchIndex(text, -1) {
			i, end := m[2*rule.value], m[2*rule.value+1]
			if i >= 0 && end > i {
				out = append(out, [2]int{i, end})
			}
		}
	}
	return out
}

// redactScreenSecrets は、資格情報の形の文字列を `***` に置き換える（1つの文の中で当てる）。
//
// **画面の行には `redactScreenLines` を使う。**こちらは行をまたがない。
//
// text: 対象の文。
// 戻り値: 伏せたあとの文。
func redactScreenSecrets(text string) string {
	mask := make([]bool, len(text))
	for _, r := range screenSecretRanges(text) {
		for i := r[0]; i < r[1]; i++ {
			mask[i] = true
		}
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if mask[i] {
			if i == 0 || !mask[i-1] {
				b.WriteString("***")
			}
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// screenHomeDir は、ホームディレクトリの絶対パスを返す（`os.UserHomeDir`。テストは手元の本物のホームで動く）。
var screenHomeDir = os.UserHomeDir

// redactScreenLines は、画面の行に伏せ字を当てる。**行の境で割れた秘密とホームのパスも伏せる。**
//
// 画面の文は `detection`（画面の格子の平文）なので、pane の幅を超える行は複数の行に割れて届く
// （`recent_unwrapped` は `blocked` のあいだ読めない。2026-11-12 に実測）。1行ずつ当てると、
// 行の境で割れたトークンの後半や、途中で割れたホームのパスが、公開の issue に残る。
//
//  1. 行をつないだ文を2つ作る。**2行目からは行頭の空白を落とす**（確認の画面の中の行は字下げされて描かれるので、
//     割れた続きの行も字下げされている見込みが高い）。落とした字下げは、元の行にそのまま残す
//     - 行の境に何も挟まない文: 語の途中で割れたトークンをつなぎ直す
//     - 行の境に空白1つを挟む文: 空白で切って折り返した `Bearer` と値のように、空白を求める規則のため
//     （何も挟まないと `BearerTOKEN` になって当たらない）
//  2. 両方の文に伏せ字の規則（`screenSecretRanges`）を当て、伏せる位置を元の行の位置へ戻して合わせる
//  3. 何も挟まない文と、その文を各行の境で切った手前の文で、ホームのパスを探す
//     （`redact.HomeRanges`。投稿の直前の縮め（設計 3-73）と同じ綴りと境界）。切った手前の文で探すのは、
//     割れて行末で終わったパスの次の行が英数字で始まると、つないだ文では境界の判定で外れるためである。
//     1文字目を `~` に、残りを消す印を付ける
//  4. 印を元の行へ戻す。伏せる文字の連なりは、行ごとに `***` 1つにまとめる
//
// **伏せすぎは許す。**つないだせいで、行末の `token=` の次の行の頭の語まで伏せることがある。
//
// lines: 画面の行。
// 戻り値: 伏せたあとの行（行の数は変えない）。
func redactScreenLines(lines []string) []string {
	type pos struct{ line, idx int }
	indents := make([]string, len(lines))
	bodies := make([]string, len(lines))
	for li, l := range lines {
		body := l
		if li > 0 {
			body = strings.TrimLeft(l, " \t")
			indents[li] = l[:len(l)-len(body)]
		}
		bodies[li] = body
	}
	// build は、行の境に sep を挟んでつないだ文と、そのバイトごとの元の位置（挟んだ文字は line = -1）を返す。
	// starts は、各行（2行目から）が始まるバイトの位置である。
	build := func(sep string) (string, []pos, []int) {
		var b strings.Builder
		var where []pos
		var starts []int
		for li, body := range bodies {
			if li > 0 {
				for range sep {
					where = append(where, pos{-1, 0})
				}
				b.WriteString(sep)
				starts = append(starts, b.Len())
			}
			for bi := 0; bi < len(body); bi++ {
				where = append(where, pos{li, bi})
			}
			b.WriteString(body)
		}
		return b.String(), where, starts
	}
	const (
		keep = iota
		mask
		drop
		tilde
	)
	marks := make([][]int, len(lines))
	for li, body := range bodies {
		marks[li] = make([]int, len(body))
	}
	set := func(where []pos, i int, m int) {
		if w := where[i]; w.line >= 0 {
			marks[w.line][w.idx] = m
		}
	}
	for _, sep := range []string{"", " "} {
		js, where, _ := build(sep)
		for _, r := range screenSecretRanges(js) {
			for i := r[0]; i < r[1]; i++ {
				set(where, i, mask)
			}
		}
	}
	if home, err := screenHomeDir(); err == nil {
		js, where, starts := build("")
		ranges := redact.HomeRanges(js, home)
		for _, p := range starts {
			ranges = append(ranges, redact.HomeRanges(js[:p], home)...)
		}
		taken := make([]bool, len(js))
	ranges:
		for _, r := range ranges {
			for i := r[0]; i < r[1]; i++ {
				if taken[i] {
					continue ranges
				}
				if w := where[i]; w.line >= 0 && marks[w.line][w.idx] == mask {
					continue ranges
				}
			}
			for i := r[0]; i < r[1]; i++ {
				taken[i] = true
				if i == r[0] {
					set(where, i, tilde)
				} else {
					set(where, i, drop)
				}
			}
		}
	}
	res := make([]string, len(lines))
	for li, body := range bodies {
		var b strings.Builder
		b.WriteString(indents[li])
		for bi := 0; bi < len(body); bi++ {
			switch marks[li][bi] {
			case mask:
				// **行ごとに `***` 1つにまとめる。**同じ行の直前のバイトも伏せたなら書かない。
				if bi == 0 || marks[li][bi-1] != mask {
					b.WriteString("***")
				}
			case drop:
			case tilde:
				b.WriteByte('~')
			default:
				b.WriteByte(body[bi])
			}
		}
		res[li] = b.String()
	}
	return res
}

// isScreenRule は「全幅の `─`（U+2500）だけの行」かを返す（前後の空白を除いて 10 個以上）。
//
// **点線の `╌`（U+254C）は区切りではない**（Bash の確認の中でコマンドを囲む）。
//
// line: 1行。
// 戻り値: 罫線だけの行なら true。
func isScreenRule(line string) bool {
	t := strings.TrimSpace(line)
	if utf8.RuneCountInString(t) < blockedScreenRuleMin {
		return false
	}
	for _, r := range t {
		if r != '─' {
			return false
		}
	}
	return true
}

// screenHeadingAllowed は、見出しとして囲みの外へ出してよい文字だけでできているかを見る。
var screenHeadingAllowed = regexp.MustCompile(`^[A-Za-z0-9 _\-.:()]+$`)

// parseBlockedScreen は、読んだ画面を解析する（副作用は無い）。
//
//  1. 制御文字と ANSI を落とす
//  2. カーソル（`❯`）がある最後の行より上で、いちばん近い「全幅の `─` だけの行」から下を確認の画面とする。
//     無ければ下の 20 行を使う
//  3. 罫線の行と末尾の空行を落とし、各行の右の空白を落とす
//  4. 資格情報の形とホームのパスを、行をまたいで伏せる（`redactScreenLines`）
//  5. 種類・見出し・許可が要る部分を見分け、20 行を超えたら中略し、2000 字で切る
//
// text: herdr が返した画面の文。
// 戻り値: 解析した結果。
func parseBlockedScreen(text string) blockedScreen {
	ja := blockedScreenCatalog()
	lines := strings.Split(sanitizeScreenText(text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}

	// **起点は、選択肢のカーソル（`❯`）がある最後の行のすぐ上の罫線である。**
	// いちばん上の罫線から始めると、会話の表示に `─` だけの行があったとき、そこから下の会話が
	// 確認の画面として載る。AskUserQuestion は罫線を2本持つ（見出しの上と、最後の選択肢の上）ので、
	// カーソルの行より上でいちばん近いものを使う（実測した4画面のどれも、カーソルの行は確認の画面の中にある）。
	cursorAt := -1
	for i, l := range lines {
		if strings.Contains(l, "❯") {
			cursorAt = i
		}
	}
	ruleAt := -1
	for i := cursorAt - 1; i >= 0; i-- {
		if isScreenRule(lines[i]) {
			ruleAt = i
			break
		}
	}
	// **伏せ字は、行を切る前に、読んだ行の全体へ行をまたいで当てる**（`redactScreenLines`。行の数は変えない）。
	// 切ってから当てると、窓の頭で割れた秘密の前半が窓の外にあり、後半が規則に当たらずに残る。
	// 切り出しの起点・罫線・種類は、伏せる前の行（raw）で決める。
	red := redactScreenLines(lines)
	var idx []int
	extracted := ruleAt >= 0
	if extracted {
		for i := ruleAt + 1; i < len(lines); i++ {
			idx = append(idx, i)
		}
	} else {
		end := len(lines)
		for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		from := 0
		if end > blockedScreenMaxLines {
			from = end - blockedScreenMaxLines
		}
		for i := from; i < end; i++ {
			idx = append(idx, i)
		}
	}
	var raw, body []string
	for _, i := range idx {
		if isScreenRule(lines[i]) {
			continue
		}
		raw = append(raw, lines[i])
		body = append(body, red[i])
	}
	for len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) == "" {
		raw, body = raw[:len(raw)-1], body[:len(body)-1]
	}
	// **種類は伏せる前の文で見分ける。**行をまたいで伏せると、行末の `password=…` の続きとして
	// 次の行の頭の語（`Do you want to` の `Do`）まで伏せることがあり、見分けが外れる。
	// 見分けに使うのは決まった英語の言い回しだけで、外へは出さない。
	s := blockedScreen{Extracted: extracted}
	s.Kind = classifyBlockedScreen(strings.Join(raw, "\n"))
	if s.Kind == blockedKindToolPermission {
		if extracted {
			for _, l := range body {
				if h := strings.TrimSpace(l); h != "" {
					if screenHeadingAllowed.MatchString(h) {
						s.Heading = h
					}
					break
				}
			}
		}
		s.NeedsApproval = needsApprovalOf(body)
	}

	if len(body) > blockedScreenMaxLines {
		cut := make([]string, 0, blockedScreenHeadLines+1+blockedScreenTailLines)
		cut = append(cut, body[:blockedScreenHeadLines]...)
		cut = append(cut, ja.T(i18n.KeyOrchestratorBlockedScreenOmitted))
		cut = append(cut, body[len(body)-blockedScreenTailLines:]...)
		body = cut
	}
	excerpt := strings.Join(body, "\n")
	if utf8.RuneCountInString(excerpt) > blockedScreenMaxRunes {
		excerpt = truncateRunes(excerpt, blockedScreenMaxRunes) + "\n" + ja.T(i18n.KeyOrchestratorBlockedScreenTruncated)
	}
	s.Excerpt = excerpt
	return s
}

// trimTrailingBlank は末尾の空行を落とす。
//
// lines: 行の並び。
// 戻り値: 末尾の空行を落とした並び。
func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// classifyBlockedScreen は確認の画面の種類を見分ける（実測した4画面の文字列で決めた）。
//
// text: 確認の画面（切り出せなかったときは画面の下の行）。
// 戻り値: 種類。
func classifyBlockedScreen(text string) blockedKind {
	// **AskUserQuestion を先に見る。**質問が「Do you want to …?」で選択肢が Yes だと、ツールの確認の
	// 文字列にも当たる。ツールの確認は `Esc to cancel · Tab to amend` で終わり、`Enter to select` を持たない（実測）。
	switch {
	case strings.Contains(text, "Enter to select") &&
		(strings.Contains(text, "Type something") || strings.Contains(text, "Chat about this")):
		return blockedKindAskUserQuestion
	case strings.Contains(text, "Yes, I trust this folder") || strings.Contains(text, "Accessing workspace:"):
		return blockedKindFolderTrust
	case strings.Contains(text, "Do you want to") && strings.Contains(text, "1. Yes"):
		return blockedKindToolPermission
	default:
		return blockedKindUnknown
	}
}

// needsApprovalOf は `don’t ask again for:` の行から、許可が要る部分を取り出す。
//
// **`&&` でつないだコマンドのうち、許可が要る部分そのものが出る**（2026-11-12 に実測）。
// `’` は U+2019 である。`'` も受ける。
//
// lines: 確認の画面の行。
// 戻り値: 許可が要る部分。無ければ空。
func needsApprovalOf(lines []string) string {
	for _, l := range lines {
		for _, mark := range []string{"don’t ask again for:", "don't ask again for:"} {
			if i := strings.Index(l, mark); i >= 0 {
				return strings.TrimSpace(l[i+len(mark):])
			}
		}
	}
	return ""
}

// truncateRunes は文を n rune までで切る。
//
// text: 対象の文。
// n: 上限。
// 戻り値: 切ったあとの文。
func truncateRunes(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	runes := []rune(text)
	return string(runes[:n])
}

// fenceFor は、中身を閉じさせない長さの backtick の囲みを返す（最長の並びより1つ長い。最低3つ）。
//
// text: 囲む中身。
// 戻り値: 囲みの記号。
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// blockedScreenOptions は、画面の部分の組み立て方を道ごとに変える。
type blockedScreenOptions struct {
	// StandaloneToolRemedy は、続けて `permissionRemedyText` を出さない道で真にする（起動系・コメントの道）。
	// 真なら、ツールの権限の確認の直し方に、規則を足す先と再起動の1文を足す。
	StandaloneToolRemedy bool
	// OmitTrustRemedy は、フォルダの信頼の直し方をもとの文言が持っている道で真にする（起動直後の道）。
	OmitTrustRemedy bool
	// OmitStatusAdvice は、再起動の1文から「Status を着手待ちへ戻す」を外す道で真にする（コメントを書かせる道）。
	// その道では後ろに `failCommentRecovery` の【対処】（完了にするか着手待ちへ戻すかを決める）が続く。
	OmitStatusAdvice bool
}

// blockedScreenSection は、引き渡しの通知に足す画面の部分を組み立てる
// （【止まった画面】【種類】【再発するか】【止まった確認への直し方】）。
//
// **【補足】は含めない。**esc を送った道だけが、最後に `blockedEscNote` を足す。
//
// s: 読んだ画面。
// opts: 道ごとの組み立て方。
// 戻り値: 改行で始まる文。
func blockedScreenSection(s blockedScreen, opts blockedScreenOptions) string {
	ja := blockedScreenCatalog()
	var b strings.Builder
	// genericRemedy は、続けて `permissionRemedyText` を出さない道（起動系・コメントの道）で、
	// 種類が分からないときに出す直し方である。**出さないと、解決方法が1つも書かれない通知になる。**
	restartText := permissionRestartText
	if opts.OmitStatusAdvice {
		restartText = permissionRestartOnlyText
	}
	genericRemedy := func() {
		if opts.StandaloneToolRemedy {
			b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRemedyGeneric))
			b.WriteString(restartText)
		}
	}
	switch {
	case s.ReadErr != "":
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenReadFailed, s.ReadErr))
		genericRemedy()
		return b.String()
	case s.NotBlocked:
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenNotBlocked))
		genericRemedy()
		return b.String()
	}
	if s.Extracted {
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenExtracted))
	} else {
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenFallback))
	}
	fence := fenceFor(s.Excerpt)
	b.WriteString("\n" + fence + "\n" + s.Excerpt + "\n" + fence)

	switch s.Kind {
	case blockedKindToolPermission:
		if s.Heading != "" {
			b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenKindToolHeading, s.Heading))
		} else {
			b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenKindTool))
		}
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRecurTool))
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRemedyTool))
		if opts.StandaloneToolRemedy {
			b.WriteString(ja.T(i18n.KeyOrchestratorBlockedScreenRemedyToolAllow))
			b.WriteString(restartText)
		}
	case blockedKindFolderTrust:
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenKindFolderTrust))
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRecurFolderTrust))
		if !opts.OmitTrustRemedy {
			b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRemedyFolderTrust))
		}
	case blockedKindAskUserQuestion:
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenKindAsk))
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRecurAsk))
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRemedyAsk))
	default:
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenKindUnknown))
		b.WriteString("\n" + ja.T(i18n.KeyOrchestratorBlockedScreenRecurUnknown))
		genericRemedy()
	}
	return b.String()
}

// blockedEscNote は、esc を送った道で最後に足す【補足】を返す。
//
// **`escMayLookLikeRejection` が真のときだけ出す**（ツールの権限の確認・AskUserQuestion・見分けられなかったもの。
// 読み直して確認の画面でなくなっていたときは出さない）。それ以外では空を返す。
//
// s: 読んだ画面。
// 戻り値: 改行で始まる【補足】。出さないなら空。
func blockedEscNote(s blockedScreen) string {
	if !s.escMayLookLikeRejection() {
		return ""
	}
	return "\n" + blockedScreenCatalog().T(i18n.KeyOrchestratorBlockedScreenEscNote)
}
