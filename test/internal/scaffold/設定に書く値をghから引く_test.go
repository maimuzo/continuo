// {"RUCM-CFG-SHA256": "6f1ee13667df2a7ecf718e0bbf5cf244f16d44ce66817b9c14edea6788bd6bf5", "SOURCE": "docs/spec/usecases/particular_case/設定に書く値をghから引く.cfg.json"}
//
// **ユースケース記述「設定に書く値をghから引く」の経路に対応づけたテストである。**
// 関数名の `P034` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **42本の経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイル（detect_test.go ほか）に在る。
package scaffold_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P002"}
//
// 目的: --owner と --project が渡されたら、その2つを引くために gh を起動しないことを確認する。
//
// **カンバンに載っているリポジトリの一覧だけは、フラグで渡されていても引く。**
// これはフラグが決めるのは「どのカンバンか」であって「そのカンバンを読むかどうか」ではないからである
// （trust.repositories はカンバンの中身を読まないと並べられない。設計 3-33）。
//
// 与える情報: 両方のフラグ相当の値と、item-list にだけ答える差し替え。
// 成功条件: gh の呼び出しが project item-list の1件だけで、渡した値がそのまま Values に入ること。
func Test_設定に書く値をghから引く_P002_フラグで渡されたらownerとカンバンの番号はghに聞かない(t *testing.T) {
	run, calls := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{
		Owner:         "acme",
		ProjectNumber: 12,
		RunGH:         run,
	})

	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "project item-list 12 --owner acme ") {
		t.Errorf("フラグで渡された値を引き直している、または item-list を叩いていない: %v", *calls)
	}
	if got.Values.Owner != "acme" || got.Values.ProjectNumber != 12 {
		t.Errorf("渡した値がそのまま使われていない: %+v", got.Values)
	}
}

