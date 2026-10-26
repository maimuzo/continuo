// Package agentplugin は、continuo が起動した Claude Code に渡す plugin
// `continuo-agent-procedures` を実行時ディレクトリへ書き出し、古い版を片付ける（issue #288）。
//
// **置き場所は次のとおりである。**
//
//	<実行時ディレクトリ>/plugins/<12桁>/continuo-agent-procedures/   ← --plugin-dir に渡す
//	<実行時ディレクトリ>/plugins/<12桁>/.orphaned_at                  ← 古い版になった時刻（現役の版には無い）
//	<実行時ディレクトリ>/plugins/.tmp-<乱数>/                          ← 初めて書き出すときの一時の場所
//	<実行時ディレクトリ>/issues/<issue>/system-prompt.md              ← その issue のシステムプロンプト
//
// **12桁は plugin の中身のハッシュである。**中身が変われば置き場所も変わるので、
// continuo を上げたあとも、前の版で起動した pane の Claude Code は前の版のディレクトリを読み続けられる。
//
// **書き出しは、Claude Code を起動する前に毎回行う。**ファイルを1つずつ期待値と比べ、
// 無いか違えば同じディレクトリの一時ファイルから rename で差し替える（CLAUDE.md の
// 「ファイルの書き換えは一時ファイルへ書いてから差し替える」）。エージェントが書き換えても、
// 次にどれかの run を起動するときに元へ戻る。
package agentplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maimuzo/continuo/plugins"
)

// 書き出すときに置き換える語である（plugins パッケージの説明を見ること）。
const (
	// HashPlaceholder は plugin の中身のハッシュ12桁に置き換わる語である。
	HashPlaceholder = plugins.HashPlaceholder
	// DirPlaceholder は書き出した plugin のディレクトリの絶対パスに置き換わる語である。
	DirPlaceholder = plugins.DirPlaceholder
)

// 置き場所の名前である。
const (
	// PluginsDirName は実行時ディレクトリの下の、plugin の版を並べるディレクトリの名前である。
	PluginsDirName = "plugins"
	// OrphanedAtName は、古い版になった時刻を書くファイルの名前である。
	OrphanedAtName = ".orphaned_at"
	// TmpPrefix は、初めて書き出すときの一時のディレクトリの名前の頭である。
	TmpPrefix = ".tmp-"
	// SystemPromptFileName は issue ごとのディレクトリに置くシステムプロンプトのファイル名である。
	//
	// **`--append-system-prompt-file` で Claude Code に渡す。**worktree の片付けで、
	// issue ごとの設定ファイルと一緒に消える（`internal/workspace` の `removeSettingsFile`）。
	SystemPromptFileName = "system-prompt.md"
)

// 権限である。**エージェントが書き換えにくいよう、ファイルは書き込みの権限を持たせない。**
// 書き換えられても、次の書き出しで元へ戻る。
const (
	dirPerm    fs.FileMode = 0o755
	filePerm   fs.FileMode = 0o444
	scriptPerm fs.FileMode = 0o555
)

// hashLen はハッシュの桁数である。
const hashLen = 12

// embedded は、埋め込んだ plugin のファイル1つである（置き換える前の中身）。
type embedded struct {
	// rel は plugin のディレクトリからの相対パスである（`/` 区切り）。
	rel string
	// data は置き換える前の中身である。
	data []byte
	// perm は書き出したときの権限である。
	perm fs.FileMode
}

// files と hash はパッケージの初期化で1回だけ求める。**埋め込みは実行中に変わらない。**
var files, dirs, hash = load()

// load は埋め込んだ plugin を読み、ファイルの一覧・ディレクトリの一覧・ハッシュを返す。
//
// **読めなければ panic する。**埋め込みは build のときに決まるので、読めないのは作りの誤りである。
//
// 戻り値の1つ目: ファイルの一覧（相対パスの順）。
// 戻り値の2つ目: ディレクトリの一覧（相対パス。plugin のディレクトリ自身は入れない）。
// 戻り値の3つ目: ハッシュ12桁。
func load() ([]embedded, []string, string) {
	root := plugins.AgentProceduresName
	var fl []embedded
	var dl []string
	err := fs.WalkDir(plugins.AgentProcedures, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		if rel == "" {
			return nil
		}
		if d.IsDir() {
			dl = append(dl, rel)
			return nil
		}
		data, err := fs.ReadFile(plugins.AgentProcedures, p)
		if err != nil {
			return err
		}
		fl = append(fl, embedded{rel: rel, data: data, perm: permFor(rel)})
		return nil
	})
	if err != nil {
		panic("agentplugin: 埋め込んだ plugin を読めません: " + err.Error())
	}
	sort.Slice(fl, func(i, j int) bool { return fl[i].rel < fl[j].rel })
	sort.Strings(dl)
	return fl, dl, computeHash(fl)
}

