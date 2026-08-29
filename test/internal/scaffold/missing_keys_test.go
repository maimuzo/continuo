package scaffold_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// fullWorkflow は `continuo init` が置く WORKFLOW.md の全文を返す。
//
// t: 呼び出し元のテスト。
// 戻り値: プレースホルダを埋めた雛形の全文。
func fullWorkflow(t *testing.T) string {
	t.Helper()
	return scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
}

// dropLines は、行頭が prefix で始まる行と、その下に続くより深い行を落とす。
//
// **雛形から設定項目を1つ消した WORKFLOW.md を作るために使う。**
// 版を上げる前の利用者のファイルは、これと同じ形をしている。
//
// t: 呼び出し元のテスト。
// content: 元の全文。
// prefix: 落とす行の書き出し（`  automated_state_rewrite:` のような形）。
// 戻り値: 落としたあとの全文。
func dropLines(t *testing.T, content, prefix string) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	var out []string
	dropping := false
	dropIndent := 0
	dropped := false
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if dropping {
			if trimmed != "" && indent > dropIndent {
				continue
			}
			dropping = false
		}
		if strings.HasPrefix(line, prefix) {
			dropping = true
			dropIndent = indent
			dropped = true
			continue
		}
		out = append(out, line)
	}
	if !dropped {
		t.Fatalf("落とす行が見つかりません: %q", prefix)
	}
	return strings.Join(out, "\n")
}

// writeWorkflowFile は WORKFLOW.md を一時ディレクトリへ置く。
//
// t: 呼び出し元のテスト。
// content: 書く全文。
// 戻り値: 置いたファイルの絶対パス。
func writeWorkflowFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.DefaultFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return path
}

// TestMissingKeys_雛形どおりの設定なら足りない項目は無い は、基準線を作る。
//
// 目的: `continuo init` が置いたままの WORKFLOW.md で、足りない項目が0件になること。
// **ここが0件でなければ、この検査は毎回誤って注意を出す。**
// 与える情報: 雛形のプレースホルダを埋めた全文。
// 成功条件: Keys が空、Patch が空、Total が1以上であること。
func TestMissingKeys_雛形どおりの設定なら足りない項目は無い(t *testing.T) {
	content := fullWorkflow(t)
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) != 0 {
		t.Fatalf("雛形どおりなのに足りないと言っている: %v", res.Keys)
	}
	if res.Patch != "" {
		t.Fatalf("足りないものが無いのに差分が出ている:\n%s", res.Patch)
	}
	if res.Total < 1 {
		t.Fatalf("雛形のキーを1つも数えていない: %d", res.Total)
	}
}

// TestMissingKeys_書かれていないキーを名前で挙げる は、版を上げて増えた項目の検出を確かめる。
//
// 目的: 雛形にあって WORKFLOW.md に無いキーを、**ドット区切りの名前で**挙げること。
// **`tracker.automated_state_rewrite` は v0.1.9 で実際に増えた項目である**（issue #85）。
// 与える情報: その1行とその説明のコメントを落とした WORKFLOW.md。
// 成功条件: Keys がその1件だけで、差分がその項目を足す形になっていること。
func TestMissingKeys_書かれていないキーを名前で挙げる(t *testing.T) {
	content := dropLines(t, fullWorkflow(t), "  automated_state_rewrite:")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) != 1 || res.Keys[0] != "tracker.automated_state_rewrite" {
		t.Fatalf("足りないキーの挙げ方が違う: %v", res.Keys)
	}
	if !strings.Contains(res.Patch, "+  automated_state_rewrite:") {
		t.Fatalf("差分がその項目を足していない:\n%s", res.Patch)
	}
	// **キーと値だけを足さない。**雛形の説明のコメントも一緒に持っていく。
	// **これが無いと、足したことは分かっても何を書ける項目なのかが分からない。**
	if !strings.Contains(res.Patch, "自動化が書く Status 名") {
		t.Fatalf("差分に雛形の説明が入っていない:\n%s", res.Patch)
	}
}

// TestMissingKeys_親ごと無いときは親の1件にまとめる は、内訳が水増しされないことを確かめる。
//
// 目的: `restart` の節を丸ごと消したとき、`restart` と `restart.orphan_running_action` の
// 2件ではなく、**`restart` の1件**として挙げること。直す手は1つしかない。
// 与える情報: `restart:` の節を落とした WORKFLOW.md。
// 成功条件: Keys が `restart` の1件で、差分が節ごと足していること。
func TestMissingKeys_親ごと無いときは親の1件にまとめる(t *testing.T) {
	content := dropLines(t, fullWorkflow(t), "restart:")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) != 1 || res.Keys[0] != "restart" {
		t.Fatalf("親ごと無いのに子まで挙げている: %v", res.Keys)
	}
	if !strings.Contains(res.Patch, "+restart:") ||
		!strings.Contains(res.Patch, "+  orphan_running_action:") {
		t.Fatalf("差分が節ごと足していない:\n%s", res.Patch)
	}
}

