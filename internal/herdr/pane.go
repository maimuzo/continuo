package herdr

import (
	"context"
	"encoding/json"
	"fmt"
)

// メソッド名・params の引数名・result の形は 2026-08-18 に `herdr api schema --json` で
// 確認済みである（docs/plans/continuo_design.md 2-1 の
// 「socket API の実在するメソッドと引数」。protocol=19 / herdr 0.8.0）。
// result に出てくる値の形は types.go を参照すること。
const (
	// MethodPaneSplit は pane を作るメソッド名である。
	MethodPaneSplit = "pane.split"
	// MethodPaneClose は pane を閉じるメソッド名である。
	MethodPaneClose = "pane.close"
	// MethodPaneRename は pane に label を書くメソッド名である。
	// **pane.split では label を書けない**ので、pane を作った直後にこれを呼ぶ（3-3）。
	MethodPaneRename = "pane.rename"
	// MethodPaneList は pane の一覧を取るメソッド名である。
	MethodPaneList = "pane.list"
	// MethodPaneReportAgent は、実プロセスを起動せずに「agent が居る pane」として
	// 登録するメソッド名である（2-1。統合テストで使う）。
	MethodPaneReportAgent = "pane.report_agent"
)

// PaneSplitParams は pane.split の params である
// （`schemas.request.$defs.PaneSplitParams`。引数は direction / cwd / env / focus /
// ratio / target_pane_id / workspace_id。**必須は direction だけである**）。
//
// **label はここでは渡せない**（引数に無い）。pane に label を書くのは PaneRename である。
type PaneSplitParams struct {
	// Direction は分割方向である。**必須の引数である。**
	// 値は "right" か "down" のどちらかである（実スキーマの SplitDirection の enum）。
	Direction string `json:"direction"`
	// Cwd は新しい pane の作業ディレクトリである。worktree のパスを渡す（3-22）。
	Cwd string `json:"cwd,omitempty"`
	// Env は新しい pane のプロセスへ渡す環境変数である
	// （設定の claude.env。例: CLAUDE_CODE_RETRY_WATCHDOG=1。3-11）。
	// **環境変数を渡せるのはここだけである。**`agent.start` に env という引数は無い（2-1）。
	//
	// なお herdr は pane を作るときに渡した環境変数を API から読み戻せない（3-3）ので、
	// ここに載せた値を後から確認する経路は無い。
	Env map[string]string `json:"env,omitempty"`
	// Focus は新しい pane へフォーカスを移すかどうかである。continuo は tab を
	// フォーカスしない運用なので偽を送る（2-1）。送らない（nil）と herdr の既定
	// （実スキーマの default は false）に従う。
	// **明示的に偽を送る必要があるのでポインタで持つ**（bool + omitempty だと偽を送れない）。
	Focus *bool `json:"focus,omitempty"`
	// Ratio は分割の比率である。0 なら送らず、herdr の既定に任せる。
	Ratio float64 `json:"ratio,omitempty"`
	// TargetPaneID は分割の基準にする pane の ID である。空なら送らず、
	// herdr の既定（呼び出し元の pane）に任せる。
	TargetPaneID string `json:"target_pane_id,omitempty"`
	// WorkspaceID は pane を作る workspace の ID である
	// （worktree.create / worktree.open が返した ID）。
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// PaneSplitResult は pane.split の result である。
//
// **形は確認済みである。**herdr 本体が配っている skill 文書（`herdr --skill`）が
// 「`pane split` returns the new pane as `.result.pane`」（原文。訳: `pane split` は
// 新しい pane を `.result.pane` として返す）と明記している。実スキーマの変種でいうと
// `pane_info`（`{"type":"pane_info","pane":PaneInfo}`）である。
type PaneSplitResult struct {
	// Type は応答の変種を表す判別子である（"pane_info"）。
	Type string `json:"type"`
	// Pane は新しく作られた pane である。
	Pane Pane `json:"pane"`
}

// PaneSplit は pane.split を呼び、新しい pane を作る。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 分割方向（必須）・cwd・環境変数など。
// 戻り値: 作られた pane の情報。herdr のエラー応答は *Error として返る
// （IsCode で判定できる）。
func (c *Client) PaneSplit(ctx context.Context, params PaneSplitParams) (*PaneSplitResult, error) {
	raw, err := c.call(ctx, MethodPaneSplit, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result PaneSplitResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodPaneSplit, err)
	}
	return &result, nil
}

// PaneRenameParams は pane.rename の params である
// （`schemas.request.$defs.PaneRenameParams`。引数は pane_id と label。
// **必須は pane_id だけである**）。
//
// **これが pane に label を書く唯一の経路である**（`pane.split` に label は無い）。
// 設計 3-3 は「pane の label に issue の URL を書く」を、再起動後の復元の第2の経路と
// 定めている。pane を作った直後にこれを呼ぶこと。
type PaneRenameParams struct {
	// PaneID は label を書き込む pane の ID である。**必須の引数である。**
	PaneID string `json:"pane_id"`
	// Label は pane に書くラベルである。**issue の URL を書く**（3-3）。
	// 空なら送らず、herdr の既定に任せる（実スキーマ上は省略可能）。
	Label string `json:"label,omitempty"`
}

