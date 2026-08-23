package herdr

import (
	"context"
	"encoding/json"

	"github.com/maimuzo/continuo/internal/i18n"
)

// メソッド名・params の引数名・result の形は 2026-08-18 に `herdr api schema --json` で
// 確認済みである（docs/plans/continuo_design.md 2-1 の
// 「socket API の実在するメソッドと引数」。protocol=19 / herdr 0.8.0）。

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
