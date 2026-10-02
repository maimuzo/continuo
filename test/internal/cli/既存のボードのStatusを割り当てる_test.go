// {"RUCM-CFG-SHA256": "8d589fc83c8eb1ce6e5f20c0184ac0229dfc87928b48f183b4273b070eb02f1e", "SOURCE": "docs/spec/usecases/particular_case/既存のボードのStatusを割り当てる.cfg.json"}
//
// **ユースケース記述「既存のボードのStatusを割り当てる」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// 関数を直に呼ぶテストは経路の一部だけを通すので、いちばん近い経路の番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
)

// {"RUCM-PATH": "P027"}
//
// Test_既存のボードのStatusを割り当てる_P027_WORKFLOWmdが無ければ尋ねずに落とす は、RUCM の代替フロー「WORKFLOWmdが無い」を確かめる。
//
// 目的: 書き換える WORKFLOW.md が無いとき、**役割の割り当てを1つも尋ねずに**落ちること。
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 1 で、stdout に質問（`[1/6]` の `[1/`）が出ていないこと。
func Test_既存のボードのStatusを割り当てる_P027_WORKFLOWmdが無ければ尋ねずに落とす(t *testing.T) {
	code, stdout, stderr := runCLI([]string{"setup", t.TempDir()}, "")
	if code != 1 {
		t.Fatalf("終了コードが 1 でない: %d（stderr: %s）", code, stderr)
	}
	if strings.Contains(stdout, "[1/") {
		t.Errorf("WORKFLOW.md が無いのに役割を尋ねている: %s", stdout)
	}
	if stderr == "" {
		t.Errorf("なぜ止まったかを stderr へ出していない")
	}
}

// {"RUCM-PATH": "P031"}
//
// Test_既存のボードのStatusを割り当てる_P031_statusFieldが空なら落とす は、尋ねる前の検査を確かめる。
//
// **5問すべて答えさせたあとで落とすと、入力が全部捨てられる**（設計 3-32）。
//
// 目的: `--status-field ""` を、カンバンを読みに行く前に弾くこと。
// 与える情報: 空文字と空白だけの文字列。
// 成功条件: 終了コードが 2。
func Test_既存のボードのStatusを割り当てる_P031_statusFieldが空なら落とす(t *testing.T) {
	for _, v := range []string{"", "   "} {
		t.Run("["+v+"]", func(t *testing.T) {
			code, _, _ := runCLI([]string{"setup", "--status-field", v}, "")
			if code != 2 {
				t.Errorf("--status-field %q の終了コードが 2 でない: %d", v, code)
			}
		})
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_既存のボードのStatusを割り当てる_P021_カンバンを読めなければ尋ねずに落とす は、RUCM の代替フロー「ボードを読めない」を確かめる。
//
// **5問すべて答えさせたあとで「カンバンを読めません」と落とすと、入力が全部捨てられる。**
//
// 目的: カンバンの Status を読めないとき、役割の割り当てを1つも尋ねないこと。
// 与える情報: 常に失敗する setupFetchStatusField。
// 成功条件: 終了コードが 1 で、stdout に質問（`[1/6]` の `[1/`）が出ないこと。
func Test_既存のボードのStatusを割り当てる_P021_カンバンを読めなければ尋ねずに落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{}, errors.New("カンバンを読めません")
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code != 1 {
		t.Errorf("終了コードが 1 でない: %d", code)
	}
	if strings.Contains(stdout, "[1/") {
		t.Error("カンバンを読めないのに役割を尋ねている")
	}
	if !strings.Contains(stderr, "カンバンを読めません") {
		t.Errorf("なぜ止まったかを出していない: %s", stderr)
	}
}

// {"RUCM-PATH": "P020"}
//
// Test_既存のボードのStatusを割り当てる_P020_選択肢が5つ未満なら尋ねずに落とす は、尋ねる前の検査を確かめる。
//
// **1つの選択肢を2つの役割へ割り当てないので、選択肢が5つ未満のカンバンでは
// 対話が必ず途中で行き止まる。**尋ねる前に落として、GitHub の画面で足すよう案内する。
//
// 目的: 選択肢が4つ以下のとき、質問を1つも出さないこと。
// 与える情報: 選択肢を3つだけ返す setupFetchStatusField。
// 成功条件: 終了コードが 0 でなく、質問が出ないこと。
func Test_既存のボードのStatusを割り当てる_P020_選択肢が5つ未満なら尋ねずに落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{Name: "Status", Options: []string{"Todo", "In Progress", "Done"}}, nil
	}}

	code, stdout, _ := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code == 0 {
		t.Error("選択肢が足りないのに成功として終わっている")
	}
	if strings.Contains(stdout, "[1/") {
		t.Error("選択肢が足りないのに役割を尋ねている")
	}
}

