// Package handoff は、同じカンバンを複数の continuo が見張るときに
// 「どのアカウントが1件を処理するか」を決める判定を持つ（docs/plans/continuo_design.md 3-77 / 3-77a / 3-77b / 3-77c）。
//
// **参加者を見分ける値は、その continuo が使っている `gh` の持ち主のログイン名である**（設計 3-77-0）。
// **同じ GitHub アカウントを複数の機械で使う運用はサポートしない**ので、
// **アカウント1つにつき continuo は1つである。**機械の名前は1バイトも使わない。
//
// **決めるのは3つだけである。**
//
//	余裕値と判定スコア … 枠の使用率から作る。入札してよいかもここで決まる
//	勝者              … 届いた入札のうち判定スコアがいちばん大きいもの。同点なら最初に投稿したもの
//	担当を外すか      … 担当者の最後の進捗報告からの経過が期限を過ぎているか
//
// **外部へは1バイトも書かない。**GitHub を叩くのは呼び出し側（internal/orchestrator）である。
// ここに置くのは「読んだものから答えを出す」部分だけなので、テストから直接呼べる。
//
// **担当は issue の担当者（assignee）で持ち、期限は hold のコメントで持つ**（設計 3-77b）。
// カンバンに新しい欄は足さない。
package handoff

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 枠の種別である（Claude の usage API が `kind` に返す値。設計 3-77）。
const (
	// LimitKindSession は5時間の枠である。
	LimitKindSession = "session"
	// LimitKindWeeklyAll は1週間全体の枠である。
	LimitKindWeeklyAll = "weekly_all"
	// LimitKindWeeklyScoped は1週間のモデル別の枠である。
	//
	// **最初から返ってくる。**「一定量を使うまで現れる」ではない（issue #199）。
	// **使っていなければ `percent: 0` で返り、`resets_at` は `null` である**
	// （2026-08-29 の実測。設計 3-15 のサンプルも同じ形である）。
	// **だから「現れたら判定に入れる」という書き方をしてはならない。**その判定は永久に発火しない。
	LimitKindWeeklyScoped = "weekly_scoped"
)

// fullPercent は「使い切り」を表す使用率である。余裕値はここから引いて作る。
const fullPercent = 100

// ThresholdPercent は「これに達すると余裕値が0以下になる」使用率を返す（設計 3-77j。issue #173）。
//
// **人間へ見せる1行を作るためだけにある。**判定そのものは `Short` が行う。
//
// **この関数が要るのは、100 という数を2つの package が別々に持たないためである。**
// **`Short` は `fullPercent - l.Percent - margin <= 0` で判定する。**
// **同じ 100 を呼ぶ側でもう一度書くと、この定数を変えたときに、
// ログが「90% に達したら止まります」と言い続けたまま、実際の門は別の値で効く。**
//
// margin: その枠のマージン（%）。
// 戻り値: これに達すると入札が止まる使用率（%）。
func ThresholdPercent(margin int) int {
	return fullPercent - margin
}

// Bid は1つの continuo が書いた入札である（設計 3-77a のコメントの形）。
//
// **JSON のキーは issue のコメントに書く形そのものである。**別の continuo が読むので、
// **キー名を変えると、古い版の continuo が書いた入札を読めなくなる。**
//
// **誰が書いたかは JSON に入らない**（設計 3-77-0）。入れると、**自分で名乗った値と
// GitHub がコメントに付ける投稿者という、同じ事実の出どころが2つできる。**
// **本文は第三者にも書けるので、他の continuo のログイン名を騙られると、
// 騙られたほうは `HasBidBy` が真になって、その回は入札しない。**
type Bid struct {
	// FiveHour は5時間余裕値である（`100 − 5時間の使用率 − 5時間マージン`）。
	FiveHour int `json:"five_hour"`
	// Weekly は1週間余裕値である（`100 − 1週間の使用率 − 1週間マージン`）。
	Weekly int `json:"weekly"`
	// Score は判定スコアである（`5時間余裕値 × 2 + 1週間余裕値`）。
	Score int `json:"score"`
	// At は投稿した時刻である。**その機械のタイムゾーンで書く**（`Z` に直さない。設計 3-77a）。
	At time.Time `json:"at"`

	// ===== ここから下はコメントの JSON に入らない。GitHub 側の値で埋める =====

	// Author は、この入札を書いたアカウントのログイン名である（設計 3-77-0）。
	//
	// **持ち回りで参加者を見分ける値そのものである。**読むときは `CollectBids` が
	// コメントの投稿者から埋め、**書くときは `bidForIssue` が gh の持ち主から埋める。**
	//
	// **書く側を忘れてはならない。**`bidForIssue` はその巡回で書いた入札の写しを
	// そのまま勝敗の判定へ混ぜるので、**空のままだと、自分が勝っても「負けた」と読む。**
	// **次の巡回で GitHub から読み直せば勝てる**（`CollectBids` が投稿者から埋める）が、
	// **`bid_window_ms: 0` は「締め切りを待たない」設定なのに、1巡回ぶん待たされる。**
	Author string `json:"-"`

	// PostedAt は、そのコメントが GitHub に作られた時刻である。
	//
	// **同点の決着はこちらで行う**（設計 3-77 の「同点なら、いちばん最初に投稿した入札」）。
	// `At` は投稿した側が自分で書いた値なので、**時計がずれている機械が
	// 過去の時刻を書けば必ず勝ててしまう。**GitHub が付けた時刻は騙れない。
	PostedAt time.Time `json:"-"`
}

