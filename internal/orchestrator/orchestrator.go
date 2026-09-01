// Package orchestrator は continuo の中心である。ボードを巡回して issue を dispatch し、
// run ごとに turn ループを回し、実行中の Status と worktree を照合して片付ける
// （docs/plans/continuo_design.md 3-4 / 3-5 / 3-8 / 3-16 / 3-21 / 3-25 / 3-27）。
//
// このパッケージが持つ状態は「実行中の一覧」1本だけである。
//
//	runs map[string]*runState   キーは project item の ID
//
// **これが「自分が取った」印であり、同時に「実行中の一覧」でもある**（設計 3-25）。
// 2つの集合を持たない。ディスクにもボードにも書かない（設計 3-4）。
//
// 巡回のループがやることは3つだけである（設計 3-8）。
//
//	候補を取る          … 空きスロットが尽きるまで dispatch する（着手の13段。3-16）
//	turn を送らせる      … NeedsPrompt が立った run の goroutine を起こす。**ブロックしない**
//	照合と片付け        … 実行中の Status を取り直し、worktree を照合する（3-9）
//
// **turn ループは run ごとの goroutine で動かす。**`agent.prompt` を wait つきで呼ぶと
// turn の終わりまで返らない（既定1時間）ので、巡回のループの中で同期的に呼んではならない。
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// hook のイベント名である（設計 1-4 / 3-2）。
const (
	hookStop             = "Stop"
	hookUserPromptSubmit = "UserPromptSubmit"
	hookSubagentStop     = "SubagentStop"
	hookSubagentStart    = "SubagentStart"
	hookNotification     = "Notification"
	hookSessionStart     = "SessionStart"
	hookPreToolUse       = "PreToolUse"
	hookPostToolUse      = "PostToolUse"
)

// taskNotificationPrefix は Claude Code が自分自身に投入するプロンプトの目印である
// （設計 1-3 / 1-7）。**これで始まる UserPromptSubmit が来たら turn は続いている。**
const taskNotificationPrefix = "<task-notification>"

// transcript を読むときの待ちである（設計 3-25）。
const (
	// transcriptFirstWait は turn が終わってから transcript を読むまでの待ちである。
	// **`Stop` hook が走る時点では、その turn の最後の text ブロックがまだ書かれていない**
	// （13件すべてで未書き込み）。0.11 秒までには解消していたが、上限を測れていないので
	// 余裕を取って 0.5 秒にする。
	transcriptFirstWait = 500 * time.Millisecond
	// transcriptRetryWait は読み直しの間隔である。
	transcriptRetryWait = 100 * time.Millisecond
	// transcriptRetryCount は読み直しの回数の上限である。
	// **5回で諦める**（待つ合計は 0.5 + 0.5 = 1.0 秒）。それでも無ければ「表明なし」として
	// 扱い、次の turn で促す。
	transcriptRetryCount = 5
)

// unknownHostName は `os.Hostname()` が答えなかったときに入札へ書く名前である（設計 3-77）。
//
// **空文字にしない。**空だと入札の JSON の `host` が空になり、
// **勝った機械の名前が issue から読めなくなる。**
const unknownHostName = "unknown-host"

// baseRetryBackoff はリトライの指数バックオフの初項である（設計 3-21）。
// 上限は agent.max_retry_backoff_ms である。
const baseRetryBackoff = 5 * time.Second

// Tracker は orchestrator が使うトラッカーの操作である。
//
// ***tracker.Adapter がこれを満たす。**インタフェースにしてあるのは、巡回1回の
// リクエスト本数（設計 3-31 の3本）をテストから数えられるようにするためである。
type Tracker interface {
	// FetchIssuesByStates は候補を並び順のまま取る（設計 4-2）。
	FetchIssuesByStates(ctx context.Context, states []string) ([]tracker.Issue, error)
	// FetchIssuesByIDs は ID 指定で取り直す。
	// **「いまの Status を書いたのは誰か」（timeline）も一緒に取る**（設計 3-54）。
	FetchIssuesByIDs(ctx context.Context, ids []string) ([]tracker.Issue, error)
	// FetchIssuesByIDsWithoutTimeline は ID 指定で取り直すが、
	// **「いまの Status を書いたのは誰か」は取らない**（設計 3-61）。
	//
	// **どちらを呼ぶかは、その呼び出し元が `Issue.StatusChangedBy` /
	// `Issue.StatusChangedByAutomation` を読むかどうかだけで決まる。**
	// **6つの呼び出し元の内訳は次のとおりである。呼び出し元を足すときは、この表も足すこと。**
	//
	//	読む     reconcileRunning       知らない Status を書き戻すか止めるかを決める（reconcile.go）
	//	読む     handleTurnEnd          同上を turn の終わりに決める（refreshIssue に真を渡す。lifecycle.go）
	//	読まない finishRunClaimed       片付けてよいかを Status だけで決める（refreshIssue に偽を渡す。lifecycle.go）
	//	読まない reconcileWorktrees     取り残された worktree を片付けてよいかを Status だけで決める（reconcile.go）
	//	読まない dispatchStatusAllowed  着手してよいかを Status だけで決める（dispatch.go）
	//	読まない refetchByIdentities    復元のときに Status と識別子だけを見る（restore.go）
	//
	// **「読まない」に timeline を渡さなくてよいのは、判断が要る場所へ届く写しが
	// 必ず「読む」側の取り直しで上書きされるからである。**知らない Status の判断
	// （`handleUnknownState` / `finishRunUnknownState`）へ入る直前には、
	// `reconcileRunning` か `handleTurnEnd` の `rs.setIssue` が必ず通っている。
	FetchIssuesByIDsWithoutTimeline(ctx context.Context, ids []string) ([]tracker.Issue, error)
	// FetchIssueByIdentifier は `<owner>/<repo>#<番号>` で1件引く（設計 3-25）。
	// **見つからないことをエラーにしない。**
	FetchIssueByIdentifier(ctx context.Context, identifier string) (tracker.Issue, bool, error)
	// UpdateStatus は Status を書き換える。**書く前に必ず ID 指定で取り直す**（設計 3-4）。
	// **戻り値の Previous がその取り直した値である。**「何から動かしたか」を issue へ
	// 書くのはこの値であって、巡回で読んだ値ではない（設計 3-29）。
	UpdateStatus(ctx context.Context, itemID, targetState string, blockedStates []string) (tracker.StatusWrite, error)
	// FetchComments は issue のコメントを取る（エージェントが書いたかの判別に使う）。
	// **selfLogin は continuo が使う gh の持ち主である**（設計 3-65）。印と併せて投稿者を
	// 見るために渡す。**空文字なら投稿者を照合せず、印だけで判別する。**
	FetchComments(ctx context.Context, issueNodeID string, cfg config.TrackerProviderCommentsConfig, markers config.TrackerCommentsConfig, selfLogin string) ([]tracker.Comment, error)
	// PostComment は continuo 自身のコメントを書く。
	// **書くのは引き渡しの通知と、Status を動かした記録の2つだけである**（設計 3-29）。
	PostComment(ctx context.Context, issueNodeID, body, selfMarker string) (*tracker.Comment, error)
	// FetchAllComments は issue のコメントを1件残らず取る（設計 3-77a）。
	// **持ち回りの印が付いたコメントも落とさない。**担当の持ち回りの判定はこれを読む。
	FetchAllComments(ctx context.Context, issueNodeID string, cfg config.TrackerProviderCommentsConfig) ([]tracker.Comment, error)
	// FetchViewer は、いま使っているトークンの持ち主を返す（設計 3-77b）。
	// **担当者を書き足すにはノード ID が要る。**
	FetchViewer(ctx context.Context) (tracker.Assignee, error)
	// AddAssignees は issue に担当者を書き足す（設計 3-77b）。**置き換えではない。**
	AddAssignees(ctx context.Context, issueNodeID string, assigneeIDs []string) ([]tracker.Assignee, error)
	// RemoveAssignees は issue から担当者を外す（設計 3-77c）。**名指しした1人だけを外す。**
	RemoveAssignees(ctx context.Context, issueNodeID string, assigneeIDs []string) ([]tracker.Assignee, error)
	// VerifyStatusOptions は Status の選択肢名がまだ設定と一致するかを検査し直す（設計 3-6）。
	VerifyStatusOptions(ctx context.Context, cfg config.TrackerConfig) error
}

