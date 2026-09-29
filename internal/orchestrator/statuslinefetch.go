package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/workspace"
)

// statusline取得（issue #284）。
//
// **statusline取得**は、使用率を受け取るために、利用者が既に信頼している clone の中で短い haiku の
// Claude Code を起動し、`hello` を1回送り、ステータスラインが運ぶ使用率を受け取って閉じることである。
// run として数えない（`agent.max_concurrent_agents` に数えない。issue に紐づけない）。
//
// **巡回の中で値を待たない。**巡回の最後に開く条件を見て goroutine を起こし、値が届いたら
// 巡回のループへ知らせる。ループは巡回を1回すぐ回して入札する。
//
// **workspace の開け閉めは Manager のメソッドを呼ぶ**（internal/workspace/statusline.go）。
// Manager は段7 と片付けと同じ loop に通すので、statusline取得の workspace が開いている clone では、
// issue の `worktree.open` が閉じるまで後に回る（開いている間に開くと、issue の親にされる）。

// statuslineFetchDirName は statusline取得の作業ディレクトリの名前である（実行時ディレクトリの下）。
// statusline取得用の設定ファイルと閉じ残しの一覧を置く。
const statuslineFetchDirName = "statusline-fetch"

// statuslineFetchListName は閉じ残しの一覧のファイル名である。
const statuslineFetchListName = "workspaces.json"

// statuslineFetchPrompt は statusline取得で送る文である。
//
// **中身に意味は無い。**使用率は応答のあとにステータスラインへ載るので、文の中身は関係ない。
// 応答を短くしてトークンを減らすため、なるべく短い文にする。
const statuslineFetchPrompt = "hello"

// statuslineValueWait は、`hello` を送ってから値を待つ上限である。
const statuslineValueWait = 3 * time.Minute

// statuslineFetchResult は statusline取得1回の結果である。
type statuslineFetchResult int

const (
	// fetchValueArrived は値が届いたことを表す。
	fetchValueArrived statuslineFetchResult = iota
	// fetchBlocked は確認の画面で止まったことを表す。
	fetchBlocked
	// fetchAgentNotFound は agent_not_found が続いたことを表す（1回だけ作り直す）。
	fetchAgentNotFound
	// fetchNotStarted は、期限までに入力を受け付ける状態にならなかったことを表す。
	fetchNotStarted
	// fetchMidwayError は途中の誤りを表す。
	fetchMidwayError
	// fetchNoValue は、応答はあったが値が無かったことを表す。
	fetchNoValue
	// fetchNoLine は、値が1行も届かなかったことを表す。
	fetchNoLine
	// fetchCanceled は止めるときの取り消しを表す（WARN を出さない）。
	fetchCanceled
)

// maybeStartStatuslineFetch は、巡回の最後に statusline取得を開く条件を見て、満たせば
// goroutine を起こす。
//
// **開く条件。**statusline を使え（sl.sock のパスがあり、DisableStatusline されていない）、
// `source: statusline` か、`source: oauth_usage_api` で usage API から切り替えていて取得止めでなく、
// 値が新しくなく、statusline取得が走っておらず、前回の試行の開始から新しさの幅を過ぎていて
// （起動して最初の巡回は前回の試行を問わない）、期限内の保管値に 100 の期間（5時間と1週間全体）が
// 無く、weekly_scoped に余裕があること。**run の画面から値が届いていれば
// 開かない**（値が新しいため）。**期限内の 100 がある間は、上限で断られるだけで値は変わらないので
// 開かない。**
//
// **weekly_scoped は判定を分ける**（issue #284）。ステータスラインが運ばないので、開いても値が
// 変わらない。**余裕が無ければ、開いても着手の判定が変わらないので開かない**
// （`weekly_scoped` に当てる線は、入札の余裕値と同じである。issue #173）。
// **この関数の中で、線は2通り使う**（実装レビュー3周目の LOW）。
// **`weekly_scoped` には余裕値**（`handoff.ShortWeekly`）、
// **`session` と `weekly_all` には「使用率が100の期間が期限内にあるか」**である。
// **後者は「開いても値が変わらない」の判定で、着手するかどうかの線ではない。**
// 「100 の期間」からは外す（外さないと、weekly_scoped が 100 のあいだ 5時間と1週間全体を
// 取り直せない）。
//
// ctx: 巡回のコンテキスト。
func (o *Orchestrator) maybeStartStatuslineFetch(ctx context.Context) {
	if !o.statuslineUsable() || o.ws == nil {
		return
	}
	source := o.cfg.RateLimit.Source
	if source != ratelimit.SourceStatusline && source != ratelimit.SourceOAuthUsageAPI {
		return
	}
	if ctx.Err() != nil {
		return
	}
	now := o.now()
	o.quotaMu.Lock()
	if source == ratelimit.SourceOAuthUsageAPI && (!o.quota.apiSwitched || o.quota.fetchStopped) {
		// **usage API が読めているあいだは開かない。取得止めのあいだも開かない**（issue #284）。
		o.quotaMu.Unlock()
		return
	}
	if o.quota.fetchRunning || o.quotaFreshLocked(now) {
		o.quotaMu.Unlock()
		return
	}
	if !o.quota.lastAttemptAt.IsZero() && now.Sub(o.quota.lastAttemptAt) < o.quotaRefreshInterval() {
		o.quotaMu.Unlock()
		return
	}
	if o.statuslineFetchPointless(now) {
		o.quotaMu.Unlock()
		return
	}
	o.quota.fetchRunning = true
	o.quota.lastAttemptAt = now
	o.quotaMu.Unlock()

	o.wg.Add(1)
	go o.runStatuslineFetch(ctx)
}