// {"RUCM-PATH": "P008"}
//
// TestRunSetup_必ず要る5つに答えれば WORKFLOW.md へ書き込む は、`continuo setup` の本筋を確かめる。
//
// 目的: 選択肢が5つあるカンバンで、必ず要る5問に答え、飛ばせる1問を飛ばしたら書き換えること。
//
// **6問目は direct chat である**（設計 3-83）。**選択肢が5つしか無いので、当てる相手がいない。**
// **そこで打ち切ってはならない。**打ち切ると、この機能を使わない人から
// `continuo setup` そのものを奪うことになる。
//
// 与える情報: 選択肢を5つ返す setupFetchStatusField と、番号の入力（最後は 0 で飛ばす）。
// 成功条件: 終了コードが 0 で、WORKFLOW.md に割り当てた選択肢名が入ること。
func Test_既存のボードのStatusを割り当てる_P008_必ず要る5つに答えればWORKFLOWmdへ書き込む(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{
			Name:    "Status",
			Options: []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了"},
		}, nil
	}}

	dir := writeWorkflowFor(t)
	code, stdout, stderr := runCLIWith(deps, []string{"setup", dir}, "1\n2\n3\n4\n5\n0\n")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stdout: %s / stderr: %s）", code, stdout, stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "WORKFLOW.md"))
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	for _, want := range []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q が書き込まれていない", want)
		}
	}
}

// {"RUCM-PATH": "P002"}
//
// Test_既存のボードのStatusを割り当てる_P002_direct_chat_stateが無いWORKFLOWmdでも止めず貼れる1行を出す は、設計 3-83k を確かめる。
//
// 目的: `tracker.direct_chat_state` はあとから足したキーなので、それより前に作った WORKFLOW.md には無い。
// **書き込みを断らず、書けなかったことを名指しし、`tracker:` の下にそのまま貼れる1行を見本に出す。**
// `tracker.direct_chat_state:` の形を見本にすると、貼った行が知らないキーになり、設定の読み込みが落ちる。
// 与える情報: `direct_chat_state` の行を消した WORKFLOW.md と、6問すべてに答える入力。
// 成功条件: 終了コードが 0 で、`  direct_chat_state: "<選んだ値>"` の行が出て、
// `tracker.direct_chat_state:` の形の見本は出ないこと。
func Test_既存のボードのStatusを割り当てる_P002_direct_chat_stateが無いWORKFLOWmdでも止めず貼れる1行を出す(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{
			Name:    "Status",
			Options: []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了", "手で話す"},
		}, nil
	}}

	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	var kept []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "direct_chat_state:") {
			continue
		}
		kept = append(kept, l)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	code, stdout, stderr := runCLIWith(deps, []string{"setup", dir}, "1\n2\n3\n4\n5\n6\n")
	if code != 0 {
		t.Fatalf("キーが無いだけで書き込みを断った: %d（stdout: %s / stderr: %s）", code, stdout, stderr)
	}
	if want := `  direct_chat_state: "手で話す"`; !strings.Contains(stdout, want) {
		t.Errorf("tracker: の下に貼れる1行を出していない（%q が欲しい）: %s", want, stdout)
	}
	if strings.Contains(stdout, "tracker.direct_chat_state:") {
		t.Errorf("貼ると知らないキーになる形の見本を出した: %s", stdout)
	}
}