// HerdrClient は orchestrator が使う herdr の socket API の部分集合である。
//
// ***herdr.Client がこれを満たす。**
// **`pane.split` と `tab.create` は含めない**（設計 4-5。1 worktree = 1 workspace）。
// `worktree.open` が作った workspace の pane を `pane.list` で引いて使う。
type HerdrClient interface {
	// PaneList は workspace の pane を引く（設計 3-16 の段8）。
	PaneList(ctx context.Context, params herdr.PaneListParams) (*herdr.PaneListResult, error)
	// WorktreeOpen は既にある worktree を workspace として開く。
	// **コメントを書かせ直すときの復元でだけ使う**（設計 3-25 の9段の段4）。
	// 着手のときは workspace の Manager が開く。
	WorktreeOpen(ctx context.Context, params herdr.WorktreeOpenParams) (*herdr.WorktreeOpenResult, error)
	// PaneRename は pane の label に `owner/repo/issues/N` を書く（設計 3-3）。
	// **人間が herdr の画面で pane を見分けるための表示名である。**continuo は読み戻さない。
	PaneRename(ctx context.Context, params herdr.PaneRenameParams) (*herdr.PaneRenameResult, error)
	// PaneClose は pane を閉じる。**worker を止める唯一の手段である**（設計 3-5）。
	PaneClose(ctx context.Context, params herdr.PaneCloseParams) (*herdr.PaneCloseResult, error)
	// AgentStartWithRetry は agent を起動する（agent_pane_busy をリトライする。設計 2-1）。
	AgentStartWithRetry(ctx context.Context, params herdr.AgentStartParams, budget, delay time.Duration) (*herdr.AgentStartResult, error)
	// AgentPrompt はプロンプトを送る。**wait つきで送る**（設計 3-2）。
	AgentPrompt(ctx context.Context, params herdr.AgentPromptParams) (*herdr.AgentPromptResult, error)
	// AgentWait は agent の状態が落ち着くまで待つ。**単独では turn の終わりを待てない**（設計 3-2）。
	AgentWait(ctx context.Context, params herdr.AgentWaitParams) (*herdr.AgentWaitResult, error)
	// AgentGet は agent の状態を読む（stall の判定と起動の確認）。
	AgentGet(ctx context.Context, params herdr.AgentGetParams) (*herdr.AgentGetResult, error)
	// AgentList は agent の一覧を読む（agent 名の重複の検査）。
	AgentList(ctx context.Context) (*herdr.AgentListResult, error)
	// AgentSendKeys は agent にキーを送る。**blocked のとき `["esc"]` を送る**（設計 3-11）。
	AgentSendKeys(ctx context.Context, params herdr.AgentSendKeysParams) error
}

// **本番の実装がこの2つのインタフェースを満たすことを、コンパイル時に確かめる。**
//
// **テストはテスト用トラッカー mockと stub しか渡さない。**`cmd/continuo` から本物の型を渡す
// 経路は第7段階で作るので、この表明が無いと、それまでシグネチャがずれても誰も気づかない。
var (
	_ Tracker     = (*tracker.Adapter)(nil)
	_ HerdrClient = (*herdr.Client)(nil)
)

// GHAuthCheckFunc は `gh` の認証がまだ有効かを検査する関数である（設計 3-6）。
//
// **毎巡回では呼ばない。**`tracker.verify_states_every`（既定20巡回に1回）の頻度で呼ぶ。
// **毎巡回で外部プロセスを起動しない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: 認証が有効なら nil。失敗したらその巡回の dispatch を飛ばす（実行中の照合は止めない）。
type GHAuthCheckFunc func(ctx context.Context) error

// ghLoginTimeout は「gh の持ち主」を取る外部プロセスに掛ける期限である（設計 3-65）。
//
// **取れなくても起動は止めない**ので、長く待つ意味が無い。
const ghLoginTimeout = 10 * time.Second

// ghLoginRetryInterval は、持ち主を取れなかったあと次に取り直すまでの間隔である（設計 3-65）。
//
// **取れるまで取り直す。**1回で諦めると、`gh api` に一度届かなかっただけで、
// **プロセスが生きている間ずっと印だけの判定に戻る。**常駐して何日も経つと、
// そのことに気づく手掛かりが起動直後の1行しか残らない。
//
// **毎巡回では取り直さない**（既定の巡回は30秒に1回）。取り直しは外部プロセスの起動であり、
// 期限（ghLoginTimeout）ぶん巡回そのものを遅らせる。
const ghLoginRetryInterval = 5 * time.Minute

// Options は Orchestrator を組み立てるための入力である。
type Options struct {
	// Config は WORKFLOW.md の front matter である。
	Config config.Config
	// PromptTemplate は WORKFLOW.md の本文（1回目のプロンプトのテンプレート。設計 5-3）である。
	PromptTemplate string
	// Tracker はボードの読み書きである。必須。
	Tracker Tracker
	// Herdr は herdr の socket API のクライアントである。必須。
	Herdr HerdrClient
	// Workspace は worktree の用意と片付けである。必須。
	Workspace *workspace.Manager
	// RateLimit は枠の読み取りである。nil なら枠の判定を行わない（`none` と同じ動き）。
	RateLimit *ratelimit.Reader
	// HookSocketPath は hook を受ける socket の絶対パスである（設定ファイルに埋め込む）。必須。
	HookSocketPath string
	// ContinuoPath は `continuo hook` を起動する実行ファイルの絶対パスである。
	// 空なら os.Executable() の結果を使う。
	ContinuoPath string
	// Logger はログの出力先である。nil なら slog.Default() を使う。
	Logger *slog.Logger
	// Now は現在時刻を返す関数である。nil なら time.Now を使う。
	Now func() time.Time
	// NewSessionUUID は Claude Code のセッション UUID を採番する関数である。
	// nil なら crypto/rand で v4 の UUID を作る。**使い回してはならない**（設計 3-3）。
	NewSessionUUID func() (string, error)
	// GHAuthCheck は `gh` の認証の検査である。nil なら検査しない。
	GHAuthCheck GHAuthCheckFunc
	// GHLogin は「continuo が使う gh の持ち主」を取る関数である（設計 3-65）。
	//
	// **nil なら `gh api user --jq .login`（tracker.RunGHAPIUserLogin）を使う。**
	// **テストは偽の関数を渡して外部プロセスの起動を避けること。**
	GHLogin tracker.GHLoginFunc
	// HostName はこの機械の名前である（設計 3-77）。**入札と hold のコメントに書く。**
	//
	// **空なら `os.Hostname()` の結果を使う。**テストは固定の名前を渡して、
	// 走らせる機械によって結果が変わらないようにすること。
	HostName string
	// TranscriptRoot は hook が渡す `transcript_path` を受け入れる根である。
	//
	// **空なら `~/.claude/projects` を使う**（Claude Code が transcript を書く場所。設計 3-15）。
	// **この外を指す `transcript_path` は捨てる。**hook の中身はエージェントが
	// 書き換えられる外部入力であり、別の run の transcript や任意のファイルを
	// 読ませる経路になる。テストが一時ディレクトリを渡せるようにしてある。
	TranscriptRoot string
}

