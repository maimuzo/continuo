package orchestrator_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
)

// subagentFixture は「親の記録と、その隣の subagents ディレクトリ」を作る。
//
// **置き場所の規則は実測で確かめられている**（docs/evidence/hooks_probe_20260817.jsonl の
// `SubagentStop` に、親の `transcript_path` と subagent の `agent_transcript_path` が
// 同じ行で入っている）。
//
// t: テスト。
// makeSubagentDir: 真なら subagents ディレクトリまで作る。
// 戻り値の1つ目: 許可された根（解決済みの絶対パス）。
// 戻り値の2つ目: 親の記録の絶対パス。
// 戻り値の3つ目: subagents ディレクトリの絶対パス（作っていなくても組み立てて返す）。
func subagentFixture(t *testing.T, makeSubagentDir bool) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("根を解決できません: %v", err)
	}
	parent := filepath.Join(resolvedRoot, "session-1.jsonl")
	if err := os.WriteFile(parent, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("親の記録を書けません: %v", err)
	}
	dir := filepath.Join(resolvedRoot, "session-1", "subagents")
	if makeSubagentDir {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("subagents ディレクトリを作れません: %v", err)
		}
	}
	return resolvedRoot, parent, dir
}

// writeSubagentTranscript は subagent の記録を1件書き、更新時刻を指定の値にする。
//
// t: テスト。
// dir: subagents ディレクトリ。
// name: ファイル名。
// modTime: 付ける更新時刻。
// 戻り値: 書いたファイルの絶対パス。
func writeSubagentTranscript(t *testing.T, dir, name string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("subagent の記録を書けません: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("更新時刻を付けられません: %v", err)
	}
	return path
}

// TestListSubagentTranscripts_subagentsディレクトリが無ければ何も返さない は、
// subagent を1つも使わなかった turn を確かめる。
//
// 目的: 設計 3-11 の「ディレクトリが無いのは正常な並びであり、エラーにも警告にもしない」
// を守っていることを示す。**存在しない置き場所を引き渡しの通知に書いてはならない。**
// 与える情報: 親の記録だけがあり、隣に subagents ディレクトリが無い置き場所。
// 成功条件: 置き場所も記録も空で返ること。
func TestListSubagentTranscripts_subagentsディレクトリが無ければ何も返さない(t *testing.T) {
	root, parent, _ := subagentFixture(t, false)

	dir, files := orchestrator.ListSubagentTranscripts(parent, root, 3)

	if dir != "" {
		t.Errorf("無いはずの置き場所を返している: %q", dir)
	}
	if len(files) != 0 {
		t.Errorf("無いはずの記録を返している: %v", files)
	}
}

// TestListSubagentTranscripts_ディレクトリが空なら置き場所だけを返す は、
// 中身が1件も無いときの返し方を確かめる。
//
// 目的: 「空の項目は行ごと出さない」（設計 3-34b）を、記録の側だけに効かせられることを示す。
// **置き場所は実在するので人間に見せてよい。**記録は1件も無いので出さない。
// 与える情報: subagents ディレクトリはあるが、`agent-*.jsonl` が1件も無い置き場所。
// 成功条件: 置き場所が返り、記録は0件であること。
func TestListSubagentTranscripts_ディレクトリが空なら置き場所だけを返す(t *testing.T) {
	root, parent, want := subagentFixture(t, true)

	dir, files := orchestrator.ListSubagentTranscripts(parent, root, 3)

	if dir != want {
		t.Errorf("置き場所が想定と違う: got %q, want %q", dir, want)
	}
	if len(files) != 0 {
		t.Errorf("空のはずなのに記録を返している: %v", files)
	}
}

