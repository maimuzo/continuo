// {"RUCM-CFG-SHA256": "95e8048780f94939c978a444aa8ba2e27646962d3478125e829d52743425908e", "SOURCE": "docs/spec/usecases/particular_case/設定ファイルを作る.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// **379本のパスは、引数の指定の組み合わせで爆発したものである。**結末は8通りしかないので、
// **終端フローごとに代表を1本ずつ**対応づける。組み合わせを全部書いても、
// 同じ経路を何度も通るだけで新しく守れるものが増えない。
// `continuo setup` が WORKFLOW.md を書き換えられない場合の検査である。
//
// **書き換えは不可分でなければならない。**途中で落ちて半分書かれた WORKFLOW.md が残ると、
// continuo は起動できず、人間も何を直せばよいか分からない。
package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// completeStatuses は5つの役割が全部埋まった割り当てを返す。
//
// 戻り値: 検査用の割り当て。
func completeStatuses() scaffold.Statuses {
	return scaffold.Statuses{
		Dispatch: "着手待ち", Running: "作業中", Review: "レビュー待ち",
		Blocked: "保留", Done: "完了",
	}
}

// TestUpdateStatuses_役割が欠けていたらファイルに触らない は、尋ねる前の検査を確かめる。
//
// 目的: 5つそろっていないとき、`ErrStatusesIncomplete` を返してファイルを読みもしないこと。
// 与える情報: 1つだけ空の割り当て。
// 成功条件: そのエラーで返り、ファイルの中身が変わらないこと。
func TestUpdateStatuses_役割が欠けていたらファイルに触らない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	before := scaffold.Template()
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	st := completeStatuses()
	st.Done = ""
	_, err := scaffold.UpdateStatuses(dir, st)
	if !errors.Is(err, scaffold.ErrStatusesIncomplete) {
		t.Fatalf("ErrStatusesIncomplete でない: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", readErr)
	}
	if string(after) != before {
		t.Error("役割が欠けているのにファイルを書き換えている")
	}
}

// TestUpdateStatuses_WORKFLOWmdが無ければエラーを返す は、対象が無い場合を確かめる。
//
// 目的: 書き換える対象が無いとき、どのパスを見たかを添えて落ちること。
// 与える情報: 空のディレクトリ。
// 成功条件: エラーが返り、Result.Path が埋まっていること。
func TestUpdateStatuses_WORKFLOWmdが無ければエラーを返す(t *testing.T) {
	got, err := scaffold.UpdateStatuses(t.TempDir(), completeStatuses())
	if err == nil {
		t.Fatal("WORKFLOW.md が無いのにエラーにならなかった")
	}
	if got.Path == "" {
		t.Error("どのパスを見たかを返していない")
	}
}

// TestUpdateStatuses_書き換えるキーが無ければ落とす は、雛形でないファイルを弾くことを確かめる。
//
// **人間が別のファイルを WORKFLOW.md という名前で置いていることがある。**
// **キーの有無を先に見ないと、5問すべて答えさせたあとで落ちて入力が捨てられる。**
//
// 目的: 書き換えるキーが1つも無いファイルを、`ErrKeysNotFound` で弾くこと。
// 与える情報: 関係のない中身のファイル。
// 成功条件: そのエラーで返り、**どのキーが無いかを示すこと。**
func TestUpdateStatuses_書き換えるキーが無ければ落とす(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte("# ただのメモ\n"), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	_, err := scaffold.UpdateStatuses(dir, completeStatuses())
	if !errors.Is(err, scaffold.ErrKeysNotFound) {
		t.Fatalf("ErrKeysNotFound でない: %v", err)
	}
	// **どのキーが足りないかを示すこと。**示さないと、人間は何を足せばよいか分からない。
	if !strings.Contains(err.Error(), "tracker.") {
		t.Errorf("足りないキーを示していない: %v", err)
	}
}

