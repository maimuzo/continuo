// {"RUCM-CFG-SHA256": "0522685f2ac9ef7313389909ac9deb636619176f727600d752a7f1a0ada70b8a", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **書き方が違う WORKFLOW.md を `continuo setup` に渡したときの検査である。**
//
// **`continuo setup` は行を1本ずつ組み立て直す。**だから、値が下の行にぶら下がっていたり、
// 改行が CRLF だったりすると、雛形のままのファイルとは違う結果になる。
// **どちらも「成功しました」と出したまま壊してはならない。**
package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P001"}
//
// TestUpdateStatuses_片付けを始めるStatusも書き換える は、**書き落としを落とす。**
//
// **ここが雛形の `["Done"]` のまま残ると、ボードの完了の選択肢が別名の環境で
// 片付けが一度も走らない。**worktree と branch が永久に残り、その理由はどこにも出ない
// （`Done` は active_states に無いので設定の検証を通る）。
//
// 目的: `cleanup.on_states` に、割り当てた完了の Status が書かれること。
// 与える情報: 雛形を置いたディレクトリと、日本語の選択肢名の割り当て。
// 成功条件: `cleanup.on_states` が `["完了"]` になり、`Done` がどこにも残らないこと。
func TestUpdateStatuses_片付けを始めるStatusも書き換える(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner: "octocat", ProjectNumber: 3,
	}); err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("書き換えられなかった: %v", err)
	}

	got := readWorkflow(t, dir)
	if !containsLineWithPrefix(got, `  on_states: ["完了"]`) {
		t.Errorf("cleanup.on_states に割り当てた完了の Status が入っていない:\n%s", frontMatterPart(got))
	}
	if strings.Contains(frontMatterPart(got), `"Done"`) {
		t.Errorf("雛形の Done が残っている:\n%s", frontMatterPart(got))
	}
}

