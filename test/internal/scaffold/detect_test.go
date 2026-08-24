package scaffold_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// fakeGH は gh の呼び出しを記録し、あらかじめ決めた応答を返す差し替え用の実行関数を作る。
//
// 本物の gh を叩くと、その場のログイン状態とボードの数でテストの結果が変わる。
// 検査したいのは「返ってきた内容をどう解釈するか」なので、コマンドの実行は差し替える。
//
// t: テストコンテキスト。
// responses: 先頭の2語（"api user" / "project list" / "project item-list"）から、
// 返す標準出力とエラーへの対応。**2語で引くのは、`project list` と `project item-list` が
// 別の応答を返す必要があるからである。**
// 戻り値: 差し替え用の実行関数と、呼ばれた引数を順に記録するスライスへのポインタ。
func fakeGH(t *testing.T, responses map[string]struct {
	out []byte
	err error
}) (scaffold.GHRunner, *[]string) {
	t.Helper()
	var calls []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		key := args[0]
		if len(args) > 1 {
			key = args[0] + " " + args[1]
		}
		r, ok := responses[key]
		if !ok {
			t.Errorf("想定していない gh の呼び出し: gh %s", strings.Join(args, " "))
			return nil, errors.New("想定外の呼び出し")
		}
		return r.out, r.err
	}
	return run, &calls
}

// ghResponse は fakeGH に渡す応答を1件作る。
//
// out: 標準出力として返す文字列。
// err: 返すエラー（nil なら成功）。
// 戻り値: fakeGH の responses に入れる値。
func ghResponse(out string, err error) struct {
	out []byte
	err error
} {
	return struct {
		out []byte
		err error
	}{out: []byte(out), err: err}
}

// oneProjectJSON は `gh project list --format json` が候補1件を返したときの出力である。
// 2026-08-19 に `gh project list --owner @me --format json`（gh 2.97.0）で実際に得た形を写した。
const oneProjectJSON = `{"projects":[{"closed":false,"number":3,"owner":{"login":"octocat","type":"User"},` +
	`"title":"AI自動進行管理","url":"https://github.com/users/octocat/projects/3"}],"totalCount":1}`

// twoProjectsJSON は候補が2件あるときの出力である。
const twoProjectsJSON = `{"projects":[` +
	`{"closed":false,"number":3,"title":"AI自動進行管理","url":"https://github.com/users/octocat/projects/3"},` +
	`{"closed":false,"number":7,"title":"試作","url":"https://github.com/users/octocat/projects/7"}],"totalCount":2}`

// twoRepoItemsJSON は `gh project item-list --format json` が、2つのリポジトリの issue と
// draft issue を1件ずつ返したときの出力である。
// 2026-08-20 に `gh project item-list 3 --owner @me --format json`（gh 2.97.0）で
// 実際に得た形から、判定に使う項目だけを写した。
const twoRepoItemsJSON = `{"items":[` +
	`{"content":{"number":188,"repository":"octocat/hello-world","type":"Issue"}},` +
	`{"content":{"number":1,"repository":"maimuzo/continuo","type":"Issue"}},` +
	`{"content":{"number":2,"repository":"maimuzo/continuo","type":"Issue"}},` +
	`{"content":{"title":"下書き","type":"DraftIssue"}}],"totalCount":4}`

// 目的: gh から owner とボードの番号を引いて、雛形の2つのプレースホルダが埋まることを確認する。
// 与える情報: `gh api user` が octocat を返し、`gh project list` が候補1件を返す差し替え。
// 成功条件: Values に octocat と 3 が入り、どちらの Field も Filled であること。
// あわせて、gh の呼び出しが api user / project list / project item-list の3件であること。
func TestDetect_ghから引いた値でownerとproject_numberが埋まる(t *testing.T) {
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
		t.Errorf("ボードの候補を引くとき、引いた owner を渡していない: %q", (*calls)[1])
	}
}