// permFor は、書き出したファイルの権限を決める。
//
// **scripts/ の下は 0555（実行できる）、それ以外は 0444 である。**
//
// rel: plugin のディレクトリからの相対パス。
// 戻り値: 権限。
func permFor(rel string) fs.FileMode {
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if seg == "scripts" {
			return scriptPerm
		}
	}
	return filePerm
}

// computeHash は、置き換える前の中身からハッシュを求める。
//
// **パス・権限・中身の3つを入れる。**どれが変わっても、別の版として書き出す。
//
// fl: ファイルの一覧（相対パスの順）。
// 戻り値: ハッシュの先頭12桁（16進の小文字）。
func computeHash(fl []embedded) string {
	h := sha256.New()
	for _, f := range fl {
		h.Write([]byte(f.rel))
		h.Write([]byte{0})
		h.Write([]byte(f.perm.String()))
		h.Write([]byte{0})
		h.Write(f.data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:hashLen]
}

// Hash は plugin の中身のハッシュ12桁を返す。
//
// **書き出し先のディレクトリの名前と、各 SKILL.md の終わりの印に入る値である。**
//
// 戻り値: 16進の小文字12桁。
func Hash() string { return hash }

// PluginDir は、書き出した plugin のディレクトリ（`--plugin-dir` に渡すもの）を返す。
//
// **ディレクトリを作らない。**作るのは Install である。
//
// runtimeDir: 実行時ディレクトリの絶対パス。
// 戻り値: `<runtimeDir>/plugins/<12桁>/continuo-agent-procedures`。
func PluginDir(runtimeDir string) string {
	return filepath.Join(VersionDir(runtimeDir, hash), plugins.AgentProceduresName)
}

// VersionDir は、ある版の置き場所を返す。
//
// runtimeDir: 実行時ディレクトリの絶対パス。
// h: 版のハッシュ12桁。
// 戻り値: `<runtimeDir>/plugins/<h>`。
func VersionDir(runtimeDir, h string) string {
	return filepath.Join(runtimeDir, PluginsDirName, h)
}

// SkillPath は、書き出した plugin の中のスキルの SKILL.md のパスを返す。
//
// pluginDir: plugin のディレクトリ（PluginDir の戻り値か、置き換える語 DirPlaceholder）。
// skill: スキルの名前。
// 戻り値: `<pluginDir>/skills/<skill>/SKILL.md`。**区切りは `/` で固定する**（システムプロンプトに書く文字列）。
func SkillPath(pluginDir, skill string) string {
	return pluginDir + "/skills/" + skill + "/SKILL.md"
}

// SkillNames は、埋め込んだ plugin のスキルの名前を返す（`skills/<名前>/SKILL.md` があるもの）。
//
// 戻り値: 名前の並び（名前の順）。
func SkillNames() []string {
	var out []string
	for _, f := range files {
		parts := strings.Split(f.rel, "/")
		if len(parts) == 3 && parts[0] == "skills" && parts[2] == "SKILL.md" {
			out = append(out, parts[1])
		}
	}
	return out
}

// Expand は、埋め込んだ中身の置き換える語を埋める。
//
// **SKILL.md を `prompt.Build` に通さない。**HTML のコメントを取り除く処理に、終わりの印を消されないためである。
//
// data: 置き換える前の中身。
// pluginDir: 書き出した plugin のディレクトリの絶対パス。
// 戻り値: 置き換えた中身。
func Expand(data []byte, pluginDir string) []byte {
	s := strings.ReplaceAll(string(data), HashPlaceholder, hash)
	s = strings.ReplaceAll(s, DirPlaceholder, pluginDir)
	return []byte(s)
}

// RawFile は、埋め込んだ plugin のファイルを、置き換える前の中身のまま返す（テストと検査のため）。
//
// rel: plugin のディレクトリからの相対パス（`/` 区切り）。
// 戻り値の1つ目: 中身。
// 戻り値の2つ目: 埋め込みに在れば真。
func RawFile(rel string) ([]byte, bool) {
	for _, f := range files {
		if f.rel == rel {
			return f.data, true
		}
	}
	return nil, false
}

// RawFiles は、埋め込んだ plugin のファイルの相対パスを返す（テストと検査のため）。
//
// 戻り値: 相対パスの並び（`/` 区切り。名前の順）。
func RawFiles() []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.rel)
	}
	return out
}