// {"RUCM-PATH": "P002"}
//
// TestUpdateStatuses_値が下の行にぶら下がっていたら書かずに止める は、
// **「成功しました」と出したあと continuo が起動しなくなる**経路を落とす。
//
// 目的: block 形式で書かれたキーがあるとき、書き換えずに ErrKeysNotRewritable で止め、
// ファイルを1バイトも変えないこと。
// 与える情報: `active_states` を `- "Ready"` の並びで書いた WORKFLOW.md。
// 成功条件: ErrKeysNotRewritable が返り、ファイルの中身が変わらず、
// そのファイルがそのあとも設定として読めること。
func TestUpdateStatuses_値が下の行にぶら下がっていたら書かずに止める(t *testing.T) {
	dir := t.TempDir()
	before := blockFormWorkflow(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	_, err := scaffold.UpdateStatuses(dir, jaStatuses)
	if !errors.Is(err, scaffold.ErrKeysNotRewritable) {
		t.Fatalf("ErrKeysNotRewritable が返らなかった: %v", err)
	}
	if got := readWorkflow(t, dir); got != before {
		t.Errorf("書き換えないはずのファイルが変わっている:\n%s", got)
	}
	if err := config.CheckFrontMatterSyntax(readWorkflow(t, dir)); err != nil {
		t.Errorf("触っていないのに読めなくなっている: %v", err)
	}
}

// {"RUCM-PATH": "P002"}
//
// TestCheckUpdatable_値が下の行にぶら下がっていたら尋ねる前に止める は、
// **5問答えさせたあとで落とさない**ことを確かめる。
//
// 目的: 対話に入る前の検査でも、書き換えられない形を見つけて止めること。
// 与える情報: `active_states` を block 形式で書いた WORKFLOW.md。
// 成功条件: ErrKeysNotRewritable が返ること。
func TestCheckUpdatable_値が下の行にぶら下がっていたら尋ねる前に止める(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(blockFormWorkflow(t)), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	if _, err := scaffold.CheckUpdatable(dir); !errors.Is(err, scaffold.ErrKeysNotRewritable) {
		t.Fatalf("ErrKeysNotRewritable が返らなかった: %v", err)
	}
}

// {"RUCM-PATH": "P002"}
//
// TestUpdateStatuses_書き換えると読めなくなるなら書かない は、**最後の関門**を確かめる。
//
// **行を1本だけ組み立て直す書き換えは、値の書き方によっては下の行を残す。**
// block 形式は名指しで止めているが、書き方はほかにもある（`>` や `|` で始まる複数行の値）。
// **どの書き方であっても、読めなくする書き換えを「成功しました」と報告してはならない。**
//
// 目的: 書き換えた結果が front matter として読めなくなるとき、書かずに止めること。
// 与える情報: `terminal_states` の値を折りたたみ記法（`>`）で書いた WORKFLOW.md。
// 成功条件: ErrWouldBreakConfig が返り、ファイルの中身が変わらないこと。
func TestUpdateStatuses_書き換えると読めなくなるなら書かない(t *testing.T) {
	dir := t.TempDir()
	before := strings.Replace(templateFor(t),
		`  terminal_states: ["Done"]`,
		"  terminal_states: >\n    [\"Done\"]", 1)
	if !strings.Contains(before, "terminal_states: >") {
		t.Fatalf("検査用のファイルを組み立てられなかった:\n%s", frontMatterPart(before))
	}
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	if err := config.CheckFrontMatterSyntax(before); err != nil {
		t.Fatalf("検査用のファイルが元から読めない: %v", err)
	}

	_, err := scaffold.UpdateStatuses(dir, jaStatuses)
	if !errors.Is(err, scaffold.ErrWouldBreakConfig) {
		t.Fatalf("ErrWouldBreakConfig が返らなかった: %v", err)
	}
	if got := readWorkflow(t, dir); got != before {
		t.Errorf("書き換えないはずのファイルが変わっている:\n%s", got)
	}
}

// {"RUCM-PATH": "P001"}
//
// TestUpdateStatuses_CRLFのファイルでもキーを見つけて書き換える は、
// **「キーが無い」という嘘の報告**を落とす。
//
// **同じファイルを `continuo doctor` は読める**（internal/config は CRLF を LF に直す）。
// setup だけが「キーがありません。continuo init で作り直してください」と案内し、
// **その案内に従うと手で直した設定が雛形で潰れる。**
//
// 目的: 改行が CRLF の WORKFLOW.md でも、キーを見つけて書き換えること。
// 与える情報: 雛形の改行を CRLF にし、**コメントを消した行を1本混ぜた**ファイル。
// **コメントのある行は、コメントの原文をそのまま戻すので改行コードも一緒に戻る。**
// 改行コードだけを見ているかを確かめるには、コメントが無い行が要る。
// 成功条件: エラー無しで書き換わり、割り当てた選択肢名が入り、
// 改行が CRLF のまま（LF だけの行が混ざらない）であること。
func TestUpdateStatuses_CRLFのファイルでもキーを見つけて書き換える(t *testing.T) {
	dir := t.TempDir()
	plain := strings.Replace(templateFor(t),
		`  dispatch_state: "Ready"                   # 着手待ちの Status。取り残された issue はここへ戻す`,
		`  dispatch_state: "Ready"`, 1)
	if strings.Contains(plain, `dispatch_state: "Ready"  `) {
		t.Fatalf("コメントを消せていない:\n%s", frontMatterPart(plain))
	}
	before := strings.ReplaceAll(plain, "\n", "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(before), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("CRLF のファイルを書き換えられなかった: %v", err)
	}

	got := readWorkflow(t, dir)
	if !strings.Contains(got, "\r\n  dispatch_state: \"着手待ち\"") {
		t.Errorf("割り当てた選択肢が CRLF の行として入っていない:\n%s", frontMatterPart(got))
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("改行に LF だけの行が混ざっている:\n%q", got)
	}
	if err := config.CheckFrontMatterSyntax(got); err != nil {
		t.Errorf("書き換えたあとに読めなくなっている: %v", err)
	}
}

// templateFor は、owner とボードの番号を埋めた雛形の全文を返す。
//
// t: 呼び出し元のテスト。
// 戻り値: WORKFLOW.md の全文。
func templateFor(t *testing.T) string {
	t.Helper()
	return scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
}

// blockFormWorkflow は、`active_states` を block 形式で書いた WORKFLOW.md の全文を返す。
//
// t: 呼び出し元のテスト。
// 戻り値: WORKFLOW.md の全文（front matter は YAML として読める形である）。
func blockFormWorkflow(t *testing.T) string {
	t.Helper()
	out := strings.Replace(templateFor(t),
		`  active_states: ["Ready", "In Progress"]`,
		"  active_states:\n    - \"Ready\"\n    - \"In Progress\"", 1)
	if !strings.Contains(out, "\n    - \"Ready\"") {
		t.Fatalf("検査用のファイルを組み立てられなかった:\n%s", frontMatterPart(out))
	}
	if err := config.CheckFrontMatterSyntax(out); err != nil {
		t.Fatalf("検査用のファイルが元から読めない: %v", err)
	}
	return out
}

// readWorkflow は書き換えたあとの WORKFLOW.md を読む。
//
// t: 呼び出し元のテスト。
// dir: WORKFLOW.md があるディレクトリ。
// 戻り値: 全文。
func readWorkflow(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "WORKFLOW.md"))
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	return string(raw)
}

// frontMatterPart は、失敗したときに出す front matter の部分を切り出す。
//
// s: WORKFLOW.md の全文。
// 戻り値: front matter（切り出せなければ全文）。
func frontMatterPart(s string) string {
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return s
	}
	return parts[1]
}