// 目的: --owner と --project が渡されたら、その2つを引くために gh を起動しないことを確認する。
//
// **ボードに載っているリポジトリの一覧だけは、フラグで渡されていても引く。**
// これはフラグが決めるのは「どのボードか」であって「そのボードを読むかどうか」ではないからである
// （trust.repositories はボードの中身を読まないと並べられない。設計 3-33）。
//
// 与える情報: 両方のフラグ相当の値と、item-list にだけ答える差し替え。
// 成功条件: gh の呼び出しが project item-list の1件だけで、渡した値がそのまま Values に入ること。
func TestDetect_フラグで渡されたらownerとボードの番号はghに聞かない(t *testing.T) {
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

// 目的: ボードの候補が複数あるとき、勝手に選ばずに候補と再実行の案内を返すことを確認する。
// 与える情報: `gh project list` が候補2件を返す差し替え。
// 成功条件: project_number が埋まらず、候補2件が Candidates に入り、
// 案内に --project が含まれること。owner のほうは埋まっていること。
func TestDetect_ボードの候補が複数なら選ばずに一覧を返す(t *testing.T) {
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

// 目的: ボードが1件も無いとき、プレースホルダを残したうえで作り方を案内することを確認する。
// 与える情報: `gh project list` が空の一覧を返す差し替え。
// 成功条件: project_number が埋まらず、案内にボードの作り方（gh project create）が含まれること。
func TestDetect_ボードが0件ならプレースホルダを残して作り方を案内する(t *testing.T) {
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
		t.Errorf("ボードの作り方を案内していない: %v", project.Advice)
	}
}

// 目的: 閉じたボードを候補に数えないことを確認する。
// 与える情報: 閉じたボード1件と開いているボード1件を返す差し替え。
// 成功条件: 開いている1件だけが候補になり、その番号が自動で埋まること。
func TestDetect_閉じたボードは候補に数えない(t *testing.T) {
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
		t.Errorf("閉じていないボードの番号が選ばれていない: got %d, want %d", got.Values.ProjectNumber, 9)
	}
}

// 目的: gh が無いときに、失敗させずにプレースホルダのまま案内を出すことを確認する。
// 与える情報: gh が見つからなかったときのエラーを返す差し替え。
// 成功条件: どちらの値も埋まらず、理由が「gh コマンドが見つかりませんでした」であり、
// owner の案内に gh auth login と --owner が含まれること。
func TestDetect_ghが無くても失敗せずに案内を返す(t *testing.T) {
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

// 目的: PATH に gh が無いとき、RunGH が ErrGHNotFound を返すことを確認する。
// この対応が壊れると、gh が入っていない人に「gh から取得できませんでした（実行ファイルがありません）」
// という、何をすればよいか分からない文言が出る。
// 与える情報: gh を含まない空のディレクトリだけを通した PATH。
// 成功条件: errors.Is で ErrGHNotFound と判定できるエラーが返ること。
func TestRunGH_ghが無ければErrGHNotFoundを返す(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := scaffold.RunGH(context.Background(), "api", "user")
	if !errors.Is(err, scaffold.ErrGHNotFound) {
		t.Fatalf("gh が無いことを表すエラーが返っていない: %v", err)
	}
}

// 目的: gh が user / organization 名として使えない文字列を返したら、雛形に書かないことを確認する。
// 与える情報: 改行と引用符を含む文字列を `gh api user` が返す差し替え。
// 成功条件: owner が埋まらず、ボードの候補も引きに行かないこと。
func TestDetect_ownerに使えない文字列は書き込まない(t *testing.T) {
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
		t.Errorf("owner が決まっていないのにボードの候補を引きに行っている: %v", calls)
	}
}

// 目的: 引いた値で雛形の該当行が、コメントごと置き換わることを確認する。
// 与える情報: owner と project_number の両方を持つ Values。
// 成功条件: プレースホルダが1つも残らず、埋めた行のコメントから
// 「ここを埋めること」が消え、コメントの桁がそろっていること。
func TestTemplateWithValues_埋めた行はコメントごと置き換わる(t *testing.T) {
	filled := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})

	if strings.Contains(filled, config.Placeholder) {
		t.Error("owner のプレースホルダが残っている")
	}
	if strings.Contains(filled, "project_number: 0") {
		t.Error("project_number のプレースホルダが残っている")
	}
	for _, want := range []string{
		"    owner: octocat                          # 例: https://github.com/maimuzo なら maimuzo",
		"    project_number: 3                       # 例: https://github.com/users/maimuzo/projects/3 なら 3",
	} {
		if !strings.Contains(filled, want) {
			t.Errorf("埋めたあとの行が想定と違う。次の行が見つからない:\n  %q", want)
		}
	}
	if strings.Contains(filled, "ここを埋めること") {
		t.Error("値が埋まっているのに「ここを埋めること」が残っている")
	}
}

// 目的: 片方しか引けなかったとき、引けたほうだけを埋めることを確認する。
// 与える情報: owner だけを持つ Values。
// 成功条件: owner が埋まり、project_number は 0 のまま残ること。
func TestTemplateWithValues_引けた値だけを埋める(t *testing.T) {
	filled := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat"})

	if strings.Contains(filled, config.Placeholder) {
		t.Error("owner が埋まっていない")
	}
	if !strings.Contains(filled, "project_number: 0") {
		t.Error("引けなかった project_number までが書き換わっている")
	}
}