// Hold は担当を取ったことを示すコメントである（設計 3-77b）。
type Hold struct {
	// Assignee は担当者にしたアカウントのログイン名である。
	//
	// **持ち回りで参加者を見分ける値である**（設計 3-77-0）。入札と違って、この欄は JSON に残す。
	// **`LatestHoldFor` が issue の担当者と突き合わせるためであり、投稿者では代われない。**
	// hold を書くのは担当を取った当人なので投稿者と一致するが、
	// **突き合わせる相手は issue の担当者であって、コメントの投稿者ではない。**
	//
	// **絞らないと何が起きるか。**hold のコメントは
	// **担当が移っても入札の回が変わっても消えない**ので、
	// **「issue のどこかに hold がある」だけで「いまの担当者は機械である」と読まれる。**
	// continuo が外れたあとに人間が自分を担当者にすると、
	// **別の continuo が古い hold を証拠にして、人間の担当を外す。**
	Assignee string `json:"assignee"`
	// Branch はこの issue のために使う branch の名前である。
	Branch string `json:"branch"`
	// At は書いた時刻である。**その機械のタイムゾーンで書く。**
	At time.Time `json:"at"`
}

// Released は期限切れの担当を外したことを知らせるコメントである（設計 3-77c）。
//
// **引き継ぐアカウントは書かない。**外すのは入札をやり直す前であり、
// **そのとき勝つ continuo はまだ決まっていない**（外した側が負けることもある）。
// 次に誰が担当になったかは、あとから現れる hold のコメントの `assignee` で読める。
type Released struct {
	// From は担当を外されたアカウントのログイン名である。
	//
	// **この欄も JSON に残す**（設計 3-77-0）。**投稿者では代われない。**
	// released を書くのは担当を外した側で、**ここに入るのは外された側だからである**
	// （`releaseExpiredAssignee`）。着手をやめて自分で消し戻すとき（`undoHandoffAcquire`）だけは
	// 投稿者と同じ値になるが、**片方で代われない以上、欄は要る。**
	From string `json:"from"`
	// Branch は担当を外されたアカウントが使っていた branch の名前である。
	Branch string `json:"branch"`
	// At は外した時刻である。**外した機械のタイムゾーンで書く。**
	At time.Time `json:"at"`
	// Reason は、自分から手放したときにその理由を入れる（issue #197）。
	//
	// **空なら「他の機械に外された」である**（設計 3-77c）。
	// **`ReleaseReasonWeeklyWaitLimit` なら「1週間の枠を待つ上限を超えて自分で手放した」である**
	// （設計 3-27）。**この2つで本文が変わる。**
	// 外された側は「この branch へ push しないでください」だが、
	// **自分から手放した側は、その直前に `workspace_hooks.after_run` で push している。**
	// **同じ本文を使うと、push した本人が「push しないでください」と書くことになる。**
	//
	// **`omitempty` を付ける。**外された側の `released` に空の欄を増やさないためである。
	// **互換のためではない。**`encoding/json` は知らない欄を黙って捨てるので、
	// **欄を足しただけで古い continuo が読めなくなることは無い**（`ParseReleased` は
	// `DisallowUnknownFields` を使っていない）。
	Reason string `json:"reason,omitempty"`
}

// 自分から手放したときの理由である（issue #197。設計 3-27）。
//
// **これは人間が issue のコメントを grep するための目印である。**
// **機械はこの値で振る舞いを変えない。**読むのは `FormatReleased` が本文を選ぶときだけで、
// **`LatestReleased` の読み手は `From` しか見ない。**
// [docs/FAQ.md](../../docs/FAQ.md) が、この文字列を載せた JSON を見本として出している。
const (
	// ReleaseReasonWeeklyWaitLimit は、1週間の枠を待つ上限を超えて自分で手放したことを表す。
	//
	// **`workspace_hooks.after_run` が成功したときだけ使う。**
	ReleaseReasonWeeklyWaitLimit = "weekly_wait_limit"
	// ReleaseReasonWeeklyWaitLimitNoPush は、同じ理由で手放したが
	// **`workspace_hooks.after_run` が走らなかった／失敗したことを表す。**
	//
	// **本文を分ける。**「実行済みです。remote の続きから始めてください」と断言すると、
	// **次に拾う機械が、入っていない commit の続きから始める。**
	ReleaseReasonWeeklyWaitLimitNoPush = "weekly_wait_limit_no_push"
)

// Margins は余裕値を作るときに引くマージンである（単位は %）。
type Margins struct {
	// FiveHour は5時間の枠から引く割合である。
	FiveHour int
	// Weekly は1週間の枠から引く割合である。
	Weekly int
}

