package herdr

// このファイルは herdr の socket API の**応答（result）に出てくる値の形**を、
// `herdr api schema --json` の `schemas.success_response.$defs` からそのまま写したものである
// （2026-08-18 に確認。protocol=19 / herdr 0.8.0）。
//
// 【ここに書いてある形は推測ではない】
// 応答のスキーマは実在する。以前は「result のフィールド名は推測である」と各所に
// 書いてあったが、その前提は誤りだった。フィールド名・型・必須かどうかは、すべて
// 上記スキーマの $defs（PaneInfo / AgentInfo / WorkspaceInfo / TabInfo / WorktreeInfo /
// PaneReadResult ほか）に定義されている。
//
// 【残っている未確定はメソッドと応答の対応づけだけである】
// 応答は `type` を判別子に持つ tagged union（`ResponseResult`）で、変種は58個ある。
// **スキーマは「どのメソッドがどの変種を返すか」を書いていない。**したがって各 *Result 型の
// GoDoc には「実測で確認済み」か「変種名からの推定」かを明記してある。
// どの *Result 型も判別子の Type を持つので、推定が外れていれば呼び出し側で気づける。

// AgentStatus は agent の状態である
// （`schemas.request.$defs.AgentStatus` と `schemas.success_response.$defs.AgentStatus`。
// enum は idle / working / blocked / done / unknown の5つ）。
//
// done は「idle と同じ状態だが、その tab がまだ人間に見られていない」を意味する（2-1）。
type AgentStatus string

// AgentStatus の取りうる値である（実スキーマの enum と1対1）。
const (
	// AgentStatusIdle は入力を受け付けられ、かつ tab が人間に見られた状態である。
	AgentStatusIdle AgentStatus = "idle"
	// AgentStatusWorking は agent が作業中の状態である。
	AgentStatusWorking AgentStatus = "working"
	// AgentStatusBlocked は herdr が承認待ち・質問の UI を検知した状態である。
	AgentStatusBlocked AgentStatus = "blocked"
	// AgentStatusDone は idle と同じ状態のうち、tab がまだ人間に見られていないものである。
	AgentStatusDone AgentStatus = "done"
	// AgentStatusUnknown は agent は居るが herdr が状態を判定できない状態である。
	// **完了を意味しない。**
	AgentStatusUnknown AgentStatus = "unknown"
)

// AgentStatuses は AgentStatus の取りうる値をすべて返す。
//
// **設定の綴りの検査（`claude.wait_until`）が引く。**検査する側が自前で一覧を持つと、
// herdr の enum が増えたときにそちらだけが古いまま残る。定数の一覧はここ1箇所に置く。
//
// 戻り値: 実スキーマの enum と同じ並び（idle / working / blocked / done / unknown）の
// 新しいスライス。呼び出し側が書き換えても他の呼び出しに影響しない。
func AgentStatuses() []AgentStatus {
	return []AgentStatus{
		AgentStatusIdle,
		AgentStatusWorking,
		AgentStatusBlocked,
		AgentStatusDone,
		AgentStatusUnknown,
	}
}

// PaneAgentState は pane.report_agent に渡す agent の状態である
// （`schemas.request.$defs.PaneAgentState`）。
//
// **AgentStatus とは別の enum である。**done が無く、idle / working / blocked / unknown の
// 4つしか受け付けない。AgentStatus の値をそのまま渡してはならない。
type PaneAgentState string

// PaneAgentState の取りうる値である（実スキーマの enum と1対1）。
const (
	// PaneAgentStateIdle は入力を受け付けられる状態である。
	PaneAgentStateIdle PaneAgentState = "idle"
	// PaneAgentStateWorking は作業中の状態である。
	PaneAgentStateWorking PaneAgentState = "working"
	// PaneAgentStateBlocked は承認待ち・質問の状態である。
	PaneAgentStateBlocked PaneAgentState = "blocked"
	// PaneAgentStateUnknown は判定できない状態である。
	PaneAgentStateUnknown PaneAgentState = "unknown"
)

// ReadSource は agent.read / pane.read で読み取る対象である
// （`schemas.request.$defs.ReadSource`）。
//
// 【CLI の綴りと socket API の綴りが違う】
// CLI は `herdr agent read --source recent-unwrapped` と**ハイフン**で書くが、
// socket API の enum は `recent_unwrapped` と**アンダースコア**である。
// CLI の綴りをそのまま socket API へ送ると拒否される。
type ReadSource string

