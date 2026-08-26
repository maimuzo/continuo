package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// HookServer は復元が呼ぶ hook の受け口である（設計 3-4 の段5d / 5e / 6b）。
//
// **順番が意味を持つ。**listen（段5d）→ 逃がし先の読み戻し（段5e）→ 配送の開始（段6b）
// でなければ、読み戻しと listen のあいだの窓に落ちた hook を誰も読まない。
//
// ***hookserver.Server がこれを満たす。**インタフェースにしてあるのは、復元のテストが
// 「どの順で呼ばれたか」を実物の socket を立てずに数えられるようにするためである。
type HookServer interface {
	// Start は socket を作って listen を始める（段5d）。届いた hook は溜めるだけである。
	Start() error
	// ReplayPending は逃がし先に溜まった hook を読み戻す（段5e の1回目の走査）。
	ReplayPending() error
	// StartDelivery は溜めた hook の配送を始める（段6b）。
	// **この中で逃がし先をもう一度走査する**（段5e の2回目）。
	StartDelivery()
}

// **本番の実装がこのインタフェースを満たすことを、コンパイル時に確かめる。**
var _ HookServer = (*hookserver.Server)(nil)

// RestoreResult は復元が何をしたかの記録である。
//
// **判断には使わない。**起動時の掃除（3-9 の手順6 / 6b）へ渡す材料と、
// ログ・テストのための観測口である。
type RestoreResult struct {
	// Worktrees は置き場所の走査で見つかり、消さずに残った worktree の絶対パスである。
	// **孤児 branch の掃除の対象のリポジトリはここから引く**（ボードを読まずに決まる）。
	Worktrees []string
	// Adopted は引き継いだ issue の識別子である。
	Adopted []string
	// AdoptedBranches は引き継いだ run の branch 名である。
	// **孤児 branch の掃除で消してはならない branch の一覧になる。**
	AdoptedBranches []string
	// ClosedPanes は復元の中で閉じた pane の ID である。
	ClosedPanes []string
	// Cleaned は片付けた worktree の絶対パスである。
	Cleaned []string
}

// restoreCandidate は復元の途中で持ち回る worktree 1件である。
type restoreCandidate struct {
	// Path は worktree の絶対パスである。
	Path string
	// Identity は読めた身元ファイルの中身である。
	Identity *workspace.Identity
	// Owner は**置き場所のパスから引いた**所有者名である（設計 3-22 の固定4階層）。
	// Repo は同じくリポジトリ名である。
	//
	// **身元ファイルからは読まない。**身元ファイルは worktree の直下にあり、
	// その worktree ではエージェントが `--permission-mode dontAsk` で動く（設計 3-16 の段9）。
	// **パスは封じ込め検査（設計 3-20）を通ったものなので、エージェントには書き換えられない。**
	Owner string
	Repo  string
}

// adoption は引き継ぐと決めた run である（段5c で組み立て、段6 で印へ入れる）。
type adoption struct {
	// Issue は取り直した issue のスナップショットである。
	Issue tracker.Issue
	// State は引き継ぐ run の実行時状態である。
	State AdoptedRun
	// NeedsPrompt は継続の指示を送るかどうかである。
	// **`agent_status` が `working` のときは偽にする**（設計 3-4 の段5a2）。
	NeedsPrompt bool
	// Branch は身元ファイルに書かれていた branch 名である（孤児 branch の掃除で残す）。
	Branch string
}

// Restore は再起動時の復元を行う（設計 3-4 の段2〜段9）。
//
// **呼ぶ順番が決まっている。**設定の検証 → `flock` → 3-6 の起動時検査 → **この関数** →
// 巡回の開始、である（設計 3-4 の「起動から復元までの順序」）。
// **巡回より先に呼ばなければならない。**先に巡回を始めると、これから引き継ぐ run の
// worktree に2つ目の Claude Code が立つ。
//
// **この中で `agent.prompt` を呼ばない**（設計 3-4 の段5c）。wait つきの呼び出しは
// turn の終わりまで返らない（既定1時間）ので、復元がそこで止まる。代わりに
// `NeedsPrompt` を立て、巡回の turn ループに非同期で送らせる。
//
// **引き継げないと決めた run の pane は必ず閉じる**（設計 3-4）。巡回には
// 「生きている pane を引き継ぐ」経路が無いので（3-16）、残すと2つ目が立つ。
//
// ctx: 呼び出しに適用するコンテキスト。
// hs: hook の受け口。**nil を渡してはならない**（段5d で listen を始められない）。
// 戻り値の1つ目: 復元の記録。**起動時の掃除（SweepOnStartup）へそのまま渡す。**
// 戻り値の2つ目: hook の受け口の listen を始められなかった場合のエラー。
// **それ以外はエラーにしない**（置き場所を読めない・ボードを取り直せない・herdr から
// 一覧を取れない、はいずれも警告を出して起動を続ける。設計 3-4 の段3）。
func (o *Orchestrator) Restore(ctx context.Context, hs HookServer) (*RestoreResult, error) {
	result := &RestoreResult{}

	// 段1b: 身元ファイルを読めない worktree を、置き場所と pane の label とボードから
	// 復元する（設計 3-49）。**段2 より先に行う。**復元できれば、段2 以降は
	// ふつうの引き継ぎの候補として扱える。
	// **復元できないものが残ったら、`workspace.on_broken_worktree` に従う**（既定は止める）。
	if err := o.handleBrokenWorktrees(ctx); err != nil {
		return result, err
	}

	// 段2: 置き場所を固定の4階層で走査し、身元ファイルを読む。
	candidates, discarded := o.scanIdentities()

	// 段3: 身元ファイルの project item の ID で、ボードを ID 指定でまとめて取り直す（1リクエスト）。
	issues, fetchFailed := o.refetchByIdentities(ctx, candidates)

	// 段4: herdr から pane と agent の一覧を取り、cwd と worktree のパスで突き合わせる。
	m := o.matchPanes(ctx, candidates, discarded)
	result.ClosedPanes = append(result.ClosedPanes, o.closeExtraPanes(ctx, m)...)

	// 段5〜5c: 引き継ぐかを決める。**turn は送らない。**
	adoptions, noPane := o.decideAdoptions(ctx, candidates, issues, fetchFailed, m, result)

	// 段5d: listen を始める。**ただし配送はまだ始めない。**
	if err := hs.Start(); err != nil {
		return result, i18n.Errorf(i18n.KeyOrchestratorRestoreHookListenFailed, err)
	}
	// 段5e: 逃がし先を読み戻す（2回目の走査は StartDelivery が行う）。
	if err := hs.ReplayPending(); err != nil {
		o.logger.Warn("逃がし先の読み戻しに失敗しました（起動は続けます）", "error", err)
	}

	// 段6: 引き継ぐと決めた issue を、実行中の一覧と印の集合の両方へ入れ直す。
	// 段7: turn 数は 1 から数え直す（newRunState がゼロから始める）。
	for _, a := range adoptions {
		if !o.Adopt(a.Issue, a.State, a.NeedsPrompt) {
			o.logger.Warn("既に印を持っているので引き継ぎを飛ばしました", "identifier", a.Issue.Identifier)
			continue
		}
		o.logger.Info("run を引き継ぎました",
			"identifier", a.Issue.Identifier,
			"agent", a.State.AgentName.String(),
			"pane_id", a.State.PaneID,
			"needs_prompt", a.NeedsPrompt)
		result.Adopted = append(result.Adopted, a.Issue.Identifier)
		if a.Branch != "" {
			result.AdoptedBranches = append(result.AdoptedBranches, a.Branch)
		}
	}

	// 段6b: 溜めた hook の配送を始める（索引ができたので流せる）。
	hs.StartDelivery()

	// 段8: 身元ファイルがあるのに pane が無い run を、取り直した Status で分岐させる。
	for _, c := range noPane {
		o.restoreWithoutPane(ctx, c, issues, fetchFailed, result)
	}

	// 消さずに残った worktree を、起動時の掃除の対象として返す。
	for _, c := range candidates {
		if !containsString(result.Cleaned, c.Path) {
			result.Worktrees = append(result.Worktrees, c.Path)
		}
	}
	result.Worktrees = append(result.Worktrees, discarded...)

	o.logger.Info("復元を終えました",
		"worktrees", len(result.Worktrees),
		"adopted", len(result.Adopted),
		"closed_panes", len(result.ClosedPanes),
		"cleaned", len(result.Cleaned))
	return result, nil
}