// TestMissingKeys_並び順を変えていても差分が当たる は、issue #85 が名指しした条件を固定する。
//
// 目的: 利用者が節の並び順を入れ替えていても、**差分がそのファイルに当たる**こと。
// **差分は雛形ではなく、利用者のファイルから組み立てている**ので当たる。
// 与える情報: `polling` の節を先頭へ動かし、`restart` の節を落とした WORKFLOW.md。
// 成功条件: `patch -p0` が通り、当てたあとに足りない項目が0件になること。
func TestMissingKeys_並び順を変えていても差分が当たる(t *testing.T) {
	content := reorderSections(t, fullWorkflow(t))
	content = dropLines(t, content, "restart:")
	content = dropLines(t, content, "  interval_ms:")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) == 0 {
		t.Fatal("足りない項目を1つも挙げていない")
	}
	applyPatch(t, res.Patch)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めません: %v", err)
	}
	res2, err := scaffold.MissingKeys(path, string(after))
	if err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を調べられません: %v", err)
	}
	if len(res2.Keys) != 0 {
		t.Fatalf("差分を当てたのに足りない項目が残っている: %v", res2.Keys)
	}
	// **当てたあとも continuo が読めること。**読めなくする差分を出してはならない。
	if err := config.CheckFrontMatterSyntax(string(after)); err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めなくなった: %v", err)
	}
}

// TestMissingKeys_frontmatterを切り出せなければ落とす は、黙って0件を返さないことを確かめる。
//
// 目的: 区切り行の無いファイルを渡したとき、「足りない項目は無い」ではなくエラーを返すこと。
// 与える情報: `---` の行が1本も無い全文。
// 成功条件: エラーが返り、Keys が空であること。
func TestMissingKeys_frontmatterを切り出せなければ落とす(t *testing.T) {
	content := "tracker:\n  provider:\n    owner: octocat\n"
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err == nil {
		t.Fatalf("front matter が無いのに通した: %v", res.Keys)
	}
	if len(res.Keys) != 0 {
		t.Fatalf("落としたのにキーを返している: %v", res.Keys)
	}
}

// TestMissingKeys_親がその行で値を決めているなら子を足りないと言わない は、
// **読めなくする差分を出さない**ことを固定する。
//
// 目的: `claude.env` を `env: {}` と書いた WORKFLOW.md で、その下の項目
// （`CLAUDE_CODE_RETRY_WATCHDOG`）を足りないものとして数えないこと。
// **`env: {}` の下へ行を足すと、YAML として読めないファイルができる。**
// 与える情報: `claude.env` の中身を落として `env: {}` に書き換えた WORKFLOW.md。
// 成功条件: `claude.env` の下のキーが Keys に1つも出ず、差分にも出ないこと。
func TestMissingKeys_親がその行で値を決めているなら子を足りないと言わない(t *testing.T) {
	// `  env:` の行を `  env: {}` に書き換え、その下にぶら下がる行を落とす。
	var out []string
	dropping, replaced := false, false
	for _, line := range strings.Split(fullWorkflow(t), "\n") {
		if dropping {
			if strings.HasPrefix(line, "    ") {
				continue
			}
			dropping = false
		}
		if strings.HasPrefix(line, "  env:") {
			out = append(out, "  env: {}")
			dropping, replaced = true, true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		t.Fatal("env の行が見つかりません")
	}
	content := strings.Join(out, "\n")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	for _, key := range res.Keys {
		if strings.HasPrefix(key, "claude.env.") {
			t.Fatalf("空だと決めてある対応表の中身を足りないと言っている: %v", res.Keys)
		}
	}
	if strings.Contains(res.Patch, "CLAUDE_CODE_RETRY_WATCHDOG") {
		t.Fatalf("読めなくする差分を出している:\n%s", res.Patch)
	}
}

// reorderSections は front matter の `polling` の節を先頭へ動かす。
//
// **利用者が並び順を変えた WORKFLOW.md を作るために使う。**
//
// t: 呼び出し元のテスト。
// content: 元の全文。
// 戻り値: 並び順を変えた全文。
func reorderSections(t *testing.T, content string) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "polling:") {
			start = i
			continue
		}
		if start >= 0 && strings.TrimSpace(line) == "" {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		t.Fatal("polling の節が見つかりません")
	}
	block := append([]string(nil), lines[start:end]...)
	rest := append([]string(nil), lines[:start]...)
	rest = append(rest, lines[end+1:]...)

	// front matter の開始の区切り行の直後へ差し込む。
	out := append([]string{rest[0]}, block...)
	out = append(out, "")
	out = append(out, rest[1:]...)
	return strings.Join(out, "\n")
}

// applyPatch は組み立てた差分を `patch -p0` で当てる。
//
// **本物の `patch` に通す。**自前で当てて確かめると、`patch` が受け取らない形の差分を
// 「当たる」と判定してしまう。**利用者の手元で走るのは `patch` である。**
//
// t: 呼び出し元のテスト。`patch` が無い環境では飛ばす。
// diff: 当てる unified diff。
func applyPatch(t *testing.T, diff string) {
	t.Helper()
	bin, err := exec.LookPath("patch")
	if err != nil {
		t.Skipf("patch が PATH にありません: %v", err)
	}
	cmd := exec.Command(bin, "-p0")
	cmd.Stdin = strings.NewReader(diff)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch が差分を当てられません: %v\n%s\n--- 差分 ---\n%s", err, out, diff)
	}
}