// Orchestrator は巡回・dispatch・turn ループ・照合・リトライ・stall 検知を持つ。
type Orchestrator struct {
	cfg            config.Config
	promptTemplate string
	tracker        Tracker
	herdr          HerdrClient
	ws             *workspace.Manager
	rl             *ratelimit.Reader
	socketPath     string
	runtimeDir     string
	continuoPath   string
	transcriptRoot string
	logger         *slog.Logger
	now            func() time.Time
	newSessionUUID func() (string, error)
	ghAuthCheck    GHAuthCheckFunc
	// ghLogin は「continuo が使う gh の持ち主」を取る関数である（設計 3-65）。
	ghLogin tracker.GHLoginFunc
	// ghLoginAttemptMu は取得そのものを1本に絞る。**外部プロセスを同時に何本も起こさない。**
	//
	// **状態を守る `ghLoginMu` と分ける。**取得は外部プロセスの実行なので
	// 最大 ghLoginTimeout だけ掛かる。同じ錠で状態も守ると、その間ずっと
	// 「取れているか」を読む側（`ghLoginName`）まで待たされる。
	ghLoginAttemptMu sync.Mutex
	// ghLoginMu は selfLogin と取得の失敗の記録を守る。
	//
	// **`mu`（runs / sessions を守るもの）と分ける。**持ち主は turn ループの goroutine から
	// 読むので、`mu` を持ったまま入る経路と重なると相互待ちになりうる。
	ghLoginMu sync.Mutex
	// selfLogin は取れた持ち主のログイン名である。**空文字は「取れていない」を表し、
	// そのときは印だけで判定する形に落ちる**（設計 3-65）。
	selfLogin string
	// ghLoginFailures は持ち主を取れなかった回数である（取れた時点で数えるのをやめる）。
	// **失敗が続いていることをログに書くために持つ。**
	ghLoginFailures int
	// ghLoginFirstFailedAt は最初に取れなかった時刻である。ゼロ値なら1回も失敗していない。
	ghLoginFirstFailedAt time.Time
	// ghLoginLastTriedAt は最後に取得を試みた時刻である。
	// **ghLoginRetryInterval を空けてから取り直すために持つ。**
	ghLoginLastTriedAt time.Time
	// knownStateNames は continuo が意味を知っている Status 名の一覧である（設計 3-50）。
	//
	// **設定から作る値であり、走っている間は変わらない。**巡回のたびに作り直すと、
	// 実行中の run 1件ごとに確保と整列をやり直すことになる（`reconcileRunning` は
	// run ごとに `isKnownState` を引く）。**組み立てのときに1度だけ計算して持つ。**
	knownStateNames []string
	// hostName はこの機械の名前である（設計 3-77）。入札と hold のコメントに書く。
	hostName string

	// mu は runs / sessions / notified / tickCount / quota を守る。
	mu sync.Mutex
	// runs は「自分が取った」印であり「実行中の一覧」でもある（設計 3-10 / 3-25）。
	// キーは project item の ID。
	runs map[string]*runState
	// sessions は Claude Code のセッション UUID から run を引く索引である。
	// hook はどの run のものかを session_id でしか名乗らない（設計 3-2）。
	sessions map[string]*runState
	// notified は未信頼のリポジトリへコメントした時刻である（設計 3-6）。
	// **キーは `<owner>/<repo>` である。**issue ごとではない。
	// 素朴に issue ごとにすると、30秒ごとに永久にコメントが積まれる。
	notified map[string]time.Time
	// labelSkipped は、必須のラベルが足りずに飛ばしたことを知らせ済みの issue である
	// （issue #134。キーは project item の ID）。
	//
	// **ログを1回だけにするためだけに持つ。**判定には1度も使わない。
	labelSkipped map[string]struct{}
	// failures は issue（project item の ID）ごとの失敗の記録である。
	//
	// **印（runs）の外に置く。**印は run が終わると消えるので、そこに数えていると
	// 「同じ理由で必ず失敗する issue」を次の巡回が0回目として拾い直してしまう。
	// **永続化はしない**（再起動したら数え直す。設計の方針）。
	failures map[string]*failureNote
	// tickCount は巡回した回数である（verify_states_every の判定に使う）。
	tickCount int
	// quota は最後に読んだ枠の状態である。nil なら読めていない。
	quota *ratelimit.Snapshot
	// quotaFetchedAt は枠を最後に読んだ時刻である（poll_interval_ms の判定に使う）。
	quotaFetchedAt time.Time
	// quotaStale は、最後に試した枠の読み取りが失敗したかである（設計 3-77）。
	//
	// **失敗したら写しを使わせない。**資格情報が切れた機械は、切れる直前の
	// 「使用率 5%」を1日中返し続ける。**入札はそれを「いちばん暇な機械」と読み、
	// 正直に読めている機械に必ず勝つ。**
	// **「読めなかったら入札しない」を、初回だけでなく常に効かせる。**
	quotaStale bool
	// viewer はいま使っているトークンの持ち主である（設計 3-77b）。
	//
	// **一度取れたら取り直さない。**持ち主が変わるのは `gh auth switch` を人間が
	// 叩いたときだけで、その操作は continuo を止めずに行うものではない。
	viewer tracker.Assignee
	// viewerLastTriedAt は、持ち主を最後に取りに行った時刻である（設計 3-77b）。
	//
	// **取れなかったときに `ghLoginRetryInterval` を空けるために持つ。**
	// 空けないと、`gh` が落ちているあいだ**巡回の候補ごとに GraphQL を1本ずつ投げる。**
	viewerLastTriedAt time.Time
	// handoffFetches は、いまの巡回で持ち回りのためにコメントを読んだ issue の数である
	// （設計 3-77a）。**巡回の頭で 0 に戻す。**
	handoffFetches int

	// wg は run ごとの turn ループの goroutine を数える。Close が待ち合わせる。
	wg sync.WaitGroup
	// shutdown は Close が閉じるコンテキストである。
	//
	// **turn ループの待ちには期限が無い**（`claude.turn_timeout_ms` は turn の総実行時間の
	// 上限ではない。設計 3-21）。呼び出し側の ctx が終わらないまま Close だけを呼ばれると、
	// `Stop` hook を待っている goroutine が永久に返らず、Close も返らない。
	// **turn ループの ctx はこれと呼び出し側の ctx の両方で終わる。**
	shutdown context.Context
	// shutdownCancel は shutdown を終わらせる。Close が呼ぶ。
	shutdownCancel context.CancelFunc
}