// scanIdentities は置き場所を走査し、同じ project item の ID が重複したら
// `created_at` が新しいほうを採る（設計 3-4 の段2）。
//
// **この段で決めるのは「どちらを採るか」だけである。**pane にはまだ触れない
// （pane の一覧を取るのは段4 であり、この段では誰が生きているかを知らない）。
// **採らなかったほうの worktree は消さずに残す。**どちらに成果があるか continuo には
// 判断できない。
//
// 戻り値の1つ目: 採った worktree（パスの昇順）。
// 戻り値の2つ目: 採らなかったほうの worktree のパス（「捨てた身元」。段4 で pane を閉じる）。
func (o *Orchestrator) scanIdentities() ([]restoreCandidate, []string) {
	scanned, err := o.ws.Scan()
	if err != nil {
		o.logger.Warn("worktree の置き場所を走査できません（引き継げる run はありません）", "error", err)
		return nil, nil
	}

	byItem := map[string]restoreCandidate{}
	var discarded []string
	for _, w := range scanned {
		if w.Err != nil {
			// **壊れた身元ファイルは無視してログに出す。消さない**（設計 3-4 の段2）。
			o.logger.Warn("身元ファイルを読めない worktree を飛ばしました（消しません）",
				"path", w.Path, "error", w.Err)
			continue
		}
		if w.Identity == nil {
			continue
		}
		if w.Identity.ProjectItemID == "" {
			o.logger.Warn("身元ファイルに project item の ID が無いので取り直せません（消しません）",
				"path", w.Path, "issue", w.Identity.IssueIdentifier)
			continue
		}
		// **`project_item_id` を鍵にする前に、置き場所と辻褄が合うかを検算する。**
		// この値もエージェントが書き換えられるので、検算しないと**別の issue の
		// 生きている run を乗っ取れる**（`continuo abandon` の pathAgrees と同じ判断）。
		owner, repo, ok := o.pathAgrees(w.Path, w.Identity)
		if !ok {
			continue
		}
		cand := restoreCandidate{Path: w.Path, Identity: w.Identity, Owner: owner, Repo: repo}
		cur, exists := byItem[w.Identity.ProjectItemID]
		if !exists {
			byItem[w.Identity.ProjectItemID] = cand
			continue
		}
		// 重複したら created_at が新しいほうを採る。
		newer, older := cur, cand
		if w.Identity.CreatedAt.After(cur.Identity.CreatedAt) {
			newer, older = older, cur
		}
		byItem[w.Identity.ProjectItemID] = newer
		discarded = append(discarded, older.Path)
		o.logger.Warn("同じ issue の worktree が2つあるので created_at が新しいほうを採ります（古いほうは消しません）",
			"issue", w.Identity.IssueIdentifier,
			"採用", newer.Path, "採用しなかった", older.Path)
	}

	candidates := make([]restoreCandidate, 0, len(byItem))
	for _, c := range byItem {
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	sort.Strings(discarded)
	return candidates, discarded
}

// pathAgrees は、身元ファイルの中身が worktree の置き場所と辻褄が合うかを検算する。
//
// **言いたいこと。**`project_item_id` はエージェントが書き換えられる。検算しないと、
// **別の issue の生きている run を乗っ取れる。**
//
// **何が起きるか。**worktree の直下の身元ファイル（`<worktree>/.continuo.json`）で、
// その worktree のエージェントは `--permission-mode dontAsk` で動く（設計 3-16 の段9）。
// そこで `project_item_id` を走行中の別 issue のものに書き換え、`created_at` を新しくすると、
// 復元の段2 は「同じ issue の worktree が2つある」と判定して**被害者の worktree を
// 『捨てた身元』にし、段4 でその生きた pane を閉じる。**以後その issue の run は
// 書き換えた側の worktree として印に入り、片付けの宛先まで入れ替わる。
//
// **検算の根拠は worktree のパスだけである。**置き場所は `<root>/<host>/<owner>/<repo>/<スラグ>` の
// 固定4階層（設計 3-22）で、封じ込め検査（設計 3-20）を通っている。**エージェントには
// 書き換えられない。**そこから引いた `<owner>/<repo>` と、身元ファイルが名乗る
// `issue_identifier` / `issue_url` の `<owner>/<repo>` を、大文字小文字を無視して比べる。
//
// **食い違ったら候補から外す。消さない。**どちらが正しいか continuo には判断できない。
//
// worktreePath: worktree の絶対パス。
// identity: 読めた身元ファイル。
// 戻り値の1つ目: 置き場所から引いた所有者名。
// 戻り値の2つ目: 置き場所から引いたリポジトリ名。
// 戻り値の3つ目: 辻褄が合えば true。**引けなかった場合と名乗りが無い場合も false**
// （分からないものを候補に採らない）。
func (o *Orchestrator) pathAgrees(worktreePath string, identity *workspace.Identity) (string, string, bool) {
	owner, repo, err := o.ws.OwnerRepoOf(worktreePath)
	if err != nil {
		o.logger.Warn("worktree の置き場所から owner/repo を引けないので候補にしません（消しません）",
			"path", worktreePath, "error", err)
		return "", "", false
	}
	claimedOwner, claimedRepo, ok := identityOwnerRepo(identity)
	if !ok {
		o.logger.Warn("身元ファイルが owner/repo を名乗っていないので候補にしません（消しません）",
			"path", worktreePath, "issue_identifier", identity.IssueIdentifier, "issue_url", identity.IssueURL)
		return "", "", false
	}
	if !strings.EqualFold(owner, claimedOwner) || !strings.EqualFold(repo, claimedRepo) {
		o.logger.Warn("身元ファイルの名乗りが worktree の置き場所と食い違うので候補にしません（消しません）",
			"path", worktreePath,
			"置き場所", owner+"/"+repo,
			"身元ファイルの名乗り", claimedOwner+"/"+claimedRepo,
			"project_item_id", identity.ProjectItemID)
		return "", "", false
	}
	return owner, repo, true
}

// issueAgreesWithPath は、取り直した issue が worktree の置き場所と同じリポジトリのものかを返す。
//
// **`project_item_id` で引いた結果も検算する。**身元ファイルの名乗りと置き場所が
// 揃っていても、**`project_item_id` だけを別 issue のものに差し替えれば**（同じリポジトリの
// 名前を名乗ったまま）取り直しは別 issue を返す。ここで止めないと、その別 issue の
// Status を落としたり worktree を片付けたりしてしまう。
//
// c: 対象の worktree（`Owner` / `Repo` は置き場所から引いた値）。
// issue: 段3 で取り直した issue。
// 戻り値: 同じリポジトリのものなら true。**draft issue（owner が空）は false。**
func (o *Orchestrator) issueAgreesWithPath(c restoreCandidate, issue tracker.Issue) bool {
	if strings.EqualFold(issue.Owner, c.Owner) && strings.EqualFold(issue.Repo, c.Repo) {
		return true
	}
	o.logger.Warn("取り直した issue が worktree の置き場所と違うリポジトリなので何もしません（pane も worktree も残します）",
		"path", c.Path,
		"置き場所", c.Owner+"/"+c.Repo,
		"取り直した issue", issue.Identifier,
		"project_item_id", c.Identity.ProjectItemID)
	return false
}

// identityOwnerRepo は身元ファイルが名乗る `<owner>/<repo>` を取り出す。
//
// **`issue_identifier`（`<owner>/<repo>#<番号>`）を先に見て、無ければ `issue_url` の
// パスから取る。**どちらも読めなければ「名乗っていない」として偽を返す。
//
// **ここで取れる値は信用しない。**呼び出し側（pathAgrees）が、置き場所から引いた値と
// 突き合わせるための材料である。
//
// identity: 読めた身元ファイル。
// 戻り値の1つ目: 名乗っている所有者名。
// 戻り値の2つ目: 名乗っているリポジトリ名。
// 戻り値の3つ目: どちらも取れたら true。
func identityOwnerRepo(identity *workspace.Identity) (string, string, bool) {
	if owner, repo, ok := splitOwnerRepo(strings.SplitN(identity.IssueIdentifier, "#", 2)[0]); ok {
		return owner, repo, true
	}
	u, err := url.Parse(strings.TrimSpace(identity.IssueURL))
	if err != nil || u.Path == "" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return splitOwnerRepo(parts[0] + "/" + parts[1])
}

// splitOwnerRepo は `<owner>/<repo>` を2つに割る。
//
// raw: 割る文字列。
// 戻り値の1つ目: 所有者名。
// 戻り値の2つ目: リポジトリ名。
// 戻り値の3つ目: どちらも空でなければ true。
func splitOwnerRepo(raw string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner, repo := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

// refetchByIdentities は身元ファイルの project item の ID で、ボードを ID 指定で
// まとめて取り直す（設計 3-4 の段3。**1リクエストで済ませる**）。
//
// **この1回が「落ちている間に届かなかった Stop の取り戻し」も兼ねる**（設計 3-19）。
// hook を待たずに、ここで現在の Status を確定させる。
//
// ctx: 呼び出しに適用するコンテキスト。
// candidates: 段2 で採った worktree。
// 戻り値の1つ目: project item の ID から issue を引く写像。
// 戻り値の2つ目: 取り直しそのものに失敗したら true（**起動は続ける。**
// ただし引き継げないので、対応する pane は閉じる）。
func (o *Orchestrator) refetchByIdentities(
	ctx context.Context,
	candidates []restoreCandidate,
) (map[string]tracker.Issue, bool) {
	if len(candidates) == 0 {
		return map[string]tracker.Issue{}, false
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.Identity.ProjectItemID)
	}
	issues, err := o.tracker.FetchIssuesByIDs(ctx, ids)
	if err != nil {
		o.logger.Warn(
			"復元のための取り直しに失敗しました（起動は続けます。引き継げない run の pane は閉じます）",
			"error", err, "count", len(ids))
		return map[string]tracker.Issue{}, true
	}
	byID := make(map[string]tracker.Issue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	return byID, false
}

// paneMatch は段4 の突き合わせの結果である。
type paneMatch struct {
	// ByWorktree は worktree の絶対パス（走査で得た値そのまま）から pane を引く写像である。
	ByWorktree map[string]herdr.Pane
	// DiscardedPanes は「捨てた身元」の worktree に付いていた pane である（段4 で閉じる）。
	DiscardedPanes []herdr.Pane
	// DuplicatePanes は、同じ worktree を cwd に持つ pane が2つ以上あったときに
	// 引き継がないと決めたほうである（段4 で閉じる）。
	//
	// **後勝ちで捨ててはならない。**捨てたほうを閉じずに残すと、設計が最も避けたい
	// 「同じ worktree に Claude Code が2つ」がそのまま残る（設計 3-4 / 3-16）。
	DuplicatePanes []herdr.Pane
	// AgentByPane は pane の ID から agent を引く写像である（`agent.list` から作る）。
	AgentByPane map[string]herdr.Agent
	// Unknown は「突き合わせができなかった」ことを表す（`pane.list` か `agent.list` が失敗した）。
	//
	// **空の写像と区別が付かないと、生きている run を全件『pane が無い』として扱う。**
	// 段8 は pane を閉じないだけで、**Status の書き換えも worktree の片付けも行う。**
	// `restart.orphan_running_action` が `to_failure_state` なら、**herdr の一時的な失敗
	// 1回で走っている全部の run が人間へ渡され、「pane が残っていませんでした」という
	// 嘘の理由が issue に投稿される。**pane は実際には生きていて、誰も閉じない。
	Unknown bool
}

// matchPanes は herdr から pane と agent の一覧を取り、pane の cwd と worktree のパスで
// 突き合わせる（設計 3-4 の段4）。
//
// **両方を `filepath.EvalSymlinks` で解決してから比較する。**置き場所はシンボリックリンクを
// 解決した実体で持っている（3-20）が、pane の cwd は Claude Code を起動したときの文字列が
// そのまま入りうる。**解決に失敗したパス（消えた worktree など）は突き合わせの対象から外す。**
//
// **身元ファイルの無い worktree に付いた pane は閉じない**（設計 3-4 の段9）。
// continuo のものと断定できないので、ログへ残して人間に見せる。
//
// **同じ worktree に pane が2つ以上あったら、1つだけ引き継ぎの相手にして残りは閉じる**
// （`DuplicatePanes`）。片方を黙って捨てると2つ目が立ったままになる。
//
// ctx: 呼び出しに適用するコンテキスト。
// candidates: 段2 で採った worktree。
// discarded: 段2 で採らなかった worktree のパス。
// 戻り値: 突き合わせの結果。**`pane.list` と `agent.list` のどちらかを取れなかった場合は
// `Unknown` を真にした空の写像を返す**（引き継ぎも段8 も行わず、次の巡回に委ねる。
// **pane は1つも閉じない**）。
func (o *Orchestrator) matchPanes(
	ctx context.Context,
	candidates []restoreCandidate,
	discarded []string,
) paneMatch {
	m := paneMatch{
		ByWorktree:  map[string]herdr.Pane{},
		AgentByPane: map[string]herdr.Agent{},
	}

	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		o.logger.Warn("pane の一覧を取れないので、この起動では pane の有無を判断しません（pane は1つも閉じません）",
			"error", err)
		m.Unknown = true
		return m
	}
	agents, err := o.herdr.AgentList(ctx)
	if err != nil {
		// **`pane.list` の失敗と同じ扱いにする。**agent 名を引けないまま先へ進むと、
		// 段8b の「pane はあるが agent 名が無い」が全件で真になり、**引き継げたはずの
		// run の pane を全件閉じる。**herdr の一時的な失敗1回で、走っている全部の
		// エージェントの作業を捨てることになる。
		// **「判断できなかった」印を付けて返す。**pane は1つも閉じず、段8 も呼ばない。
		o.logger.Warn("agent の一覧を取れないので、この起動では pane の有無を判断しません（pane は1つも閉じません）",
			"error", err)
		m.Unknown = true
		return m
	}
	for _, a := range agents.Agents {
		if a.PaneID == "" || a.Name == "" {
			continue
		}
		m.AgentByPane[a.PaneID] = a
	}

	// worktree 側の解決済みパスを引けるようにする。
	resolvedToPath := map[string]string{}
	for _, c := range candidates {
		if resolved, ok := resolvePath(c.Path); ok {
			resolvedToPath[resolved] = c.Path
		}
	}
	discardedResolved := map[string]bool{}
	for _, path := range discarded {
		if resolved, ok := resolvePath(path); ok {
			discardedResolved[resolved] = true
		}
	}

	for _, pane := range list.Panes {
		if pane.Cwd == "" {
			continue
		}
		resolvedCwd, ok := resolvePath(pane.Cwd)
		if !ok {
			continue
		}
		if path, hit := resolvedToPath[resolvedCwd]; hit {
			prev, dup := m.ByWorktree[path]
			if !dup {
				m.ByWorktree[path] = pane
				continue
			}
			// 同じ worktree を cwd に持つ pane が2つ以上ある。
			// **後勝ちで上書きしてはならない。**上書きされたほうは引き継がれも
			// 閉じられも記録されもせず、「同じ worktree に Claude Code が2つ」が
			// そのまま残る（設計 3-4 / 3-16。段2 の「同じ issue の worktree が2つ」の対称形）。
			// **pane の ID の昇順で1つだけ残す。**`pane.list` が返す順に結果を依存させない。
			keep, drop := prev, pane
			if pane.PaneID < prev.PaneID {
				keep, drop = pane, prev
			}
			m.ByWorktree[path] = keep
			m.DuplicatePanes = append(m.DuplicatePanes, drop)
			o.logger.Warn("同じ worktree に pane が2つあります（1つだけ引き継ぎ、残りは閉じます）",
				"path", path, "引き継ぐ", keep.PaneID, "閉じる", drop.PaneID)
			continue
		}
		if discardedResolved[resolvedCwd] {
			m.DiscardedPanes = append(m.DiscardedPanes, pane)
			continue
		}
		// 段9: 置き場所の内側なのに身元ファイルが無い worktree の pane である。
		// **閉じずにログへ残す**（continuo のものと断定できない）。
		if isUnder(o.ws.ResolvedRoot(), resolvedCwd) {
			o.logger.Warn("身元ファイルの無い worktree に pane がありました（閉じません。人間が確かめてください）",
				"pane_id", pane.PaneID, "cwd", pane.Cwd)
		}
	}
	return m
}

// closeExtraPanes は、引き継ぐ相手にならないと段4 で決まった pane を閉じる
// （設計 3-4 の段4）。対象は次の2つである。
//
//	「捨てた身元」の worktree に付いていた pane  … 同じ issue に2つの Claude Code が居る
//	同じ worktree に2つ以上あった pane の残り     … 同じ worktree に2つの Claude Code が居る
//
// **どちらも残してはならない。**巡回には「生きている pane を引き継ぐ」経路が無いので
// （設計 3-16）、残すと2つ目が立ったままになる。
//
// ctx: 呼び出しに適用するコンテキスト。
// m: 段4 の突き合わせの結果。
// 戻り値: 閉じた pane の ID。
func (o *Orchestrator) closeExtraPanes(ctx context.Context, m paneMatch) []string {
	var closed []string
	for _, pane := range m.DiscardedPanes {
		o.logger.Warn("同じ issue の2つ目の worktree に pane があったので閉じます（worktree は残します）",
			"pane_id", pane.PaneID, "cwd", pane.Cwd)
		if o.closePane(ctx, pane.PaneID) {
			closed = append(closed, pane.PaneID)
		}
	}
	for _, pane := range m.DuplicatePanes {
		o.logger.Warn("同じ worktree に2つ目の pane があったので閉じます（worktree は残します）",
			"pane_id", pane.PaneID, "cwd", pane.Cwd)
		if o.closePane(ctx, pane.PaneID) {
			closed = append(closed, pane.PaneID)
		}
	}
	return closed
}

// decideAdoptions は pane が生きている run について引き継ぐかを決める
// （設計 3-4 の段5 / 5a / 5a2 / 5b / 5c）。
//
// **この中で turn を送らない。**組み立てるのは `AdoptedRun` までである。
//
// ctx: 呼び出しに適用するコンテキスト。
// candidates: 段2 で採った worktree。
// issues: 段3 で取り直した issue。
// fetchFailed: 取り直しそのものに失敗したかどうか。
// m: 段4 の突き合わせの結果。
// result: 復元の記録（閉じた pane と片付けた worktree を積む）。
// 戻り値の1つ目: 引き継ぐと決めた run（段6 で印へ入れる）。
// 戻り値の2つ目: 身元ファイルはあるが pane が無い worktree（段8 で扱う）。
func (o *Orchestrator) decideAdoptions(
	ctx context.Context,
	candidates []restoreCandidate,
	issues map[string]tracker.Issue,
	fetchFailed bool,
	m paneMatch,
	result *RestoreResult,
) ([]adoption, []restoreCandidate) {
	var adoptions []adoption
	var noPane []restoreCandidate

	if m.Unknown {
		// **「一覧を取れなかった」を「pane が無い」と読み替えてはならない。**
		// 段8 は Status を書き換え、`cleanup.on_states` なら worktree まで消す。
		// **pane が無いことを実際に確かめられたときだけ動かす。**
		for _, c := range candidates {
			o.logger.Warn("herdr の一覧を取れなかったので、この worktree は判断を保留します（次の巡回に委ねます）",
				"identifier", c.Identity.IssueIdentifier, "path", c.Path)
		}
		return nil, nil
	}

	for _, c := range candidates {
		pane, alive := m.ByWorktree[c.Path]
		if !alive {
			noPane = append(noPane, c)
			continue
		}
		a, ok := o.decideOne(ctx, c, pane, issues, fetchFailed, m, result)
		if ok {
			adoptions = append(adoptions, a)
		}
	}
	return adoptions, noPane
}

// decideOne は pane が生きている run 1件について引き継ぐかを決める。
//
// **引き継げないと決めたときは、その場で pane を閉じる**（設計 3-4）。
//
// ctx: 呼び出しに適用するコンテキスト。
// c: 対象の worktree と身元ファイル。
// pane: 突き合わせが付いた pane。
// issues: 段3 で取り直した issue。
// fetchFailed: 取り直しそのものに失敗したかどうか。
// m: 段4 の突き合わせの結果（agent 名を引く）。
// result: 復元の記録。
// 戻り値の1つ目: 引き継ぐと決めた run。
// 戻り値の2つ目: 引き継ぐなら true。
func (o *Orchestrator) decideOne(
	ctx context.Context,
	c restoreCandidate,
	pane herdr.Pane,
	issues map[string]tracker.Issue,
	fetchFailed bool,
	m paneMatch,
	result *RestoreResult,
) (adoption, bool) {
	identifier := c.Identity.IssueIdentifier

	// 段3 の分岐: 取り直しそのものに失敗した run は引き継げない。**pane は閉じる。**
	if fetchFailed {
		o.logger.Warn("取り直しに失敗したので引き継ぎません（pane を閉じ、worktree と Status は残します）",
			"identifier", identifier, "pane_id", pane.PaneID)
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}

	issue, found := issues[c.Identity.ProjectItemID]
	if !found {
		// 段5a: 取り直しで見つからなかった → **pane も worktree も残す。**印にも入れない。
		o.logger.Warn("取り直しで見つからなかったので何もしません（pane も worktree も残します）",
			"identifier", identifier, "project_item_id", c.Identity.ProjectItemID)
		return adoption{}, false
	}
	// **取り直した issue が置き場所と同じリポジトリのものかを確かめる**（設計 3-22）。
	// 身元ファイルの `project_item_id` だけを別 issue のものへ差し替えられた場合、
	// ここで止めないと**無関係の issue の pane を閉じ、Status を動かすことになる。**
	if !o.issueAgreesWithPath(c, issue) {
		return adoption{}, false
	}

	switch {
	case o.ws.ShouldCleanup(issue.State):
		// 段5a: cleanup.on_states → pane を閉じてから worktree と branch を片付ける。
		// **terminal_states ではない**（既定値はどちらも Done だが別のキーである）。
		o.logger.Info("Status が cleanup.on_states なので pane を閉じて片付けます",
			"identifier", identifier, "状態", issue.State)
		o.closePaneInto(ctx, pane.PaneID, result)
		o.cleanupInto(ctx, c, issue, result)
		return adoption{}, false
	case containsFold(o.cfg.Tracker.ActiveStates, issue.State):
		// 引き継ぐ側。段5a2 以降へ進む。
	default:
		// 段5a: 引き渡し（In Review / Blocked）→ **pane も worktree も残す。**
		// **Status を巻き戻してはならない。**印にも実行中の一覧にも入れない。
		o.logger.Info("人間へ引き渡した状態なので、pane も worktree も残して何もしません",
			"identifier", identifier, "状態", issue.State)
		return adoption{}, false
	}

	// 3-23: socket のパスが前回と違っていたら引き継がない。**両方のパスをログに出す。**
	if c.Identity.SocketPath != "" && c.Identity.SocketPath != o.socketPath {
		o.logger.Warn(
			"hook を受ける socket のパスが前回と違うので引き継ぎません"+
				"（pane を閉じます。worktree と Status は残すので次の巡回で再 dispatch されます）",
			"identifier", identifier,
			"前回のパス", c.Identity.SocketPath, "今回のパス", o.socketPath)
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}

	// 段5: agent.list から pane_id に対応する agent 名を引く。
	// **agent.prompt / agent.wait の宛先は agent 名である。**pane ID では送れない。
	agent, hasAgent := m.AgentByPane[pane.PaneID]
	if !hasAgent {
		// 段8b: pane はあるが agent 名が無い → pane を閉じ、worktree と Status は残す。
		o.logger.Warn("pane に agent 名が無いので、この Claude Code へはもう送れません（pane を閉じます）",
			"identifier", identifier, "pane_id", pane.PaneID)
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}
	agentName, warnings := normalize.Normalize(agent.Name)
	for _, w := range warnings {
		o.logger.Warn("agent 名の正規化で情報が落ちました", "identifier", identifier, "警告", w.Message)
	}

	// 段5: pane の agent_session から Claude Code のセッション UUID を取る。
	// **無ければ身元ファイルの値を使う**（pane が持っていないことがある。3-18）。
	sessionUUID, ok := pane.SessionUUID()
	if !ok {
		sessionUUID = c.Identity.SessionUUID
	}
	if sessionUUID == "" {
		// hook はどの run のものかを session_id でしか名乗らない（3-2）。
		// **対応づけを復元できないので引き継がない**（段8b と同じ扱い）。
		o.logger.Warn("セッション UUID を取れないので hook の対応づけを復元できません（pane を閉じます）",
			"identifier", identifier, "pane_id", pane.PaneID)
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}

	// 段5a2: **Status だけで決めてはならない。**herdr の agent_status も見る。
	needsPrompt := false
	awaitTurnEnd := false
	switch agent.AgentStatus {
	case herdr.AgentStatusIdle, herdr.AgentStatusDone:
		needsPrompt = true
	case herdr.AgentStatusWorking:
		// **引き継ぐが NeedsPrompt を立てない。**走っている最中に投げると turn が混ざる。
		// 前の turn の Stop は逃がし先か socket から届く。届かなければ stall 検知で拾う。
		// **代わりに「turn の終わりを待つ」を立てる。**立てないと turn ループの goroutine が
		// 1本も起きず、届いた Stop を誰も読まないまま claude.turn_timeout_ms まで放置される
		// （表明もその turn ぶんは一度も読まれない）。
		needsPrompt = false
		awaitTurnEnd = true
	case herdr.AgentStatusBlocked:
		// **引き継がない。**このまま turn を送ると、保留中の権限要求が承認されて実行される
		// （3-11 で実測。3/3）。**esc は送らない**（pane ごと閉じるので要求も消える）。
		o.logger.Warn("権限の確認で止まっているので引き継ぎません（failure_state へ落として pane を閉じます）",
			"identifier", identifier, "pane_id", pane.PaneID)
		o.moveToFailure(ctx, issue,
			"再起動したとき、Claude Code が確認の画面で止まっていました（herdr が返した状態: blocked）。"+
				"**このまま turn を送ると、保留中の権限の要求が承認されて実行されます**"+
				"（実測で3回中3回）。だから引き継がずに人間へ渡しました。"+
				"\n【確かめ方】continuo が pane を閉じたので画面は残っていません。"+
				"worktree の中身（下記）を見て、どこまで進んだかを確かめてください。"+
				"\n【よくある原因】許可されていないコマンドを実行しようとした / フォルダの信頼が切れた。"+
				"\n【対処】許可が要るなら WORKFLOW.md の `claude.permissions.allow` に足してから、"+
				"Status を着手待ちへ戻してください。",
			handoffContext{WorktreePath: c.Path})
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	default:
		// 取れない / 知らない値 → pane を閉じ、worktree と Status を残す（段8b と同じ）。
		o.logger.Warn("agent_status を判断できないので引き継ぎません（pane を閉じます）",
			"identifier", identifier, "agent_status", string(agent.AgentStatus))
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}

	// 段5b: **引き継いだ回数の上限を、turn を送る前に見る。**
	if o.cfg.Agent.MaxTakeover > 0 && c.Identity.TakeoverCount >= o.cfg.Agent.MaxTakeover {
		o.logger.Warn("引き継いだ回数が上限に達したので引き継ぎません（無駄な turn を1回も送りません）",
			"identifier", identifier,
			"takeover_count", c.Identity.TakeoverCount, "max_takeover", o.cfg.Agent.MaxTakeover)
		o.moveToFailure(ctx, issue, fmt.Sprintf(
			"continuo を再起動して同じ issue を引き継いだ回数が、上限の %d 回に達しました。"+
				"**これ以上引き継いでも同じところで落ちる可能性が高い**ので、人間へ渡します。"+
				"\n【確かめ方】worktree の中身（下記）を見て、作業がどこまで進んだかを確かめてください。"+
				"\n【よくある原因】continuo が繰り返し落ちている / Claude Code が起動のたびに失敗している。"+
				"\n【対処】原因を直してから Status を着手待ちへ戻してください。"+
				"引き継ぎの上限は WORKFLOW.md の `agent.max_takeover` で変えられます（いまは %d）。",
			o.cfg.Agent.MaxTakeover, o.cfg.Agent.MaxTakeover),
			handoffContext{WorktreePath: c.Path})
		o.closePaneInto(ctx, pane.PaneID, result)
		return adoption{}, false
	}
	if _, err := o.ws.IncrementTakeover(ctx, c.Path); err != nil {
		// **引き継ぎは続ける。**pane は生きており、閉じると作業の成果を捨てることになる。
		o.logger.Warn("引き継いだ回数を身元ファイルへ書き戻せませんでした（引き継ぎは続けます）",
			"identifier", identifier, "error", err)
	}

	// 段5c: runState を組み立てる。**turn は送らない**（NeedsPrompt を立てるだけ）。
	base, _ := normalize.Normalize("")
	return adoption{
		Issue: issue,
		State: AdoptedRun{
			AgentName:        agentName,
			PaneID:           pane.PaneID,
			SessionUUID:      sessionUUID,
			WorktreePath:     c.Path,
			Base:             base,
			SettingsPath:     c.Identity.SettingsPath,
			HerdrWorkspaceID: c.Identity.HerdrWorkspaceID,
			Revision:         pane.Revision,
			AwaitTurnEnd:     awaitTurnEnd,
		},
		NeedsPrompt: needsPrompt,
		Branch:      c.Identity.Branch,
	}, true
}

// restoreWithoutPane は、身元ファイルがあるのに pane が無い run を扱う（設計 3-4 の段8）。
//
// **この表は段8 専用である。**pane が生きている run は段5〜7 で扱い、ここは通らない。
//
//	cleanup.on_states … worktree と branch を片付ける（restart.orphan_running_action は見ない）
//	active_states     … restart.orphan_running_action の3値で分岐する
//	それ以外          … 何もしない（Status を巻き戻さない）
//	見つからなかった   … 何もしない（ログに出す。勝手に消さない）
//
// ctx: 呼び出しに適用するコンテキスト。
// c: 対象の worktree と身元ファイル。
// issues: 段3 で取り直した issue。
// fetchFailed: 取り直しそのものに失敗したかどうか。
// result: 復元の記録。
func (o *Orchestrator) restoreWithoutPane(
	ctx context.Context,
	c restoreCandidate,
	issues map[string]tracker.Issue,
	fetchFailed bool,
	result *RestoreResult,
) {
	identifier := c.Identity.IssueIdentifier
	if fetchFailed {
		o.logger.Warn("取り直しに失敗したので、pane の無い worktree は次の巡回に委ねます",
			"identifier", identifier, "path", c.Path)
		return
	}
	issue, found := issues[c.Identity.ProjectItemID]
	if !found {
		o.logger.Warn("取り直しで見つからなかったので何もしません（worktree は残します。勝手に消しません）",
			"identifier", identifier, "path", c.Path)
		return
	}
	// **取り直した issue が置き場所と同じリポジトリのものかを確かめる**（設計 3-22）。
	if !o.issueAgreesWithPath(c, issue) {
		return
	}

	switch {
	case o.ws.ShouldCleanup(issue.State):
		o.logger.Info("pane が無く Status が cleanup.on_states なので片付けます",
			"identifier", identifier, "状態", issue.State)
		o.cleanupInto(ctx, c, issue, result)
	case containsFold(o.cfg.Tracker.ActiveStates, issue.State):
		o.applyOrphanRunningAction(ctx, issue, c)
	default:
		o.logger.Info("人間へ引き渡した状態なので、worktree も Status も残して何もしません",
			"identifier", identifier, "状態", issue.State)
	}
}

// applyOrphanRunningAction は `restart.orphan_running_action` の3値で分岐する
// （設計 3-4。**`active_states` のときだけ効く**）。
//
//	redispatch（既定）  … **復元の中では何もしない。**印にも入れず、次の巡回に委ねる
//	to_dispatch_state  … Status を dispatch_state へ戻す
//	to_failure_state   … Status を failure_state へ落として人間に渡す
//
// **`redispatch` で dispatch してはならない。**復元の中で dispatch すると、
// 着手の段11 の待ちで最大1時間止まる。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 取り直した issue。
// c: 対象の worktree と身元ファイル。
func (o *Orchestrator) applyOrphanRunningAction(ctx context.Context, issue tracker.Issue, c restoreCandidate) {
	switch o.cfg.Restart.OrphanRunningAction {
	case "to_dispatch_state":
		o.logger.Info("pane の無い実行中の run の Status を dispatch_state へ戻します",
			"identifier", issue.Identifier, "遷移先", o.cfg.Tracker.DispatchState)
		if _, err := o.tracker.UpdateStatus(
			ctx, issue.ID, o.cfg.Tracker.DispatchState, o.cfg.Tracker.TerminalStates); err != nil {
			o.logger.Warn("Status を戻せません", "identifier", issue.Identifier, "error", err)
		}
	case "to_failure_state":
		o.logger.Info("pane の無い実行中の run を人間へ渡します（worktree は残します）",
			"identifier", issue.Identifier, "遷移先", o.cfg.Tracker.FailureState)
		o.moveToFailure(ctx, issue,
			"continuo を再起動したとき、この issue の Claude Code の pane が残っていませんでした。"+
				"**作業の途中で pane だけが閉じられた**か、herdr ごと落ちたと考えられます。"+
				"\n【確かめ方】worktree の中身（下記）を見て、どこまで進んだかを確かめてください。"+
				"commit が残っていれば作業は無駄になっていません。"+
				"\n【よくある原因】herdr の再起動 / 人間が pane を閉じた。"+
				"\n【対処】続きから進めてよければ Status を着手待ちへ戻してください。"+
				"この振る舞いは WORKFLOW.md の `restart.orphan_running_action` で変えられます"+
				"（いまは to_failure_state。redispatch にすると同じ worktree で自動的に再開します）。",
			handoffContext{WorktreePath: c.Path})
	default:
		// redispatch（既定）。**復元の中では何もしない。**
		o.logger.Info("pane の無い実行中の run は次の巡回に委ねます（復元の中では dispatch しません）",
			"identifier", issue.Identifier, "path", c.Path)
	}
}

// moveToFailure は Status を `failure_state` へ落とし、引き渡しの通知を issue へ書く。
//
// **書く前に必ず ID 指定で取り直す**（`UpdateStatus` が内部で行う）。
// **取り直した結果が `terminal_states` に入っていたら書かない**（エージェントが先に
// `Done` へ動かしていた結果を巻き戻さないため）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
// reason: 人間へ見せる理由。
// hc: 「調べるところ」に出す場所。空の項目は行ごと出さない。
func (o *Orchestrator) moveToFailure(ctx context.Context, issue tracker.Issue, reason string, hc handoffContext) {
	if _, err := o.tracker.UpdateStatus(
		ctx, issue.ID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates); err != nil {
		o.logger.Warn("Status を落とせません",
			"identifier", issue.Identifier, "遷移先", o.cfg.Tracker.FailureState, "error", err)
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		return
	}
	if _, err := o.tracker.PostComment(ctx, nodeID,
		buildHandoffComment(issue.Identifier, reason, hc),
		o.cfg.Tracker.Comments.SelfMarker); err != nil {
		o.logger.Warn("引き渡しの通知を投稿できませんでした", "identifier", issue.Identifier, "error", err)
	}
}

// cleanupInto は worktree と branch を片付け、片付けた記録を result へ積む。
//
// ctx: 呼び出しに適用するコンテキスト。
// c: 対象の worktree と身元ファイル。
// issue: 取り直した issue（コメントの投稿先を引く）。
// result: 復元の記録。
func (o *Orchestrator) cleanupInto(ctx context.Context, c restoreCandidate, issue tracker.Issue, result *RestoreResult) {
	// **base はここでは渡さない。**空で渡すと `Manager.effectiveBase` が
	// 身元ファイルの `base` を読んで補う（着手の段6 で書いてある）。
	// 身元ファイルが古くて `base` が空のときだけ「判定できない」として見送られる。
	var base normalize.SafeName
	if o.cleanupPath(ctx, issue.Identifier, c.Path, base, issueNodeID(issue)) {
		result.Cleaned = append(result.Cleaned, c.Path)
	}
}

// closePane は pane を閉じる。
//
// ctx: 呼び出しに適用するコンテキスト。
// paneID: 閉じる pane の ID。空文字なら何もしない。
// 戻り値: 閉じられたら true。
func (o *Orchestrator) closePane(ctx context.Context, paneID string) bool {
	if paneID == "" {
		return false
	}
	if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: paneID}); err != nil {
		o.logger.Warn("pane を閉じられませんでした", "pane_id", paneID, "error", err)
		return false
	}
	return true
}