// SkipReason は「入札しない」と決めた理由である（設計 3-77 の表）。
type SkipReason int

const (
	// SkipNone は入札してよいことを表す。
	SkipNone SkipReason = iota
	// SkipQuotaUnreadable は枠を読めなかったことを表す。
	//
	// **読めないと使用率0（＝いちばん暇）に見え、必ず勝ってしまう。**だから黙る。
	SkipQuotaUnreadable
	// SkipNoHeadroom は5時間余裕値と1週間余裕値のどちらかが0以下であることを表す。
	//
	// **`rate_limit.pause_above_percent` は消えた**（人間の決定。2026-09-06。issue #173）。
	// **余裕値と同じことを2つの閾値で言っていて、使い分けができていなかった。**
	// 既定（マージン10）では余裕値のほうが低い使用率で先に効くので、
	// **あちらは一度も発火していなかった。**
	SkipNoHeadroom
)

// String は理由を人間が読める1語で返す（ログに出す）。
//
// 戻り値: 理由を表す語。
func (r SkipReason) String() string {
	switch r {
	case SkipNone:
		return "入札してよい"
	case SkipQuotaUnreadable:
		return "枠を読めない"
	case SkipNoHeadroom:
		return "余裕値が0以下"
	default:
		return "不明"
	}
}

// Short は「その枠に余裕が無いか」を判定する関数を作る（設計 3-27 / 3-77。issue #173 / #197）。
//
// **「人間のための取り置きへ食い込むか」を問う線である。**次の2箇所で使う。
//
//	入札するかどうか              … Evaluate（余裕値が0以下なら入札しない）
//	1週間の枠を待つ上限を超えたか … Orchestrator.weeklyWaitExceededWith
//
// **枠待ちの印には使わない。**あちらは `Full`（使用率100）である。
// **問いが違う。**印は「Claude Code が本当に応答できないか」を問うもので、
// **使用率90%では普通に応答する。**そこで打ち切りの時計を止めると、
// **本当に固まった run が、5時間の枠が90%を割るまで殺されない。**
// **既定では最大で6時間、スロットと pane を握り続ける**（2026-09-06 の6段の段4）。
//
//	その枠の余裕値 = 100 − その枠の使用率 − その種別のマージン
//	余裕が無い枠   = 余裕値 <= 0
//
// **マージンは種別ごとに引く。**5時間の枠には `five_hour_margin_percent`、
// 1週間の枠（`weekly_all` と `weekly_scoped`）には `weekly_margin_percent` を引く。
//
// **知らない種別は数えない。**`Evaluate` は `SessionPercent` と `WeeklyPercent` から
// 余裕値を作るので、**その2つが見ない種別をここで数えると、線がまた2本に割れる。**
// usage API が種別を増やしたとき、**この関数だけが真を返して枠待ちの印が立ち、
// 入札の側は素通しで新しい issue を取り続ける**という食い違いが起きる。
// **種別を増やすときは、`SessionPercent` か `WeeklyPercent` のどちらかへ足すこと。**
//
// margins: 引くマージン（%）。
// 戻り値: その枠に余裕が無ければ true を返す関数。**`Snapshot` の選別に渡す。**
func Short(margins Margins) func(l ratelimit.Limit) bool {
	return func(l ratelimit.Limit) bool {
		switch {
		case IsWeeklyKind(l.Kind):
			return fullPercent-l.Percent-margins.Weekly <= 0
		case matchesKind(l.Kind, sessionKinds):
			return fullPercent-l.Percent-margins.FiveHour <= 0
		default:
			return false
		}
	}
}

// Full は「その枠を使い切っているか」を判定する関数を作る（設計 3-27）。
//
// **枠待ちの印を立てる／外す判定は、こちらを使う。**「余裕が無い」ではない。
//
// **線を2つ持つ理由。**この2つは違う問いだからである。
//
//	使い切っている（100） … **Claude Code が本当に応答できない。**打ち切りの時計を止めてよい
//	余裕が無い（0以下）   … **人間のための取り置きへ食い込む。**新しい仕事を取らない
//
// **枠待ちの印に「余裕が無い」を使ってはならない**（人間の決定を広げすぎた。2026-09-06 に取り下げた）。
// **使用率90%では Claude Code は普通に応答する。**そこで打ち切りの時計を止めると、
// **本当に固まった run が、5時間の枠が90%を割るまで殺されない。**
// 印が外れたあと `LastSeenAt` が現在時刻へ進むので、**さらに `claude.turn_timeout_ms` を待つ。**
// **既定では最大で6時間、スロットと pane を握り続ける。**
//
// **`>=` で比べる。**API が 100 を超える値を返しても取りこぼさない。
//
// 戻り値: その枠を使い切っていれば true を返す関数。**`Snapshot` の選別に渡す。**
func Full() func(l ratelimit.Limit) bool {
	return func(l ratelimit.Limit) bool {
		return l.Percent >= fullPercent
	}
}