// New は Orchestrator を組み立てる。
//
// opts: 設定・トラッカー・herdr・workspace・枠の読み取り・socket のパス・ログ。
// 戻り値: 組み立てた Orchestrator。Tracker / Herdr / Workspace が nil の場合、
// **Config に Status 名が1つも無い場合**、HookSocketPath が空または絶対パスでない場合、
// `continuo` の実行ファイルの場所を決められない場合はエラーを返す。
func New(opts Options) (*Orchestrator, error) {
	if opts.Tracker == nil {
		return nil, errors.New("トラッカーのアダプタ（Tracker）が nil です")
	}
	if opts.Herdr == nil {
		return nil, errors.New("herdr のクライアント（Herdr）が nil です")
	}
	if opts.Workspace == nil {
		return nil, errors.New("worktree の管理（Workspace）が nil です")
	}
	if opts.HookSocketPath == "" || !filepath.IsAbs(opts.HookSocketPath) {
		return nil, fmt.Errorf(
			"hook を受ける socket のパス %q が絶対パスではありません（設定ファイルへ埋め込めない）",
			opts.HookSocketPath)
	}
	// **知っている Status の一覧は組み立てのときに1度だけ計算する**（`knownStateNames`）。
	// **計算に使う設定が空のまま渡されても、いままでは黙って通っていた。**
	// 1つも取れないと、continuo は**ボード上のどの Status も「知らない Status」と判定し、
	// 着手した run を片端から止める。**しかも止めた理由には「いま知っているのは です」と
	// 空欄が出るだけで、原因が読み取れない。
	// **他の必須の依存と同じく、ここで名前つきのエラーにする。**
	knownStateNames := config.KnownStates(opts.Config.Tracker)
	if len(knownStateNames) == 0 {
		return nil, errors.New(
			"continuo が扱う Status が1つも設定されていません（Config）" +
				"（WORKFLOW.md の tracker.active_states / terminal_states / running_state / " +
				"dispatch_state / failure_state / status_signal_map の遷移先を確かめてください）")
	}

	continuoPath := opts.ContinuoPath
	if continuoPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, i18n.Errorf(i18n.KeyOrchestratorNewExecutablePathUnknown, err)
		}
		continuoPath = exe
	}
	if !filepath.IsAbs(continuoPath) {
		return nil, fmt.Errorf("continuo の実行ファイルのパス %q が絶対パスではありません", continuoPath)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// hook が渡す transcript_path を受け入れる根を決める（外部入力の検査に使う）。
	transcriptRoot, rootErr := resolveTranscriptRoot(opts.TranscriptRoot)
	if rootErr != nil {
		// **起動は止めない。**根が決まらなくても「通常のファイルか」の検査は効く。
		logger.Warn("transcript の置き場所の根を決められません（置き場所の検査だけを行いません）",
			"error", rootErr)
	}
	nowFunc := opts.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	newUUID := opts.NewSessionUUID
	if newUUID == nil {
		newUUID = NewSessionUUID
	}
	// **持ち主の取得は既定で本物の `gh` を呼ぶ**（設計 3-65）。`GHAuthCheck` のように
	// 「nil なら何もしない」にすると、**呼び出し元が渡し忘れた瞬間に印だけの判定へ静かに戻る。**
	ghLogin := opts.GHLogin
	if ghLogin == nil {
		ghLogin = tracker.RunGHAPIUserLogin
	}
	// **この機械の名前を決める**（設計 3-77）。入札と hold のコメントに書く値である。
	// **取れなくても起動は止めない。**空のまま入札すると誰が入札したのか読めなくなるので、
	// そのときだけ固定の名前へ落とす（勝っても、どの機械かは hold の `assignee` で辿れる）。
	hostName := strings.TrimSpace(opts.HostName)
	if hostName == "" {
		name, err := os.Hostname()
		if err != nil || strings.TrimSpace(name) == "" {
			logger.Warn("この機械の名前を取れないので、入札には固定の名前を使います",
				"使う名前", unknownHostName, "error", err)
			hostName = unknownHostName
		} else {
			hostName = strings.TrimSpace(name)
		}
	}

	shutdown, shutdownCancel := context.WithCancel(context.Background())

	return &Orchestrator{
		cfg:            opts.Config,
		promptTemplate: opts.PromptTemplate,
		tracker:        opts.Tracker,
		herdr:          opts.Herdr,
		ws:             opts.Workspace,
		rl:             opts.RateLimit,
		socketPath:     opts.HookSocketPath,
		runtimeDir:     filepath.Dir(opts.HookSocketPath),
		continuoPath:   continuoPath,
		transcriptRoot: transcriptRoot,
		logger:         logger,
		now:            nowFunc,
		newSessionUUID: newUUID,
		ghAuthCheck:    opts.GHAuthCheck,
		ghLogin:        ghLogin,
		// **集めるのは `config.KnownStates` の1箇所だけである**（設計 3-57）。
		// **起動時にボードと照合する一覧（`tracker` の `requiredStatesForBootstrap`）は、
		// 同じ関数の戻り値そのものである。**ずれると、起動時に通した設定が実行時には
		// 別の意味になる（対応表のキーは、どちらにも入れない）。
		knownStateNames: knownStateNames,
		hostName:        hostName,

		runs:           map[string]*runState{},
		sessions:       map[string]*runState{},
		notified:       map[string]time.Time{},
		labelSkipped:   map[string]struct{}{},
		failures:       map[string]*failureNote{},
		shutdown:       shutdown,
		shutdownCancel: shutdownCancel,
	}, nil
}

// Run は巡回のループを回す。ctx が終わるまで返らない。
//
// **turn ループはここでは回さない。**run ごとの goroutine が回す（設計 3-8）。
//
// ctx: 巡回を止めるコンテキスト。
// 戻り値: ctx が終わったときの理由（context.Canceled など）。
func (o *Orchestrator) Run(ctx context.Context) error {
	interval := time.Duration(o.cfg.Polling.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	o.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			o.Tick(ctx)
		}
	}
}

// Close は turn ループへ終わるように伝え、終わるのを待つ。
//
// **待つ前に必ず伝える。**turn ループは `Stop` hook が来るまで期限なしで待つので
// （`claude.turn_timeout_ms` は turn の総実行時間の上限ではない。設計 3-21）、
// 伝えずに待つと永久に返らない。
//
// **pane は閉じない。**巡回を止めるだけでは run を諦めたことにならない（設計 3-4 の
// 「起動時の検査で落ちたとき、pane を閉じてはならない」と同じ考え方）。
func (o *Orchestrator) Close() {
	o.shutdownCancel()
	o.wg.Wait()
}