// closePaneInto は pane を閉じ、閉じた記録を result へ積む。
//
// ctx: 呼び出しに適用するコンテキスト。
// paneID: 閉じる pane の ID。
// result: 復元の記録。
func (o *Orchestrator) closePaneInto(ctx context.Context, paneID string, result *RestoreResult) {
	if o.closePane(ctx, paneID) {
		result.ClosedPanes = append(result.ClosedPanes, paneID)
	}
}

// resolvePath はパスのシンボリックリンクを解決する（設計 3-4 の段4）。
//
// path: 解決するパス。
// 戻り値の1つ目: 解決済みの絶対パス。
// 戻り値の2つ目: 解決できれば true。**消えた worktree などは false になり、
// 突き合わせの対象から外れる。**
func resolvePath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	return filepath.Clean(resolved), true
}

// isUnder は path が root の内側にあるかを字句だけで判定する。
//
// root: 基準のディレクトリ（解決済み）。
// path: 判定するパス（解決済み）。
// 戻り値: root の内側なら true（root そのものは偽）。
func isUnder(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// containsString は集合に値が含まれるかを返す。
//
// values: 探す先。
// target: 探す値。
// 戻り値: 含まれていれば true。
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// ===== 壊れた worktree の復元と、見つけたときの止まり方（設計 3-49）=====

// handleBrokenWorktrees は、身元ファイルを読めない worktree を手掛かりから復元し、
// 復元できなかったものを `workspace.on_broken_worktree` に従って扱う（設計 3-49）。
//
// **復元より先に、消すことは一度も考えない。**壊れた worktree の中には、まだ push して
// いない成果が残っていることがある。**continuo は1バイトも消さない。**
//
// **復元を先に試す理由。**着手は worktree を作ってから身元ファイルを書く（3-16 の段6〜段9）
// ので、**その間で落ちると身元ファイルの無い worktree ができる。**それは「壊れた」のでは
// なく「書き終える前に落ちた」だけであり、置き場所とボードから元どおりに組み立て直せる。
//
// **復元できなかったものは、既定では起動を止める。**飛ばして走り続けると、その issue は
// ボードの上で running_state のまま誰にも触られず、**人間が気づくのは何時間も後になる。**
// 止まれば、被害はその時点で止まり、壊れていることをすぐ知れる。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: `workspace.on_broken_worktree` が `stop` で、復元できない worktree が
// 1件でもあった場合のエラー。**それ以外は nil である**（置き場所を走査できない場合も
// 警告を出して起動を続ける）。
func (o *Orchestrator) handleBrokenWorktrees(ctx context.Context) error {
	broken, err := o.ws.ScanBroken()
	if err != nil {
		o.logger.Warn("置き場所を走査できないので、壊れた worktree の検査を行いません（起動は続けます）",
			"error", err)
		return nil
	}
	if len(broken) == 0 {
		return nil
	}

	// **pane を引くのは、壊れた worktree が1件でもあったときだけである。**
	// herdr への呼び出しを1回増やすので、平常時は1回も呼ばない。
	panes, agents := o.panesByCwd(ctx)

	var stillBroken []workspace.BrokenWorktree
	for _, b := range broken {
		if o.recoverIdentity(ctx, b, panes, agents) {
			continue
		}
		stillBroken = append(stillBroken, b)
	}
	if len(stillBroken) == 0 {
		return nil
	}

	// **何が起きているかと、次に何をすべきかを、必ず両方出す**（設計 3-49）。
	identityFile := o.ws.IdentityFileName()
	summary := make([]string, 0, len(stillBroken))
	for _, b := range stillBroken {
		o.logger.Error("身元を確かめられない worktree があります（continuo は消しません）",
			"path", b.Path, "何が起きているか", b.What(identityFile))
		for _, step := range b.NextSteps() {
			o.logger.Error("次にこれをしてください", "path", b.Path, "手順", step)
		}
		summary = append(summary, b.Path)
	}

	if o.cfg.Workspace.OnBrokenWorktree == config.OnBrokenWorktreeSkip {
		o.logger.Warn("workspace.on_broken_worktree が skip なので、壊れた worktree を飛ばして起動を続けます",
			"count", len(stillBroken), "paths", summary)
		return nil
	}
	// 止める側。**何が壊れているか・どの worktree か・次に何をすべきかを出してから終わる。**
	first := stillBroken[0]
	return i18n.Errorf(i18n.KeyOrchestratorRestoreBrokenWorktreeStop,
		len(stillBroken),
		strings.Join(summary, "\n  "),
		first.What(identityFile),
		strings.Join(first.NextSteps(), "\n  "))
}

// panesByCwd は herdr の pane を、解決済みの cwd から引ける形にする（設計 3-49）。
//
// **段4 の matchPanes とは別に引く。**あちらは身元ファイルを読めた worktree だけを
// 相手にするので、**まさに読めなかった worktree の pane が入らない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 解決済みの cwd から pane を引く写像。**引けなければ空である。**
// 戻り値の2つ目: pane の ID から agent を引く写像。**引けなければ空である。**
func (o *Orchestrator) panesByCwd(ctx context.Context) (map[string]herdr.Pane, map[string]herdr.Agent) {
	byCwd := map[string]herdr.Pane{}
	byPane := map[string]herdr.Agent{}

	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		o.logger.Warn("pane の一覧を取れないので、pane の label を復元の手掛かりに使いません", "error", err)
		return byCwd, byPane
	}
	for _, pane := range list.Panes {
		if pane.Cwd == "" {
			continue
		}
		resolved, ok := resolvePath(pane.Cwd)
		if !ok {
			continue
		}
		if _, dup := byCwd[resolved]; dup {
			// **同じ cwd に pane が2つあるなら、どちらの label を信じてよいか決められない。**
			// 手掛かりとして使わない（置き場所からの切り出しは残る）。
			o.logger.Warn("同じ cwd に pane が2つあるので、label を復元の手掛かりに使いません",
				"cwd", pane.Cwd)
			byCwd[resolved] = herdr.Pane{}
			continue
		}
		byCwd[resolved] = pane
	}

	agents, err := o.herdr.AgentList(ctx)
	if err != nil {
		o.logger.Warn("agent の一覧を取れないので、agent 名を復元の手掛かりに使いません", "error", err)
		return byCwd, byPane
	}
	for _, a := range agents.Agents {
		if a.PaneID == "" || a.Name == "" {
			continue
		}
		byPane[a.PaneID] = a
	}
	return byCwd, byPane
}

