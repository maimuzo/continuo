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
// 本物の gh を叩くと、その場のログイン状態とカンバンの数でテストの結果が変わる。
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
	`{"content":{"number":1,"repository":"acme/anvil","type":"Issue"}},` +
	`{"content":{"number":2,"repository":"acme/anvil","type":"Issue"}},` +
	`{"content":{"title":"下書き","type":"DraftIssue"}}],"totalCount":4}`

// 目的: PATH に gh が無いとき、RunGH が ErrGHNotFound を返すことを確認する。
// この対応が壊れると、gh が入っていない人に「gh から取得できませんでした（実行ファイルがありません）」
// という、何をすればよいか分からない文言が出る。
// 与える情報: gh を含まない空のディレクトリだけを通した PATH。
// 成功条件: errors.Is で ErrGHNotFound と判定できるエラーが返ること。
func TestRunGH_ghが無ければErrGHNotFoundを返す(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := scaffold.RunGHForHost("github.com")(context.Background(), "api", "user")
	if !errors.Is(err, scaffold.ErrGHNotFound) {
		t.Fatalf("gh が無いことを表すエラーが返っていない: %v", err)
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
		"    owner: octocat                          # 例: https://github.com/octocat なら octocat",
		"    project_number: 3                       # 例: https://github.com/users/octocat/projects/3 なら 3",
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

// orgProjectJSON は organization が持つカンバン1件の出力である。
const orgProjectJSON = `{"projects":[{"closed":false,"number":6,"owner":{"login":"octodev","type":"Organization"},` +
	`"title":"チームの看板","url":"https://github.com/orgs/octodev/projects/6"}],"totalCount":1}`

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