// Tick は巡回を1回だけ回す。
//
// 順番は設計 3-25 / 3-16 が定めるとおりである。
//
//  1. verify_states_every に1回、Status の選択肢名と gh の認証を検査する
//     （**バックオフ明けの再 dispatch より前。**再 dispatch も段0 から入り直す dispatch
//     なので、検査に落ちた巡回では見送る）
//  2. バックオフが明けた run を拾う（**候補の取得より前。**空きスロットの計算に効く）
//  3. 枠を読む（poll_interval_ms に1回。`rate_limit.source: none` なら1回も叩かない）
//  4. 候補を取る                 ← 巡回の GraphQL リクエスト 1本目
//  5. 実行中の Status を照合する  ← 2本目
//  6. worktree を照合する         ← 3本目
//  7. stall を判定する
//  8. 空きスロットが尽きるまで印を付ける（着手の段0〜1）。**段2以降は別の goroutine で回す**
//  9. turn ループを起こす（NeedsPrompt / 引き継いだ working の run）
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) Tick(ctx context.Context) {
	o.mu.Lock()
	o.tickCount++
	tick := o.tickCount
	o.mu.Unlock()

	// **「gh の持ち主」を取る**（設計 3-65）。**取れるまで ghLoginRetryInterval ごとに
	// 取り直し、一度取れたらそれ以降は取りに行かない**（判定は ghLoginDue）。
	// 取れないあいだも巡回は続ける（印だけで判定する形に落ちる）。
	o.ensureGHLogin(ctx)

	// **検査を先に行う。**落ちた巡回では新規の dispatch も再 dispatch も見送る
	// （再 dispatch も着手の段0 から入り直す dispatch である。設計 3-25）。
	dispatchAllowed := o.verifyPeriodically(ctx, tick)

	o.resumeBackoff(ctx, dispatchAllowed)
	o.pollQuota(ctx)

	candidates, err := o.tracker.FetchIssuesByStates(ctx, o.cfg.Tracker.ActiveStates)
	if err != nil {
		o.logger.Warn("候補の取得に失敗しました（この巡回の dispatch は行いません）", "error", err)
		dispatchAllowed = false
	}

	o.reconcileRunning(ctx)
	o.reconcileWorktrees(ctx)
	o.checkStalls(ctx)

	if dispatchAllowed {
		o.dispatchCandidates(ctx, candidates)
	}

	o.wakeRuns(ctx)
}

// verifyPeriodically は Status の選択肢名と `gh` の認証を、
// `tracker.verify_states_every` の頻度で検査する（設計 3-6 の「巡回ごとに検査するもの」）。
//
// **毎巡回では行わない。**選択肢名が変わるのは人間がボードを触ったときだけであり、
// **毎巡回で外部プロセス（`gh`）を起動しない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// tick: 何回目の巡回か（1始まり）。
// 戻り値: この巡回で dispatch してよければ true。**失敗しても実行中の照合は止めない。**
func (o *Orchestrator) verifyPeriodically(ctx context.Context, tick int) bool {
	every := o.cfg.Tracker.VerifyStatesEvery
	if every <= 0 {
		// 0 なら起動時の1回だけ（Bootstrap）。巡回では検査しない。
		return true
	}
	if (tick-1)%every != 0 {
		return true
	}

	if err := o.tracker.VerifyStatusOptions(ctx, o.cfg.Tracker); err != nil {
		o.logger.Warn(
			"Status の選択肢名が設定と一致しません（この巡回の dispatch を飛ばします。実行中の照合は止めません）",
			"error", err)
		return false
	}
	if o.ghAuthCheck != nil {
		if err := o.ghAuthCheck(ctx); err != nil {
			o.logger.Warn(
				"gh の認証が有効ではありません（この巡回の dispatch を飛ばします。実行中の照合は止めません）",
				"error", err)
			return false
		}
	}
	return true
}

// ensureGHLogin は「continuo が使う gh の持ち主」を、取れるまで取り直す（設計 3-65）。
//
// **これが分からないと、印（`tracker.comments.marker` / `self_marker`）だけで
// 「continuo の側が書いたコメント」を判定することになる。**印は本文の先頭に置く
// ただの文字列なので、issue にコメントできる人なら誰でも同じものを書ける。
//
// **取れなくても起動も巡回も止めない。**止めると、`gh api` に一時的に届かないだけで
// continuo が動かなくなる。**取れないあいだは印だけの判定へ落ちる。**
//
// **1回で諦めない。**諦めると、`gh api` に一度届かなかっただけで、プロセスが生きている
// あいだずっと印だけの判定に戻る。**取れるまで ghLoginRetryInterval ごとに取り直し、
// 取り直しに失敗するたびに、連続して失敗している回数を添えて WARN を残す。**
// 常駐して何日経っても、いま印だけで判定していることがログから読める。
//
// **取れたら、そのあとは取り直さない。**持ち主が変わるのは `gh auth switch` を人間が
// 叩いたときだけで、その操作は continuo を止めずに行うものではない。
//
// ctx: 呼び出しに適用するコンテキスト。**既に終わっているなら何もしない**
// （止められている最中に外部プロセスを起こさない。次の起動でやり直せる）。
func (o *Orchestrator) ensureGHLogin(ctx context.Context) {
	if ctx.Err() != nil || o.ghLogin == nil || !o.ghLoginDue() {
		return
	}
	// **取得そのものは1本に絞る。**巡回とコメントの確認は別の goroutine から来る。
	o.ghLoginAttemptMu.Lock()
	defer o.ghLoginAttemptMu.Unlock()
	// **待っているあいだに、別の呼び出しが取り終えていることがある。**
	if !o.ghLoginDue() {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, ghLoginTimeout)
	defer cancel()
	login, err := o.ghLogin(runCtx)
	if err != nil {
		if ctx.Err() != nil {
			// **止められただけである。**失敗として数えない（数えると、`Ctrl+C` のたびに
			// 「取れません」が1行増え、本当に取れないときと見分けがつかなくなる）。
			o.logger.Debug("止められたので、gh の持ち主の取得は次の起動に回します")
			return
		}
		failures, since := o.noteGHLoginFailure()
		o.logger.Warn(
			"gh の持ち主を取れません（コメントの印だけで判定します。"+
				"第三者が同じ印で書いたコメントをエージェントのものと読み違えることがあります）",
			"連続して失敗した回数", failures, "最初に失敗した時刻", since, "error", err)
		return
	}
	o.ghLoginMu.Lock()
	o.selfLogin = login
	o.ghLoginMu.Unlock()
	o.logger.Info("gh の持ち主を確認しました（コメントの印と併せて見ます）", "login", login)
}

// ghLoginDue は、いま持ち主を取りに行くべきかを返す（設計 3-65）。
//
// 戻り値: まだ取れておらず、かつ前に試してから ghLoginRetryInterval が過ぎていれば true。
// **1回目（まだ1度も試していない）は必ず true になる。**
func (o *Orchestrator) ghLoginDue() bool {
	o.ghLoginMu.Lock()
	defer o.ghLoginMu.Unlock()
	if o.selfLogin != "" {
		return false
	}
	if o.ghLoginLastTriedAt.IsZero() {
		return true
	}
	return !o.now().Before(o.ghLoginLastTriedAt.Add(ghLoginRetryInterval))
}