// recoverIdentity は、身元ファイルを読めない worktree の身元ファイルを書き直す（設計 3-49）。
//
// **手掛かりは3つある。**
//
//	置き場所のパス  … `<root>/<host>/<owner>/<repo>/<スラグ>` の固定4階層。スラグに issue の番号が入る
//	pane の label   … `owner/repo/issues/N`（設計 3-3）。スラグから切り出せなかったときに使う
//	ボードの issue  … 上の2つで組み立てた `<owner>/<repo>#<番号>` で1件だけ引き直す
//
// **どの手掛かりも、そのままでは信じない。**引き直した issue から**スラグを作り直し、
// 目の前のディレクトリ名と一致すること**を確かめる（ExpectedSlugFor）。ここを外すと、
// **pane の label を書き換えるだけで、別の issue の worktree として復元させられる**
// （label は herdr の CLI から誰でも書き換えられる。continuo だけのものではない）。
//
// **base は復元しない。**worktree を作ったときの base は、どの手掛かりにも残っていない。
// 空のままにすると片付けは「判定できない」として見送る（3-9 の手順2b）ので、
// **消す側へ倒れることは無い。**
//
// ctx: 呼び出しに適用するコンテキスト。
// b: 身元を確かめられない worktree。
// panes: 解決済みの cwd から引く pane。
// agents: pane の ID から引く agent。
// 戻り値: 身元ファイルを書き直せたら true。
func (o *Orchestrator) recoverIdentity(
	ctx context.Context,
	b workspace.BrokenWorktree,
	panes map[string]herdr.Pane,
	agents map[string]herdr.Agent,
) bool {
	if b.Clue == nil {
		o.logger.Warn("置き場所の4階層に合わないので復元できません（消しません）", "path", b.Path)
		return false
	}
	pane := panes[resolveOrCleanPath(b.Path)]

	for _, number := range recoveryNumbers(b, pane) {
		identifier := fmt.Sprintf("%s/%s#%d", b.Clue.Owner, b.Clue.Repo, number)
		issue, found, err := o.tracker.FetchIssueByIdentifier(ctx, identifier)
		if err != nil {
			o.logger.Warn("復元のために issue を引けませんでした（消しません）",
				"path", b.Path, "identifier", identifier, "error", err)
			continue
		}
		if !found {
			o.logger.Warn("復元のために引いた issue がボードにありません（消しません）",
				"path", b.Path, "identifier", identifier)
			continue
		}
		if !o.slugAgrees(b, issue) {
			continue
		}
		if o.writeRecoveredIdentity(ctx, b, issue, pane, agents) {
			return true
		}
		return false
	}
	o.logger.Warn("手掛かりから issue を確かめられないので復元できません（消しません）",
		"path", b.Path, "置き場所", b.Clue.Owner+"/"+b.Clue.Repo, "スラグ", b.Clue.Slug)
	return false
}

