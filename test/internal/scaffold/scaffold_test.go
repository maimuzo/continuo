// Package scaffold_test は internal/scaffold の振る舞いを、公開 API
// （scaffold.WriteTemplate / scaffold.Template）を通して検証する。
//
// CLI（cmd/continuo）の run は package main の非公開関数なので test/ から呼べない。
// したがって「終了コード 1 にする」「標準エラーへ出す」といった受け入れの基準は、
// WriteTemplate が返す sentinel error の種類で判定する（docs/plans/impl/01_config.md）。
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

// wantWorkflowPath は、dir に書き出したときに Result.Path が返すはずのパスを組み立てる。
//
// WriteTemplate は Result.Path を symlink を辿った先（実体）で返す。
// macOS の t.TempDir() は /var/folders/...（/private/var への symlink）を返すので、
// filepath.Join(dir, "WORKFLOW.md") をそのまま期待値にすると、実装が正しくても食い違う。
//
// t: テストコンテキスト。
// dir: 書き出す先のディレクトリ。
// 戻り値: symlink を解決したディレクトリの直下の WORKFLOW.md の絶対パス。
func wantWorkflowPath(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("ディレクトリの実体を辿れない（%s）: %v", dir, err)
	}
	return filepath.Join(real, "WORKFLOW.md")
}

