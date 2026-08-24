package orchestrator

import (
	"time"

	"github.com/maimuzo/continuo/internal/tracker"
)

// failureClearGrace は、失敗の記録をボードの Status で消してよいと判断するまでの猶予である。
//
// **なぜ猶予が要るか。**候補の一覧は GitHub のサーバ側の検索結果であり、
// continuo が直前に書いた Status が索引へ反映されるまで遅れる（設計 3-34）。
// 猶予を置かないと、`failure_state` へ落とした直後の巡回で「まだ候補に見えている
// ＝人間が Status を戻した」と誤って読み、記録を消して同じ失敗を繰り返す。
//
// **既定の巡回の間隔（30秒）の2回ぶんにしてある。**人間が Status を戻してから
// 拾い直すまで、最大でこの長さだけ待つことになる。
const failureClearGrace = 60 * time.Second

// failureNote は issue（project item の ID）1件ぶんの失敗の記録である。
//
// **メモリ上にしか持たない。**永続化層は作らない（再起動したら0から数え直す）。
type failureNote struct {
	// Count は続けて失敗した回数である。
	Count int
	// LastAt は最後に失敗した時刻である。
	LastAt time.Time
	// Reason は最後の失敗の理由の要約である（ログに出す）。
	Reason string
	// MovedToFailure は、その失敗でボードの Status を `failure_state` へ実際に書けたかである。
	//
	// **書けたかどうかで「人間が動かした」の見分け方が変わる。**書けていれば、
	// そのあと候補（active_states）に見えること自体が人間の操作の証拠になる。
	// 書けていなければ Status は動いていないので、証拠にならない。
	MovedToFailure bool
	// Notified は「もう拾わない」ことを人間へ1度知らせたかである。
	// **毎巡回で同じ警告を積まないために持つ。**
	Notified bool
}

// noteFailure は issue 1件の失敗を1つ数える（設計 3-16 / 3-21）。
//
// **印（runs）の外に数える。**印は run が終わると消えるので、印の中の RetryCount では
// 「同じ理由で必ず失敗する issue」を止められない（次の巡回が0回目として拾い直す）。
//
// issueID: project item の ID。
// reason: 人間へ見せる理由。
// movedToFailure: その失敗でボードの Status を `failure_state` へ書けたか。
// 戻り値: 数え終わったあとの回数。
func (o *Orchestrator) noteFailure(issueID, reason string, movedToFailure bool) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	note, ok := o.failures[issueID]
	if !ok {
		note = &failureNote{}
		o.failures[issueID] = note
	}
	note.Count++
	note.LastAt = o.now()
	note.Reason = summaryLine(reason)
	note.MovedToFailure = movedToFailure
	return note.Count
}

// forgetFailure は issue 1件の失敗の記録を消す。
//
// **run が最後まで通ったときに呼ぶ。**次にその issue が失敗したら0から数え直す。
//
// issueID: project item の ID。
func (o *Orchestrator) forgetFailure(issueID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.failures, issueID)
}

// skipByFailure は、過去の失敗を理由にこの候補を飛ばすかを判定する。
//
// **`agent.max_retries` を超えた issue は、人間がボードの Status を動かすまで拾わない。**
// 超えていない issue はそのまま着手してよい（待てば通る失敗もあるため）。
//
// 記録をいつ消すかは forgettableLocked が決める。
//
// issue: 判定する候補。
// 戻り値: 飛ばすなら true。
func (o *Orchestrator) skipByFailure(issue tracker.Issue) bool {
	o.mu.Lock()
	note, ok := o.failures[issue.ID]
	if !ok {
		o.mu.Unlock()
		return false
	}
	if o.forgettableLocked(note) {
		delete(o.failures, issue.ID)
		o.mu.Unlock()
		return false
	}
	if note.Count <= o.cfg.Agent.MaxRetries {
		o.mu.Unlock()
		return false
	}
	first := !note.Notified
	note.Notified = true
	count := note.Count
	reason := note.Reason
	o.mu.Unlock()

	if first {
		o.logger.Warn("同じ issue が続けて失敗しているので、これ以上は拾いません"+
			"（ボードの Status を動かすと拾い直します）",
			"identifier", issue.Identifier, "失敗の回数", count,
			"max_retries", o.cfg.Agent.MaxRetries, "理由", reason)
		return true
	}
	o.logger.Debug("失敗の回数を使い切った issue を飛ばしました",
		"identifier", issue.Identifier, "失敗の回数", count)
	return true
}

// forgettableLocked は、失敗の記録をここで消してよいかを返す。
//
// **o.mu を保持したまま呼ぶこと。**
//
// **見分け方は、失敗のときにボードへ failure_state を書けたかで変わる。**
//
//	書けた     … ボードは failure_state にある。それが候補（active_states）に
//	             見えている以上、**人間が Status を動かした**ということである。
//	             ただし絞り込みの索引の遅れを踏むので、failureClearGrace のあいだは信じない
//	書けなかった … Status は動いていないので、人間が動かしたかを Status から知る手立てが無い。
//	             **代わりに時間で緩める。**書けない原因は GitHub へ届かないことが多く、
//	             いずれ直る。緩める間隔はリトライのバックオフの上限に合わせる
//	             （`agent.max_retry_backoff_ms`。新しい数を増やさない）
//
// note: その issue の失敗の記録。
// 戻り値: 消してよければ true。
func (o *Orchestrator) forgettableLocked(note *failureNote) bool {
	if note.MovedToFailure {
		return o.now().Sub(note.LastAt) >= failureClearGrace
	}
	return o.now().Sub(note.LastAt) >= o.failureCooldown()
}

// failureCooldown は「ボードへ落とせなかった失敗」を数え直すまでの間隔を返す。
//
// 戻り値: `agent.max_retry_backoff_ms`（ただし failureClearGrace を下回らない）。
func (o *Orchestrator) failureCooldown() time.Duration {
	d := time.Duration(o.cfg.Agent.MaxRetryBackoffMs) * time.Millisecond
	if d < failureClearGrace {
		return failureClearGrace
	}
	return d
}
