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
//
// **worktree 系の3つは Read（既定5秒）ではなく Startup（既定60秒）で待つ。**
// これらは herdr 側で workspace・tab・pane を作ったり `git worktree` を動かしたりする
// 「待ちを伴う呼び出し」であり、pane.rename や pane.list のような即答の呼び出しとは
// 性質が違う（設計 5-2 の `read_timeout_ms` は「待ちを伴う呼び出しには適用しない」）。
// 5秒で切ると、着手の段7（worktree.open）が原因の分かりにくい形で失敗し、しかも herdr 側では
// workspace が作られている可能性がある（片付けの取りこぼしになる）。
// **所要時間の実測はまだ取れていない。**取れたら専用の上限を Timeouts に足すか検討すること。
const (
	// MethodWorktreeCreate は worktree を作って herdr の workspace として開く
	// メソッド名である。
	MethodWorktreeCreate = "worktree.create"
	// MethodWorktreeOpen は**既にある** worktree を herdr の workspace として開く
	// メソッド名である。worktree を作らない（2-1）。
	MethodWorktreeOpen = "worktree.open"
	// MethodWorktreeRemove は worktree を消すメソッド名である。
	MethodWorktreeRemove = "worktree.remove"
)

// WorktreeCreateParams は worktree.create の params である
// （`schemas.request.$defs.WorktreeCreateParams`。引数は path / branch / base / cwd /
// focus / label / workspace_id。**必須の引数は無い**）。
//
// Path が指定できることは実測で確認済みである（3-3: 「worktree.create の path 指定が
// 効くことを実測で確認した」）。
type WorktreeCreateParams struct {
	// Path は herdr の workspace として開く worktree の絶対パスである（3-22 の
	// 置き場所の規則に従って continuo 側で決め打ちする）。
	Path string `json:"path,omitempty"`
	// Branch は worktree が指す branch 名である。
	Branch string `json:"branch,omitempty"`
	// Base は branch を作る元になる ref である。
	Base string `json:"base,omitempty"`
	// Cwd は元のリポジトリの作業ディレクトリである（どのリポジトリの worktree かを
	// herdr に伝えるために使う）。
	Cwd string `json:"cwd,omitempty"`
	// Focus は作った workspace へフォーカスを移すかどうかである。continuo は tab を
	// フォーカスしない運用なので偽を送る（2-1）。送らない（nil）と herdr の既定
	// （実スキーマの default は false）に従う。
	// **明示的に偽を送る必要があるのでポインタで持つ**（bool + omitempty だと偽を送れない）。
	Focus *bool `json:"focus,omitempty"`
	// Label は workspace に貼るラベルである。**issue の URL を書く**（3-3）。
	Label string `json:"label,omitempty"`
	// WorkspaceID は開き先の workspace の ID である。
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// WorktreeCreateResult は worktree.create の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 応答のスキーマ自体は実在するが、**スキーマは「どのメソッドがどの変種を返すか」を
// 書いていない。**変種 `worktree_created`
// （`{"type":"worktree_created","workspace":…,"tab":…,"root_pane":…,"worktree":…}`）を
// 返すものとして解釈する。推定が外れていれば Type で気づける。
//
// **以前この型が持っていた workspace_id / path を result の直下に置く形は誤りだった。**
// 片付け（worktree.remove）に渡す herdr workspace の ID（3-9）は
// Workspace.WorkspaceID にある。開かれた worktree の絶対パスは Worktree.Path にある。
type WorktreeCreateResult struct {
	// Type は応答の変種を表す判別子である（推定: "worktree_created"）。
	Type string `json:"type"`
	// Workspace は開かれた herdr workspace である。**worktree.remove の引数に使う
	// ID はここにある**（3-9。path でも branch でもなく Workspace.WorkspaceID を渡す）。
	Workspace Workspace `json:"workspace"`
	// Tab は workspace の中に作られた tab である。
	Tab Tab `json:"tab"`
	// RootPane は tab の中に作られた最初の pane である。
	// **agent を起動する pane はここから取る**（別途 pane.split しない場合）。
	RootPane Pane `json:"root_pane"`
	// Worktree は作られた git の worktree である。絶対パスは Worktree.Path にある。
	Worktree Worktree `json:"worktree"`
}

// WorktreeCreate は worktree.create を呼び、worktree を herdr の workspace として開く。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 開く worktree のパス・branch など。
// 戻り値: 開かれた workspace・tab・pane・worktree。herdr のエラー応答は *Error として返る。
func (c *Client) WorktreeCreate(ctx context.Context, params WorktreeCreateParams) (*WorktreeCreateResult, error) {
	raw, err := c.call(ctx, MethodWorktreeCreate, params, c.timeouts.Startup)
	if err != nil {
		return nil, err
	}
	var result WorktreeCreateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodWorktreeCreate, err)
	}
	return &result, nil
}