// PaneRenameResult は pane.rename の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 応答のスキーマ自体は実在するが、**スキーマは「どのメソッドがどの変種を返すか」を
// 書いていない。**pane を対象にした更新なので `pane_info`
// （`{"type":"pane_info","pane":PaneInfo}`）を返すものとして解釈する。
// 推定が外れていれば Type が "pane_info" 以外になるので、呼び出し側で気づける。
type PaneRenameResult struct {
	// Type は応答の変種を表す判別子である（推定: "pane_info"）。
	Type string `json:"type"`
	// Pane は label を書いたあとの pane である。
	Pane Pane `json:"pane"`
}

// PaneRename は pane.rename を呼び、pane に label を書く（3-3 の復元の第2の経路）。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: pane の ID（必須）と書き込む label。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) PaneRename(ctx context.Context, params PaneRenameParams) (*PaneRenameResult, error) {
	raw, err := c.call(ctx, MethodPaneRename, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result PaneRenameResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodPaneRename, err)
	}
	return &result, nil
}

// PaneCloseParams は pane.close の params である
// （`schemas.request.$defs.PaneTarget`。**引数は pane_id だけで、必須である**）。
type PaneCloseParams struct {
	// PaneID は閉じる pane の ID である。
	PaneID string `json:"pane_id"`
}

// PaneCloseResult は pane.close の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 実スキーマの変種のうち、閉じたことだけを伝える形は `ok`（`{"type":"ok"}`。
// **type 以外のフィールドを持たない**）である。これを返すものとして解釈する。
// **`closed` というフィールドは実スキーマのどの変種にも存在しない**ので持たせていない。
type PaneCloseResult struct {
	// Type は応答の変種を表す判別子である（推定: "ok"）。
	Type string `json:"type"`
}

// PaneClose は pane.close を呼び、pane を閉じる。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 閉じる pane の ID。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) PaneClose(ctx context.Context, params PaneCloseParams) (*PaneCloseResult, error) {
	raw, err := c.call(ctx, MethodPaneClose, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result PaneCloseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodPaneClose, err)
	}
	return &result, nil
}

// PaneListParams は pane.list の params である
// （`schemas.request.$defs.PaneListParams`。**引数は workspace_id だけで、必須ではない**）。
type PaneListParams struct {
	// WorkspaceID は一覧を絞り込む workspace の ID である。空なら全 workspace が対象。
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// PaneListResult は pane.list の result である。
//
// **形は実測で確認済みである。**`herdr pane list`（読み取りのみ）の応答は
// `{"id":"cli:pane:list","result":{"panes":[…],"type":"pane_list"}}` であった。
type PaneListResult struct {
	// Type は応答の変種を表す判別子である（"pane_list"）。
	Type string `json:"type"`
	// Panes は一覧である。
	Panes []Pane `json:"panes"`
}

// PaneList は pane.list を呼び、pane の一覧を取る。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 絞り込み条件。ゼロ値なら全件。
// 戻り値: pane の一覧。herdr のエラー応答は *Error として返る。
func (c *Client) PaneList(ctx context.Context, params PaneListParams) (*PaneListResult, error) {
	raw, err := c.call(ctx, MethodPaneList, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result PaneListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodPaneList, err)
	}
	return &result, nil
}

// PaneReportAgentParams は pane.report_agent の params である
// （`schemas.request.$defs.PaneReportAgentParams`。必須は pane_id / source / agent /
// state の4つ。ほかに agent_session_id / agent_session_path / message / seq がある）。
//
// **このメソッドは、実プロセスを起動せずに任意の pane を「agent が居る pane」として
// 登録する**（2-1）。統合テストで、実際に Claude Code を起動せずに状態遷移を
// 再現するために使う。
type PaneReportAgentParams struct {
	// PaneID は agent が居ることにする pane の ID である。
	PaneID string `json:"pane_id"`
	// Source は状態の報告元である。**実スキーマ上は任意の文字列で、enum ではない。**
	Source string `json:"source"`
	// Agent は agent の種別である（例: "claude"）。
	Agent string `json:"agent"`
	// State は agent の状態である。**AgentStatus ではなく PaneAgentState である**
	// （done が無く、idle / working / blocked / unknown の4つだけ）。
	State PaneAgentState `json:"state"`
	// AgentSessionID は Claude Code のセッション UUID である。
	// Pane.AgentSession から読み戻す経路（3-2・3-4）をテストで再現するときに使う。
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// AgentSessionPath はセッションファイルのパスである。
	AgentSessionPath string `json:"agent_session_path,omitempty"`
	// Message は状態に添える説明である。
	Message string `json:"message,omitempty"`
	// Seq は報告の順序を示す連番である。0 なら送らない。
	Seq uint64 `json:"seq,omitempty"`
}

// PaneReportAgentResult は pane.report_agent の result である。
//
// 【メソッドと応答の対応づけは推定である】
// pane を対象にした更新なので `pane_info`（`{"type":"pane_info","pane":PaneInfo}`）を
// 返すものとして解釈する。推定が外れていれば Type で気づける。
type PaneReportAgentResult struct {
	// Type は応答の変種を表す判別子である（推定: "pane_info"）。
	Type string `json:"type"`
	// Pane は登録後の pane である。
	Pane Pane `json:"pane"`
}

// PaneReportAgent は pane.report_agent を呼び、実プロセスを起動せずに pane を
// 「agent が居る pane」として登録する（2-1。統合テストで使う）。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: pane の ID・報告元・agent の種別・状態（4つとも必須）。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) PaneReportAgent(
	ctx context.Context,
	params PaneReportAgentParams,
) (*PaneReportAgentResult, error) {
	raw, err := c.call(ctx, MethodPaneReportAgent, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result PaneReportAgentResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodPaneReportAgent, err)
	}
	return &result, nil
}