// 目的: 位置引数で渡したディレクトリの直下に WORKFLOW.md が1つだけ置かれることを確認する。
// 与える情報: 空の一時ディレクトリ。force は偽。
// 成功条件: エラーにならず、Result.Path が <ディレクトリ>/WORKFLOW.md の絶対パスであり、
// Overwritten が偽で、そのディレクトリの中身が WORKFLOW.md の1件だけであること。
// 書き出した中身が設計 5-2 / 5-3 に照らして雛形として成立していること
// （scaffold.Template() と突き合わせると、雛形を壊しても通ってしまうので照合先にしない）。
func TestWriteTemplate_指定したディレクトリの直下にWORKFLOW_mdだけを置く(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteTemplate(dir, false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	want := wantWorkflowPath(t, dir)
	if result.Path != want {
		t.Errorf("Result.Path が想定と違う: got %q, want %q", result.Path, want)
	}
	if result.Overwritten {
		t.Error("新規に作成したのに Overwritten が真になっている")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("書き出した先を読めない: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "WORKFLOW.md" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("置くのは WORKFLOW.md の1ファイルだけであるべきなのに %v が置かれている", names)
	}

	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	assertTemplateFollowsDesign(t, "書き出した WORKFLOW.md", string(got))
}

// 目的: 位置引数を省いたら、いまいるディレクトリに書くことを確認する。
// 与える情報: 一時ディレクトリへ移動した状態で、dir に空文字を渡す。
// 成功条件: そのディレクトリの直下に WORKFLOW.md ができ、Result.Path がその絶対パスであること。
func TestWriteTemplate_位置引数を省くといまいるディレクトリに書く(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	result, err := scaffold.WriteTemplate("", false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	// macOS の t.TempDir() は /var/... を返すが、これは /private/var への symlink である。
	// os.Getwd は解決後のパスを返すため、比較する前に両方を解決してそろえる。
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("一時ディレクトリの実体を解決できない: %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(filepath.Dir(result.Path))
	if err != nil {
		t.Fatalf("書き出した先の実体を解決できない: %v", err)
	}
	if gotDir != wantDir {
		t.Errorf("いまいるディレクトリに書かれていない: got %q, want %q", gotDir, wantDir)
	}
	if filepath.Base(result.Path) != "WORKFLOW.md" {
		t.Errorf("書き出したファイル名が想定と違う: got %q", filepath.Base(result.Path))
	}
	if _, err := os.Stat(filepath.Join(dir, "WORKFLOW.md")); err != nil {
		t.Errorf("いまいるディレクトリに WORKFLOW.md ができていない: %v", err)
	}
}

// 目的: 相対パスを渡しても Result.Path が絶対パスに正規化されることを確認する。
// 与える情報: 一時ディレクトリへ移動したうえで、その直下のサブディレクトリを相対パス "sub" で渡す。
// 成功条件: Result.Path が絶対パスであり、"sub/WORKFLOW.md" を指していること。
func TestWriteTemplate_相対パスを渡してもResultのPathは絶対パスになる(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("テスト用のサブディレクトリを作れない: %v", err)
	}
	t.Chdir(dir)

	result, err := scaffold.WriteTemplate("sub", false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	if !filepath.IsAbs(result.Path) {
		t.Fatalf("Result.Path が絶対パスでない: %q", result.Path)
	}
	if filepath.Base(filepath.Dir(result.Path)) != "sub" {
		t.Errorf("Result.Path が sub の直下を指していない: %q", result.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "WORKFLOW.md")); err != nil {
		t.Errorf("sub の直下に WORKFLOW.md ができていない: %v", err)
	}
}

// 目的: 既に WORKFLOW.md があるとき、force を付けなければ上書きしないことを確認する。
// 与える情報: 中身が "既存の内容\n" の WORKFLOW.md がある一時ディレクトリ。force は偽。
// 成功条件: ErrAlreadyExists が返り、既存の中身が1バイトも変わっていないこと。
// あわせて Result.Path に衝突したパスが入っていること（CLI がその文言に使う）。
func TestWriteTemplate_既にファイルがあれば上書きせずErrAlreadyExistsを返す(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const existing = "既存の内容\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("テスト用の既存ファイルを置けない: %v", err)
	}

	result, err := scaffold.WriteTemplate(dir, false)
	if !errors.Is(err, scaffold.ErrAlreadyExists) {
		t.Fatalf("ErrAlreadyExists が返らなかった: %v", err)
	}
	if want := wantWorkflowPath(t, dir); result.Path != want {
		t.Errorf("Result.Path に衝突したパスが入っていない: got %q, want %q", result.Path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("既存ファイルを読めない: %v", err)
	}
	if string(got) != existing {
		t.Errorf("上書きしないはずなのに中身が変わっている: got %q, want %q", string(got), existing)
	}
}

// 目的: force を付けたときだけ上書きし、上書きしたことが戻り値で分かることを確認する。
// 与える情報: 中身が "既存の内容\n" の WORKFLOW.md がある一時ディレクトリ。force は真。
// 成功条件: エラーにならず、Overwritten が真で、既存の内容が消え、
// 中身が設計 5-2 / 5-3 に照らして雛形として成立していること。
func TestWriteTemplate_forceを付けたときだけ上書きする(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const existing = "既存の内容\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("テスト用の既存ファイルを置けない: %v", err)
	}

	result, err := scaffold.WriteTemplate(dir, true)
	if err != nil {
		t.Fatalf("force を付けたのに書き出せなかった: %v", err)
	}
	if !result.Overwritten {
		t.Error("既存を上書きしたのに Overwritten が偽になっている")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	if strings.Contains(string(got), existing) {
		t.Error("force を付けたのに既存の内容が残っている")
	}
	assertTemplateFollowsDesign(t, "force で上書きした WORKFLOW.md", string(got))
}

// 目的: 既存のファイルが無いときに force を付けても、上書きしたとは報告しないことを確認する。
// 与える情報: 空の一時ディレクトリ。force は真。
// 成功条件: エラーにならず、Overwritten が偽であること。
func TestWriteTemplate_force付きでも新規作成ならOverwrittenは偽(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteTemplate(dir, true)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	if result.Overwritten {
		t.Error("新規に作成したのに Overwritten が真になっている")
	}
}

// 目的: 位置引数のディレクトリが無ければ、作らずにエラーにすることを確認する（--force でも作らない）。
// 与える情報: 存在しないディレクトリのパス。force が偽の場合と真の場合の両方。
// 成功条件: どちらも ErrDirNotFound が返り、そのディレクトリが作られていないこと。
func TestWriteTemplate_ディレクトリが無ければ作らずにエラーにする(t *testing.T) {
	for _, force := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "存在しない")

		_, err := scaffold.WriteTemplate(dir, force)
		if !errors.Is(err, scaffold.ErrDirNotFound) {
			t.Fatalf("force=%v: ErrDirNotFound が返らなかった: %v", force, err)
		}
		if _, statErr := os.Stat(dir); statErr == nil {
			t.Errorf("force=%v: ディレクトリを作ってはいけないのに作られている: %s", force, dir)
		}
	}
}

// 目的: 位置引数がファイルのパスだったらエラーにすることを確認する（受けるのはディレクトリだけ）。
// 与える情報: 既存のファイルのパス。
// 成功条件: ErrNotADirectory が返り、そのファイルの中身が変わっていないこと。
func TestWriteTemplate_ファイルのパスを渡すとエラーになる(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "なにかのファイル.txt")
	const existing = "触ってはいけない\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("テスト用のファイルを置けない: %v", err)
	}

	_, err := scaffold.WriteTemplate(path, false)
	if !errors.Is(err, scaffold.ErrNotADirectory) {
		t.Fatalf("ErrNotADirectory が返らなかった: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ファイルを読めない: %v", err)
	}
	if string(got) != existing {
		t.Errorf("関係の無いファイルが書き換えられている: got %q, want %q", string(got), existing)
	}
}