// noteGHLoginFailure は持ち主を取れなかったことを記録する（設計 3-65）。
//
// 戻り値の1つ目: 連続して失敗した回数（今回を含む）。
// 戻り値の2つ目: 最初に失敗した時刻。
func (o *Orchestrator) noteGHLoginFailure() (int, time.Time) {
	o.ghLoginMu.Lock()
	defer o.ghLoginMu.Unlock()
	now := o.now()
	o.ghLoginLastTriedAt = now
	o.ghLoginFailures++
	if o.ghLoginFirstFailedAt.IsZero() {
		o.ghLoginFirstFailedAt = now
	}
	return o.ghLoginFailures, o.ghLoginFirstFailedAt
}

// ghLoginName は取れている「gh の持ち主」を返す（設計 3-65）。
//
// 戻り値: ログイン名。**取れていなければ空文字**（呼び出し側は印だけで判定する）。
func (o *Orchestrator) ghLoginName() string {
	o.ghLoginMu.Lock()
	defer o.ghLoginMu.Unlock()
	return o.selfLogin
}

// pollQuota は枠を読む（設計 3-27）。
//
// **`rate_limit.source: none` なら1回も叩かない**（Reader.Enabled が偽になる）。
// 読む間隔は `rate_limit.poll_interval_ms`（既定5分）である。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) pollQuota(ctx context.Context) {
	if o.rl == nil || !o.rl.Enabled() {
		return
	}
	interval := time.Duration(o.cfg.RateLimit.PollIntervalMs) * time.Millisecond
	now := o.now()

	o.mu.Lock()
	last := o.quotaFetchedAt
	o.mu.Unlock()
	if !last.IsZero() && interval > 0 && now.Sub(last) < interval {
		return
	}

	snap, err := o.rl.Fetch(ctx)
	if err != nil {
		// **写しを古いままにしない**（設計 3-77）。入札は枠の写しで判定するので、
		// **読めなくなった機械が、最後に読めた「暇な」値で入札し続ける。**
		// **止めるのは入札だけである。**枠待ちと dispatch を止める閾値は、
		// 最後に読めた値を使い続ける（読めないことを理由に走行中の run を捨てない）。
		o.mu.Lock()
		o.quotaStale = true
		o.mu.Unlock()
		o.logger.Warn("枠の読み取りに失敗しました（読めるまで入札しません）", "error", err)
		return
	}

	o.mu.Lock()
	o.quotaFetchedAt = now
	o.quotaStale = false
	if snap != nil {
		o.quota = snap
	}
	o.mu.Unlock()
}

// quotaForBid は、入札の判定に使ってよい枠の写しを返す（設計 3-77）。
//
// **最後の読み取りに失敗していたら nil を返す。**`handoff.Evaluate` は nil を
// 「枠を読めなかった」と読み、**入札そのものを取りやめる。**
// **古い写しで入札させない。**資格情報が切れた機械は、切れる直前の「使用率 5%」を
// 1日中返し続け、**正直に読めている機械に必ず勝つ。**
//
// 戻り値: 枠の状態。読めていない・最後の読み取りに失敗していれば nil。
func (o *Orchestrator) quotaForBid() *ratelimit.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.quotaStale {
		return nil
	}
	return o.quota
}

// quotaSnapshot は最後に読んだ枠の状態を返す。
//
// 戻り値: 枠の状態。読めていなければ nil。
func (o *Orchestrator) quotaSnapshot() *ratelimit.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.quota
}

// dispatchPaused は「新規の dispatch を止める」閾値を超えているかを返す（設計 3-27）。
//
// **これは「枠待ち」とは別の判定である。**閾値（既定95%）を超えただけでは枠待ちとみなさない。
// 走行中の turn は止めないし、時計も止めない。
//
// 戻り値: 新規の dispatch を止めるべきなら true。
func (o *Orchestrator) dispatchPaused() bool {
	snap := o.quotaSnapshot()
	if snap == nil {
		return false
	}
	return snap.MaxPercent() > o.cfg.RateLimit.PauseAbovePercent
}

// wakeRuns は turn ループの goroutine を必要な run について起こす（設計 3-8 / 3-4 の段5a2・段5c）。
//
// **起こす対象は2種類ある。**
//
//	awaitTurnEnd が立っている  … 復元で `working` を引き継いだ run。**turn は送らず**、
//	                             走っている turn の終わりを待つところから入る（段5a2）
//	NeedsPrompt が立っている   … 次の turn を送る run（段5c、および起こし損ねた run）
//
// **巡回のループはブロックしない。**turn ループは run ごとの goroutine で回す。
// **起こせなかったら印を立て直す。**古い turn ループが待ち受けから戻っていないと
// 2本目は立てられないので、黙って捨てずに次の巡回で起こし直す。
//
// ctx: turn ループへ渡すコンテキスト。
func (o *Orchestrator) wakeRuns(ctx context.Context) {
	for _, rs := range o.snapshotRuns() {
		if rs.isFinished() {
			continue
		}
		// **turn を送る前に、担当がこの機械のままかを1回だけ確かめる**（設計 3-77c）。
		// **効くのは復元した run と、この機能より前に着手した run だけである。**
		// **確かめずに送ると、担当が既に移っていても丸ごと1回ぶん働く**（`after_run` も走る）。
		if lost, newHost := o.handoffLostOnResume(ctx, rs); lost {
			o.stopBecauseHandoffLost(ctx, rs, newHost)
			continue
		}
		if rs.takeAwaitTurnEnd() {
			if !o.startTurnLoop(ctx, rs, true) {
				rs.setAwaitTurnEnd()
			}
			continue
		}
		if !rs.takeNeedsPrompt() {
			continue
		}
		if !o.startTurnLoop(ctx, rs, false) {
			o.logger.Warn("turn ループが既に走っているので、次の巡回で起こし直します",
				"identifier", rs.issue().Identifier)
			rs.setNeedsPrompt()
		}
	}
}

// snapshotRuns は印の集合の写しを返す（走査中に map を書き換えても壊れないようにする）。
//
// 戻り値: 印の集合に入っている runState の並び。
func (o *Orchestrator) snapshotRuns() []*runState {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]*runState, 0, len(o.runs))
	for _, rs := range o.runs {
		out = append(out, rs)
	}
	return out
}

// claim は「自分が取った」印を付ける（着手の段1。設計 3-16）。
//
// **空きスロットの検査（段-1）と dispatch 直前の検査（段0）を通ったあとで呼ぶ。**
// 印を付けてから弾くと、印が残る。
//
// issueID: project item の ID。
// issue: issue のスナップショット。
// 戻り値の1つ目: 付けた印。
// 戻り値の2つ目: 既に印を持っていた場合は false（**dispatch しない**）。
func (o *Orchestrator) claim(issueID string, issue tracker.Issue) (*runState, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.runs[issueID]; exists {
		return nil, false
	}
	rs := newRunState(issueID, issue, o.now())
	o.runs[issueID] = rs
	return rs, true
}

// release は印を外し、セッションの索引からも外す。
//
// **run が終わった（片付けた・worker を止めた・failure_state へ落とした）ときだけ呼ぶ。**
// バックオフ中の run は外さない（外すと30秒後の巡回で即座に拾い直される。設計 3-25）。
//
// rs: 外す run。
func (o *Orchestrator) release(rs *runState) {
	rs.markFinished()
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.runs, rs.IssueID)
	for uuid, cur := range o.sessions {
		if cur == rs {
			delete(o.sessions, uuid)
		}
	}
}