// writeRecoveredIdentity は、裏の取れた issue から身元ファイルを組み立てて書く（設計 3-49）。
//
// **書くのは、置き場所とボードと pane から確かめられたものだけである。**
// `base` と `settings_path` は復元しない（どの手掛かりにも残っていない）。
// **takeover_count は 0 から数え直す。**引き継いだ回数は身元ファイルにしか無く、
// それが読めなかったのだから、**復元した値を推測で埋めてはならない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// b: 身元を確かめられない worktree。
// issue: 裏の取れた issue。
// pane: その worktree を cwd に持つ pane（無ければゼロ値）。
// agents: pane の ID から引く agent。
// 戻り値: 書けたら true。
func (o *Orchestrator) writeRecoveredIdentity(
	ctx context.Context,
	b workspace.BrokenWorktree,
	issue tracker.Issue,
	pane herdr.Pane,
	agents map[string]herdr.Agent,
) bool {
	branch, warnings, err := workspace.RenderBranch(o.cfg.Herdr.Worktree.BranchTemplate, toIssueRef(issue))
	if err != nil {
		o.logger.Warn("復元のために branch 名を組み立てられませんでした（消しません）",
			"path", b.Path, "identifier", issue.Identifier, "error", err)
		return false
	}
	for _, w := range warnings {
		o.logger.Warn("復元で組み立てた branch 名の正規化で情報が落ちました", "warning", w.Message)
	}

	identity := workspace.Identity{
		IssueURL:        issueURL(issue),
		IssueIdentifier: issue.Identifier,
		ProjectItemID:   issue.ID,
		Branch:          branch.String(),
		CreatedAt:       o.now(),
	}
	if pane.PaneID != "" {
		// **pane から取れるものは取る。**取れなくても復元は成立する
		// （herdr workspace の ID が無ければ、片付けは worktree の登録から引き直す）。
		identity.HerdrWorkspaceID = pane.WorkspaceID
		if uuid, ok := pane.SessionUUID(); ok {
			identity.SessionUUID = uuid
		}
		if agent, ok := agents[pane.PaneID]; ok {
			identity.AgentName = agent.Name
		}
	}
	// **書き込む直前に封じ込め検査を通す**（設計 3-20）。走査は `os.ReadDir` で
	// 名前をたどるだけなので、置き場所の4階層目が外を指すシンボリックリンクだと、
	// **置き場所の外側へ身元ファイルを書く。**読むだけの走査と違い、ここは書き込みである。
	resolved, err := workspace.CheckContainmentResolved(o.ws.ResolvedRoot(), b.Path)
	if err != nil {
		o.logger.Warn("復元先が置き場所の内側だと確かめられないので身元ファイルを書きません（消しません）",
			"path", b.Path, "error", err)
		return false
	}
	if err := o.ws.WriteIdentity(ctx, resolved, identity); err != nil {
		o.logger.Warn("復元した身元ファイルを書けませんでした（消しません）",
			"path", b.Path, "identifier", issue.Identifier, "error", err)
		return false
	}
	o.logger.Info("身元ファイルを復元しました",
		"path", b.Path, "identifier", issue.Identifier, "branch", identity.Branch,
		"herdr_workspace_id", identity.HerdrWorkspaceID, "agent", identity.AgentName)
	return true
}