// TestListSubagentTranscripts_更新時刻の新しい順に上限まで返す は、並びと件数の上限を確かめる。
//
// 目的: 「新しい順に最大3件」を守っていることを示す。**止まった直前に何が動いていたかを
// 辿れるようにするためであり、全件を並べるとコメントが読めなくなる。**
// 与える情報: 更新時刻の違う `agent-*.jsonl` が4件と、拾ってはならない `notes.jsonl`。
// 成功条件: 新しい順に3件だけ返り、`agent-` で始まらないファイルが混ざらないこと。
func TestListSubagentTranscripts_更新時刻の新しい順に上限まで返す(t *testing.T) {
	root, parent, subagentDir := subagentFixture(t, true)
	base := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	oldest := writeSubagentTranscript(t, subagentDir, "agent-1111.jsonl", base)
	middle := writeSubagentTranscript(t, subagentDir, "agent-2222.jsonl", base.Add(1*time.Hour))
	newer := writeSubagentTranscript(t, subagentDir, "agent-3333.jsonl", base.Add(2*time.Hour))
	newest := writeSubagentTranscript(t, subagentDir, "agent-4444.jsonl", base.Add(3*time.Hour))
	writeSubagentTranscript(t, subagentDir, "notes.jsonl", base.Add(4*time.Hour))

	dir, files := orchestrator.ListSubagentTranscripts(parent, root, 3)

	if dir != subagentDir {
		t.Errorf("置き場所が想定と違う: got %q, want %q", dir, subagentDir)
	}
	want := []string{newest, newer, middle}
	if len(files) != len(want) {
		t.Fatalf("返った件数が想定と違う: got %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("新しい順になっていない: got %v, want %v", files, want)
		}
	}
	for _, f := range files {
		if f == oldest {
			t.Errorf("上限を超えて古いものまで返している: %v", files)
		}
	}
}

// TestListSubagentTranscripts_通常のファイルでないものは混ぜない は、
// 名前だけ合っている実体を弾くことを確かめる。
//
// 目的: 「通常のファイルだけ残す」を守っていることを示す。**ディレクトリや FIFO を
// 「開けばよい」と案内すると、人間はそこで行き止まりになる。**
// 与える情報: `agent-*.jsonl` という名前のディレクトリと、通常のファイル1件。
// 成功条件: 通常のファイルだけが返ること。
func TestListSubagentTranscripts_通常のファイルでないものは混ぜない(t *testing.T) {
	root, parent, subagentDir := subagentFixture(t, true)
	base := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	regular := writeSubagentTranscript(t, subagentDir, "agent-real.jsonl", base)
	if err := os.MkdirAll(filepath.Join(subagentDir, "agent-dir.jsonl"), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	if err := os.Symlink(regular, filepath.Join(subagentDir, "agent-link.jsonl")); err != nil {
		t.Fatalf("シンボリックリンクを作れません: %v", err)
	}

	dir, files := orchestrator.ListSubagentTranscripts(parent, root, 3)

	if dir != subagentDir {
		t.Errorf("置き場所が想定と違う: got %q, want %q", dir, subagentDir)
	}
	if len(files) != 1 || files[0] != regular {
		t.Fatalf("通常のファイルでないものを混ぜている: got %v, want [%s]", files, regular)
	}
}

// TestListSubagentTranscripts_根の外を指すなら何も返さない は、置き場所の検査を確かめる。
//
// 目的: `acceptTranscriptPath` と同じ検査（**まず解決してから根と比べる**）を通していることを示す。
// **hook が渡す `transcript_path` は外部入力である**（設計 3-2 / 3-23）。
// 与える情報: 実在する subagents ディレクトリと、それを含まない別の根。
// 成功条件: 置き場所も記録も空で返ること。
func TestListSubagentTranscripts_根の外を指すなら何も返さない(t *testing.T) {
	_, parent, subagentDir := subagentFixture(t, true)
	writeSubagentTranscript(t, subagentDir, "agent-1111.jsonl", time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC))
	otherRoot := t.TempDir()

	dir, files := orchestrator.ListSubagentTranscripts(parent, otherRoot, 3)

	if dir != "" {
		t.Errorf("根の外の置き場所を返している: %q", dir)
	}
	if len(files) != 0 {
		t.Errorf("根の外の記録を返している: %v", files)
	}
}