// 目的: 自動で埋めた WORKFLOW.md が、そのまま continuo の設定として読めることを確認する。
// 与える情報: owner と project_number を埋めて書き出したファイル。
// 成功条件: config.Load がエラーを返さず、埋めた値がそのまま読み出せること。
// 雛形として成立していること（設計 5-2 / 5-3 に照らして）もあわせて確かめる。
func TestWriteTemplateWithValues_埋めたファイルはそのまま読み込める(t *testing.T) {
	dir := t.TempDir()

	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	assertTemplateFollowsDesign(t, "自動で埋めた WORKFLOW.md", string(raw))

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("自動で埋めた雛形を読み込めなかった: %v", err)
	}
	if loaded.Config.Tracker.Provider.Owner != "octocat" {
		t.Errorf("owner が反映されていない: got %q", loaded.Config.Tracker.Provider.Owner)
	}
	if loaded.Config.Tracker.Provider.ProjectNumber != 3 {
		t.Errorf("project_number が反映されていない: got %d", loaded.Config.Tracker.Provider.ProjectNumber)
	}
}

// 目的: 受け付ける owner の文字の範囲を、GitHub の user / organization 名の規則に合わせることを確認する。
// 与える情報: 通る名前と通らない名前。
// 成功条件: 英数字で始まり英数字とハイフンだけの39文字以内だけが通ること。
func TestValidOwner_受け付ける文字を絞る(t *testing.T) {
	for _, name := range []string{"octocat", "a", "my-org", "A1-b2", strings.Repeat("a", 39)} {
		if !scaffold.ValidOwner(name) {
			t.Errorf("受け付けるべき名前が弾かれた: %q", name)
		}
	}
	for _, name := range []string{"", "-abc", "a b", "a\nb", `a"b`, "a/b", strings.Repeat("a", 40)} {
		if scaffold.ValidOwner(name) {
			t.Errorf("弾くべき名前が通った: %q", name)
		}
	}
}

// fieldOf は Detection から、指定したキーの Field を取り出す。
//
// t: テストコンテキスト。
// d: Detect が返した結果。
// key: 取り出すキー（scaffold.OwnerKey / scaffold.ProjectKey）。
// 戻り値: そのキーの Field。見つからなければテストを失敗させる。
func fieldOf(t *testing.T, d scaffold.Detection, key string) scaffold.Field {
	t.Helper()
	for _, f := range d.Fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("%s についての報告が返っていない: %+v", key, d.Fields)
	return scaffold.Field{}
}

// containsSubstring は文字列の一覧のどれかが、指定した部分文字列を含むかを返す。
//
// lines: 調べる文字列の一覧。
// sub: 含まれていてほしい部分文字列。
// 戻り値: どれか1つでも含んでいれば真。
func containsSubstring(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// emptyProjectJSON は候補が1件も無いときの出力である。
const emptyProjectJSON = `{"projects":[],"totalCount":0}`

// orgProjectJSON は organization が持つボード1件の出力である。
const orgProjectJSON = `{"projects":[{"closed":false,"number":6,"owner":{"login":"TS3-SE4","type":"Organization"},` +
	`"title":"チームの看板","url":"https://github.com/orgs/TS3-SE4/projects/6"}],"totalCount":1}`

// ownerAwareGH は、`project list` の応答を owner ごとに変えられる差し替えである。
//
// **既定の fakeGH は `project list` を1つの応答にしか結び付けられない。**
// ログイン名では0件、organization では1件、という状況を作れない。
//
// login: `gh api user` が返すログイン名。
// orgs: `gh api user/orgs` が返す organization の名前（改行区切りで返す）。
// byOwner: owner ごとの `gh project list` の出力。無い owner には空を返す。
// items: `gh project item-list` の出力。
// 戻り値: 差し替えの関数と、呼び出しの記録。
func ownerAwareGH(t *testing.T, login string, orgs []string, byOwner map[string]string, items string) (scaffold.GHRunner, *[]string) {
	t.Helper()
	var calls []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch {
		case len(args) >= 2 && args[0] == "api" && args[1] == "user":
			return []byte(login + "\n"), nil
		case len(args) >= 2 && args[0] == "api" && args[1] == "user/orgs":
			return []byte(strings.Join(orgs, "\n") + "\n"), nil
		case len(args) >= 1 && args[0] == "project" && args[1] == "list":
			owner := ""
			for i, a := range args {
				if a == "--owner" && i+1 < len(args) {
					owner = args[i+1]
				}
			}
			if out, ok := byOwner[owner]; ok {
				return []byte(out), nil
			}
			return []byte(emptyProjectJSON), nil
		case len(args) >= 1 && args[0] == "project" && args[1] == "item-list":
			return []byte(items), nil
		}
		t.Errorf("想定していない gh の呼び出し: gh %s", strings.Join(args, " "))
		return nil, errors.New("想定外の呼び出し")
	}
	return run, &calls
}