// statuslineFetchPointless は、開いても着手の判定が変わらないかを返す。o.quotaMu を持って呼ぶ。
//
// 期限内の 5時間か1週間全体に 100 がある（上限で断られるだけで値は変わらない）か、
// weekly_scoped に余裕が無い（ステータスラインは weekly_scoped を運ばないので、
// 開いても判定が変わらない）なら true。
//
// **線は入札の余裕値と同じ1本である**（人間の決定。2026-09-06。issue #173）。
// **`rate_limit.pause_above_percent` は消えた。****ここに別の閾値を置いてはならない。**
// **置くと、入札が黙る使用率と statusline取得をやめる使用率がずれ、
// 「入札を見送っているのに値を取り直し続ける」帯と「取り直さないのに入札する」帯ができる。**
func (o *Orchestrator) statuslineFetchPointless(now time.Time) bool {
	// **マージンは `bidMargins` から取る**（実装レビュー1周目の LOW）。
	// **ここで手で組み立てると、キーを1本増やしたときに片方だけが直る。**
	shortWeekly := handoff.ShortWeekly(o.bidMargins())
	for kind, w := range o.quota.windows {
		if !w.ResetsAt.After(now) {
			continue
		}
		if kind == handoff.LimitKindWeeklyScoped {
			if shortWeekly(ratelimit.Limit{Kind: kind, Percent: w.Percent}) {
				return true
			}
			continue
		}
		if w.Percent >= 100 {
			return true
		}
	}
	return false
}

// statuslineUsable は statusline を使えるかを返す（sl.sock のパスがあり、DisableStatusline
// されていない。issue #284）。statusLine を書くかと statusline取得を開くかの判定に使う。
func (o *Orchestrator) statuslineUsable() bool {
	return o.slSocketPath != "" && !o.slDisabled.Load()
}