// ShortWeekly は「1週間の枠のうち、余裕が無いもの」を判定する関数を作る
// （設計 3-27。issue #197）。
//
// **`Short` と種別の判定を掛け合わせただけである。**
// **呼び出し側で毎回組み立てさせない。**組み立て方が2通りになると、
// **1週間の枠を待つ上限の判定と、その待ち先の時刻を引く判定がずれる。**
//
// margins: 引くマージン（%）。
// 戻り値: 1週間の枠で、かつ余裕が無ければ true を返す関数。
func ShortWeekly(margins Margins) func(l ratelimit.Limit) bool {
	short := Short(margins)
	return func(l ratelimit.Limit) bool {
		return IsWeeklyKind(l.Kind) && short(l)
	}
}

// WeeklyPercent は1週間の使用率を返す（設計 3-77）。
//
// **1週間全体の枠とモデル別の枠のうち、いちばん大きいものを採る。**
// **モデル別の枠は最初から返ってくる**（issue #199）。使っていなければ `percent: 0` なので、
// **最大を採れば、使っていない枠は自動的に判定へ効かない。**
//
// snap: 読み取った枠の一覧。
// 戻り値の1つ目: いちばん大きい1週間の使用率。
// 戻り値の2つ目: 1週間の枠が1件でもあれば true。
func WeeklyPercent(snap *ratelimit.Snapshot) (int, bool) {
	return maxPercentOfKinds(snap, LimitKindWeeklyAll, LimitKindWeeklyScoped)
}

// SessionPercent は5時間の使用率を返す（設計 3-77）。
//
// snap: 読み取った枠の一覧。
// 戻り値の1つ目: 5時間の枠の使用率（複数あればいちばん大きいもの）。
// 戻り値の2つ目: 5時間の枠が1件でもあれば true。
func SessionPercent(snap *ratelimit.Snapshot) (int, bool) {
	return maxPercentOfKinds(snap, LimitKindSession)
}

// IsWeeklyKind は、その枠の種別が1週間の枠かどうかを返す（issue #197）。
//
// **どの `kind` が1週間の枠かを知っているのは、この package だけである。**
// `internal/ratelimit` へ置くと、あちらが `kind` の意味を持つことになり、
// **同じ知識が2箇所に散る。**
//
// **大文字小文字を無視して比べる**（`matchesKind` と同じ扱い）。
//
// kind: 枠の種別。
// 戻り値: `weekly_all` か `weekly_scoped` なら true。
func IsWeeklyKind(kind string) bool {
	return matchesKind(kind, weeklyKinds)
}

// weeklyKinds は1週間の枠の種別である。
//
// **その場で組み立てない。**`IsWeeklyKind` は使い切っている枠の数だけ呼ばれるので、
// **呼ぶたびに slice を作ると、判定1回につき確保が1つ増える。**
var weeklyKinds = []string{LimitKindWeeklyAll, LimitKindWeeklyScoped}

// sessionKinds は5時間の枠の種別である。
//
// **1件しか無くても、その場で組み立てない。**理由は `weeklyKinds` と同じである。
// **`Short` は枠1件につき1回呼ばれ、`Short` 自身が run ごと・巡回ごとに何度も回る。**
var sessionKinds = []string{LimitKindSession}

// maxPercentOfKinds は、指定した種別の枠のうちいちばん大きい使用率を返す。
//
// snap: 読み取った枠の一覧。
// kinds: 数える種別。
// 戻り値の1つ目: いちばん大きい使用率。
// 戻り値の2つ目: 該当する枠が1件でもあれば true。
func maxPercentOfKinds(snap *ratelimit.Snapshot, kinds ...string) (int, bool) {
	if snap == nil {
		return 0, false
	}
	best := 0
	found := false
	for _, l := range snap.Limits {
		if !matchesKind(l.Kind, kinds) {
			continue
		}
		if !found || l.Percent > best {
			best = l.Percent
			found = true
		}
	}
	return best, found
}

// matchesKind は枠の種別が一覧のどれかと一致するかを返す。
//
// **大文字小文字を無視して比べる。**provider が綴りを変えても判定が落ちないようにする。
//
// kind: 枠の種別。
// kinds: 探す種別の一覧。
// 戻り値: 一致すれば true。
func matchesKind(kind string, kinds []string) bool {
	for _, k := range kinds {
		if strings.EqualFold(strings.TrimSpace(kind), k) {
			return true
		}
	}
	return false
}