// recoveryNumbers は復元で試す issue の番号を、手掛かりの強い順に並べる（設計 3-49）。
//
// **置き場所のパスが先である。**パスは封じ込め検査（3-20）を通っており、
// エージェントには書き換えられない。pane の label は herdr の CLI から書き換えられる
// ので、**パスから切り出せなかったときの補いとしてだけ使う。**
//
// b: 身元を確かめられない worktree。
// pane: その worktree を cwd に持つ pane（無ければゼロ値）。
// 戻り値: 試す番号（重複は除く）。
func recoveryNumbers(b workspace.BrokenWorktree, pane herdr.Pane) []int {
	var numbers []int
	if b.Clue != nil && b.Clue.Number > 0 {
		numbers = append(numbers, b.Clue.Number)
	}
	if n, ok := issueNumberFromPaneLabel(pane.Label); ok && !containsInt(numbers, n) {
		numbers = append(numbers, n)
	}
	return numbers
}

// slugAgrees は、引き直した issue から作り直したスラグが、目の前のディレクトリ名と
// 一致するかを返す（設計 3-49）。
//
// **これが復元の最後の関門である。**手掛かり（置き場所の番号・pane の label）は
// どちらも「候補を出す」だけの役目であり、**正しいかどうかはここでしか確かめない。**
//
// b: 身元を確かめられない worktree。
// issue: 引き直した issue。
// 戻り値: 一致すれば true。
func (o *Orchestrator) slugAgrees(b workspace.BrokenWorktree, issue tracker.Issue) bool {
	if !strings.EqualFold(issue.Owner, b.Clue.Owner) || !strings.EqualFold(issue.Repo, b.Clue.Repo) {
		o.logger.Warn("引き直した issue が置き場所と違うリポジトリなので復元しません（消しません）",
			"path", b.Path, "置き場所", b.Clue.Owner+"/"+b.Clue.Repo, "引き直した issue", issue.Identifier)
		return false
	}
	slug, err := o.ws.ExpectedSlugFor(toIssueRef(issue))
	if err != nil {
		o.logger.Warn("引き直した issue のスラグを組み立てられないので復元しません（消しません）",
			"path", b.Path, "identifier", issue.Identifier, "error", err)
		return false
	}
	if slug != b.Clue.Slug {
		o.logger.Warn("引き直した issue のスラグが置き場所のディレクトリ名と違うので復元しません（消しません）",
			"path", b.Path, "置き場所のディレクトリ名", b.Clue.Slug, "issue から作ったスラグ", slug,
			"identifier", issue.Identifier)
		return false
	}
	return true
}