// WorktreeOpenParams は worktree.open の params である
// （`schemas.request.$defs.WorktreeOpenParams`。引数は path / branch / cwd / focus /
// label / workspace_id。**必須の引数は無い**）。
//
// **worktree.create との違いは「作らない」ことである**（2-1）。git 側での worktree の
// 作成（`git worktree add` 等）を continuo が自分で行う場合（3-22）や、
// 再起動後に既にある worktree を開き直す場合（3-4）はこちらを使う。
// base が引数に無いのはそのためである。
type WorktreeOpenParams struct {
	// Path は herdr の workspace として開く、既にある worktree の絶対パスである。
	//
	// **Path と Branch は片方だけ埋める。**両方渡すと herdr が
	// `invalid_request: exactly one of path or branch is required` で弾く（実測: 2026-08-20）。
	Path string `json:"path,omitempty"`
	// Branch は worktree が指す branch 名である。**Path を埋めたなら空にする。**
	Branch string `json:"branch,omitempty"`
	// Cwd は元のリポジトリの作業ディレクトリである。
	Cwd string `json:"cwd,omitempty"`
	// Focus は開いた workspace へフォーカスを移すかどうかである
	// （WorktreeCreateParams.Focus と同じ理由でポインタで持つ）。
	Focus *bool `json:"focus,omitempty"`
	// Label は workspace に貼るラベルである。**issue の URL を書く**（3-3）。
	Label string `json:"label,omitempty"`
	// WorkspaceID は開き先の workspace の ID である。
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// WorktreeOpenResult は worktree.open の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 変種 `worktree_opened`（worktree_created と同じ4つに `already_open` が加わったもの）を
// 返すものとして解釈する。
type WorktreeOpenResult struct {
	// Type は応答の変種を表す判別子である（推定: "worktree_opened"）。
	Type string `json:"type"`
	// Workspace は開かれた herdr workspace である。worktree.remove に渡す ID はここにある。
	Workspace Workspace `json:"workspace"`
	// Tab は workspace の中の tab である。
	Tab Tab `json:"tab"`
	// RootPane は tab の中の最初の pane である。
	RootPane Pane `json:"root_pane"`
	// Worktree は開かれた git の worktree である。
	Worktree Worktree `json:"worktree"`
	// AlreadyOpen が真なら、その worktree は既に herdr で開かれていて、
	// この呼び出しでは新しく開いていない。**再起動後の復元（3-4）で、
	// 二重に開いてしまっていないかの判定に使える。**
	AlreadyOpen bool `json:"already_open"`
}

// WorktreeOpen は worktree.open を呼び、**既にある** worktree を herdr の workspace として
// 開く（worktree は作らない。2-1）。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 開く worktree のパス・branch など。
// 戻り値: 開かれた workspace・tab・pane・worktree と、既に開かれていたかどうか。
// herdr のエラー応答は *Error として返る。
func (c *Client) WorktreeOpen(ctx context.Context, params WorktreeOpenParams) (*WorktreeOpenResult, error) {
	raw, err := c.call(ctx, MethodWorktreeOpen, params, c.timeouts.Startup)
	if err != nil {
		return nil, err
	}
	var result WorktreeOpenResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodWorktreeOpen, err)
	}
	return &result, nil
}

// WorktreeRemoveParams は worktree.remove の params である
// （`schemas.request.$defs.WorktreeRemoveParams`。引数は workspace_id と force。
// **workspace_id が必須である**）。
//
// 引数が path でも branch でもなく workspace_id であることは実測で確認済みである
// （3-9: 「引数は path でも branch でもなく herdr workspace の ID である（実測）」）。
type WorktreeRemoveParams struct {
	// WorkspaceID は worktree.create / worktree.open が返した herdr workspace の ID である。
	// path や branch 名では herdr の worktree.remove API を呼べない（3-9 の実測）。
	WorkspaceID string `json:"workspace_id"`
	// Force は未コミットの変更があっても消すかどうかである。
	Force bool `json:"force,omitempty"`
}

// WorktreeRemoveResult は worktree.remove の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 変種 `worktree_removed`
// （`{"type":"worktree_removed","workspace_id":…,"path":…,"forced":…}`）を返すものとして
// 解釈する。
//
// **以前この型が持っていた removed というフィールドは実スキーマのどの変種にも無い。**
// 消えた worktree のパスは Path、force が効いたかどうかは Forced で分かる。
type WorktreeRemoveResult struct {
	// Type は応答の変種を表す判別子である（推定: "worktree_removed"）。
	Type string `json:"type"`
	// WorkspaceID は消した worktree を開いていた herdr workspace の ID である。
	WorkspaceID string `json:"workspace_id"`
	// Path は消した worktree の絶対パスである。
	Path string `json:"path"`
	// Forced は未コミットの変更を押し切って消したかどうかである。
	Forced bool `json:"forced"`
}

// WorktreeRemove は worktree.remove を呼び、worktree を消す。
//
// herdr workspace として開いていない worktree はこの API では消せない（3-9）。
// branch の削除はこの API の範囲外であり、continuo が別途 `git branch -D` を実行する
// 必要がある（3-9。herdr は branch を消さない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 消す workspace の ID（path でも branch でもない。3-9）。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) WorktreeRemove(ctx context.Context, params WorktreeRemoveParams) (*WorktreeRemoveResult, error) {
	raw, err := c.call(ctx, MethodWorktreeRemove, params, c.timeouts.Startup)
	if err != nil {
		return nil, err
	}
	var result WorktreeRemoveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s の応答を解析できません: %w", MethodWorktreeRemove, err)
	}
	return &result, nil
}