// ReadSource の取りうる値である（実スキーマの enum と1対1）。
const (
	// ReadSourceVisible は現在描画されている表示範囲である。
	ReadSourceVisible ReadSource = "visible"
	// ReadSourceRecent は直近の出力である（折り返しをそのまま含む）。
	ReadSourceRecent ReadSource = "recent"
	// ReadSourceRecentUnwrapped は直近の出力から折り返しを繋いだものである。
	// ログや会話の読み取りにはこれを使う。**綴りはアンダースコアである**（CLI はハイフン）。
	ReadSourceRecentUnwrapped ReadSource = "recent_unwrapped"
	// ReadSourceDetection は herdr が agent の検知に使っている平文の下部バッファである。
	ReadSourceDetection ReadSource = "detection"
)

// ReadFormat は読み取った内容の書式である（`schemas.request.$defs.ReadFormat`）。
type ReadFormat string

// ReadFormat の取りうる値である（実スキーマの enum と1対1）。
const (
	// ReadFormatText は ANSI エスケープ列を含まない平文である（herdr の既定）。
	ReadFormatText ReadFormat = "text"
	// ReadFormatANSI は色や装飾の ANSI エスケープ列を含む形式である。
	ReadFormatANSI ReadFormat = "ansi"
)

// AgentSessionKind は AgentSession.Value が何を指しているかである
// （`schemas.success_response.$defs.AgentSessionRefKind`。enum は id / path）。
type AgentSessionKind string

// AgentSessionKind の取りうる値である（実スキーマの enum と1対1）。
const (
	// AgentSessionKindID は Value がセッションの ID であることを意味する。
	// Claude Code の場合、この値がセッション UUID である（実測で確認済み）。
	AgentSessionKindID AgentSessionKind = "id"
	// AgentSessionKindPath は Value がセッションのファイルパスであることを意味する。
	AgentSessionKindPath AgentSessionKind = "path"
)

// AgentSession は pane で動いている agent のセッション情報である
// （`schemas.success_response.$defs.AgentSessionInfo`。4項目とも必須）。
//
// 実測（`herdr agent list` の応答。読み取りのみ）:
//
//	"agent_session":{"agent":"claude","kind":"id","source":"herdr:claude",
//	                 "value":"9fb773d0-a9bb-45bc-8a8b-a7cf21a8d2f0"}
type AgentSession struct {
	// Source はセッション情報の出どころである（実測: "herdr:claude"）。
	Source string `json:"source"`
	// Agent は agent の種別である（実測: "claude"）。
	Agent string `json:"agent"`
	// Kind は Value が ID とパスのどちらであるかを表す。
	Kind AgentSessionKind `json:"kind"`
	// Value はセッションの ID またはパスである（Kind による）。
	Value string `json:"value"`
}

// PaneScroll は pane のスクロール位置である
// （`schemas.success_response.$defs.PaneScrollInfo`。3項目とも必須）。
type PaneScroll struct {
	// OffsetFromBottom は最下行からの現在のずれ（行数）である。
	OffsetFromBottom uint64 `json:"offset_from_bottom"`
	// MaxOffsetFromBottom は最下行からずらせる上限（行数）である。
	MaxOffsetFromBottom uint64 `json:"max_offset_from_bottom"`
	// ViewportRows は表示範囲の行数である。
	ViewportRows uint64 `json:"viewport_rows"`
}

