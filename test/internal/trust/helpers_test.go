package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/maimuzo/continuo/internal/trust"
)

// claudeConfigName は Claude Code が信頼済みフォルダを記録しているファイルの名前である。
//
// **テストは必ず t.TempDir() の下に作った偽のホームディレクトリを使う。**
// 実物の `~/.claude.json` には認証情報を含む全設定が入っており、テストが触ってはならない。
const claudeConfigName = ".claude.json"

// fakeHome は t.TempDir() の下に偽のホームディレクトリを作り、`.claude.json` を置く。
//
// t: テストコンテキスト。
// contents: 置く `.claude.json` の中身。**空文字ならファイルを作らない。**
// 戻り値の1つ目: 偽のホームディレクトリの絶対パス。
// 戻り値の2つ目: 置いた `.claude.json` の絶対パス。
func fakeHome(t *testing.T, contents string) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, claudeConfigName)
	if contents != "" {
		// 実物と同じ 0600 で置く（権限を引き継ぐことを確かめられるようにする）。
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("偽の %s を置けなかった: %v", claudeConfigName, err)
		}
	}
	return home, path
}

// initRepo は t.TempDir() の下に git リポジトリを1つ作る。
//
// **commit は作らない。**信頼の鍵に使う `git rev-parse --show-toplevel` は
// commit が無くても答えるので、テストの前提を増やさない。
//
// t: テストコンテキスト。
// name: 作るディレクトリの名前。
// 戻り値: 作ったリポジトリの絶対パス（**シンボリックリンクは解決していない**）。
func initRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("リポジトリのディレクトリを作れなかった: %v", err)
	}
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init に失敗した: %v: %s", err, out)
	}
	return dir
}

// trustKeyOf は、そのリポジトリについて continuo が使う信頼の鍵を返す。
//
// **テスト側でパスを組み立てない。**鍵は `git rev-parse --path-format=absolute
// --show-toplevel` の出力であり、macOS では t.TempDir() のシンボリックリンクが
// 解決されるため、渡したパスとは違う文字列になる。
//
// t: テストコンテキスト。
// clonePath: リポジトリの絶対パス。
// 戻り値: 信頼の鍵。
func trustKeyOf(t *testing.T, clonePath string) string {
	t.Helper()
	key, err := trust.RunGitToplevel(context.Background(), clonePath)
	if err != nil {
		t.Fatalf("信頼の鍵を求められなかった: %v", err)
	}
	return key
}

// writeJSON は path に JSON のファイルを1つ置く（途中のディレクトリも作る）。
//
// t: テストコンテキスト。
// path: 置くファイルの絶対パス。
// contents: 中身。
func writeJSON(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れなかった: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("%s を置けなかった: %v", path, err)
	}
}

// staticClones は「owner/repo から clone のパスを引く」を、決め打ちの対応で差し替える。
//
// **本物の ghq を叩くと、その機械に何が clone されているかでテストの結果が変わる。**
//
// paths: "owner/repo" から clone の絶対パスへの対応。**対応が無ければ空文字を返す**
// （clone が無い場合と同じ扱い）。
// 戻り値: 差し替え用の関数。
func staticClones(paths map[string]string) trust.CloneResolver {
	return func(_ context.Context, owner, repo string) (string, error) {
		return paths[owner+"/"+repo], nil
	}
}

// readFile は path の中身を文字列で返す。
//
// t: テストコンテキスト。
// path: 読むファイルの絶対パス。
// 戻り値: 中身。
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", path, err)
	}
	return string(raw)
}

// decodeJSON は JSON の文字列を any へ読み込む。
//
// **書き換えの前後を「中身として同じか」で比べるために使う。**字下げや並び順の違いを
// 無視して、値そのものが変わっていないかだけを見る。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す呼び名。
// raw: JSON の文字列。
// 戻り値: 読み込んだ値。
func decodeJSON(t *testing.T, label, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%s を JSON として読めなかった: %v\n%s", label, err, raw)
	}
	return v
}

// projectEntry は `~/.claude.json` の projects から1件を取り出す。
//
// t: テストコンテキスト。
// raw: `~/.claude.json` の中身。
// key: 信頼の鍵。
// 戻り値の1つ目: その鍵の記述。
// 戻り値の2つ目: その鍵があれば true。
func projectEntry(t *testing.T, raw, key string) (map[string]any, bool) {
	t.Helper()
	root, ok := decodeJSON(t, "~/.claude.json", raw).(map[string]any)
	if !ok {
		t.Fatalf("~/.claude.json のトップレベルがオブジェクトではない")
	}
	projects, ok := root["projects"].(map[string]any)
	if !ok {
		return nil, false
	}
	entry, ok := projects[key].(map[string]any)
	return entry, ok
}

// assertSameJSON は2つの JSON が中身として同じであることを確かめる。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す呼び名。
// want: 期待する側の JSON。
// got: 実際の JSON。
func assertSameJSON(t *testing.T, label, want, got string) {
	t.Helper()
	if !reflect.DeepEqual(decodeJSON(t, label+"（期待）", want), decodeJSON(t, label+"（実際）", got)) {
		t.Errorf("%s の中身が変わっている\n期待:\n%s\n実際:\n%s", label, want, got)
	}
}

// symlink は oldname を指す symlink を newname に作る。
//
// oldname: リンク先の絶対パス。
// newname: 作る symlink の絶対パス。
// 戻り値: 作れなかった場合のエラー。
func symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

// marshalJSON は値を JSON の文字列に直す（比較用）。
//
// v: 直す値。
// 戻り値の1つ目: JSON の文字列。
// 戻り値の2つ目: 変換に失敗した場合のエラー。
func marshalJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// requireCommands は必要な外部コマンドが PATH に無ければテストを飛ばす。
//
// **黙って通さない。**t.Skip はテストの一覧に理由つきで残るので、
// 何が確かめられなかったかが後から分かる。
//
// t: テストコンテキスト。
// names: PATH にあるべきコマンドの名前。
func requireCommands(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s が PATH に無いので、この検査は実行できません: %v", n, err)
		}
	}
}

// asExitError は err が終了コードを持つエラーかどうかを判定する。
//
// err: 判定するエラー。
// target: 一致した場合に書き込む先。
// 戻り値: 一致すれば true。
func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

// writeWorkflow は CLI のテスト用に、trust.repositories を2件だけ書いた WORKFLOW.md を置く。
//
// **人間が要らない行を消したあとの状態を再現する**（設計 3-33）。
// 雛形そのものは `continuo init` が書くので、ここでは必要な最小限だけを書く。
//
// t: テストコンテキスト。
// path: 置く WORKFLOW.md の絶対パス。
func writeWorkflow(t *testing.T, path string) {
	t.Helper()
	const contents = `---
tracker:
  provider:
    owner: maimuzo
    project_number: 3
    status_field: Status
trust:
  repositories:
    - "maimuzo/demo-a"
    - "maimuzo/demo-b"
---

{{.issue.identifier}} を実装してください。
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WORKFLOW.md を置けなかった: %v", err)
	}
}
