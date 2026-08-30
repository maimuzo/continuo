package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/instance"
)

// InstanceMarkerName は、`--id` を付けた continuo が自分の置き場所の直下に置く目印の
// ファイル名である（設計 3-17f）。
//
// **これが無いと、既定の `continuo abandon` が二度と何も片付けられなくなる。**
// `--id e2e` の worktree は `<workspace.root>/e2e/<host>/<owner>/<repo>/<スラグ>` に置かれる。
// 既定側の走査は `<workspace.root>` から**ちょうど4階層**を返すので（scan.go の scanDepth）、
// `<workspace.root>/e2e/<host>/<owner>/<repo>` が返る。**そこに身元ファイルは無いので
// `ScanUnidentified` が拾い、`continuo abandon` は「身元ファイルが無いディレクトリがあります」
// として、判断を保留したまま止まる。**
//
// **このファイル1つでは飛ばさない**（isOtherInstanceRoot を見よ）。
// **worktree の中ではエージェントが `--permission-mode dontAsk` で動く**ので、
// **置き場所の中に置かれたファイルは、それだけでは主張の裏付けにならない。**
const InstanceMarkerName = ".continuo-instance"

// instanceMarker は目印のファイルの中身である。
//
// **`id` を書くのは、ディレクトリ名および `~/.continuo/id/<名前>/` と突き合わせるためである。**
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

// InstanceRegistryDir は `--id` ごとの置き場所が並ぶディレクトリ（`~/.continuo/id`）を返す。
//
// **`--id <名前>` を1度でも使った continuo は、必ずここに `<名前>/` を持つ**
// （`instance.Layout.EnsureLockDir` がロックを置く前に作る）。**`workspace.root` の
// 外にあり、`--permission-mode dontAsk` のエージェントが相対パスで届く場所ではない。**
//
// homeDir: `~` として使うディレクトリ（Manager が持っているもの）。
// 戻り値: `<homeDir>/.continuo/id` の絶対パス。
func InstanceRegistryDir(homeDir string) string {
	return filepath.Join(homeDir, instance.DirName, instance.IDDirName)
}

// isOtherInstanceRoot は、dir が別の continuo（`--id <dir の名前>`）の置き場所かを見る。
//
// **走査は、真を返したディレクトリの下へ1階層も入らない。**そこにあるものは
// 別の continuo のものであり、**こちらが数えても消しても判断しても、すべて誤りである。**
//
// **3つ揃ったときだけ飛ばす**（設計 3-17f）。
//
//  1. 目印が読めて、その `id` が dir の名前と一致する
//  2. その名前が `--id` として使える形である（instance.ValidateID）
//  3. `~/.continuo/id/<名前>/` が実在する（その名前で continuo が動いた証拠）
//
// **1つだけでは足りない。**目印は `workspace.root` の中にあり、そこでは
// エージェントが `--permission-mode dontAsk` で動く。**worktree から4つ上へ
// `.continuo-instance` を書いて `{"id":"github.com"}` と名乗るだけで、
// `github.com` の下の worktree が3つの走査から全部消えていた。**そうなると復元は0件になり、
// **`continuo abandon` は「worktree が無い」経路に入って、生きている worktree の
// branch を消しにいく。**
//
// **2 がその名乗りを止める**（`github.com` は `.` を含むので `--id` に書けない）。
// **3 は、名乗った名前で continuo が実際に動いたことを、置き場所の外で確かめる。**
//
// **防ぎきれないもの。**`~/.continuo/id/` へ絶対パスでディレクトリを作れる相手には、
// この3つとも揃えられる。**ただしその相手は同じ手で flock も覚え書きも置き換えられる**ので、
// そのときは `--id` による隔離そのものが既に成立していない。
// **ここで守るのは「worktree の中から相対パスで書ける範囲」である。**
//
// registryDir: `~/.continuo/id` の絶対パス（InstanceRegistryDir が返すもの）。
// dir: 置き場所の直下にあるディレクトリの絶対パス。
// 戻り値: 3つとも揃えば true。**1つでも欠ければ false**（飛ばさずに、いつもどおり数える）。
func isOtherInstanceRoot(registryDir, dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, InstanceMarkerName))
	if err != nil {
		return false
	}
	var marker instanceMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false
	}
	if marker.ID == "" || marker.ID != filepath.Base(dir) {
		return false
	}
	// **`--id` に書けない名前は名乗れない。**ホスト名（`github.com`）はここで落ちる。
	if err := instance.ValidateID(marker.ID); err != nil {
		return false
	}
	// **その名前で continuo が動いた証拠を、置き場所の外で確かめる。**
	info, err := os.Stat(filepath.Join(registryDir, marker.ID))
	return err == nil && info.IsDir()
}