// Pane は herdr の pane である（`schemas.success_response.$defs.PaneInfo`）。
//
// 必須は pane_id / terminal_id / workspace_id / tab_id / focused / agent_status /
// revision の7つで、残りは省略されうる（型が ["string","null"] のものを含む）。
// **フィールド名は `herdr pane list` の実測（読み取りのみ）でも確認済みである。**
type Pane struct {
	// PaneID は pane を指す ID（例: "w1:p2"）である。
	PaneID string `json:"pane_id"`
	// TerminalID は pane の中の端末の ID である（例: "term_658a8d9202f7e1"）。
	// **pane の指定には使えない**（herdr の agent 系のメソッドは端末 ID を受け付けない）。
	TerminalID string `json:"terminal_id"`
	// WorkspaceID は pane が属する workspace の ID である（例: "w1"）。
	WorkspaceID string `json:"workspace_id"`
	// TabID は pane が属する tab の ID である（例: "w1:t1"）。
	TabID string `json:"tab_id"`
	// Focused は pane が herdr の UI でフォーカスされているかどうかである。
	Focused bool `json:"focused"`
	// AgentStatus は pane に居る agent の状態である。agent が居ない pane では
	// AgentStatusUnknown になる（実測）。
	AgentStatus AgentStatus `json:"agent_status"`
	// Revision は pane の内容の版である。読み取り結果の鮮度の照合に使える。
	Revision uint64 `json:"revision"`
	// Agent は pane に居る agent の種別である（例: "claude"）。居なければ空である。
	Agent string `json:"agent,omitempty"`
	// AgentSession は pane で動いている agent のセッション情報である。
	// 再起動後の復元手順の段5（3-4）で、ここから Claude Code のセッション UUID を取り、
	// hook が渡す session_id との対応づけを復元する（3-2 の要）。取り出しは SessionUUID を使う。
	AgentSession *AgentSession `json:"agent_session,omitempty"`
	// Cwd は pane の作業ディレクトリである。worktree のパスに issue 番号を含める運用
	// （3-3・3-22）の照合に使う。
	Cwd string `json:"cwd,omitempty"`
	// ForegroundCwd は pane で動いている前面のプロセスの作業ディレクトリである。
	ForegroundCwd string `json:"foreground_cwd,omitempty"`
	// DisplayAgent は herdr の UI に表示する agent 名である。
	DisplayAgent string `json:"display_agent,omitempty"`
	// Label は pane に貼られたラベルである。**`owner/repo/issues/N` を書く**（3-3）。
	// **人間が herdr の画面で pane を見分けるための表示名である。**continuo は読み戻さない。
	// **書き込みは pane.split ではできない。**pane を作った直後に PaneRename を呼ぶこと。
	Label string `json:"label,omitempty"`
	// Scroll は pane のスクロール位置である。
	Scroll *PaneScroll `json:"scroll,omitempty"`
	// StateLabels は pane.report_metadata で書いた揮発性の状態ラベルである。
	// **herdr の再起動で消えるので復元の根拠にしてはならない**（3-3）。
	StateLabels map[string]string `json:"state_labels,omitempty"`
	// TerminalTitle は端末が設定したタイトルである（装飾文字を含みうる）。
	TerminalTitle string `json:"terminal_title,omitempty"`
	// TerminalTitleStripped は TerminalTitle から装飾文字を落としたものである。
	TerminalTitleStripped string `json:"terminal_title_stripped,omitempty"`
	// Title は pane.report_metadata で書いた揮発性のタイトルである（StateLabels と同じ扱い）。
	Title string `json:"title,omitempty"`
	// Tokens は pane.report_metadata で書いた揮発性の付加情報である（StateLabels と同じ扱い）。
	Tokens map[string]string `json:"tokens,omitempty"`
}

// SessionUUID は AgentSession から Claude Code のセッション UUID を取り出す（3-2）。
//
// AgentSession.Kind が AgentSessionKindID のときだけ、その Value をセッション UUID として
// 返す。Kind が AgentSessionKindPath のときはセッションファイルのパスであって UUID では
// ないため、取り出せなかったものとして扱う。
//
// 戻り値: 取り出せた UUID と true。AgentSession が無い場合、Kind が id でない場合、
// Value が空の場合は空文字と false を返す（呼び出し側は「復元できない pane」として扱うこと）。
func (p Pane) SessionUUID() (string, bool) {
	if p.AgentSession == nil {
		return "", false
	}
	if p.AgentSession.Kind != AgentSessionKindID || p.AgentSession.Value == "" {
		return "", false
	}
	return p.AgentSession.Value, true
}