// Evaluate は枠から入札の中身を作る（設計 3-77 の式と、投稿しない条件）。
//
//	5時間余裕値 = 100 − 5時間の使用率 − 5時間マージン
//	1週間余裕値 = 100 − 1週間の使用率 − 1週間マージン
//	判定スコア  = 5時間余裕値 × 2 + 1週間余裕値
//
// **投稿しない条件は2つある。**枠を読めなかった・どちらかの余裕値が0以下。
// **どちらも「黙る」だけで、ほかの機械はこの機械を待たない。**
//
// **`rate_limit.pause_above_percent` の判定は消えた**（人間の決定。2026-09-06。issue #173）。
// **余裕値と同じことを2つの閾値で言っていて、使い分けができていなかった。**
//
// **「読めなかった」は「枠が1件も返ってこなかった」である。**返ってきた中に
// 特定の種別が無いのは、使用率0として扱う。
// **usage API が将来 kind を増やしても、知らない kind が1つ欠けただけで黙らないためである。**
//
// snap: 読み取った枠の一覧。**nil なら「枠を読めなかった」である。**
// quotaEnabled: 枠を読む設定になっているか（`rate_limit.source` が `none` でないか）。
// **偽なら使用率を0として扱い、閾値の判定も行わない。**
// **「読めなかった」と言い分ける。**`none` は運用者が「枠で判定しない」と決めた状態であり、
// **そこで黙ると、その機械は1件も処理しなくなる**（枠の判定を切る逃げ道が塞がる）。
// margins: 引くマージン（%）。
// at: 入札に書く時刻。**その機械のタイムゾーンのまま渡すこと。**
// 戻り値の1つ目: 組み立てた入札（入札しないときの中身は使わない）。**`Author` は空である。**
// **書く直前に `bidForIssue` が gh の持ち主で埋める**（設計 3-77-0）。ここで埋めないのは、
// **枠の判定が gh の持ち主を引くより先に走るからである**（`evaluateBid` は `viewerIdentity` より前）。
// 戻り値の2つ目: 入札しないと決めた理由。SkipNone なら入札してよい。
func Evaluate(
	snap *ratelimit.Snapshot,
	quotaEnabled bool,
	margins Margins,
	at time.Time,
) (Bid, SkipReason) {
	sessionPercent, weeklyPercent := 0, 0
	if quotaEnabled {
		// **1件も読めていない（写しが無い）なら「読めなかった」である。**
		// **読めないと使用率0（＝いちばん暇）に見え、必ず勝ってしまう。**
		//
		// **写しがあって特定の種別が載っていないのは、別の話である。**
		// **その1件が欠けていることは「読めなかった」ではない。**
		// **usage API が将来 kind を増やしたとき、知らない kind が1つ欠けただけで
		// 黙る機械が出ると、その issue は誰にも進まない。**
		if snap == nil || len(snap.Limits) == 0 {
			return Bid{}, SkipQuotaUnreadable
		}
		sessionPercent, _ = SessionPercent(snap)
		weeklyPercent, _ = WeeklyPercent(snap)
	}

	fiveHour := fullPercent - sessionPercent - margins.FiveHour
	weekly := fullPercent - weeklyPercent - margins.Weekly
	// **0 も「余裕が無い」に含める**（人間の決定。2026-09-06。issue #173）。
	// **余裕値0 は、マージンをちょうど食い潰した状態である。**そこから着手すると、
	// **人間のために取り置いた分へ食い込む。**
	//
	// **`Short` と同じ線にしてある。**枠待ちの印を立てる線・1週間の枠を待つ上限の線と
	// **1点でもずれると、同じ帯で run の扱いが2通りに分かれる。**
	if fiveHour <= 0 || weekly <= 0 {
		return Bid{}, SkipNoHeadroom
	}
	return Bid{
		FiveHour: fiveHour,
		Weekly:   weekly,
		Score:    Score(fiveHour, weekly),
		At:       at,
	}, SkipNone
}

// Score は判定スコアを返す（設計 3-77）。
//
//	判定スコア = 5時間余裕値 × 2 + 1週間余裕値
//
// **5時間の枠に重みを置く。**先に尽きるのは5時間の枠であり、
// そこに余裕がある機械のほうが「いま」動かせる。
//
// fiveHour: 5時間余裕値。
// weekly: 1週間余裕値。
// 戻り値: 判定スコア。
func Score(fiveHour, weekly int) int {
	return fiveHour*2 + weekly
}

// Winner は届いた入札から勝者を選ぶ（設計 3-77）。
//
// **判定スコアがいちばん大きい入札。同点なら、いちばん最初に投稿した入札。**
// **同じコメントの列を読んだ continuo は同じ勝者に行き着く**ので、担当者を書く前に
// もう一度担当者を読み直す段は置かない。
//
// **投稿の時刻は GitHub が付けた `PostedAt` で比べる**（`At` は投稿者が自分で書いた値であり、
// 時計を戻せば必ず勝ててしまう）。**それも同じなら、投稿したアカウントの名前の小さい順で決める。**
// 決め手を最後まで用意しないと、continuo ごとに違う勝者を選び、2つが同じ issue を掴む。
//
// bids: 届いた入札（順不同）。
// 戻り値の1つ目: 勝った入札。
// 戻り値の2つ目: 入札が1件でもあれば true。
func Winner(bids []Bid) (Bid, bool) {
	if len(bids) == 0 {
		return Bid{}, false
	}
	best := bids[0]
	for _, b := range bids[1:] {
		if beats(b, best) {
			best = b
		}
	}
	return best, true
}

