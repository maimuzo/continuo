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
// 目的: `tracker.provider.comments` を `comments: {}` と書いた WORKFLOW.md で、
// その下の項目（`max` / `order`）を足りないものとして数えないこと。
// **`comments: {}` の下へ行を足すと、YAML として読めないファイルができる。**
// 与える情報: `tracker.provider.comments` の中身を落として `comments: {}` に書き換えた WORKFLOW.md。
// 成功条件: その下のキーが Keys に1つも出ず、差分にも出ないこと。
func TestMissingKeys_親がその行で値を決めているなら子を足りないと言わない(t *testing.T) {
	// `    comments:` の行を `    comments: {}` に書き換え、その下にぶら下がる行を落とす。
	var out []string
	dropping, replaced := false, false
	for _, line := range strings.Split(fullWorkflow(t), "\n") {
		if dropping {
			if strings.HasPrefix(line, "      ") {
				continue
			}
			dropping = false
		}
		if !replaced && strings.HasPrefix(line, "    comments:") {
			out = append(out, "    comments: {}")
			dropping, replaced = true, true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		t.Fatal("tracker.provider.comments の行が見つかりません")
	}
	content := strings.Join(out, "\n")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	for _, key := range res.Keys {
		if strings.HasPrefix(key, "tracker.provider.comments.") {
			t.Fatalf("空だと決めてある対応表の中身を足りないと言っている: %v", res.Keys)
		}
	}
	if strings.Contains(res.Patch, "oldest_first") {
		t.Fatalf("読めなくする差分を出している:\n%s", res.Patch)
	}
}

// TestMissingKeys_字下げが雛形と違っても当てたあと読める は、
// **差分を当てて設定ファイルを壊さない**ことを固定する。
//
// 目的: front matter を4スペースで書いた WORKFLOW.md でも、差し込む行の字下げが
// **同じ深さの兄弟に揃う**こと。**親の行どうしを比べても子の深さは決まらない。**
// 雛形の2スペースのまま差し込むと、当てたあとに front matter が読めなくなる。
// 与える情報: front matter を4スペース刻みに書き直し、`tracker.automated_state_rewrite`
// （v0.1.9 で増えた項目）を落とした WORKFLOW.md。
// 成功条件: `patch -p0` が通り、当てたあとに front matter が読めて、足りない項目が0件になること。
func TestMissingKeys_字下げが雛形と違っても当てたあと読める(t *testing.T) {
	content := doubleIndent(t, fullWorkflow(t))
	content = dropLines(t, content, "    automated_state_rewrite:")
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) != 1 || res.Keys[0] != "tracker.automated_state_rewrite" {
		t.Fatalf("足りないキーの挙げ方が違う: %v", res.Keys)
	}
	// **兄弟と同じ4スペースで差し込む。**2スペースのままだと YAML として読めなくなる。
	if !strings.Contains(res.Patch, "+    automated_state_rewrite:") {
		t.Fatalf("差し込む行の字下げが兄弟に揃っていない:\n%s", res.Patch)
	}
	applyPatch(t, res.Patch)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めません: %v", err)
	}
	if err := config.CheckFrontMatterSyntax(string(after)); err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めなくなった: %v", err)
	}
	res2, err := scaffold.MissingKeys(path, string(after))
	if err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を調べられません: %v", err)
	}
	if len(res2.Keys) != 0 {
		t.Fatalf("差分を当てたのに足りない項目が残っている: %v", res2.Keys)
	}
}

// TestMissingKeys_名前を利用者が決める対応表の中身は数えない は、
// **消せない注意を出し続けない**ことを固定する。
//
// 目的: `claude.env` の下に並ぶ環境変数の名前を、設定項目として数えないこと。
// **あそこに何を並べるかは利用者が決める。**雛形の `CLAUDE_CODE_RETRY_WATCHDOG` は例であり、
// **黙らせる手段は無いので、意図して外した人が永久に `!` を出され続けることになる。**
// 与える情報: 雛形の環境変数の名前を自分のものに替えただけの WORKFLOW.md。
// 成功条件: 足りない項目が0件で、差分が空であること。
func TestMissingKeys_名前を利用者が決める対応表の中身は数えない(t *testing.T) {
	content := strings.Replace(fullWorkflow(t), "CLAUDE_CODE_RETRY_WATCHDOG", "MY_OWN_VAR", 1)
	if !strings.Contains(content, "MY_OWN_VAR") {
		t.Fatal("環境変数の行が見つかりません")
	}
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	if len(res.Keys) != 0 {
		t.Fatalf("利用者が決める環境変数の名前を設定項目として数えている: %v", res.Keys)
	}
	if res.Patch != "" {
		t.Fatalf("足りないものが無いのに差分が出ている:\n%s", res.Patch)
	}
}

// TestMissingKeys_既にある見出しのコメントを二重に足さない は、
// **当てたあとに同じ行が2本並ばない**ことを固定する。
//
// 目的: 雛形が持つ節の見出しのコメント（`# ===== 後始末・使用量・二重起動の防止 =====`）は、
// **その下の節を1つ足すときに一緒に持っていかれる。**利用者のファイルにその行が既にあるなら、
// **差分に入れてはならない。**
// 与える情報: その見出しの直下にある `naming:` の節だけを落とした WORKFLOW.md。
// 成功条件: `patch -p0` が通り、当てたあとに見出しの行が1本だけであること。
func TestMissingKeys_既にある見出しのコメントを二重に足さない(t *testing.T) {
	const heading = "# ===== 後始末・使用量・二重起動の防止 ====="
	content := dropLines(t, fullWorkflow(t), "naming:")
	if strings.Count(content, heading) != 1 {
		t.Fatalf("見出しのコメントが1本ではない: %d", strings.Count(content, heading))
	}
	path := writeWorkflowFile(t, content)

	res, err := scaffold.MissingKeys(path, content)
	if err != nil {
		t.Fatalf("足りない項目を調べられません: %v", err)
	}
	applyPatch(t, res.Patch)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めません: %v", err)
	}
	if got := strings.Count(string(after), heading); got != 1 {
		t.Fatalf("見出しのコメントが %d 本になった:\n%s", got, res.Patch)
	}
	if err := config.CheckFrontMatterSyntax(string(after)); err != nil {
		t.Fatalf("当てたあとの WORKFLOW.md を読めなくなった: %v", err)
	}
}

// doubleIndent は front matter の字下げを2倍にする。
//
// **雛形と違う深さで書いた WORKFLOW.md を作るために使う。**
// 2スペース刻みで書かれた雛形が、4スペース刻みになる。
//
// t: 呼び出し元のテスト。
// content: 元の全文。
// 戻り値: 字下げを2倍にした全文。
func doubleIndent(t *testing.T, content string) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if n := len(line) - len(trimmed); n > 0 {
			line = strings.Repeat(" ", n*2) + trimmed
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
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