// Agent は herdr が認識している agent である
// （`schemas.success_response.$defs.AgentInfo`）。
//
// 必須は terminal_id / agent_status / workspace_id / tab_id / pane_id / focused /
// revision の7つである。**Name は必須ではない**（名前を付けていない agent では空になる。
// `herdr agent list` の実測でも name の無い項目が返った）。
type Agent struct {
	// Name は agent に付けた名前である（`^[a-z][a-z0-9_-]{0,31}$`）。
	// **名前を付けていない agent では空である。**
	Name string `json:"name,omitempty"`
	// Agent は agent の種別である（例: "claude"）。
	Agent string `json:"agent,omitempty"`
	// AgentStatus は agent の状態である。**`status` ではない。**
	AgentStatus AgentStatus `json:"agent_status"`
	// AgentSession は agent のセッション情報である（Pane.AgentSession と同じ形）。
	AgentSession *AgentSession `json:"agent_session,omitempty"`
	// PaneID は agent が居る pane の ID である。
	PaneID string `json:"pane_id"`
	// TabID は agent が居る tab の ID である。
	TabID string `json:"tab_id"`
	// WorkspaceID は agent が居る workspace の ID である。
	WorkspaceID string `json:"workspace_id"`
	// TerminalID は agent が居る端末の ID である。
	TerminalID string `json:"terminal_id"`
	// Focused は agent の pane がフォーカスされているかどうかである。
	Focused bool `json:"focused"`
	// Revision は pane の内容の版である。
	Revision uint64 `json:"revision"`
	// InteractiveReady は herdr が「対話入力を受け付けられる」と判断したかどうかである。
	// **プロンプトを実際に受け付けられることを意味しない**（信頼確認のダイアログが
	// 出ていても真になりうる。2-1）。
	InteractiveReady bool `json:"interactive_ready,omitempty"`
	// LaunchPending は起動処理がまだ終わっていないかどうかである。
	LaunchPending bool `json:"launch_pending,omitempty"`
	// ScreenDetectionSkipped は画面からの状態検知を飛ばしたかどうかである。
	ScreenDetectionSkipped bool `json:"screen_detection_skipped,omitempty"`
	// StateChangeSeq は状態が変わるたびに増える連番である。
	StateChangeSeq uint64 `json:"state_change_seq,omitempty"`
	// Cwd は agent の pane の作業ディレクトリである。
	Cwd string `json:"cwd,omitempty"`
	// ForegroundCwd は前面のプロセスの作業ディレクトリである。
	ForegroundCwd string `json:"foreground_cwd,omitempty"`
	// DisplayAgent は herdr の UI に表示する agent 名である。
	DisplayAgent string `json:"display_agent,omitempty"`
	// StateLabels は揮発性の状態ラベルである（Pane.StateLabels と同じ扱い）。
	StateLabels map[string]string `json:"state_labels,omitempty"`
	// TerminalTitle は端末が設定したタイトルである。
	TerminalTitle string `json:"terminal_title,omitempty"`
	// TerminalTitleStripped は TerminalTitle から装飾文字を落としたものである。
	TerminalTitleStripped string `json:"terminal_title_stripped,omitempty"`
	// Title は揮発性のタイトルである。
	Title string `json:"title,omitempty"`
	// Tokens は揮発性の付加情報である。
	Tokens map[string]string `json:"tokens,omitempty"`
}

// SessionUUID は Agent の AgentSession から Claude Code のセッション UUID を取り出す
// （Pane.SessionUUID と同じ規則）。
//
// 戻り値: 取り出せた UUID と true。取り出せなければ空文字と false。
func (a Agent) SessionUUID() (string, bool) {
	if a.AgentSession == nil {
		return "", false
	}
	if a.AgentSession.Kind != AgentSessionKindID || a.AgentSession.Value == "" {
		return "", false
	}
	return a.AgentSession.Value, true
}

// WorkspaceWorktree は workspace が開いている worktree の情報である
// （`schemas.success_response.$defs.WorkspaceWorktreeInfo`。5項目とも必須）。
type WorkspaceWorktree struct {
	// RepoKey はリポジトリを一意に指す鍵である（実測では .git のパス）。
	RepoKey string `json:"repo_key"`
	// RepoName はリポジトリ名である。
	RepoName string `json:"repo_name"`
	// RepoRoot はリポジトリの root のパスである。
	RepoRoot string `json:"repo_root"`
	// CheckoutPath はこの workspace が開いている作業ディレクトリのパスである。
	CheckoutPath string `json:"checkout_path"`
	// IsLinkedWorktree が真なら、git worktree で作られた副次の作業ディレクトリである。
	IsLinkedWorktree bool `json:"is_linked_worktree"`
}

// Workspace は herdr の workspace である
// （`schemas.success_response.$defs.WorkspaceInfo`）。
type Workspace struct {
	// WorkspaceID は workspace の ID である（例: "w1"）。worktree.remove に渡す
	// のはこの値である（path でも branch でもない。3-9）。
	WorkspaceID string `json:"workspace_id"`
	// Number は workspace の通し番号である。
	Number uint `json:"number"`
	// Label は workspace に貼られたラベルである。**`owner/repo/issues/N` を書く**（3-3）。
	// **人間が herdr の画面で workspace を見分けるための表示名である。**continuo は読み戻さない。
	// 書き込みは WorkspaceRename で行う。
	Label string `json:"label"`
	// Focused は workspace がフォーカスされているかどうかである。
	Focused bool `json:"focused"`
	// PaneCount は workspace に属する pane の数である。
	PaneCount uint `json:"pane_count"`
	// TabCount は workspace に属する tab の数である。
	TabCount uint `json:"tab_count"`
	// ActiveTabID は現在選ばれている tab の ID である。
	ActiveTabID string `json:"active_tab_id"`
	// AgentStatus は workspace 全体としての agent の状態である。
	AgentStatus AgentStatus `json:"agent_status"`
	// Worktree は workspace が開いている worktree の情報である。worktree でなければ nil。
	Worktree *WorkspaceWorktree `json:"worktree,omitempty"`
	// Tokens は揮発性の付加情報である。
	Tokens map[string]string `json:"tokens,omitempty"`
}