// bindSession はセッション UUID から run を引ける状態にする。
//
// **hook はどの run のものかを session_id でしか名乗らない**（設計 3-2）。
// 再 dispatch のたびに UUID は新しくなるので、古い対応は消してから入れる。
//
// rs: 対象の run。
// sessionUUID: 新しいセッション UUID。
func (o *Orchestrator) bindSession(rs *runState, sessionUUID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for uuid, cur := range o.sessions {
		if cur == rs {
			delete(o.sessions, uuid)
		}
	}
	o.sessions[sessionUUID] = rs
}

// lookupRunByID は project item の ID で印を引く。
//
// issueID: project item の ID。
// 戻り値の1つ目: 見つかった run。
// 戻り値の2つ目: 印を持っていれば true。
func (o *Orchestrator) lookupRunByID(issueID string) (*runState, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rs, ok := o.runs[issueID]
	return rs, ok
}

// lookupRunBySession はセッション UUID で run を引く（hook の配送に使う）。
//
// sessionUUID: Claude Code のセッション UUID。
// 戻り値の1つ目: 見つかった run。
// 戻り値の2つ目: 索引にあれば true。
func (o *Orchestrator) lookupRunBySession(sessionUUID string) (*runState, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rs, ok := o.sessions[sessionUUID]
	return rs, ok
}

// Adopt は復元（第7段階）が引き継ぐと決めた run を印の集合へ入れ直す（設計 3-4 の段6）。
//
// **入れ直さないと、pane が生きているのに印が無い issue ができ、次の巡回で
// もう1つ dispatch されて同じ worktree に Claude Code が2つ立つ。**
//
// **turn は送らない。**代わりに NeedsPrompt を立て、巡回の turn ループが非同期に送る
// （設計 3-4 の段5c。復元の中で wait つきの agent.prompt を呼ぶと1時間返らない）。
//
// issue: 引き継ぐ issue のスナップショット。
// state: 引き継ぐ run の実行時状態（agent 名・pane・セッション UUID・worktree など）。
// needsPrompt: 継続の指示を送るなら true。**`agent_status` が `working` のときは偽にする**
// （走っている最中に投げると turn が混ざる。設計 3-4 の段5a2）。
// 戻り値: 新しく入れ直したら true。既に印を持っていた場合は false（何もしない）。
func (o *Orchestrator) Adopt(issue tracker.Issue, state AdoptedRun, needsPrompt bool) bool {
	o.mu.Lock()
	if _, ok := o.runs[issue.ID]; ok {
		o.mu.Unlock()
		return false
	}
	now := o.now()
	rs := newRunState(issue.ID, issue, now)
	rs.AgentName = state.AgentName
	rs.PaneID = state.PaneID
	rs.SessionUUID = state.SessionUUID
	rs.WorktreePath = state.WorktreePath
	rs.Base = state.Base
	rs.SettingsPath = state.SettingsPath
	rs.HerdrWorkspaceID = state.HerdrWorkspaceID
	// **引き継いだ pane の画面の版を種にする**（設計 3-21）。種を入れないと、
	// 最初の stall の判定が必ず「版が変わった」になり、打ち切りまでに
	// `claude.turn_timeout_ms` を2回またぐことになる。
	rs.LastRevision = state.Revision
	// 引き継いだ時刻を入れる（「この run が書いたコメント」の判別に使う。設計 3-25）。
	rs.StartedAt = now
	rs.NeedsPrompt = needsPrompt
	// **`agent_status` が `working` の run はこちらを立てる**（設計 3-4 の段5a2）。
	// turn は送らないが、走っている turn の `Stop` を読む goroutine は要る。
	rs.awaitTurnEnd = state.AwaitTurnEnd
	// **SendFirstPrompt は立てない**（ゼロ値の偽のままにする）。走っている worker を
	// そのまま引き継いでいるので、送るのは**継続の指示（5-4）**である。
	// **1回目の本文（5-3）ではない**（設計 3-4 の段5c）。
	// エージェントは issue の URL も完了の作法も既に知っている。
	// **turn 数を 1 から数え直すのは打ち切りの計算のためであって、1回目をやり直すことではない。**
	o.runs[issue.ID] = rs
	if state.SessionUUID != "" {
		o.sessions[state.SessionUUID] = rs
	}
	o.mu.Unlock()
	return true
}

// AdoptedRun は復元が引き継ぐ run の実行時状態である（設計 3-4 の段5c）。
//
// **PromptID は含めない。**復元できないので空のままにする（空のときは prompt_id の
// 照合を行わない）。**TurnCount も含めない。**1 から数え直す（設計 3-4 の段7）。
type AdoptedRun struct {
	// AgentName は herdr の agent 名である（agent.list から引き直したもの）。
	AgentName normalize.SafeName
	// PaneID は herdr の pane ID である。
	PaneID string
	// SessionUUID は Claude Code のセッション UUID である（pane の agent_session から取る）。
	SessionUUID string
	// WorktreePath は worktree の絶対パスである。
	WorktreePath string
	// Base は worktree を作ったときの base である。
	Base normalize.SafeName
	// SettingsPath は issue ごとの Claude Code の設定ファイルのパスである。
	SettingsPath string
	// HerdrWorkspaceID は herdr の workspace の ID である。
	HerdrWorkspaceID string
	// Revision は引き継いだ pane の画面の版である（`pane.list` が返す `revision`）。
	//
	// **stall の判定の種になる**（設計 3-21）。0 のままでも判定は動くが、
	// 最初の判定が必ず「版が変わった」になるぶん、打ち切りが1周期ぶん遅れる。
	Revision uint64
	// AwaitTurnEnd は「turn を送らずに、走っている turn の終わりを待つ」ことを表す。
	//
	// **`agent_status` が `working` の run を引き継ぐときに真にする**（設計 3-4 の段5a2）。
	// **`NeedsPrompt` とは同時に立てない。**立てないと turn ループの goroutine が1本も
	// 起きず、その run の `Stop` hook を誰も読まないまま claude.turn_timeout_ms まで放置される。
	AwaitTurnEnd bool
}