// beats は入札 a が入札 b より強いかを返す。
//
// **3段目はアカウントの名前の小さい順である**（設計 3-77d）。
// **大文字小文字は畳む。**GitHub のログイン名は大文字小文字を区別しないので、
// **畳まないと `octocat` と `Octocat` が別の順位に落ち、continuo ごとに違う勝者を選ぶ。**
//
// **ここで決めるのは順序だけである。**「同じアカウントか」の判定はここでは1度もしない。
// **その判定は全部 `strings.EqualFold` が持っている**（`HasBidBy` / `LatestHoldFor` /
// `lastProgressOf` / 勝者と自分の突き合わせ）。**畳み方が違っても、判定の側は影響を受けない。**
//
// **空文字はどのログイン名よりも小さいので、ここへ空が来てはならない。**
// **入札の作り手は2つある。**読んだものは `CollectBids` が投稿者の空なものを1件も通さない。
// **書いたばかりのものは `bidForIssue` が gh の持ち主で埋める**（設計 3-77-0）。
// **後者を埋め忘れると、その入札が3段目で必ず勝ち、勝者がどの continuo とも一致しなくなる。**
//
// a: 比べる入札。
// b: 比べられる入札。
// 戻り値: a のほうが強ければ true。
func beats(a, b Bid) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if !a.PostedAt.Equal(b.PostedAt) {
		return a.PostedAt.Before(b.PostedAt)
	}
	return strings.ToLower(a.Author) < strings.ToLower(b.Author)
}

// Deadline は入札の締め切りを返す（設計 3-77）。
//
// **数えはじめるのは「入札が1件も無い issue への最初の投稿」である。**
// 自分が書いた時刻ではない。**同じコメントの列を読んだ機械は同じ締め切りに行き着く。**
//
// bids: その issue に付いている入札。
// window: `tracker.provider.handoff.bid_window_ms` の長さ。
// 戻り値の1つ目: 締め切りの時刻。
// 戻り値の2つ目: 入札が1件でもあれば true。
func Deadline(bids []Bid, window time.Duration) (time.Time, bool) {
	if len(bids) == 0 {
		return time.Time{}, false
	}
	first := bids[0].PostedAt
	for _, b := range bids[1:] {
		if b.PostedAt.Before(first) {
			first = b.PostedAt
		}
	}
	return first.Add(window), true
}