// {"RUCM-PATH": "P022"}
//
// Test_既存のボードのStatusを割り当てる_P022_カンバンの番号が決まらなければ候補を出して落とす は、setup の案内を確かめる。
//
// **`continuo setup` はどのカンバンの Status を読むかを決められないと進めない。**
// **候補を出さずに落とすと、人間は何を指定すればよいか分からない。**
//
// 目的: カンバンの番号が決まらないとき、候補の一覧と直し方を出して落ちること。
// 与える情報: 候補が3件あって決まらない検出結果。
// 成功条件: 終了コードが 0 でなく、候補の名前と `--project` の案内が出ること。
func Test_既存のボードのStatusを割り当てる_P022_カンバンの番号が決まらなければ候補を出して落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, _ scaffold.DetectOptions) scaffold.Detection {
		return scaffold.Detection{
			Values: scaffold.Values{Owner: "octocat"},
			Fields: []scaffold.Field{
				{Key: scaffold.OwnerKey, Filled: true, Value: "octocat", Reason: "gh api user が返しました"},
				{
					Key:    scaffold.ProjectKey,
					Reason: "カンバンの候補が3件あります",
					Candidates: []scaffold.Project{
						{Number: 3, Title: "開発カンバン", URL: "https://github.com/users/octocat/projects/3"},
						{Number: 8, Title: "検証用", URL: "https://github.com/users/octocat/projects/8"},
						{Number: 9, Title: "使い捨て", URL: "https://github.com/users/octocat/projects/9"},
					},
				},
			},
		}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code == 0 {
		t.Error("カンバンが決まらないのに成功として終わっている")
	}
	out := stdout + stderr
	for _, want := range []string{"開発カンバン", "検証用", "使い捨て", "--project"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q を出していない:\n%s", want, out)
		}
	}
	if strings.Contains(stdout, "[1/") {
		t.Error("カンバンが決まらないのに役割を尋ねている")
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_既存のボードのStatusを割り当てる_P021_WORKFLOWmdに書かれたカンバンを使う は、**書いてあるのに聞き直す**のを落とす。
//
// 目的: フラグが無いとき、WORKFLOW.md の owner とカンバンの番号を検出へ渡すこと。
// 与える情報: `owner: octocat` / `project_number: 42` を書いた WORKFLOW.md。
// 成功条件: 検出がその2つを受け取り、使うカンバンが画面に出ること。
func Test_既存のボードのStatusを割り当てる_P021_WORKFLOWmdに書かれたカンバンを使う(t *testing.T) {
	var got scaffold.DetectOptions
	dir := writeWorkflowWith(t, "octocat", 42)

	_, stdout, _ := runCLIWith(recordingDetect(&got), []string{"setup", dir}, "")

	if got.Owner != "octocat" || got.ProjectNumber != 42 {
		t.Fatalf("WORKFLOW.md に書かれたカンバンを使っていない: %+v", got)
	}
	if !strings.Contains(stdout, "42") {
		t.Errorf("どのカンバンを読むかが画面に出ていない:\n%s", stdout)
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_既存のボードのStatusを割り当てる_P021_フラグはWORKFLOWmdより強い は、明示した指定が勝つことを確かめる。
//
// 目的: `--owner` と `--project` を渡したとき、WORKFLOW.md の値ではなくフラグを使うこと。
// 与える情報: `owner: octocat` / `project_number: 42` を書いた WORKFLOW.md と、別の値のフラグ。
// 成功条件: 検出がフラグの値を受け取ること。
func Test_既存のボードのStatusを割り当てる_P021_フラグはWORKFLOWmdより強い(t *testing.T) {
	var got scaffold.DetectOptions
	dir := writeWorkflowWith(t, "octocat", 42)

	runCLIWith(recordingDetect(&got), []string{"setup", "--owner", "acme", "--project", "7", dir}, "")

	if got.Owner != "acme" || got.ProjectNumber != 7 {
		t.Fatalf("フラグの指定が使われていない: %+v", got)
	}
}
