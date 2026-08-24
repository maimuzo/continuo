package herdr

import (
	"context"
	"encoding/json"

	"github.com/maimuzo/continuo/internal/i18n"
)

// メソッド名・params の引数名・result の形は 2026-08-18 に `herdr api schema --json` で
// 確認済みである（docs/plans/continuo_design.md 2-1 の
// 「socket API の実在するメソッドと引数」。protocol=19 / herdr 0.8.0）。

// MethodWorkspaceList は herdr の workspace の一覧を取るメソッド名である。
//
// 引数は無い（`herdr api schema --json` の request に WorkspaceListParams が無い）。
const MethodWorkspaceList = "workspace.list"

// MethodWorkspaceClose は herdr workspace を閉じるメソッド名である。
//
// **worktree.remove では閉じきれない workspace があるので要る。**
// worktree.open に cwd を渡すと、herdr は「その worktree の workspace」に加えて
// 「cwd のリポジトリの workspace」も開く（実測: 2026-08-24。test/live で確認した）。
// worktree.remove は前者しか閉じないので、後者はこれで閉じる。
// 引数は `schemas.request.$defs.WorkspaceTarget`（workspace_id のみ。必須）である。
const MethodWorkspaceClose = "workspace.close"

// WorkspaceListResult は workspace.list の result である。
//
// **形は実測で確認済みである。**`herdr workspace list`（読み取りのみ）の応答は
// `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[…]}}` であった。
type WorkspaceListResult struct {
	// Type は応答の変種を表す判別子である（"workspace_list"）。
	Type string `json:"type"`
	// Workspaces は一覧である。
	Workspaces []Workspace `json:"workspaces"`
}

// WorkspaceList は workspace.list を呼び、herdr の workspace の一覧を取る。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: workspace の一覧。herdr のエラー応答は *Error として返る。
func (c *Client) WorkspaceList(ctx context.Context) (*WorkspaceListResult, error) {
	raw, err := c.call(ctx, MethodWorkspaceList, nil, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result WorkspaceListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodWorkspaceList, err)
	}
	return &result, nil
}

// WorkspaceCloseParams は workspace.close の params である
// （`schemas.request.$defs.WorkspaceTarget`。**引数は workspace_id だけで、必須である**）。
type WorkspaceCloseParams struct {
	// WorkspaceID は閉じる workspace の ID である。
	WorkspaceID string `json:"workspace_id"`
}

// WorkspaceCloseResult は workspace.close の result である。
//
// 実測の応答は `{"id":"cli:workspace:close","result":{"type":"ok"}}` であった
// （変種 `ok` は type 以外のフィールドを持たない）。
type WorkspaceCloseResult struct {
	// Type は応答の変種を表す判別子である（"ok"）。
	Type string `json:"type"`
}

// WorkspaceClose は workspace.close を呼び、herdr workspace を閉じる。
//
// **worktree の実体は消さない。**worktree ごと消すのは WorktreeRemove である。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 閉じる workspace の ID。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) WorkspaceClose(ctx context.Context, params WorkspaceCloseParams) (*WorkspaceCloseResult, error) {
	raw, err := c.call(ctx, MethodWorkspaceClose, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result WorkspaceCloseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodWorkspaceClose, err)
	}
	return &result, nil
}

// MethodWorkspaceRename は herdr workspace に label を書くメソッド名である。
//
// worktree.create / worktree.open は label を引数で渡せるが、**開いたあとに label を
// 書き換える経路はこれである。**設計 3-3 は workspace の label に issue の URL を書くと
// 定めている（再起動後の復元の第1の経路）。
const MethodWorkspaceRename = "workspace.rename"

// WorkspaceRenameParams は workspace.rename の params である
// （`schemas.request.$defs.WorkspaceRenameParams`。引数は workspace_id と label で、
// **2つとも必須である**）。
type WorkspaceRenameParams struct {
	// WorkspaceID は label を書き込む herdr workspace の ID である。**必須の引数である。**
	WorkspaceID string `json:"workspace_id"`
	// Label は workspace に書くラベルである。**issue の URL を書く**（3-3）。
	// **必須の引数である**ので、空でも送る（omitempty を付けない）。
	Label string `json:"label"`
}

// WorkspaceRenameResult は workspace.rename の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 応答のスキーマ自体は実在するが、**スキーマは「どのメソッドがどの変種を返すか」を
// 書いていない。**workspace を対象にした更新なので `workspace_info`
// （`{"type":"workspace_info","workspace":WorkspaceInfo}`）を返すものとして解釈する。
// 推定が外れていれば Type が "workspace_info" 以外になるので、呼び出し側で気づける。
type WorkspaceRenameResult struct {
	// Type は応答の変種を表す判別子である（推定: "workspace_info"）。
	Type string `json:"type"`
	// Workspace は label を書いたあとの workspace である。
	Workspace Workspace `json:"workspace"`
}

// WorkspaceRename は workspace.rename を呼び、herdr workspace に label を書く
// （3-3 の復元の第1の経路）。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: workspace の ID と label（2つとも必須）。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る。
func (c *Client) WorkspaceRename(
	ctx context.Context,
	params WorkspaceRenameParams,
) (*WorkspaceRenameResult, error) {
	raw, err := c.call(ctx, MethodWorkspaceRename, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result WorkspaceRenameResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodWorkspaceRename, err)
	}
	return &result, nil
}
