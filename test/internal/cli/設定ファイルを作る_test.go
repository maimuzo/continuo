// {"RUCM-CFG-SHA256": "aabc96cf347649c45020e53dc347244a1e1eac8a57051a6455c70f2f64e0d39f", "SOURCE": "docs/spec/usecases/particular_case/設定ファイルを作る.cfg.json"}
//
// **ユースケース記述「設定ファイルを作る」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **終端フローごとに代表を対応づける。**18本の経路の全部には書かない。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストは、
// 同じディレクトリの別のファイルへ足す。
package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P001"}
//
// 目的: `continuo init` が置くのはちょうど2枚であることを確かめる（設計 5-3o）。
//
// **WORKFLOW.md が設定で、continuo-ci.yaml は CI へ移すための見本である。**
// **設定は1枚のままである**（設計 5-3g）。2枚目は front matter を持たず、
// **continuo は起動時に1バイトも読まない。**
//
// **3枚目が増えていないことも見る。**置くものが増えるたびに、
// 利用者は「何を .github/workflows/ へ移すのか」を毎回考えることになる。
//
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 0 で、2枚だけが在り、WORKFLOW.md に本文が入っており、
// 画面に配置の案内が出ていること。
func Test_設定ファイルを作る_P001_置くのは設定とCIの雛形の2枚(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "WORKFLOW.md") {
		t.Errorf("標準出力に WORKFLOW.md の行がありません: %q", stdout)
	}
	if !strings.Contains(stdout, scaffold.CIFileName()) {
		t.Errorf("標準出力に %s の行がありません: %q", scaffold.CIFileName(), stdout)
	}
	// **配置の案内を出すこと。**置いただけでは何も起きないので、
	// **移すのは人間である**ことを画面で伝える（設計 5-3o）。
	if !strings.Contains(stdout, ".github/workflows/") {
		t.Errorf("標準出力に配置の案内がありません: %q", stdout)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めません: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{scaffold.CIFileName(), "WORKFLOW.md"}
	sort.Strings(want)
	if !slices.Equal(names, want) {
		t.Errorf("置かれたファイルが %v ではありません: %v", want, names)
	}

	got := readFile(t, filepath.Join(dir, "WORKFLOW.md"))
	if !strings.Contains(got, "## テストの走らせ方") {
		t.Error("WORKFLOW.md に本文の雛形が入っていません（固有の指示を書く場所が消えています）")
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_設定ファイルを作る_P001_引けた値と引けなかった理由を両方出す は、`continuo init` の報告を確かめる。
//
// **`continuo init` は値を埋めるだけでなく、「なぜその値になったか」を出す。**
// **埋まらなかったキーは、何をすればよいかを出す。**出さないと、人間はプレースホルダの
// ままのファイルを渡されて途方に暮れる。
//
// 目的: 埋まったキーの値と理由、埋まらなかったキーの理由と直し方を出すこと。
// 与える情報: owner は埋まり、カンバンの番号は候補が複数で埋まらない検出結果。
// 成功条件: 両方の理由と、候補の一覧と、プレースホルダが残っている旨が出ること。
func Test_設定ファイルを作る_P001_引けた値と引けなかった理由を両方出す(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, _ scaffold.DetectOptions) scaffold.Detection {
		return scaffold.Detection{
			Values: scaffold.Values{Owner: "octocat"},
			Fields: []scaffold.Field{
				{Key: scaffold.OwnerKey, Filled: true, Value: "octocat", Reason: "gh api user が返しました"},
				{
					Key:    scaffold.ProjectKey,
					Reason: "カンバンの候補が2件あります",
					Candidates: []scaffold.Project{
						{Number: 3, Title: "開発カンバン", URL: "https://github.com/users/octocat/projects/3"},
						{Number: 9, Title: "検証用", URL: "https://github.com/users/octocat/projects/9"},
					},
					Advice: []string{"`continuo init --project <番号>` で指定してください"},
				},
			},
		}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"init", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	for _, want := range []string{
		"octocat",     // 埋まった値
		"gh api user", // 埋まった理由
		"カンバンの候補が2件あります", // 埋まらなかった理由
		"開発カンバン",         // 候補の一覧
		"検証用",
		"--project", // 直し方
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q を出していない:\n%s", want, stdout)
		}
	}
}

// {"RUCM-PATH": "P008"}
//
// Test_設定ファイルを作る_P008_後ろに書いたforceが効く は、`continuo init` でも並べ替えが効くことを確かめる。
//
// **1つのサブコマンドだけ違う挙動にしない。**`init <ディレクトリ> --force` が効かないと、
// 「上書きされなかった」と悩むことになる。
//
// 目的: 既に WORKFLOW.md があるディレクトリで `init <ディレクトリ> --force` が上書きすること。
// 与える情報: 人間が足した行を含む WORKFLOW.md があるディレクトリ。
// 成功条件: 終了コードが 0 で、足した行が消えている（＝上書きされた）こと。
func Test_設定ファイルを作る_P008_後ろに書いたforceが効く(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "# 人間が手で足した行\n"
	if err := os.WriteFile(path, []byte(scaffold.Template()+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	deps := cli.Deps{ScaffoldDetect: fixedDetection}

	code, _, stderr := runCLIWith(deps, []string{"init", dir, "--force"}, "")

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if strings.HasSuffix(string(got), mark) {
		t.Error("--force を後ろに書いたのに上書きされていない")
	}
}

// {"RUCM-PATH": "P010"}
//
// 目的: 書き出せなかったときの文言が、書き出せなかった当のファイルを名乗ることを
// 確かめる（設計 5-3g）。
//
// **文言の側にファイルの名前を書くと、別のファイルが落ちたときに無事なほうを名乗る。**
// 読む人は、名乗られたほうを消しに行く。
//
// 与える情報: `WORKFLOW.md` という名前の**ディレクトリ**を置いたディレクトリと、
// `--force`（--force のときだけ writeOne が名指しして止める経路へ入る）。
// 成功条件: 終了コードが 1 で、標準エラーに `WORKFLOW.md` と絶対パスの両方が出ていること。
func Test_設定ファイルを作る_P010_書けないとき落ちた当のファイルを名乗る(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("テスト用のディレクトリを作れません: %v", err)
	}

	code, _, stderr := runInitOffline("--force", dir)

	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）: %q", code, stderr)
	}
	if !strings.Contains(stderr, "WORKFLOW.md") {
		t.Errorf("書けなかったファイルの名前が出ていません: %q", stderr)
	}
	if !strings.Contains(stderr, path) {
		t.Errorf("どこで落ちたかのパスが出ていません: %q", stderr)
	}
}

// {"RUCM-PATH": "P011"}
//
// 目的: 片方だけ在るときは、足りないほうを置いて 0 で終えることを確かめる（設計 5-3o）。
//
// **これが移行の唯一の手順である。**版を上げた利用者が `continuo init` を叩くと、
// 足りない continuo-ci.yaml だけが増える。
// **`--force` を要求してはならない。**要求すると、利用者が手で書いた本文を潰す
// `--force` を打たせることになる（設計 5-3g）。
//
// 与える情報: WORKFLOW.md だけを置いたディレクトリ。**本文に人間が足した行を入れておく。**
// 成功条件: 終了コードが 0 で、continuo-ci.yaml が増え、
// WORKFLOW.md に足した行が1バイトも消えていないこと。
func Test_設定ファイルを作る_P011_片方だけ在るなら足りないほうを置いて0で終える(t *testing.T) {
	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "\n# 人間が手で足した行\n"
	before := readFile(t, path)
	if err := os.WriteFile(path, []byte(before+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（0 であるべきです。stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, scaffold.CIFileName()) {
		t.Errorf("標準出力に %s の行がありません: %q", scaffold.CIFileName(), stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.CIFileName())); err != nil {
		t.Errorf("%s が置かれていません: %v", scaffold.CIFileName(), err)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, mark) {
		t.Error("人間が足した行が消えています")
	}
}

// {"RUCM-PATH": "P012"}
//
// 目的: 既に WORKFLOW.md が在るときは、終了コード 1 で `--force` を勧めることを確かめる
// （設計 5-3g）。
//
// 与える情報: WORKFLOW.md を置いたディレクトリ。
// 成功条件: 終了コードが 1 で、`--force` の案内が出ること。
//
// **2枚とも在るときだけ 1 で終える**（設計 5-3o）。
// **WORKFLOW.md だけが在るときは、足りない continuo-ci.yaml を置いて 0 で終える。**
// そちらは下の `Test_設定ファイルを作る_P011_片方だけ在るなら足りないほうを置いて0で終える` が見る。
func Test_設定ファイルを作る_P012_2枚とも在るなら終了コード1(t *testing.T) {
	dir := writeWorkflowFor(t)
	// **2枚目も置いてから叩く。**1枚目だけだと、足りないほうを置いて 0 で終わる。
	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("%s を置けません: %v", scaffold.CIFileName(), err)
	}

	code, _, stderr := runInitOffline(dir)
	if code != 1 {
		t.Errorf("終了コードが %d です（1 であるべきです）", code)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("--force の案内がありません: %q", stderr)
	}
}

// {"RUCM-PATH": "P012"}
//
// Test_設定ファイルを作る_P012_雛形を置いてから2度目は上書きしない は、`--force` の要否を確かめる。
//
// **`continuo init` が既にある WORKFLOW.md を黙って上書きすると、
// 利用者が手で直した行（`trust.repositories` から消した行など）が全部消える。**
//
// **`continuo init` は2枚を置く**（設計 5-3o）。**設定は WORKFLOW.md の1枚のままで、
// 2枚目の continuo-ci.yaml は CI へ移すための見本である**（設計 5-3g）。
//
// **1度目で2枚とも置かれるので、2度目は「2枚とも既にある」に当たり、`--force` を勧めて 1 で終える。**
// **1度目を飛ばして2度目だけを見ると、足りないほうを置いて 0 で終えるので、この検査は成り立たない。**
//
// **この注釈は、かつて事実でなかった。**commit a4e984c が PROJECT_SPECIFIC_PROMPT.md を
// 2枚目に置いたあと1枚へ戻され、**「2枚を置くようになった」という注釈だけが残っていた。**
// **コードと整合していない注釈は、読む人を誤らせる。**
//
// 目的: 2度目の `init` が、既にある WORKFLOW.md を `--force` 無しでは書き換えないこと。
// 与える情報: 既に WORKFLOW.md があるディレクトリ。**1度目の init で2枚目も置かれる。**
// 成功条件: WORKFLOW.md の中身が変わらないこと。2度目の終了コードが 0 でないこと。
func Test_設定ファイルを作る_P012_雛形を置いてから2度目は上書きしない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "# 人間が手で足した行\n"
	if err := os.WriteFile(path, []byte(scaffold.Template()+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	// **gh は叩かせない**（fixedDetection の説明のとおり、叩くと `go test` が github.com へ出る）。
	runInitOffline(dir)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if !strings.HasSuffix(string(got), mark) {
		t.Error("人間が足した行が消えている")
	}

	// 2回目。**既に在るので、`--force` を勧めて止まる。**
	code, _, _ := runInitOffline(dir)
	if code == 0 {
		t.Error("既にあるのに上書きを許している")
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if !strings.HasSuffix(string(got), mark) {
		t.Error("人間が足した行が消えている")
	}
}

// {"RUCM-PATH": "P014"}
//
// 目的: `WORKFLOW.md` が symlink のとき、辿らずに止めることを確かめる（設計 5-3g）。
//
// **辿ると、指定されたディレクトリの外にあるリンク先を雛形で潰す。**
// `--force` でも辿ってはならない。
//
// 与える情報: 別のディレクトリにある target.md を指す symlink を
// `WORKFLOW.md` として置いたディレクトリ。`--force` の有無の両方。
// 成功条件: どちらも終了コードが 1 で、リンク先の中身が1バイトも変わっておらず、
// symlink が実体のファイルに置き換わっていないこと。
func Test_設定ファイルを作る_P014_書き出す先がsymlinkならリンク先を書き換えずに止まる(t *testing.T) {
	for _, force := range []bool{false, true} {
		dir, target, link := dirWithWorkflowSymlink(t)

		var args []string
		if force {
			args = append(args, "--force")
		}
		code, stdout, stderr := runInitOffline(append(args, dir)...)
		if code != 1 {
			t.Errorf("--force=%v: 終了コードが %d です（1 であるべきです）\n  stdout: %s\n  stderr: %s",
				force, code, stdout, stderr)
		}

		if got := readFile(t, target); got != symlinkTargetBody {
			t.Errorf("--force=%v: symlink を辿って指定ディレクトリの外を書き換えています: got %q, want %q",
				force, got, symlinkTargetBody)
		}

		info, lstatErr := os.Lstat(link)
		if lstatErr != nil {
			t.Fatalf("--force=%v: symlink を確認できません: %v", force, lstatErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("--force=%v: symlink が実体のファイルに置き換わっています", force)
		}
	}
}

// {"RUCM-PATH": "P018"}
//
// Test_設定ファイルを作る_P018_ownerの形が不正なら外部へ接続する前に落とす は、段0 の検査を確かめる。
//
// 目的: `--owner` に GitHub のアカウント名として成り立たない文字列を渡したとき、
// `gh` を1回も起動せずに 2 で止まること。
// 与える情報: 空白や記号を含む owner。
// 成功条件: すべて終了コードが 2。
func Test_設定ファイルを作る_P018_ownerの形が不正なら外部へ接続する前に落とす(t *testing.T) {
	for _, owner := range []string{"has space", "-leading", "trailing-", "a/b", strings.Repeat("x", 40)} {
		t.Run(owner, func(t *testing.T) {
			code, _, stderr := runCLI([]string{"init", "--owner", owner, t.TempDir()}, "")
			if code != 2 {
				t.Errorf("owner=%q の終了コードが 2 でない: %d（stderr: %s）", owner, code, stderr)
			}
		})
	}
}

// {"RUCM-PATH": "P018"}
//
// Test_設定ファイルを作る_P018_projectが0以下なら落とす は、カンバンの番号の検査を確かめる。
//
// 目的: `--project 0` や負の数を、カンバンを引きに行く前に弾くこと。
// 与える情報: 0 と -1。
// 成功条件: 終了コードが 2。
func Test_設定ファイルを作る_P018_projectが0以下なら落とす(t *testing.T) {
	for _, n := range []string{"0", "-1"} {
		t.Run(n, func(t *testing.T) {
			code, _, _ := runCLI([]string{"init", "--project", n, t.TempDir()}, "")
			if code != 2 {
				t.Errorf("--project %s の終了コードが 2 でない: %d", n, code)
			}
		})
	}
}