// runStatuslineFetch は statusline取得を1回行う goroutine である。
//
// **試行は、閉じる仕事の Do が返り、この goroutine が終わるまで「走っている」。**
// 前の試行の閉じる仕事が次の試行の押さえを放す順が作れないようにするためである。
// `agent_not_found` の作り直しも同じ試行の中で行う。
func (o *Orchestrator) runStatuslineFetch(parent context.Context) {
	defer o.wg.Done()
	defer func() {
		o.quotaMu.Lock()
		o.quota.fetch = nil
		o.quota.fetchRunning = false
		o.quotaMu.Unlock()
	}()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer context.AfterFunc(o.shutdown, cancel)()

	startup := time.Duration(o.cfg.Herdr.StartupTimeoutMs) * time.Millisecond
	overall := 2*startup + statuslineValueWait
	fctx, cancelF := context.WithTimeout(ctx, overall)
	defer cancelF()

	// **試行を始める前に、閉じ残しを片付ける。**使える clone が無くても片付ける
	// （前の試行が閉じられなかった workspace は、clone の有無と関係なく残っている）。
	o.cleanupStatuslineLeftovers(ctx)

	clone, err := o.chooseStatuslineClone()
	if err != nil {
		o.logger.Warn("statusline取得ができません（途中の誤り: clone を選ぶ判定に失敗しました。ghq と git が使えるか、~/.claude.json を読めるかを確かめてください）",
			"error", err)
		return
	}
	if clone == "" {
		o.logger.Warn("statusline取得ができません（使える clone が無い: trust.repositories に、手元に clone があって信頼済みのリポジトリが1つもありません。"+
			"1つ書いて continuo trust を叩き、continuo を立て直してください）",
			"trust.repositories", o.cfg.Trust.Repositories)
		return
	}

	settingsPath, err := o.writeStatuslineFetchSettings()
	if err != nil {
		o.logger.Warn("statusline取得ができません（途中の誤り: statusline取得用の設定ファイルを書けません）", "error", err)
		return
	}

	for attempt := 0; attempt < 2; attempt++ {
		result, detail, prompted := o.statuslineAttempt(fctx, ctx, clone, settingsPath)
		if result == fetchAgentNotFound && attempt == 0 && ctx.Err() == nil {
			// **新しい workspace と新しい UUID で1回だけやり直す。**前の workspace を閉じる
			// Do は statuslineAttempt の中で返り終えている。
			o.logger.Info("statusline取得の Claude Code を herdr が見つけられないので、workspace を作り直して1回だけやり直します")
			continue
		}
		if o.maybeStopStatuslineFetch(result, detail, prompted) {
			return
		}
		o.reportStatuslineFetch(result, detail, startup)
		return
	}
}

// maybeStopStatuslineFetch は、値の届かなかった取得のあとに取得止めを立てるかを決める（issue #284）。
//
// **立てるのは次の全部を満たすときだけである。**`source: oauth_usage_api`（usage API から
// 切り替えている）で、haiku に `hello` を送った（AgentPrompt を呼んだ）あとに、値が届かず
// 止めるときの取り消し以外で終わり、**終わった時点でこの起動のあいだに1度も使用率を読めていない**。
//
// **誤りの種類ではなく、話しかけた結果で止める。**課金が起きるのは haiku に話しかけたときだけで、
// API キーの機械は usage API も成功せず使用率を持つ行も来ないので、1度も読めない。立て直しごとの
// 課金を1回で止める。Pro / Max の機械は、1度でも読めていれば止まらない（上限で断られても、
// 期間が明けたあとに取り直せる）。**終わった時点で判定する**（取得の途中で usage API が読めたり、
// pane の行が届いたりすれば、印が立っていて止めない）。
//
// result: 取得の結果。
// detail: 途中の誤りの文面。
// prompted: AgentPrompt を呼んだか。
// 戻り値: 取得止めを立てたら true（呼び出し側は、ふだんの WARN を出さない）。
func (o *Orchestrator) maybeStopStatuslineFetch(result statuslineFetchResult, detail string, prompted bool) bool {
	if o.cfg.RateLimit.Source != ratelimit.SourceOAuthUsageAPI || !prompted {
		return false
	}
	if result == fetchValueArrived || result == fetchCanceled {
		return false
	}
	o.quotaMu.Lock()
	stop := !o.quota.everRead
	if stop {
		o.quota.fetchStopped = true
	}
	o.quotaMu.Unlock()
	if !stop {
		return false
	}
	o.logger.Warn("statusline取得を止めます（haiku に話しかけても使用率が届かず、この起動のあいだに使用率を1度も読めていません）。"+
		"API キーで Claude Code を使っているなら、rate_limit.source を none にしてください（しないと、continuo を立て直すたびに haiku の会話が1回従量で課金されます）。"+
		"Pro / Max なら、usage API が読めるか run のステータスラインから使用率が届けば自動で戻ります。戻らなければ continuo を立て直してください",
		"reason", statuslineFetchResultName(result), "error", detail)
	return true
}