// BidsBefore は締め切りまでに届いた入札だけを返す（設計 3-77 の「届いた入札だけで決まる」）。
//
// **締め切りちょうどの投稿は含める。**境界で機械ごとに答えが割れないよう、
// 「`deadline` より後のものだけを捨てる」に固定する。
//
// bids: その issue に付いている入札。
// deadline: 締め切りの時刻。
// 戻り値: 締め切りまでに届いた入札（**渡された配列は書き換えない**）。
func BidsBefore(bids []Bid, deadline time.Time) []Bid {
	out := make([]Bid, 0, len(bids))
	for _, b := range bids {
		if b.PostedAt.After(deadline) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// RoundBids は、いまの回の入札だけを返す（設計 3-77e）。
//
// **入札のコメントは消えない。**1回ごとに新しいコメントを書くので、**古い回の入札は
// issue に残り続ける。**締め切りは「いちばん古い入札 + window」なので、
// **古い入札を数に入れたままにすると、締め切りは永久にその古い時刻から数えられる。**
// 締め切りは常に過ぎたことになり、**次の回が1度も始まらない。**
//
// **回の区切りは2つある。どちらも issue のコメントから読める。**
//
//	前の回を閉じたコメント … hold か released が現れた時刻。それより前の入札は前の回のものである
//	決着の猶予切れ         … 締め切りからさらに window。勝ったアカウントが担当者を書けずに消えた回である
//
// **どちらも記憶に持たない。**同じコメントの列を読んだ機械は同じ答えに行き着く。
//
// **回が終わっていたら、その回の入札は1件も返さない。**呼び出し側は新しい入札を書き、
// そこから次の回が始まる。
//
// **`window` が 0 以下なら、猶予切れでは区切らない。**締め切りを待たない設定に決着の猶予は無い。
// **前の回を閉じたコメントによる区切りは、そのときも効く。**
//
// comments: issue に付いているコメントの全件。
// now: いまの時刻。
// window: `tracker.provider.handoff.bid_window_ms` の長さ。
// 戻り値: いまの回の入札（**コメントの並び順のまま。1件も無ければ空**）。
func RoundBids(comments []CommentView, now time.Time, window time.Duration) []Bid {
	bids := CollectBids(comments)
	cut, _ := RoundStart(comments)
	for {
		bids = bidsFrom(bids, cut)
		if len(bids) == 0 || window <= 0 {
			return bids
		}
		deadline, ok := Deadline(bids, window)
		if !ok {
			return bids
		}
		// **締め切りからさらに window。**勝ったアカウントが担当者を書くまでの猶予である。
		expiry := deadline.Add(window)
		if !now.After(expiry) {
			return bids
		}
		// **この回は終わっている。**猶予までに届いた入札を落として、残りで数え直す。
		// **1回で切り上げない。**終わった回が2つ以上積まれている issue があるので、
		// 残った入札の中でいちばん古いものから、もう一度同じ判定を行う。
		cut = expiry
	}
}

// bidsFrom は、その時刻より前に投稿された入札を落とす。
//
// **同じ時刻の入札は残す。**GitHub がコメントに付ける時刻は秒どまりで、
// **担当を外した直後に書く入札は released のコメントと同じ秒に入る。**
// そこで落とすと、その機械は入札を書くそばから自分で捨てることになり、
// **巡回のたびに issue のコメントが1件ずつ増え続ける。**
//
// bids: 絞り込む入札。
// cut: この時刻より前のものを落とす。ゼロ値なら1件も落とさない。
// 戻り値: 残った入札（**渡された配列は書き換えない**）。
func bidsFrom(bids []Bid, cut time.Time) []Bid {
	if cut.IsZero() {
		return bids
	}
	out := make([]Bid, 0, len(bids))
	for _, b := range bids {
		if b.PostedAt.Before(cut) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// IsMarked は、コメント本文が持ち回りの印のどれかで始まっているかを返す（設計 3-77a）。
//
// **投稿者は問わない。**この印が付いたコメントは、誰が書いたものでも
// エージェントへ渡す入力から外す。
//
// body: コメント本文。
// 戻り値: 入札・hold・released のどれかの印で始まっていれば true。
func IsMarked(body string) bool {
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, config.HandoffBidMarker) ||
		strings.HasPrefix(trimmed, config.HandoffHoldMarker) ||
		strings.HasPrefix(trimmed, config.HandoffReleasedMarker)
}

// FormatBid は入札のコメントの本文を組み立てる（設計 3-77a）。
//
// **時刻はその機械のタイムゾーンで書く。**`Z`（協定世界時）に直さない。
// 人間がログと突き合わせるとき、手元の時計と合っているほうが読みやすい。
//
// **JSON の下に、人間が読む2行を置く**（released と同じ形。設計 3-77c）。
// **このコメントは1台で動かしていても必ず出る。**JSON だけだと、issue を開いた人には
// `five_hour` が何の値なのかも、次に何が起きるのかも読めない。
//
// **足す文に `}` を入れてはならない。**payloadAfterMarker が最初の `{` と
// **最後の `}`** の間を切り出すので、あとから現れる `}` は JSON の終わりとして読まれる。
//
// **散文にはアカウントの名前を出すが、JSON には入らない**（設計 3-77-0）。
// **issue を開いた人が、投稿者の欄と本文を見比べずに読めるようにするためである。**
// **continuo はこの散文を識別子として読まない。**ただし `payloadAfterMarker` は
// **本文の末尾まで走査する**ので、上の `}` の決まりが効く。
//
// b: 書く入札。**`Author` を埋めてから渡すこと**（散文に差し込む）。
// window: 入札の締め切りまでの長さ（`tracker.provider.handoff.bid_window_ms`）。
// **0 以下なら「締め切りを待たない」と書く**（そういう設定にできる）。
// 戻り値: 印を先頭に置いたコメント本文。
func FormatBid(b Bid, window time.Duration) string {
	return config.HandoffBidMarker + "\n" + marshalLine(b) + "\n\n" +
		i18n.T(i18n.KeyHandoffBidCandidacy, b.Author) + "\n" +
		bidDeadlineLine(window) + "\n"
}

// bidDeadlineLine は「担当がいつ決まるか」の1行を返す。
//
// **分に丸めて、切り上げる。**既定の 180000 ミリ秒はちょうど3分になる。
// **切り捨てにすると、30秒の設定が「約0分後」になる。**読む人が待てばよい長さを
// 読み取れなくなるので、1分未満は「約1分後」へ寄せる。
//
// **1分のときだけ別の文言を引く。**英語には複数形があり、分数を差し込む文言に 1 を渡すと
// **"in about 1 minutes" と出る。**英語は DefaultLang なので、**言語を選んでいない利用者には
// これが出る。**日本語には複数形が無いので、どちらの文言でも同じ形になる。
//
// window: 入札の締め切りまでの長さ。
// 戻り値: 人間が読む1行。
func bidDeadlineLine(window time.Duration) string {
	if window <= 0 {
		return i18n.T(i18n.KeyHandoffBidNoDeadline)
	}
	minutes := int((window + time.Minute - 1) / time.Minute)
	if minutes == 1 {
		return i18n.T(i18n.KeyHandoffBidDeadlineOne)
	}
	return i18n.T(i18n.KeyHandoffBidDeadline, minutes)
}

// FormatHold は hold のコメントの本文を組み立てる（設計 3-77b）。
//
// **JSON の下に、人間が読む2行を置く**（released と同じ形。設計 3-77c）。
// 誰が担当になったのか・なぜその機械なのか・これから何が始まるのかを、
// issue の上だけで読めるようにする。
//
// **足す文に `}` を入れてはならない**（FormatBid と同じ理由）。
//
// h: 書く hold。
// 戻り値: 印を先頭に置いたコメント本文。
func FormatHold(h Hold) string {
	return config.HandoffHoldMarker + "\n" + marshalLine(h) + "\n\n" +
		i18n.T(i18n.KeyHandoffHoldAssigned, h.Assignee) + "\n" +
		holdStartingLine(h.Branch) + "\n"
}

// holdStartingLine は「これから何が始まるか」の1行を返す。
//
// **branch の名前が空のときは、名前を出さない文へ落とす。**呼び出し側は
// branch 名を組み立てられなかったときに空文字を渡してくる（`branchNameFor`）ので、
// **そのまま差し込むと「これから branch  で作業を始めます」と出る。**
//
// branch: hold に書いた branch の名前。
// 戻り値: 人間が読む1行。
func holdStartingLine(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return i18n.T(i18n.KeyHandoffHoldStartingNoBranch)
	}
	return i18n.T(i18n.KeyHandoffHoldStarting, branch)
}

// marshalLine は JSON を1行にして返す。
//
// **失敗しない形しか渡さない**（どの型も文字列・整数・時刻だけを持つ）。
// **それでも失敗したら空の JSON を返す。**印だけのコメントになるが、
// **投稿そのものを止めない**（止めると、その機械は永久に入札できない）。
//
// v: 書き出す値。
// 戻り値: 1行の JSON。
func marshalLine(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ParseBid はコメント本文から入札を読む（設計 3-77a）。
//
// **印で始まっていないコメントは入札ではない。**
// **JSON を読めないコメントも入札として数えない**（人間が印だけ真似て書いたときに、
// 使用率0の入札が生まれて必ず勝ってしまう）。
//
// **投稿者の分からないコメントも数えない。**GitHub は削除済みアカウントのコメントに
// 投稿者を付けない（[internal/tracker/tracker.go](../tracker/tracker.go) の `Comment.Author`）。
// **数えると、その入札が勝った回は、どの continuo も着手しなくなる**
// （勝った入札の投稿者が、どの continuo とも一致しない）。
// **判定スコアがいちばん大きければ、同点にならなくても勝つ。**
// **同点でも、投稿が早ければ2段目で勝つ。**投稿の時刻まで同じなら3段目で、
// 空文字はどのログイン名よりも小さいので必ず勝つ。
//
// **時刻の入っていないコメントも数えない。**これは `host` の欄を消したときに、
// **その欄の空検査が受け持っていた「本文だけ真似たコメントを数えない」を置き換えたものである。**
// **回の締め切りは GitHub が付けた投稿時刻（`PostedAt`）から数えるので、この検査では動かせない。**
// 止められるのは、**中身の無いコメントが判定スコア0の入札として数に入ること**である。
// continuo は入札に必ず `at` を書くので、
// **入っていないものは continuo が書いたものではない。**
// **この検査が無いと、`{}` だけの本文が判定スコア0の入札として通る。**
// `Deadline` はいちばん古い投稿時刻を起点にするので、
// **手で印だけ真似たコメント1件で、その回の締め切りが早まる。**
//
// body: コメント本文。
// author: GitHub がそのコメントに付けた投稿者のログイン名。**空なら入札として読まない。**
// postedAt: GitHub がそのコメントに付けた作成時刻（同点の決着と締め切りに使う）。
// 戻り値の1つ目: 読み取った入札。
// 戻り値の2つ目: 入札として読めれば true。
func ParseBid(body, author string, postedAt time.Time) (Bid, bool) {
	login := strings.TrimSpace(author)
	if login == "" {
		return Bid{}, false
	}
	payload, ok := payloadAfterMarker(body, config.HandoffBidMarker)
	if !ok {
		return Bid{}, false
	}
	var b Bid
	if err := json.Unmarshal([]byte(payload), &b); err != nil {
		return Bid{}, false
	}
	if b.At.IsZero() {
		return Bid{}, false
	}
	b.Author = login
	b.PostedAt = postedAt
	return b, true
}

// ParseHold はコメント本文から hold を読む（設計 3-77b）。
//
// body: コメント本文。
// 戻り値の1つ目: 読み取った hold。
// 戻り値の2つ目: hold として読めれば true。
func ParseHold(body string) (Hold, bool) {
	payload, ok := payloadAfterMarker(body, config.HandoffHoldMarker)
	if !ok {
		return Hold{}, false
	}
	var h Hold
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		return Hold{}, false
	}
	return h, true
}

// payloadAfterMarker は、印で始まるコメントから JSON の部分を切り出す。
//
// **印の直後の1行だけを読むのではない。**`{` から最後の `}` までを取る。
// 人間が読む文を JSON の下へ足しても壊れないようにするためである（released のコメントがそう）。
//
// body: コメント本文。
// marker: 探す印。
// 戻り値の1つ目: JSON の部分。
// 戻り値の2つ目: 印で始まっていて、JSON らしき部分があれば true。
func payloadAfterMarker(body, marker string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, marker) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
	start := strings.Index(rest, "{")
	end := strings.LastIndex(rest, "}")
	if start < 0 || end < start {
		return "", false
	}
	return rest[start : end+1], true
}