// TestDetect_ログイン名にボードが無ければorganizationも探す は、issue #7 の状況を確かめる。
//
// **`gh api user` はログイン名しか返さない。**organization に置いたボードは、
// ログイン名で探しても1件も出ない。**GitHub Enterprise で organization にボードを
// 置いていた利用者が、`continuo setup` で1歩も進めなかった。**
//
// 目的: ログイン名のボードが0件なら、所属する organization のボードも探すこと。
// 与える情報: ログイン名では0件、organization `TS3-SE4` では1件を返す gh。
// 成功条件: ボードの番号が埋まり、**owner も organization に決め直される**こと。
func TestDetect_ログイン名にボードが無ければorganizationも探す(t *testing.T) {
	run, calls := ownerAwareGH(t, "yusuke-omichi-ctc", []string{"TS3-SE4"},
		map[string]string{"TS3-SE4": orgProjectJSON}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.ProjectNumber != 6 {
		t.Errorf("organization のボードを拾えていない: got %d, want 6", got.Values.ProjectNumber)
	}
	// **owner を決め直さないと、どこにも存在しない組み合わせが書かれる。**
	// `project_number` は organization のボードを指すのに、`owner` はログイン名のまま、という状態。
	if got.Values.Owner != "TS3-SE4" {
		t.Errorf("owner をボードの持ち主に合わせていない: got %q, want %q", got.Values.Owner, "TS3-SE4")
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

// TestDetect_ログイン名にボードがあればorganizationを探さない は、余計な呼び出しをしないことを確かめる。
//
// **見つかっているのに所属を引くと、無駄にレートリミットを使う。**
//
// 目的: ログイン名のボードが1件あれば、`api user/orgs` を呼ばないこと。
// 与える情報: ログイン名で1件を返す gh。
// 成功条件: `api user/orgs` を1度も呼ばないこと。
func TestDetect_ログイン名にボードがあればorganizationを探さない(t *testing.T) {
	run, calls := ownerAwareGH(t, "octocat", []string{"some-org"},
		map[string]string{"octocat": oneProjectJSON}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	if got.Values.Owner != "octocat" || got.Values.ProjectNumber != 3 {
		t.Errorf("ログイン名のボードを使っていない: owner=%q number=%d", got.Values.Owner, got.Values.ProjectNumber)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "api user/orgs") {
			t.Errorf("ログイン名で見つかったのに所属を引いている: %v", *calls)
		}
	}
}

// TestDetect_どこにもボードが無ければ探した先を全部出す は、案内の中身を確かめる。
//
// **「見つかりません」だけでは、どこを探したのかが分からない。**
// 利用者は `--owner` に何を渡せばよいかを判断できない。
//
// 目的: ログイン名でも organization でも0件のとき、探した owner を全部示すこと。
// 与える情報: どの owner でも0件を返す gh。
// 成功条件: 理由にログイン名と organization の両方が出て、`--owner` の案内があること。
func TestDetect_どこにもボードが無ければ探した先を全部出す(t *testing.T) {
	run, _ := ownerAwareGH(t, "yusuke-omichi-ctc", []string{"TS3-SE4", "another-org"},
		map[string]string{}, twoRepoItemsJSON)

	got := scaffold.Detect(context.Background(), scaffold.DetectOptions{RunGH: run})

	var reason string
	var advice []string
	for _, f := range got.Fields {
		if f.Key == scaffold.ProjectKey {
			reason, advice = f.Reason, f.Advice
		}
	}
	for _, want := range []string{"yusuke-omichi-ctc", "TS3-SE4", "another-org"} {
		if !strings.Contains(reason, want) {
			t.Errorf("探した owner %q が理由に出ていない: %q", want, reason)
		}
	}
	joined := strings.Join(advice, "\n")
	if !strings.Contains(joined, "--owner") {
		t.Errorf("--owner を案内していない: %q", joined)
	}
}