// Tab は herdr の tab である（`schemas.success_response.$defs.TabInfo`）。
type Tab struct {
	// TabID は tab の ID である（例: "w1:t1"）。
	TabID string `json:"tab_id"`
	// WorkspaceID は tab が属する workspace の ID である。
	WorkspaceID string `json:"workspace_id"`
	// Number は tab の通し番号である。
	Number uint `json:"number"`
	// Label は tab に貼られたラベルである。
	Label string `json:"label"`
	// Focused は tab がフォーカスされているかどうかである。
	Focused bool `json:"focused"`
	// PaneCount は tab に属する pane の数である。
	PaneCount uint `json:"pane_count"`
	// AgentStatus は tab 全体としての agent の状態である。
	AgentStatus AgentStatus `json:"agent_status"`
}

// Worktree は git の worktree である
// （`schemas.success_response.$defs.WorktreeInfo`）。
//
// 必須は path / is_bare / is_detached / is_prunable / is_linked_worktree / label の6つで、
// branch と open_workspace_id は省略されうる。
// **フィールド名は `herdr worktree list` の実測（読み取りのみ）でも確認済みである。**
type Worktree struct {
	// Path は worktree の絶対パスである。
	Path string `json:"path"`
	// Label は worktree に付いているラベルである。
	Label string `json:"label"`
	// Branch は worktree が指す branch 名である。detached なら空になりうる。
	Branch string `json:"branch,omitempty"`
	// OpenWorkspaceID は、この worktree を開いている herdr workspace の ID である。
	// 開かれていなければ空である。**片付け（worktree.remove）に渡すのはこの値である**（3-9）。
	OpenWorkspaceID string `json:"open_workspace_id,omitempty"`
	// IsBare が真なら bare リポジトリである。
	IsBare bool `json:"is_bare"`
	// IsDetached が真なら branch を指していない（detached HEAD）。
	IsDetached bool `json:"is_detached"`
	// IsPrunable が真なら git worktree prune の対象である（実体が消えている）。
	IsPrunable bool `json:"is_prunable"`
	// IsLinkedWorktree が真なら git worktree で作られた副次の作業ディレクトリである。
	IsLinkedWorktree bool `json:"is_linked_worktree"`
}

// WorktreeSource は worktree.list が「どのリポジトリを見て答えたか」である
// （`schemas.success_response.$defs.WorktreeSourceInfo`）。
type WorktreeSource struct {
	// RepoKey はリポジトリを一意に指す鍵である（実測では .git のパス）。
	RepoKey string `json:"repo_key"`
	// RepoName はリポジトリ名である。
	RepoName string `json:"repo_name"`
	// RepoRoot はリポジトリの root のパスである。
	RepoRoot string `json:"repo_root"`
	// SourceCheckoutPath は問い合わせの起点になった作業ディレクトリのパスである。
	SourceCheckoutPath string `json:"source_checkout_path"`
	// SourceWorkspaceID は起点の workspace の ID である。無ければ空である。
	SourceWorkspaceID string `json:"source_workspace_id,omitempty"`
}

// PaneRead は pane の画面を読み取った結果である
// （`schemas.success_response.$defs.PaneReadResult`。8項目とも必須）。
type PaneRead struct {
	// PaneID は読み取った pane の ID である。
	PaneID string `json:"pane_id"`
	// WorkspaceID は読み取った pane が属する workspace の ID である。
	WorkspaceID string `json:"workspace_id"`
	// TabID は読み取った pane が属する tab の ID である。
	TabID string `json:"tab_id"`
	// Source は読み取った対象である（要求した ReadSource がそのまま返る）。
	Source ReadSource `json:"source"`
	// Format は読み取った内容の書式である。
	Format ReadFormat `json:"format"`
	// Text は読み取った画面の内容である。
	Text string `json:"text"`
	// Revision は読み取った時点の pane の内容の版である。
	Revision uint64 `json:"revision"`
	// Truncated は要求した行数に届かず切り詰められたかどうかである。
	Truncated bool `json:"truncated"`
}
