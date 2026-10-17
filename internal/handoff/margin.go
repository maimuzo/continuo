package handoff

import (
	"time"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// MarginCut は「その期間でマージンを切ったか」を判定する関数を作る（issue #262。設計 3-27a）。
//
// **`Short` との違いは、マージンが0の期間を数えないことだけである。**
//
//	Short     … 余裕値が0以下（マージン0の期間の使用率100も含む）。入札と、巡回の手放しが使う
//	MarginCut … マージンが1以上で、余裕値が0以下。走っている run を切り上げさせるかと、着手の前の判定が使う
//
// **マージン0の期間を数えないのは、人間が決めた「マージンが0%ではない場合は、マージンが切れたら
// 作業を切り上げる」のとおりである。**マージン0の期間の使用率100は「マージンを切った」ではなく、
// 「Claude Code が動けない」（`CannotWork`）か、使っていないモデルの期間（何もしない）に当たる。
//
// **モデル別の1週間の期間（`weekly_scoped`）の使用率100も、マージンが1以上なら「切った」に入る。**
// `CannotWork` が数えないので、ここで拾わないと、使っていないモデルの期間が100%のときに
// Claude Code は `margin-status` を読んで切り上げるのに、continuo は「続けてください」を送り続ける。
//
// margins: 引くマージン（%）。
// 戻り値: その期間でマージンを切っていれば true を返す関数。**`Snapshot` の選別に渡す。**
func MarginCut(margins Margins) func(l ratelimit.Limit) bool {
	short := Short(margins)
	return func(l ratelimit.Limit) bool {
		margin := margins.FiveHour
		if IsWeeklyKind(l.Kind) {
			margin = margins.Weekly
		}
		return margin >= 1 && short(l)
	}
}

// CannotWork は「Claude Code が動けないか」を判定する関数を作る（issue #262。設計 3-27a）。
//
// **5時間の期間か、全体の1週間の期間（`weekly_all`）が使用率100のときだけ真である。**
// **モデル別の1週間の期間（`weekly_scoped`）は数えない。**continuo は Claude Code がどのモデルを
// 使っているかを知らない（設計 3-27）ので、使っていないモデルの期間が100%でも、Claude Code は動ける。
//
// **`Full` との違いはそこだけである。**`Full` は使用率100の待ち（`isQuotaWaitingWith`）が使い、
// その run が `claude.turn_timeout_ms` 黙ったという証拠と組で当てる。**turn の終わりと着手の前には
// その証拠が無いので、`Full` をそのまま当ててはならない。**
//
// 戻り値: その期間で Claude Code が動けなければ true を返す関数。**`Snapshot` の選別に渡す。**
func CannotWork() func(l ratelimit.Limit) bool {
	full := Full()
	return func(l ratelimit.Limit) bool {
		if !matchesKind(l.Kind, sessionKinds) && !matchesKind(l.Kind, []string{LimitKindWeeklyAll}) {
			return false
		}
		return full(l)
	}
}

// Destination は、マージンを切ったときの行き先である（issue #262。設計 3-27a の行き先の表）。
type Destination int

const (
	// DestNone は、行き先の表に当たらないことを表す（マージンを切った期間も、動けない期間も無い）。
	DestNone Destination = iota
	// DestBlocked は、Status を `failure_state` へ動かすことを表す（担当者は残す）。
	DestBlocked
	// DestAwait は、走っている run に「行き先を待つ」印を立て、担当を外すかを巡回の手放しに任せることを表す。
	//
	// **走っている run だけが返す。**担当を外すかは、今の巡回の手放し（`weeklyWaitExceededWith`）が
	// 新しい値で決める。**ここで経過を測らないのは、経過で測る判定がそこに既にあり、1か所に寄せるためである。**
	DestAwait
	// DestRelease は、run を始めずに（または始めた run の最初の turn のあとに）担当を外すことを表す。
	//
	// **着手の前だけが返す。**
	DestRelease
)

// String は行き先を1語で返す（ログに出す）。
//
// 戻り値: 行き先を表す語。
func (d Destination) String() string {
	switch d {
	case DestNone:
		return "なし"
	case DestBlocked:
		return "Blocked へ戻す"
	case DestAwait:
		return "行き先を待つ"
	case DestRelease:
		return "担当を外す"
	default:
		return "不明"
	}
}

// DestinationPhase は、行き先を決める場面である。
type DestinationPhase int

const (
	// PhaseRunning は、走っている run の turn の終わり（と、「行き先を待つ」run の巡回）である。
	PhaseRunning DestinationPhase = iota
	// PhaseBeforeDispatch は、担当が自分の issue に着手する前（run が無い）である。
	PhaseBeforeDispatch
)

// Decide は、マージンを切ったときの行き先を決める（issue #262。設計 3-27a の行き先の表）。
//
// **当てる期間。**マージンを切った期間（`MarginCut`）。着手の前は、Claude Code が動けない期間
// （`CannotWork`。マージン0の期間を含む）も当てる。**走っている run で Claude Code が動けないときは、
// 呼び出し側が先に今の動き（リセットを待つ）へ回すので、ここへは来ない。**
//
// **1週間の期間が1つでも当たれば、1週間の期間の行で決める。**「担当を外す」が出うるのは1週間の期間だけである。
// 1週間の期間（全体とモデル別）は、今の巡回の手放しと同じく、期間を分けずに1つの判定で決め、次の順に当てる。
//
//	待つ上限が0（上限なし）                                   … Blocked
//	読めている期間のどれかで、リセットまでが待つ上限を超える   … 走っている run は行き先を待つ。着手の前は担当を外す
//	どれか1つでもリセットの時刻を読めない                     … 走っている run は行き先を待つ。着手の前は Blocked
//	上限以内                                                 … Blocked
//
// **上限を超えると分かっている期間があれば、読めない期間があっても「超える」を使う。**
// 人間の「blockedに戻すで良い」（2026-10-17）は、読めないときだけを訊いた答えである。
// **着手の前に読めなければ Blocked にするのは、run が無いと、待つあいだに経過を測る相手が無いためである。**
//
// snap: 判定に使う使用率（使える値）。nil なら DestNone を返す（読めていないときは止めない。設計 3-77i）。
// margins: 引くマージン（%）。
// waitLimit: 1週間の期間のリセットを待つ上限。0 以下は上限なし。
// now: いまの時刻。
// phase: 行き先を決める場面。
// 戻り値: 行き先。
func Decide(snap *ratelimit.Snapshot, margins Margins, waitLimit time.Duration, now time.Time, phase DestinationPhase) Destination {
	if snap == nil {
		return DestNone
	}
	cut := MarginCut(margins)
	cannot := CannotWork()
	hit := func(l ratelimit.Limit) bool {
		if cut(l) {
			return true
		}
		return phase == PhaseBeforeDispatch && cannot(l)
	}
	var weekly []ratelimit.Limit
	found := false
	for _, l := range snap.Limits {
		if !hit(l) {
			continue
		}
		found = true
		if IsWeeklyKind(l.Kind) {
			weekly = append(weekly, l)
		}
	}
	if !found {
		return DestNone
	}
	if len(weekly) == 0 || waitLimit <= 0 {
		return DestBlocked
	}
	unknown := false
	for _, l := range weekly {
		if l.ResetsAt == nil {
			unknown = true
			continue
		}
		if l.ResetsAt.Sub(now) > waitLimit {
			if phase == PhaseBeforeDispatch {
				return DestRelease
			}
			return DestAwait
		}
	}
	if unknown {
		if phase == PhaseBeforeDispatch {
			return DestBlocked
		}
		return DestAwait
	}
	return DestBlocked
}