// issueNumberFromPaneLabel は pane の label（`owner/repo/issues/N`。設計 3-3）から
// issue の番号を取り出す。
//
// **herdr.IssueLabel の逆である。**label は herdr の CLI から誰でも書き換えられるので、
// **ここで取れた番号は候補にすぎない**（slugAgrees が裏を取る）。
//
// label: pane の label。
// 戻り値の1つ目: issue の番号。
// 戻り値の2つ目: 形が合って正の整数として読めたら true。
func issueNumberFromPaneLabel(label string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(label), "/")
	if len(parts) != 4 || parts[2] != "issues" {
		return 0, false
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

// containsInt は整数の並びに値が入っているかを返す。
//
// values: 探す先。
// target: 探す値。
// 戻り値: 入っていれば true。
func containsInt(values []int, target int) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// resolveOrCleanPath はシンボリックリンクを解決した絶対パスを返す。解決できなければ Clean する。
//
// **pane の cwd と worktree のパスを同じ土俵で比べるために要る**（panesByCwd は
// 解決済みの cwd を鍵にしている）。
//
// path: 対象のパス。
// 戻り値: 比較に使えるパス。
func resolveOrCleanPath(path string) string {
	if resolved, ok := resolvePath(path); ok {
		return resolved
	}
	return filepath.Clean(path)
}