// statuslineFetchResultName は結果をログに載せる名前にする。
func statuslineFetchResultName(result statuslineFetchResult) string {
	switch result {
	case fetchValueArrived:
		return "value_arrived"
	case fetchBlocked:
		return "blocked"
	case fetchAgentNotFound:
		return "agent_not_found"
	case fetchNotStarted:
		return "timeout"
	case fetchMidwayError:
		return "midway_error"
	case fetchNoValue:
		return "no_value"
	case fetchNoLine:
		return "no_line"
	case fetchCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// statuslineAttempt は、workspace を作って Claude Code を起動し、`hello` を送って値を待ち、閉じる。
//
// fctx: 全体の上限を掛けたコンテキスト。
// ctx: 全体の上限を掛ける前のコンテキスト（止めるときに取り消される）。
// clone: cwd に使う clone のパス。
// settingsPath: statusline取得用の設定ファイル。
// 戻り値: 結果と、途中の誤りの文面と、AgentPrompt を呼んだか（取得止めの判定に使う）。
func (o *Orchestrator) statuslineAttempt(
	fctx, ctx context.Context, clone, settingsPath string,
) (statuslineFetchResult, string, bool) {
	uuid, err := NewSessionUUID()
	if err != nil {
		return fetchMidwayError, err.Error(), false
	}
	name := statuslineAgentName(uuid)
	watch := newFetchWatch(uuid)
	o.quotaMu.Lock()
	// **起動する前に基準 0 として登録する。**最初の行が「新しい応答の行」として扱われるように。
	o.quota.sessions[uuid] = sessionMark{baseAPIMs: 0, seenAt: o.now()}
	o.quota.fetch = watch
	o.quotaMu.Unlock()

	// **作る仕事は、全体の上限を掛ける前の ctx で呼ぶ**（Manager の中で期限を外し、取り消しは生かす）。
	ws, err := o.ws.OpenStatuslineWorkspace(ctx, clone)
	if err != nil {
		// **herdr が作ったのに応答が届かなかった workspace を、閉じ残しの一覧へ載せる。**
		// 載せないと ID を知る者が居なくなり、誰も閉じない（次の試行か次の起動で閉じる）。
		var createErr *workspace.StatuslineCreateError
		if errors.As(err, &createErr) {
			for _, id := range createErr.Orphans {
				if addErr := o.addStatuslineLeftover(id); addErr != nil {
					o.logger.Warn("statusline取得の workspace を閉じ残しの一覧へ載せられません（herdr の画面で手で閉じてください）",
						"workspace_id", id, "error", addErr)
				}
			}
		}
		if ctx.Err() != nil {
			return fetchCanceled, "", false
		}
		return fetchMidwayError, err.Error(), false
	}
	if err := o.addStatuslineLeftover(ws.WorkspaceID); err != nil {
		o.closeStatuslineFetchWorkspace(ctx, ws, fetchMidwayError)
		return fetchMidwayError, err.Error(), false
	}

	result, detail, prompted := o.startAndWaitStatusline(fctx, ctx, ws, name, uuid, settingsPath, watch)
	if result == fetchValueArrived {
		// **閉じるより先に知らせる。**閉じる呼び出しのぶん入札を遅らせない。
		o.notifyStatusline()
	}
	o.closeStatuslineFetchWorkspace(ctx, ws, result)
	return result, detail, prompted
}

// startAndWaitStatusline は Claude Code を起動し、入力を受け付けるまで待って `hello` を送り、値を待つ。
//
// 戻り値の3つ目は、AgentPrompt を呼んだか（haiku に話しかけたか）である。**呼んだ時点から
// 会話1回ぶん課金されうる**ので、取得止めの判定（maybeStopStatuslineFetch）に使う。
func (o *Orchestrator) startAndWaitStatusline(
	fctx, ctx context.Context, ws workspace.StatuslineWorkspace,
	name normalize.SafeName, uuid, settingsPath string, watch *fetchWatch,
) (result statuslineFetchResult, detail string, prompted bool) {
	// canceled は、止めるときの取り消しなら fetchCanceled、全体の上限の期限切れなら fetchNotStarted を返す。
	// prompted はそのまま返す（AgentPrompt を呼んだあとの期限切れは、取得止めの判定に数える）。
	canceled := func() (statuslineFetchResult, string, bool) {
		if ctx.Err() != nil {
			return fetchCanceled, "", prompted
		}
		return fetchNotStarted, "", prompted
	}
	args := []string{
		"--settings", settingsPath,
		"--model", "haiku",
		"--permission-mode", "dontAsk",
		"--restricted",
		"--strict-mcp-config",
		"--system-prompt", "Reply with one word.",
		"--tools", "",
		"--disable-slash-commands",
		"--session-id", uuid,
	}
	if _, err := o.herdr.AgentStartWithRetry(fctx, herdr.AgentStartParams{
		Name:   name,
		Kind:   o.cfg.Claude.Kind,
		PaneID: ws.PaneID,
		Args:   args,
	}, agentStartBusyBudget, agentStartRetryDelay); err != nil {
		if fctx.Err() != nil {
			return canceled()
		}
		return fetchMidwayError, err.Error(), false
	}

	startup := time.Duration(o.cfg.Herdr.StartupTimeoutMs) * time.Millisecond
	deadline := o.now().Add(startup)
	var notFoundSince time.Time
	for ready := false; !ready; {
		got, err := o.herdr.AgentGet(fctx, herdr.AgentGetParams{Target: name})
		switch {
		case err != nil && herdr.IsCode(err, herdr.ErrCodeAgentNotFound):
			if notFoundSince.IsZero() {
				notFoundSince = o.now()
			}
			if o.now().Sub(notFoundSince) >= startup/2 {
				return fetchAgentNotFound, "", false
			}
		case err != nil:
			if fctx.Err() != nil {
				return canceled()
			}
			return fetchMidwayError, err.Error(), false
		default:
			notFoundSince = time.Time{}
			switch got.Agent.AgentStatus {
			case herdr.AgentStatusBlocked:
				return fetchBlocked, "", false
			case herdr.AgentStatusIdle, herdr.AgentStatusDone:
				ready = got.Agent.InteractiveReady
			}
			if !ready && o.now().After(deadline) {
				return fetchNotStarted, "", false
			}
		}
		if ready {
			break
		}
		select {
		case <-fctx.Done():
			return canceled()
		case <-time.After(time.Second):
		}
	}
	// **呼ぶ前に印を立てる。**AgentPrompt が誤りを返しても、送れている場合がある。
	prompted = true
	if _, err := o.herdr.AgentPrompt(fctx, herdr.AgentPromptParams{Target: name, Text: statuslineFetchPrompt}); err != nil {
		if fctx.Err() != nil {
			return canceled()
		}
		return fetchMidwayError, err.Error(), true
	}
	timer := time.NewTimer(statuslineValueWait)
	defer timer.Stop()
	select {
	case <-watch.done:
		return fetchValueArrived, "", true
	case <-timer.C:
	case <-fctx.Done():
		if ctx.Err() != nil {
			return fetchCanceled, "", true
		}
	}
	o.quotaMu.Lock()
	saw := watch.sawResponse
	o.quotaMu.Unlock()
	if saw {
		return fetchNoValue, "", true
	}
	return fetchNoLine, "", true
}

// closeStatuslineFetchWorkspace は statusline取得の workspace を閉じ、閉じ残しの一覧を直す。
//
// **止めるときにも取り消さない**（押さえを放すのはこの仕事だけ。Manager の中で WithoutCancel にする）。
func (o *Orchestrator) closeStatuslineFetchWorkspace(
	ctx context.Context, ws workspace.StatuslineWorkspace, result statuslineFetchResult,
) {
	outcome, err := o.ws.CloseStatuslineWorkspace(ctx, ws)
	switch outcome {
	case workspace.StatuslineClosed:
		o.removeStatuslineLeftover(ws.WorkspaceID)
	case workspace.StatuslineParentWithChild:
		o.logger.Warn("statusline取得の workspace が issue の親にされたので、子の issue の workspace が閉じたあとに閉じます",
			"workspace_id", ws.WorkspaceID)
	default:
		if result == fetchCanceled {
			return
		}
		o.logger.Warn("statusline取得ができません（閉じられなかった: statusline取得の workspace が herdr の画面に残ります。"+
			"次の試行か起動時に閉じ直します。herdr の画面で手で閉じてもかまいません）",
			"workspace_id", ws.WorkspaceID, "error", err)
	}
}

// reportStatuslineFetch は結果ごとに WARN を出す。値が届いたときと止めるときは出さない。
func (o *Orchestrator) reportStatuslineFetch(result statuslineFetchResult, detail string, startup time.Duration) {
	switch result {
	case fetchValueArrived, fetchCanceled:
	case fetchBlocked:
		o.logger.Warn("statusline取得ができません（確認の画面で止まった: 選んだ clone を Claude Code が信頼済みと見なしていません。" +
			"~/.claude.json の記録と Claude Code の版を確かめてください）")
	case fetchAgentNotFound, fetchNotStarted:
		o.logger.Warn("statusline取得ができません（起動しなかった: 期限までに入力を受け付ける状態になりませんでした。"+
			"Claude Code が 2.1.248 以上か（--restricted を持つか）を確かめてください）",
			"herdr.startup_timeout_ms", startup.Milliseconds())
	case fetchNoValue:
		o.logger.Warn("statusline取得ができません（応答はあったが値が無い: 3分の間に応答はありましたが、使用率を持つ行が届きませんでした。" +
			"使用率が届くのは Pro / Max だけです。それ以外の契約か API キーなら rate_limit.source: none にしてください。上限に当たっている場合もあります）")
	case fetchNoLine:
		o.logger.Warn("statusline取得ができません（値が1行も届かなかった: 3分の間に応答が1度もありませんでした。" +
			"上限に当たっている・組織の managed settings に statusLine が書かれている・古い herdr が確認の画面で入力を受け付けると答えた、のどれかが考えられます。" +
			"API キーで Claude Code を使っているなら rate_limit.source: none にしてください）")
	default:
		o.logger.Warn("statusline取得ができません（途中の誤り）", "error", detail)
	}
}

// chooseStatuslineClone は statusline取得に使う clone を選ぶ。
//
// **起動時に読んだ trust.repositories を上から見て、手元に clone があって Claude Code に
// 信頼されている最初の1つ**（issue の run と同じ判定。`~/.claude.json` は読むだけ）。
// 試行のたびに選ぶ。**git を起こす。**
//
// 戻り値: clone のパス。無ければ空文字。判定そのものに失敗したら誤り。
func (o *Orchestrator) chooseStatuslineClone() (string, error) {
	var firstErr error
	for _, entry := range o.cfg.Trust.Repositories {
		owner, repo, ok := strings.Cut(entry, "/")
		if !ok || owner == "" || repo == "" {
			continue
		}
		path, err := o.ws.TrustedClonePath(owner, repo)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if path != "" {
			return path, nil
		}
	}
	return "", firstErr
}

// statuslineAgentName は statusline取得の agent の名前を作る（`sl-` と UUID の先頭12桁の16進）。
func statuslineAgentName(uuid string) normalize.SafeName {
	hex := strings.ReplaceAll(uuid, "-", "")
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return normalize.SafeName("sl-" + strings.ToLower(hex))
}

// statuslineFetchDir は statusline取得の作業ディレクトリを返す。
func (o *Orchestrator) statuslineFetchDir() string {
	return filepath.Join(o.runtimeDir, statuslineFetchDirName)
}

// statuslineFetchSettings は statusline取得用の設定ファイルの中身である。
//
// **持つのは statusLine と env だけである。**`--restricted` で起動するので、利用者・プロジェクト・
// ローカルの設定ファイルは読まれない。continuo も利用者の `~/.claude/settings.json` を読まない（設計 3-12）。
type statuslineFetchSettings struct {
	StatusLine *statusLineSetting `json:"statusLine"`
	Env        map[string]string  `json:"env,omitempty"`
}

// writeStatuslineFetchSettings は statusline取得用の設定ファイルを書く（0600。一時ファイルから差し替える）。
//
// **env は claude.env から CLAUDE_CODE_RETRY_WATCHDOG を除き、CLAUDE_CODE_SKIP_PROMPT_HISTORY=1 を
// 足したもの**（会話の記録を残さない。issue の run と同じく設定ファイルの env で受け取る）。
//
// 戻り値: 書いたファイルの絶対パス。
func (o *Orchestrator) writeStatuslineFetchSettings() (string, error) {
	dir := o.statuslineFetchDir()
	if err := os.MkdirAll(dir, settingsDirPerm); err != nil {
		return "", err
	}
	env := map[string]string{}
	for k, v := range o.cfg.Claude.Env {
		if k == "CLAUDE_CODE_RETRY_WATCHDOG" {
			continue
		}
		env[k] = v
	}
	env["CLAUDE_CODE_SKIP_PROMPT_HISTORY"] = "1"
	data, err := json.MarshalIndent(statuslineFetchSettings{
		StatusLine: o.statusLineSetting(),
		Env:        env,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, settingsFileName)
	if err := atomicfile.Write(path, append(data, '\n'), settingsFilePerm); err != nil {
		return "", err
	}
	return path, nil
}

// PrepareStatusline は、daemon が起動時検査のあと・復元の前に呼ぶ（issue #284）。
//
//  1. 閉じ残しの statusline取得の workspace を片付ける（`rate_limit.source` によらない）
//  2. `source` が `none` でなければ quota.json を読む（sl.sock の受け付けと usage API の最初の
//     読み取りより前）
//
// **復元より前に片付ける。**復元の片付けの `worktree.open` が、落ちたあとの閉じ残しを
// issue の親にしうるためである。
func (o *Orchestrator) PrepareStatusline(ctx context.Context) {
	o.cleanupStatuslineLeftovers(ctx)
	if o.cfg.RateLimit.Source != ratelimit.SourceNone {
		o.loadQuota()
	}
}

// cleanupStatuslineLeftovers は閉じ残しの一覧の workspace を Manager で片付け、一覧を書き直す。
func (o *Orchestrator) cleanupStatuslineLeftovers(ctx context.Context) {
	if o.ws == nil || o.runtimeDir == "" {
		return
	}
	o.fetchListMu.Lock()
	defer o.fetchListMu.Unlock()
	ids, err := o.readStatuslineLeftoversLocked()
	if err != nil {
		o.logger.Warn("閉じ残しの statusline取得の一覧を読めません", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	remaining := o.ws.CloseLeftoverStatuslineWorkspaces(ctx, ids)
	if err := o.writeStatuslineLeftoversLocked(remaining); err != nil {
		o.logger.Warn("閉じ残しの statusline取得の一覧を書けません", "error", err)
	}
}

// addStatuslineLeftover は、作った workspace の ID を閉じ残しの一覧へ足す。
func (o *Orchestrator) addStatuslineLeftover(id string) error {
	o.fetchListMu.Lock()
	defer o.fetchListMu.Unlock()
	ids, err := o.readStatuslineLeftoversLocked()
	if err != nil {
		return err
	}
	for _, have := range ids {
		if have == id {
			return nil
		}
	}
	return o.writeStatuslineLeftoversLocked(append(ids, id))
}

// removeStatuslineLeftover は、閉じた workspace の ID を閉じ残しの一覧から外す。
func (o *Orchestrator) removeStatuslineLeftover(id string) {
	o.fetchListMu.Lock()
	defer o.fetchListMu.Unlock()
	ids, err := o.readStatuslineLeftoversLocked()
	if err != nil {
		o.logger.Warn("閉じ残しの statusline取得の一覧を読めません", "error", err)
		return
	}
	keep := ids[:0]
	for _, have := range ids {
		if have != id {
			keep = append(keep, have)
		}
	}
	if err := o.writeStatuslineLeftoversLocked(keep); err != nil {
		o.logger.Warn("閉じ残しの statusline取得の一覧を書けません", "error", err)
	}
}

// statuslineLeftoverPath は閉じ残しの一覧のパスを返す。
func (o *Orchestrator) statuslineLeftoverPath() string {
	return filepath.Join(o.statuslineFetchDir(), statuslineFetchListName)
}

// readStatuslineLeftoversLocked は閉じ残しの一覧を読む。o.fetchListMu を持って呼ぶ。
func (o *Orchestrator) readStatuslineLeftoversLocked() ([]string, error) {
	data, err := os.ReadFile(o.statuslineLeftoverPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("%s: %w", o.statuslineLeftoverPath(), err)
	}
	return ids, nil
}

// writeStatuslineLeftoversLocked は閉じ残しの一覧を書く（一時ファイルから差し替える）。
// o.fetchListMu を持って呼ぶ。
func (o *Orchestrator) writeStatuslineLeftoversLocked(ids []string) error {
	if err := os.MkdirAll(o.statuslineFetchDir(), settingsDirPerm); err != nil {
		return err
	}
	if ids == nil {
		ids = []string{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return atomicfile.Write(o.statuslineLeftoverPath(), append(data, '\n'), settingsFilePerm)
}