// TestUpdateStatuses_書き換えても権限を保つ は、ファイルの属性を変えないことを確かめる。
//
// **`~/.claude.json` ほどではないが、WORKFLOW.md にもボードの番号などが入る。**
// 書き換えのたびに権限が緩むと、いつの間にか他ユーザーから読める状態になる。
//
// 目的: 書き換えの前後でファイルの権限が変わらないこと。
// 与える情報: 0600 で置いた WORKFLOW.md。
// 成功条件: 書き換え後も 0600 であること。
func TestUpdateStatuses_書き換えても権限を保つ(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(scaffold.Template()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, completeStatuses()); err != nil {
		t.Fatalf("UpdateStatuses が失敗した: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を stat できません: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("権限が変わっている: %04o", got)
	}
}

// {"RUCM-PATH": "P003"}
//
// TestWriteTemplate_既にあれば force なしで拒む は、上書きの条件を確かめる。
//
// **人間が手で直した行が消えるので、黙って上書きしてはならない。**
//
// 目的: 既に WORKFLOW.md があるとき、`force` が偽なら `ErrAlreadyExists` を返すこと。
// 与える情報: 既にファイルがあるディレクトリ。
// 成功条件: そのエラーで返り、中身が変わらないこと。
func TestWriteTemplate_既にあればforceなしで拒む(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const before = "# 人間が置いたもの\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	_, err := scaffold.WriteTemplate(dir, false)
	if !errors.Is(err, scaffold.ErrAlreadyExists) {
		t.Fatalf("ErrAlreadyExists でない: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", readErr)
	}
	if string(after) != before {
		t.Error("force が偽なのに上書きしている")
	}
}

// {"RUCM-PATH": "P002"}
//
// TestWriteTemplate_forceなら上書きして上書きしたと返す は、`--force` の経路を確かめる。
//
// 目的: `force` が真なら上書きし、Result.Overwritten を真にすること。
// 与える情報: 既にファイルがあるディレクトリ。
// 成功条件: 雛形の中身になり、Overwritten が真であること。
func TestWriteTemplate_forceなら上書きして上書きしたと返す(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("# 古いもの\n"), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	got, err := scaffold.WriteTemplate(dir, true)
	if err != nil {
		t.Fatalf("WriteTemplate が失敗した: %v", err)
	}
	if !got.Overwritten {
		t.Error("上書きしたのに Overwritten が偽になっている")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", readErr)
	}
	if strings.Contains(string(after), "古いもの") {
		t.Error("上書きできていない")
	}
	if !strings.Contains(string(after), "tracker:") {
		t.Error("雛形の中身になっていない")
	}
}

// {"RUCM-PATH": "P006"}
//
// TestWriteTemplate_ディレクトリが無ければエラーを返す は、置き場所の検査を確かめる。
//
// 目的: 存在しないディレクトリを指されたら `ErrDirNotFound` を返すこと。
// 与える情報: 存在しないパス。
// 成功条件: そのエラーで返ること。
func TestWriteTemplate_ディレクトリが無ければエラーを返す(t *testing.T) {
	_, err := scaffold.WriteTemplate(filepath.Join(t.TempDir(), "no-such-dir"), false)
	if !errors.Is(err, scaffold.ErrDirNotFound) {
		t.Fatalf("ErrDirNotFound でない: %v", err)
	}
}

// {"RUCM-PATH": "P005"}
//
// TestWriteTemplate_ディレクトリでなければエラーを返す は、置き場所の種類を確かめる。
//
// 目的: ファイルを指されたら `ErrNotADirectory` を返すこと。
// 与える情報: ファイルのパス。
// 成功条件: そのエラーで返ること。
func TestWriteTemplate_ディレクトリでなければエラーを返す(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("ファイルを作れません: %v", err)
	}

	_, err := scaffold.WriteTemplate(file, false)
	if !errors.Is(err, scaffold.ErrNotADirectory) {
		t.Fatalf("ErrNotADirectory でない: %v", err)
	}
}

// TestUpdateStatuses_コメントが無い行でも書き換える は、雛形を人間が整理した場合を確かめる。
//
// **人間は行の右側のコメントを消すことがある。**コメントの有無で書き換えが失敗すると、
// 「掃除したら setup が通らなくなった」という分かりにくい壊れ方をする。
//
// 目的: コメントを全部消した WORKFLOW.md でも、7つのキーを書き換えること。
// 与える情報: 行の右側のコメントを落とした WORKFLOW.md。
// 成功条件: 割り当てた選択肢名が全部入り、**コメントを勝手に足さないこと。**
func TestUpdateStatuses_コメントが無い行でも書き換える(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")

	// **行の右側の `#` から後ろを落とす。**front matter の中だけが対象なので、
	// 行頭が `#` の行（markdown の見出し）は残す。
	var b strings.Builder
	for _, line := range strings.Split(scaffold.Template(), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if i := strings.Index(line, "#"); i > 0 && !strings.HasPrefix(trimmed, "#") {
			line = strings.TrimRight(line[:i], " ")
		}
		b.WriteString(line + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, completeStatuses()); err != nil {
		t.Fatalf("UpdateStatuses が失敗した: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	for _, want := range []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q が書き込まれていない", want)
		}
	}
	// **コメントが無かった行に、勝手にコメントを足さないこと。**
	for _, line := range strings.Split(string(got), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "dispatch_state:") {
			if strings.Contains(line, "#") {
				t.Errorf("コメントが無かった行にコメントを足している: %q", line)
			}
		}
	}
}

// TestUpdateStatuses_入れ子の深いキーも書き換える は、`status_signal_map` の中を確かめる。
//
// **`tracker.status_signal_map.review` はキーを3段たどる。**
// 途中で親のブロックを抜けたことに気づかないと、別の場所の同名キーを書き換えてしまう。
//
// 目的: 入れ子の中のキーを正しく書き換えること。
// 与える情報: 雛形そのまま。
// 成功条件: `status_signal_map` の中の `review` と `blocked` が書き換わること。
func TestUpdateStatuses_入れ子の深いキーも書き換える(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(scaffold.Template()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, completeStatuses()); err != nil {
		t.Fatalf("UpdateStatuses が失敗した: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}

	// **`status_signal_map` のブロックの中だけを見る。**
	inMap := false
	found := map[string]string{}
	for _, line := range strings.Split(string(got), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "status_signal_map:") {
			inMap = true
			continue
		}
		if inMap {
			indent := len(line) - len(trimmed)
			if trimmed != "" && indent <= 2 {
				break
			}
			for _, key := range []string{"review:", "blocked:"} {
				if strings.HasPrefix(trimmed, key) {
					found[key] = trimmed
				}
			}
		}
	}
	if !strings.Contains(found["review:"], "レビュー待ち") {
		t.Errorf("status_signal_map.review が書き換わっていない: %q", found["review:"])
	}
	if !strings.Contains(found["blocked:"], "保留") {
		t.Errorf("status_signal_map.blocked が書き換わっていない: %q", found["blocked:"])
	}
}
