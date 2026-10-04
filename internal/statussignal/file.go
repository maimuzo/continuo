package statussignal

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileName は、取り得る値を `continuo hook` へ渡すファイルの名前である（issue #274）。
//
// 置き場所は issue ごとのディレクトリの直下である。
//
//	<実行時ディレクトリ>/issues/<issue のスラグ>/status-signal.json
//
// **hook の逃がし先（`pending/`）の中には置かない。**本体は逃がし先の `.json` を
// 「届かなかった hook」として読むので、そこへ置くと hook の1件として配られる。
const FileName = "status-signal.json"

// File は、取り得る値を `continuo hook` へ渡すファイルの中身である（issue #274）。
//
// **引数ではなくファイルで渡す。**hook の引数を足すと、古い実行ファイルへ戻したときに、
// 既に書かれた設定ファイルの hook が知らない引数で落ち、turn の終わりが届かなくなる。
// ファイルなら、古い hook は読まないだけである。
//
// **この形を変える変更は、hook と本体の約束を変える。**動いている本体が書いたファイルを、
// 新しくビルドした hook が読むためである（`CLAUDE.md` の「hook の挙動が変化する変更」）。
type File struct {
	// Identifier は、いま作業している issue の識別子である（`<owner>/<repo>#<番号>`）。
	// **対象を書かない表明の行と、`#<番号>` だけの対象を、本体と同じに解決するために要る。**
	// 無いと、hook は `#45 review` の `#45` を値と読み、正しい表明を差し戻す。
	Identifier string `json:"identifier"`
	// Prefix は表明の印である（`tracker.status_signal_prefix`）。
	Prefix string `json:"prefix"`
	// Values は取り得る値と行き先である（`tracker.status_signal_map`）。
	// 行き先が null の値は、Status を動かさない。
	Values map[string]*string `json:"values"`
}

// PathInIssueDir は、issue ごとのディレクトリから、ファイルのパスを返す。
//
// issueDir: issue ごとのディレクトリの絶対パス。
// 戻り値: ファイルの絶対パス。
func PathInIssueDir(issueDir string) string {
	return filepath.Join(issueDir, FileName)
}

// PathFromPendingDir は、hook の逃がし先のパスから、ファイルのパスを返す。
//
// **hook が知っている場所は `--socket` と `--pending-dir` の2つだけである。**
// 逃がし先は issue ごとのディレクトリの直下に在るので、その親を引く。
//
// pendingDir: `--pending-dir` に渡された絶対パス。
// 戻り値: ファイルの絶対パス。
func PathFromPendingDir(pendingDir string) string {
	return PathInIssueDir(filepath.Dir(filepath.Clean(pendingDir)))
}

// Encode はファイルの中身を JSON にする。
//
// f: 書く中身。
// 戻り値: 末尾に改行を付けた JSON と、JSON 化できなかった場合のエラー。
func Encode(f File) ([]byte, error) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Read はファイルを読む。
//
// **使える中身かどうかは `Usable` で確かめること。**読めたことと、調べる材料が
// 揃っていることは別である。
//
// path: ファイルの絶対パス。
// 戻り値: 読んだ中身と、無い・読めない・JSON として解釈できない場合のエラー。
func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Usable は、表明を調べる材料が揃っているかを返す。
//
// **1つでも欠けていたら、hook は調べない。**識別子が無いと対象を本体と同じに解決できず、
// 印が無いと行を拾えず、値が無いと全部の表明が決まり以外になる。
func (f File) Usable() bool {
	return f.Identifier != "" && f.Prefix != "" && len(f.Values) > 0
}