// OnHook は hookserver から hook を1件受け取る（hookserver.HookSink の実装）。
//
// **判断はここではほとんどしない。**外部入力を検査し、stall の時計を進め、prompt_id と
// transcript のパスを覚え、turn の終わりの判定に使うイベントを run の受け口へ流すだけである。
//
// ev: 届いた hook。
// 戻り値: 知っている session_id なら true。知らなければ false（hookserver が警告を出して捨てる）。
// **検査に落ちて捨てた hook でも true を返す**（session_id そのものは知っているため）。
func (o *Orchestrator) OnHook(ev hookserver.HookEvent) bool {
	rs, ok := o.lookupRunBySession(ev.SessionID)
	if !ok {
		return false
	}

	// **hook の中身は外部入力である**（設計 3-2）。`cwd` と `transcript_path` を先に検査し、
	// 通らなかったものは捨てる。とくに `transcript_path` は、そのまま `os.Open` へ渡すと
	// FIFO で永久に返らず、turn ループの goroutine ごと固まる。
	ev, ok = o.sanitizeHookEvent(ev, rs)
	if !ok {
		return true
	}

	// `agent_type` が空文字の SubagentStop は数えない（設計 1-3。対応する SubagentStart が
	// 0/22・0/44 で存在せず、道具を1つも使わない turn でも出る）。
	if ev.HookEventName == hookSubagentStop && ev.AgentType == "" {
		o.logger.Debug("agent_type が空文字の SubagentStop を捨てました",
			"identifier", rs.issue().Identifier, "session_id", ev.SessionID)
		return true
	}

	if ev.HookEventName == hookNotification && ev.NotificationType == "permission_prompt" {
		// どの turn が権限の確認で止まったかを記録する（設計 3-11）。
		o.logger.Warn("権限の確認で止まりました",
			"identifier", rs.issue().Identifier, "prompt_id", ev.PromptID)
	}

	rs.noteHook(ev, o.now())

	// **走っている subagent を覚える**（設計 3-11）。`blocked` で esc を送る前に、
	// 走っているものが無いかを見るためだけに使う。**turn の終わりの判定には使わない。**
	switch ev.HookEventName {
	case hookSubagentStart:
		rs.noteSubagentStart(ev.AgentID, ev.AgentType)
	case hookSubagentStop:
		// **ここへ来るのは `agent_type` が入っているものだけである**（上で捨てている）。
		rs.noteSubagentStop(ev.AgentID)
	}

	if !isTurnBoundaryHook(ev) {
		return true
	}
	select {
	case rs.hookCh <- ev:
	default:
		o.logger.Warn("turn の終わりの判定に使う hook の受け口があふれたので捨てました",
			"identifier", rs.issue().Identifier, "hook", ev.HookEventName)
	}
	return true
}

// isTurnBoundaryHook は turn の終わりの判定に使う hook かを返す（設計 3-2）。
//
// 使うのは2つだけである。
//
//	Stop              turn の終わりの判定の起点
//	UserPromptSubmit  `<task-notification>` の検出（来たら turn は続いている）
//
// **`PreToolUse` / `PostToolUse` は使わない。**生きていることの確認だけに使う（設計 3-21）。
//
// ev: 判定する hook。
// 戻り値: 判定に使うなら true。
func isTurnBoundaryHook(ev hookserver.HookEvent) bool {
	switch ev.HookEventName {
	case hookStop:
		return true
	case hookUserPromptSubmit:
		return strings.HasPrefix(strings.TrimSpace(ev.Prompt), taskNotificationPrefix)
	default:
		return false
	}
}

// IssueSlug は issue の識別子から、設定ファイルと逃がし先の置き場所に使うスラグを作る
// （設計 3-2 の「`<issue のスラグ>` の作り方を1つに決める」）。
//
// **英数字とハイフン以外を全部ハイフンに置き換え、連続するハイフンを1つにまとめ、
// 小文字にする。**例: `Octocat/Hello_World#188` → `octocat-hello-world-188`。
//
// identifier: `<owner>/<repo>#<番号>` の形の識別子。
// 戻り値: スラグ。前後のハイフンは落とす。空になる場合は "issue" を返す。
func IssueSlug(identifier string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(identifier) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if prevHyphen {
			continue
		}
		b.WriteByte('-')
		prevHyphen = true
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "issue"
	}
	return s
}

// issueDir は issue ごとの実行時ディレクトリを返す（設計 3-12 / 3-19）。
//
//	<実行時ディレクトリ>/issues/<issue のスラグ>
//
// identifier: issue の識別子。
// 戻り値: issue ごとのディレクトリの絶対パス。
func (o *Orchestrator) issueDir(identifier string) string {
	return filepath.Join(o.runtimeDir, hookserver.IssuesDirName, IssueSlug(identifier))
}

// pendingDir は hook の逃がし先を返す（設計 3-19）。
//
// identifier: issue の識別子。
// 戻り値: 逃がし先の絶対パス。
func (o *Orchestrator) pendingDir(identifier string) string {
	return filepath.Join(o.issueDir(identifier), hookserver.PendingDirName)
}

// RunningIdentifiers は印の集合（＝実行中の一覧）に入っている issue の識別子を返す。
//
// **印の集合は「実行中の一覧」と同じ1本である**（設計 3-25）。この関数は、その1本を
// 外から観測するためだけのものであり、判断には使わない。
//
// 戻り値: 印を持っている issue の識別子（順序は不定）。
func (o *Orchestrator) RunningIdentifiers() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, 0, len(o.runs))
	for _, rs := range o.runs {
		out = append(out, rs.issue().Identifier)
	}
	return out
}

// RunView は印を持っている run を外から観測するための写しである。
//
// **判断には使わない。**巡回・turn ループ・stall 検知はすべて内部の runState を見る。
// これは監視（第9段階のダッシュボード）と検査のための読み取り口である。
type RunView struct {
	// Identifier は issue の識別子である。
	Identifier string
	// TurnCount は continuo が送った turn の回数である（設計 3-14）。
	// **Claude Code 自身が投入する `<task-notification>` は数えていない。**
	TurnCount int
	// RetryCount は stall や起動失敗で積んだリトライの回数である。
	RetryCount int
	// BackoffUntil はこの時刻まで再 dispatch しない、という時刻である。
	BackoffUntil time.Time
	// WaitingQuota は枠待ちと判定して時計を止めていることを表す。
	WaitingQuota bool
	// State は最後に取り直した Status である。
	State string
	// Title は issue のタイトルである。
	// **外部から来る文字列である。**表示するときは必ずエスケープすること。
	Title string
	// URL は issue の URL である。draft issue は URL を持たないので空文字になる。
	URL string
	// StartedAt はこの run が最初の turn を送った時刻である。ゼロ値ならまだ送っていない。
	StartedAt time.Time
	// LastHookAt は最後に hook を実際に受けた時刻である。
	// **ゼロ値なら、この run から hook を1件も受けていない。**
	// 人間が「エージェントが生きているか」を判断するのはこの値である。
	LastHookAt time.Time
	// StallClockAt は stall の時計である（設計 3-21）。
	// **hook 以外でも進む**（turn を送った・枠待ちを外した・猶予を与えた）。
	// **「最後に hook を受けた時刻」ではない。**それは LastHookAt である。
	StallClockAt time.Time
	// Tokens はこの run が始めてからの累計のトークンである（設計 3-15）。
	// **`requestId` で重複排除済みで、再 dispatch でセッションが変わった分も足してある。**
	Tokens TokenUsage
	// TokensAt は Tokens を集計した時刻である。ゼロ値なら一度も集計していない。
	TokensAt time.Time
}

// RunViews は印の集合に入っている run の写しを返す。
//
// 戻り値: run の写し（順序は不定）。
func (o *Orchestrator) RunViews() []RunView {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]RunView, 0, len(o.runs))
	for _, rs := range o.runs {
		snap := rs.snapshot()
		out = append(out, RunView{
			Identifier:   snap.Identifier,
			TurnCount:    snap.TurnCount,
			RetryCount:   snap.RetryCount,
			BackoffUntil: snap.BackoffUntil,
			WaitingQuota: snap.WaitingQuota,
			State:        snap.State,
			Title:        snap.Title,
			URL:          snap.URL,
			StartedAt:    snap.StartedAt,
			LastHookAt:   snap.LastHookAt,
			StallClockAt: snap.LastSeenAt,
			Tokens:       snap.Tokens,
			TokensAt:     snap.TokensAt,
		})
	}
	return out
}
