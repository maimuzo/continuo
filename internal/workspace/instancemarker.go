package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
)

// InstanceMarkerName は、`--id` を付けた continuo が自分の置き場所の直下に置く目印の
// ファイル名である（設計 3-17b）。
//
// **これが無いと、既定の `continuo abandon` が二度と何も片付けられなくなる。**
// `--id e2e` の worktree は `<workspace.root>/e2e/<host>/<owner>/<repo>/<スラグ>` に置かれる。
// 既定側の走査は `<workspace.root>` から**ちょうど4階層**を返すので（scan.go の scanDepth）、
// `<workspace.root>/e2e/<host>/<owner>/<repo>` が返る。**そこに身元ファイルは無いので
// `ScanUnidentified` が拾い、`continuo abandon` は「身元ファイルが無いディレクトリがあります」
// として、判断を保留したまま止まる。**
//
// **名前の一覧を持つ形にはしない。**既定側は、どんな `--id` が使われたかを知らない
// （`--id` は起動のたびに人間が付けるフラグであって、どこにも登録されない）。
// **身元ファイルの有無で当てる形にもしない。**着手の途中で落ちた worktree には
// 身元ファイルが無いので、そのときだけ当たらなくなる。
// **置いた側が名乗る形にすれば、当てる必要そのものが無くなる。**
const InstanceMarkerName = ".continuo-instance"

// instanceMarker は目印のファイルの中身である。
//
// **`id` を書くのは、ディレクトリ名と突き合わせるためである。**
// worktree の中ではエージェントが `--permission-mode dontAsk` で動くので、
// **このファイルは書かれうる。**中身とディレクトリ名が一致することを条件にすれば、
// **飛ばさせたい場所に、その名前の `--id` を使ったという主張を置く**必要が生じ、
// でたらめな1バイトでは飛ばせなくなる。
type instanceMarker struct {
	// ID は `--id` に渡された名前である（置き場所の最下層のディレクトリ名と同じ）。
	ID string `json:"id"`
}

// WriteInstanceMarker は `--id` を付けた continuo の置き場所に目印を書く。
//
// **`--id` を付けた起動と `continuo abandon --id` の両方が、必ず通ること。**
// 片方だけが書くと、目印がある起動と無い起動が混ざる。
//
// 書き出す先とサンプル（`--id e2e`、`workspace.root` が `~/worktrees` のとき）:
//
//	~/worktrees/e2e/.continuo-instance
//
//	{
//	  "id": "e2e"
//	}
//
// root: シンボリックリンクを解決済みの置き場所（`<元の workspace.root>/<名前>`）。
// id: `--id` に渡された名前。
// 戻り値: 書けなかった場合のエラー。**起動を止める。**書けないまま動かすと、
// 既定側の `continuo abandon` が片付けられなくなる。
func WriteInstanceMarker(root, id string) error {
	data, err := json.MarshalIndent(instanceMarker{ID: id}, "", "  ")
	if err != nil {
		return i18n.Errorf(i18n.KeyWorkspaceInstanceMarkerWriteFailed, root, err)
	}
	data = append(data, '\n')
	// **一時ファイルへ書き切ってから差し替える**（CLAUDE.md の「絶対に守る制約」4）。
	if err := atomicfile.Write(filepath.Join(root, InstanceMarkerName), data, 0o600); err != nil {
		return i18n.Errorf(i18n.KeyWorkspaceInstanceMarkerWriteFailed, root, err)
	}
	return nil
}

// isOtherInstanceRoot は、dir が別の continuo（`--id <dir の名前>`）の置き場所かを見る。
//
// **走査は、真を返したディレクトリの下へ1階層も入らない。**そこにあるものは
// 別の continuo のものであり、**こちらが数えても消しても判断しても、すべて誤りである。**
//
// dir: 置き場所の直下にあるディレクトリの絶対パス。
// 戻り値: 目印が読めて、その `id` が dir の名前と一致すれば true。
// **読めない・壊れている・名前が食い違うときは false**（飛ばさずに、いつもどおり数える）。
func isOtherInstanceRoot(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, InstanceMarkerName))
	if err != nil {
		return false
	}
	var marker instanceMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false
	}
	return marker.ID != "" && marker.ID == filepath.Base(dir)
}