// 目的: 書き出す先の WORKFLOW.md が symlink のとき、辿らずに止まることを確認する。
// symlink を辿って書くと、指定されたディレクトリの外にあるリンク先を雛形で潰す。
// 与える情報: 別のディレクトリにある target.md への symlink を WORKFLOW.md として置いた
// ディレクトリ。force が偽の場合と真の場合の両方。
// 成功条件: どちらも ErrSymlink が返り、リンク先の中身が1バイトも変わっていないこと。
// あわせて symlink がそのまま残っている（実体のファイルに置き換わっていない）こと。
func TestWriteTemplate_書き出す先がsymlinkならリンク先を書き換えずに止まる(t *testing.T) {
	for _, force := range []bool{false, true} {
		base := t.TempDir()
		dir := filepath.Join(base, "dir")
		outside := filepath.Join(base, "outside")
		for _, d := range []string{dir, outside} {
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatalf("force=%v: テスト用のディレクトリを作れない: %v", force, err)
			}
		}

		target := filepath.Join(outside, "target.md")
		const existing = "外部の大事なファイル\n"
		if err := os.WriteFile(target, []byte(existing), 0o644); err != nil {
			t.Fatalf("force=%v: テスト用のリンク先を置けない: %v", force, err)
		}
		link := filepath.Join(dir, "WORKFLOW.md")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("force=%v: テスト用の symlink を張れない: %v", force, err)
		}

		_, err := scaffold.WriteTemplate(dir, force)
		if !errors.Is(err, scaffold.ErrSymlink) {
			t.Fatalf("force=%v: ErrSymlink が返らなかった: %v", force, err)
		}

		got, readErr := os.ReadFile(target)
		if readErr != nil {
			t.Fatalf("force=%v: リンク先を読めない: %v", force, readErr)
		}
		if string(got) != existing {
			t.Errorf("force=%v: symlink を辿って指定ディレクトリの外を書き換えている: got %q, want %q",
				force, string(got), existing)
		}

		info, lstatErr := os.Lstat(link)
		if lstatErr != nil {
			t.Fatalf("force=%v: symlink を確認できない: %v", force, lstatErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("force=%v: symlink が実体のファイルに置き換わっている", force)
		}
	}
}