// {"RUCM-PATH": "P027"}
//
// Test_設定に書く値をghから引く_P027_ログイン名にカンバンが無ければorganizationも探す は、issue #7 の状況を確かめる。
//
// **`gh api user` はログイン名しか返さない。**organization に置いたカンバンは、
// ログイン名で探しても1件も出ない。**GitHub Enterprise で organization にカンバンを
// 置いていた利用者が、`continuo setup` で1歩も進めなかった。**
//
// 目的: ログイン名のカンバンが0件なら、所属する organization のカンバンも探すこと。
// 与える情報: ログイン名では0件、organization `octodev` では1件を返す gh。
// 成功条件: カンバンの番号が埋まり、**owner も organization に決め直される**こと。
func Test_設定に書く値をghから引く_P027_ログイン名にカンバンが無ければorganizationも探す(t *testing.T) {
	run, calls := ownerAwareGH(t, "octocat", []string{"octodev"},
		map[string]string{"octodev": orgProjectJSON}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.ProjectNumber != 6 {
		t.Errorf("organization のカンバンを拾えていない: got %d, want 6", got.Values.ProjectNumber)
	}
	// **owner を決め直さないと、どこにも存在しない組み合わせが書かれる。**
	// `project_number` は organization のカンバンを指すのに、`owner` はログイン名のまま、という状態。
	if got.Values.Owner != "octodev" {
		t.Errorf("owner をカンバンの持ち主に合わせていない: got %q, want %q", got.Values.Owner, "octodev")
	}
	if !got.AllFilled() {
		t.Errorf("両方埋まったのに AllFilled が偽である: %+v", got.Fields)
	}

	// **所属を引いていること。**引かなければ organization にはたどり着けない。
	found := false
	for _, c := range *calls {
		if strings.HasPrefix(c, "api user/orgs") {
			found = true
		}
	}
	if !found {
		t.Errorf("所属する organization を引いていない: %v", *calls)
	}
}

// {"RUCM-PATH": "P032"}
//
// 目的: カンバンが1件も無いとき、プレースホルダを残したうえで作り方を案内することを確認する。
// 与える情報: `gh project list` が空の一覧を返す差し替え。
// 成功条件: project_number が埋まらず、案内にカンバンの作り方（gh project create）が含まれること。
func Test_設定に書く値をghから引く_P032_カンバンが0件ならプレースホルダを残して作り方を案内する(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user": ghResponse("octocat\n", nil),
		// **ログイン名で0件なら、所属する organization も探す**（issue #7）。
		// ここでは所属が1つも無い状況にする。
		"api user/orgs": ghResponse("", nil),
		"project list":  ghResponse(`{"projects":[],"totalCount":0}`, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.ProjectNumber != 0 {
		t.Errorf("候補が0件なのに番号が入っている: %d", got.Values.ProjectNumber)
	}
	project := fieldOf(t, got, scaffold.ProjectKey)
	if !containsSubstring(project.Advice, "gh project create") {
		t.Errorf("カンバンの作り方を案内していない: %v", project.Advice)
	}
}

// {"RUCM-PATH": "P032"}
//
// Test_設定に書く値をghから引く_P032_どこにもカンバンが無ければ探した先を全部出す は、案内の中身を確かめる。
//
// **「見つかりません」だけでは、どこを探したのかが分からない。**
// 利用者は `--owner` に何を渡せばよいかを判断できない。
//
// 目的: ログイン名でも organization でも0件のとき、探した owner を全部示すこと。
// 与える情報: どの owner でも0件を返す gh。
// 成功条件: 理由にログイン名と organization の両方が出て、`--owner` の案内があること。
func Test_設定に書く値をghから引く_P032_どこにもカンバンが無ければ探した先を全部出す(t *testing.T) {
	run, _ := ownerAwareGH(t, "octocat", []string{"octodev", "another-org"},
		map[string]string{}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	var reason string
	var advice []string
	for _, f := range got.Fields {
		if f.Key == scaffold.ProjectKey {
			reason, advice = f.Reason, f.Advice
		}
	}
	for _, want := range []string{"octocat", "octodev", "another-org"} {
		if !strings.Contains(reason, want) {
			t.Errorf("探した owner %q が理由に出ていない: %q", want, reason)
		}
	}
	joined := strings.Join(advice, "\n")
	if !strings.Contains(joined, "--owner") {
		t.Errorf("--owner を案内していない: %q", joined)
	}
}

// {"RUCM-PATH": "P034"}
//
// 目的: gh から owner とカンバンの番号を引いて、雛形の2つのプレースホルダが埋まることを確認する。
// 与える情報: `gh api user` が octocat を返し、`gh project list` が候補1件を返す差し替え。
// 成功条件: Values に octocat と 3 が入り、どちらの Field も Filled であること。
// あわせて、gh の呼び出しが api user / project list / project item-list の3件であること。
func Test_設定に書く値をghから引く_P034_ghから引いた値でownerとproject_numberが埋まる(t *testing.T) {
	run, calls := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("octocat\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.Owner != "octocat" {
		t.Errorf("owner が引けていない: got %q, want %q", got.Values.Owner, "octocat")
	}
	if got.Values.ProjectNumber != 3 {
		t.Errorf("project_number が引けていない: got %d, want %d", got.Values.ProjectNumber, 3)
	}
	if !got.AllFilled() {
		t.Errorf("両方埋まったのに AllFilled が偽である: %+v", got.Fields)
	}
	if len(*calls) != 3 {
		t.Errorf("gh の呼び出しは api user / project list / project item-list の3件であるべき: %v", *calls)
	}
	if !strings.HasPrefix((*calls)[1], "project list --owner octocat ") {
		t.Errorf("カンバンの候補を引くとき、引いた owner を渡していない: %q", (*calls)[1])
	}
}

// {"RUCM-PATH": "P034"}
//
// 目的: 閉じたカンバンを候補に数えないことを確認する。
// 与える情報: 閉じたカンバン1件と開いているカンバン1件を返す差し替え。
// 成功条件: 開いている1件だけが候補になり、その番号が自動で埋まること。
func Test_設定に書く値をghから引く_P034_閉じたカンバンは候補に数えない(t *testing.T) {
	closedAndOpen := `{"projects":[` +
		`{"closed":true,"number":1,"title":"終わった板","url":"https://example.invalid/1"},` +
		`{"closed":false,"number":9,"title":"いま使う板","url":"https://example.invalid/9"}],"totalCount":2}`
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("octocat\n", nil),
		"project list":      ghResponse(closedAndOpen, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.ProjectNumber != 9 {
		t.Errorf("閉じていないカンバンの番号が選ばれていない: got %d, want %d", got.Values.ProjectNumber, 9)
	}
}

// {"RUCM-PATH": "P034"}
//
// Test_設定に書く値をghから引く_P034_ログイン名にカンバンがあればorganizationを探さない は、余計な呼び出しをしないことを確かめる。
//
// **見つかっているのに所属を引くと、無駄にレートリミットを使う。**
//
// 目的: ログイン名のカンバンが1件あれば、`api user/orgs` を呼ばないこと。
// 与える情報: ログイン名で1件を返す gh。
// 成功条件: `api user/orgs` を1度も呼ばないこと。
func Test_設定に書く値をghから引く_P034_ログイン名にカンバンがあればorganizationを探さない(t *testing.T) {
	run, calls := ownerAwareGH(t, "octocat", []string{"some-org"},
		map[string]string{"octocat": oneProjectJSON}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.Owner != "octocat" || got.Values.ProjectNumber != 3 {
		t.Errorf("ログイン名のカンバンを使っていない: owner=%q number=%d", got.Values.Owner, got.Values.ProjectNumber)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "api user/orgs") {
			t.Errorf("ログイン名で見つかったのに所属を引いている: %v", *calls)
		}
	}
}

// {"RUCM-PATH": "P034"}
//
// 目的: カンバンに載っているリポジトリを、重複なく辞書順で拾うことを確認する。
// あわせて draft issue を数えないことを見る（リポジトリに属していないので、
// 信頼させる対象が存在しない）。
//
// 与える情報: 同じリポジトリの issue を2件、別のリポジトリの issue を1件、
// draft issue を1件返す `gh project item-list` の差し替え。
// 成功条件: 2件が辞書順で並び、draft issue が数えられていないこと。
func Test_設定に書く値をghから引く_P034_カンバンに載っているリポジトリを重複なく並べる(t *testing.T) {
	run, calls := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("octocat\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	want := []string{"acme/anvil", "octocat/hello-world"}
	if strings.Join(got.Values.Repositories, ",") != strings.Join(want, ",") {
		t.Errorf("拾ったリポジトリが想定と違う: got %v, want %v", got.Values.Repositories, want)
	}
	if !strings.Contains((*calls)[2], "project item-list 3 --owner octocat ") {
		t.Errorf("決まったカンバンの番号と owner で項目を引いていない: %q", (*calls)[2])
	}
}

// {"RUCM-PATH": "P034"}
//
// 目的: 拾ったあとに「要らない行を消せ」と案内することを確認する。
//
// **拾った一覧をそのまま信頼させてはならない**（設計 3-33）。
// ここで案内が出ないと、人間が削る手順そのものが誰にも伝わらない。
//
// 与える情報: リポジトリを2件返す差し替え。
// 成功条件: 案内に「消して」と `continuo trust --dry-run` が含まれること。
func Test_設定に書く値をghから引く_P034_拾ったあとに要らない行を消せと案内する(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("octocat\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse(twoRepoItemsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	field := fieldOf(t, got, scaffold.RepositoriesKey)
	if !field.Filled {
		t.Fatalf("拾えているのに埋まった扱いになっていない: %+v", field)
	}
	if !containsSubstring(field.Advice, "消して") {
		t.Errorf("要らない行を消すことを案内していない: %v", field.Advice)
	}
	if !containsSubstring(field.Advice, "continuo trust --dry-run") {
		t.Errorf("何を許すことになるかの確かめ方を案内していない: %v", field.Advice)
	}
}

// {"RUCM-PATH": "P037"}
//
// 目的: カンバンの項目を引けなくても失敗せず、手で書ける案内を出すことを確認する。
//
// **雛形そのものは書けるので、ここで止めない。**
//
// 与える情報: `gh project item-list` がエラーを返す差し替え。
// 成功条件: repositories が埋まらず、案内に trust.repositories が含まれること。
func Test_設定に書く値をghから引く_P037_カンバンの項目を引けなくても失敗しない(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":          ghResponse("octocat\n", nil),
		"project list":      ghResponse(oneProjectJSON, nil),
		"project item-list": ghResponse("", scaffold.ErrGHNotFound),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if len(got.Values.Repositories) != 0 {
		t.Errorf("引けていないのに値が入っている: %v", got.Values.Repositories)
	}
	field := fieldOf(t, got, scaffold.RepositoriesKey)
	if field.Filled {
		t.Error("引けていないのに埋まった扱いになっている")
	}
	if !containsSubstring(field.Advice, "trust.repositories") {
		t.Errorf("手で書けることを案内していない: %v", field.Advice)
	}
}

// {"RUCM-PATH": "P038"}
//
// 目的: カンバンの候補が複数あるとき、勝手に選ばずに候補と再実行の案内を返すことを確認する。
// 与える情報: `gh project list` が候補2件を返す差し替え。
// 成功条件: project_number が埋まらず、候補2件が Candidates に入り、
// 案内に --project が含まれること。owner のほうは埋まっていること。
func Test_設定に書く値をghから引く_P038_カンバンの候補が複数なら選ばずに一覧を返す(t *testing.T) {
	run, _ := fakeGH(t, map[string]struct {
		out []byte
		err error
	}{
		"api user":     ghResponse("octocat\n", nil),
		"project list": ghResponse(twoProjectsJSON, nil),
	})

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.ProjectNumber != 0 {
		t.Errorf("候補が複数なのに番号を選んでいる: %d", got.Values.ProjectNumber)
	}
	if got.Values.Owner != "octocat" {
		t.Errorf("owner まで埋まらなくなっている: %q", got.Values.Owner)
	}
	project := fieldOf(t, got, scaffold.ProjectKey)
	if len(project.Candidates) != 2 {
		t.Fatalf("候補の一覧が返っていない: %+v", project.Candidates)
	}
	if project.Candidates[0].Number != 3 || project.Candidates[1].Number != 7 {
		t.Errorf("候補の番号が想定と違う: %+v", project.Candidates)
	}
	if !containsSubstring(project.Advice, "--project") {
		t.Errorf("--project で選び直せることを案内していない: %v", project.Advice)
	}
}

// {"RUCM-PATH": "P042"}
//
// 目的: gh が無いときに、失敗させずにプレースホルダのまま案内を出すことを確認する。
// 与える情報: gh が見つからなかったときのエラーを返す差し替え。
// 成功条件: どちらの値も埋まらず、理由が「gh コマンドが見つかりませんでした」であり、
// owner の案内に gh auth login と --owner が含まれること。
func Test_設定に書く値をghから引く_P042_ghが無くても失敗せずに案内を返す(t *testing.T) {
	run := func(_ context.Context, _ ...string) ([]byte, error) {
		return nil, scaffold.ErrGHNotFound
	}

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.Owner != "" || got.Values.ProjectNumber != 0 {
		t.Errorf("gh が無いのに値が入っている: %+v", got.Values)
	}
	if got.AllFilled() {
		t.Error("何も埋まっていないのに AllFilled が真である")
	}
	owner := fieldOf(t, got, scaffold.OwnerKey)
	if owner.Reason != "gh コマンドが見つかりませんでした" {
		t.Errorf("gh が無いことを理由として出していない: %q", owner.Reason)
	}
	if !containsSubstring(owner.Advice, "gh auth login") {
		t.Errorf("ログインの案内が出ていない: %v", owner.Advice)
	}
	if !containsSubstring(owner.Advice, "--owner") {
		t.Errorf("--owner で指定できることを案内していない: %v", owner.Advice)
	}
}

// {"RUCM-PATH": "P042"}
//
// 目的: gh が user / organization 名として使えない文字列を返したら、雛形に書かないことを確認する。
// 与える情報: 改行と引用符を含む文字列を `gh api user` が返す差し替え。
// 成功条件: owner が埋まらず、カンバンの候補も引きに行かないこと。
func Test_設定に書く値をghから引く_P042_ownerに使えない文字列は書き込まない(t *testing.T) {
	var calls []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte("oct\"ocat\nowner: attacker"), nil
	}

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.Owner != "" {
		t.Errorf("受け付けてはならない owner が入っている: %q", got.Values.Owner)
	}
	if len(calls) != 1 {
		t.Errorf("owner が決まっていないのにカンバンの候補を引きに行っている: %v", calls)
	}
}