// 目的: 位置引数のディレクトリ自身が symlink のとき、Result.Path が実体側のパスになることを確認する。
//
// os.Stat も os.OpenFile もディレクトリの symlink は辿るので、書き込みは必ずリンク先へ落ちる。
// ここでリンク側のパスを報告すると、実際に書いた場所と報告が食い違う。
//
// 与える情報: 実体のディレクトリ real と、それを指す symlink の link。link を位置引数に渡す。
// 成功条件: Result.Path が real の直下を指し、link 側のパスではないこと。
// あわせて実体の側に WORKFLOW.md ができており、symlink が実体のディレクトリに置き換わっていないこと。
func TestWriteTemplate_ディレクトリがsymlinkならResultのPathは実体側になる(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("テスト用の実体ディレクトリを作れない: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("テスト用の symlink を張れない: %v", err)
	}

	result, err := scaffold.WriteTemplate(link, false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	want := wantWorkflowPath(t, real)
	if result.Path != want {
		t.Errorf("Result.Path が実体側になっていない: got %q, want %q", result.Path, want)
	}
	if result.Path == filepath.Join(link, "WORKFLOW.md") {
		t.Errorf("Result.Path が symlink 側のパスのままである: %q", result.Path)
	}

	if _, err := os.Stat(want); err != nil {
		t.Errorf("実体の側に WORKFLOW.md が置かれていない: %v", err)
	}

	info, lstatErr := os.Lstat(link)
	if lstatErr != nil {
		t.Fatalf("symlink を確認できない: %v", lstatErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("ディレクトリの symlink が実体に置き換わっている")
	}
}

// 目的: 雛形に、埋める場所を示す2つの手がかりが両方入っていることを確認する。
// 与える情報: scaffold.Template() の全文。
// 成功条件: プレースホルダの値（__FILL_ME__）と、コメントの「ここを埋めること」の両方を含むこと。
func TestTemplate_プレースホルダと埋める指示の両方を含む(t *testing.T) {
	tmpl := scaffold.Template()

	if !strings.Contains(tmpl, config.Placeholder) {
		t.Errorf("雛形にプレースホルダ %q が含まれていない", config.Placeholder)
	}
	if !strings.Contains(tmpl, "# ここを埋めること") {
		t.Error("雛形に「# ここを埋めること」のコメントが含まれていない")
	}
	if !strings.Contains(tmpl, "project_number: 0") {
		t.Error("雛形の tracker.provider.project_number が 0（数値のプレースホルダ）になっていない")
	}
}

// 目的: 書き出したままの WORKFLOW.md を config.Load に通すと、
// 残っているプレースホルダを1つのエラーに全部並べて落ちることを確認する。
// 与える情報: scaffold.WriteTemplate が書いたままの WORKFLOW.md。
// 成功条件: エラーになり、その文言に tracker.provider.owner と
// tracker.provider.project_number の2件が両方名指しで並んでいること。
// あわせて、値域の説明（「0より大きい整数にすること」）が出ていないこと
// （プレースホルダの検出が値の検証より先に走っていることの確認）。
func TestWriteTemplate_書き出したままのWORKFLOW_mdはプレースホルダのエラーで落ちる(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplate(dir, false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	_, err = config.Load(result.Path)
	if err == nil {
		t.Fatal("プレースホルダが残っているのに config.Load が通ってしまった")
	}
	msg := err.Error()

	for _, key := range []string{"tracker.provider.owner", "tracker.provider.project_number"} {
		if !strings.Contains(msg, key) {
			t.Errorf("エラーの文言に %s が名指しされていない: %s", key, msg)
		}
	}
	if !strings.Contains(msg, "プレースホルダ") {
		t.Errorf("エラーの文言が「プレースホルダのまま」であることを伝えていない: %s", msg)
	}
	if strings.Contains(msg, "0より大きい整数にすること") {
		t.Errorf("project_number は「プレースホルダのまま」と報告すべきなのに、値域の説明が出ている: %s", msg)
	}
}

// 目的: プレースホルダを埋めた WORKFLOW.md が config.Load を通ることを確認する。
// 雛形が設定として成立していること（設計 5-2 のキー構成と一致していること）の確認でもある。
// 与える情報: 書き出した雛形の __FILL_ME__ を owner 名に、project_number: 0 を 3 に置き換えたファイル。
// 成功条件: config.Load がエラーを返さず、埋めた値が反映されていること。
func TestWriteTemplate_プレースホルダを埋めれば読み込める(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplate(dir, false)
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出した雛形を読めない: %v", err)
	}
	filled := strings.Replace(string(raw), config.Placeholder, "acme", 1)
	filled = strings.Replace(filled, "project_number: 0", "project_number: 3", 1)
	if err := os.WriteFile(result.Path, []byte(filled), 0o644); err != nil {
		t.Fatalf("埋めた雛形を書き戻せない: %v", err)
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("プレースホルダを埋めた雛形を読み込めなかった: %v", err)
	}
	if loaded.Config.Tracker.Provider.Owner != "acme" {
		t.Errorf("tracker.provider.owner が反映されていない: got %q, want %q", loaded.Config.Tracker.Provider.Owner, "acme")
	}
	if loaded.Config.Tracker.Provider.ProjectNumber != 3 {
		t.Errorf("tracker.provider.project_number が反映されていない: got %d, want %d", loaded.Config.Tracker.Provider.ProjectNumber, 3)
	}
	if loaded.PromptTemplate == "" {
		t.Error("雛形の本文（プロンプトのテンプレート）が空になっている")
	}
	if !strings.Contains(loaded.PromptTemplate, "{{.issue.identifier}}") {
		t.Error("雛形の本文に {{.issue.identifier}} が含まれていない（設計 5-3 の本文と食い違っている）")
	}
}
