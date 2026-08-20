package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// backquoteToken は偽の gh のソースの中で、バッククオートの代わりに書く印である。
//
// **Go の raw string literal の中にはバッククオートを書けない。**構造体タグを書くために
// この印を置き、書き出す直前にバッククオートへ置き換える。
const backquoteToken = "@BQ@"

// fakeGHSource は PATH の先頭へ置く偽の `gh` のソースである。
//
// **本物の gh は1回も起動しない。**認証情報も読ませない。
// **状態を持つ。**`gh project item-add` で足した issue は次の `gh project item-list` に出る。
// GraphQL 側（fakegithub_test.go）が同じ board.json を読み書きするので、
// continuo が GraphQL で書き換えた Status も次の `gh project item-list` に出る。
//
// 応答するサブコマンドは次のとおりである（実際の gh の出力の形に合わせてある）。
//
//	gh auth status [--hostname H]           継続できる認証（Active account: true / scope に project）
//	gh auth token                           トークンの文字列（tracker.provider.token_source: gh_auth）
//	gh api user --jq .login                 ログイン名
//	gh project list --owner X [--format json]
//	gh project field-list N --owner X [--format json]
//	gh project item-list N --owner X [--format json]
//	gh project item-add N --owner X --url U
//	gh issue create --repo R --title T --body B
//	gh issue view <URL> [--comments]
//	gh issue comment <URL> --body B
const fakeGHSource = `package main

// 偽の gh である。board.json 1枚を唯一の状態として、実際の gh の出力の形で答える。
// **本物の GitHub へは1バイトも送らない。**

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ghComment は issue に付いたコメント1件である。
type ghComment struct {
	ID        string @BQ@json:"id"@BQ@
	Body      string @BQ@json:"body"@BQ@
	CreatedAt string @BQ@json:"created_at"@BQ@
	Author    string @BQ@json:"author"@BQ@
}

// ghIssue はリポジトリの issue 1件である。
type ghIssue struct {
	NodeID        string @BQ@json:"node_id"@BQ@
	Number        int    @BQ@json:"number"@BQ@
	Title         string @BQ@json:"title"@BQ@
	Body          string @BQ@json:"body"@BQ@
	Repo          string @BQ@json:"repo"@BQ@
	URL           string @BQ@json:"url"@BQ@
	DefaultBranch string @BQ@json:"default_branch"@BQ@
	OnBoard       bool   @BQ@json:"on_board"@BQ@
	ItemID        string @BQ@json:"item_id"@BQ@
	State         string @BQ@json:"state"@BQ@
	CreatedAt     string @BQ@json:"created_at"@BQ@
}

// ghBoard は偽のボード1枚ぶんの状態である。
type ghBoard struct {
	Login         string                 @BQ@json:"login"@BQ@
	Owner         string                 @BQ@json:"owner"@BQ@
	ProjectNumber int                    @BQ@json:"project_number"@BQ@
	ProjectTitle  string                 @BQ@json:"project_title"@BQ@
	ProjectURL    string                 @BQ@json:"project_url"@BQ@
	ProjectID     string                 @BQ@json:"project_id"@BQ@
	StatusField   string                 @BQ@json:"status_field"@BQ@
	StatusOptions []string               @BQ@json:"status_options"@BQ@
	Issues        []*ghIssue             @BQ@json:"issues"@BQ@
	Comments      map[string][]ghComment @BQ@json:"comments"@BQ@
	Calls         []string               @BQ@json:"calls"@BQ@
	NextNumber    int                    @BQ@json:"next_number"@BQ@
	NextItem      int                    @BQ@json:"next_item"@BQ@
	NextComment   int                    @BQ@json:"next_comment"@BQ@
}

// fail は標準エラーへ理由を出して終了コード 1 で終わる。
//
// format: 書式。
// args: 差し込む値。
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// flagValue は引数の並びから --name の値を取り出す。
//
// args: 引数の並び。
// name: 探すフラグ名（-- を含める）。
// 戻り値の1つ目: 値。
// 戻り値の2つ目: 見つかれば真。
func flagValue(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"="), true
		}
	}
	return "", false
}

// hasFlag は引数の並びに --name があるかを返す。
//
// args: 引数の並び。
// name: 探すフラグ名（-- を含める）。
// 戻り値: あれば真。
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

// firstPositional はフラグでない最初の引数を返す。
//
// **フラグの値は飛ばす。**--owner octofake の octofake を位置引数と取り違えないため。
//
// args: 引数の並び。
// 戻り値: 位置引数。無ければ空文字。
func firstPositional(args []string) string {
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && a != "--comments" && a != "--json" {
				skip = true
			}
			continue
		}
		return a
	}
	return ""
}

// wantJSON は --format json が指定されたかを返す。
//
// args: 引数の並び。
// 戻り値: 指定されていれば真。
func wantJSON(args []string) bool {
	v, ok := flagValue(args, "--format")
	return ok && v == "json"
}

// printJSON は値を JSON にして標準出力へ書く。
//
// v: 書き出す値。
func printJSON(v any) {
	encoded, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail("JSON にできません: %v", err)
	}
	fmt.Println(string(encoded))
}

// loadBoard はボードの JSON を読む。
//
// path: ボードの JSON のパス。
// 戻り値: 読み取ったボード。
func loadBoard(path string) *ghBoard {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("偽のボードを読めません: %v", err)
	}
	var b ghBoard
	if err := json.Unmarshal(raw, &b); err != nil {
		fail("偽のボードを解釈できません: %v", err)
	}
	if b.Comments == nil {
		b.Comments = map[string][]ghComment{}
	}
	return &b
}

// saveBoard はボードの JSON を書く（一時ファイルへ書いてから rename する）。
//
// path: ボードの JSON のパス。
// b: 書き出すボード。
func saveBoard(path string, b *ghBoard) {
	encoded, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fail("偽のボードを JSON にできません: %v", err)
	}
	tmp := path + ".tmp.gh"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o600); err != nil {
		fail("偽のボードを書けません: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		fail("偽のボードを置き換えられません: %v", err)
	}
}

// main は1回の gh の呼び出しを処理する。
//
// **ボードのロックを取ってから読み、処理し、書き戻す。**GraphQL 側と同時に走るため。
func main() {
	args := os.Args[1:]
	if len(args) < 2 {
		fail("偽の gh: サブコマンドが足りません: %v", args)
	}
	path := os.Getenv("CONTINUO_E2E_BOARD")
	if path == "" {
		fail("偽の gh: 環境変数 CONTINUO_E2E_BOARD が空です")
	}

	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fail("偽の gh: ロックを開けません: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		fail("偽の gh: ロックを取れません: %v", err)
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	b := loadBoard(path)
	b.Calls = append(b.Calls, args[0]+" "+args[1])
	dispatch(b, args)
	saveBoard(path, b)
}

// dispatch はサブコマンドごとの処理を行う。
//
// b: いまのボード。書き込み系のサブコマンドではここを書き換える。
// args: gh に渡された引数。
func dispatch(b *ghBoard, args []string) {
	rest := args[2:]
	switch args[0] + " " + args[1] {
	case "auth status":
		authStatus(b)
	case "auth token":
		fmt.Println("gho_e2e_fake_token")
	case "api user":
		fmt.Println(b.Login)
	case "project list":
		projectList(b, rest)
	case "project field-list":
		projectFieldList(b, rest)
	case "project item-list":
		projectItemList(b, rest)
	case "project item-add":
		projectItemAdd(b, rest)
	case "issue create":
		issueCreate(b, rest)
	case "issue view":
		issueView(b, rest)
	case "issue comment":
		issueComment(b, rest)
	default:
		fail("偽の gh: 知らないサブコマンドです: %v", args)
	}
}

// authStatus は @BQ@gh auth status@BQ@ の出力を返す（**継続できる認証**の形）。
//
// b: いまのボード。
func authStatus(b *ghBoard) {
	fmt.Println("github.com")
	fmt.Println("  ✓ Logged in to github.com account " + b.Login + " (keyring)")
	fmt.Println("  - Active account: true")
	fmt.Println("  - Git operations protocol: https")
	fmt.Println("  - Token: gho_************************************")
	fmt.Println("  - Token scopes: 'gist', 'project', 'read:org', 'repo', 'workflow'")
}

// projectList は @BQ@gh project list@BQ@ の出力を返す。
//
// b: いまのボード。
// args: サブコマンドより後ろの引数。
func projectList(b *ghBoard, args []string) {
	if wantJSON(args) {
		printJSON(map[string]any{
			"projects": []any{map[string]any{
				"id":     b.ProjectID,
				"number": b.ProjectNumber,
				"title":  b.ProjectTitle,
				"url":    b.ProjectURL,
				"closed": false,
				"public": false,
				"items":  map[string]any{"totalCount": len(onBoard(b))},
				"owner":  map[string]any{"type": "User", "login": b.Owner},
			}},
			"totalCount": 1,
		})
		return
	}
	fmt.Printf("%d\t%s\topen\t%s\n", b.ProjectNumber, b.ProjectTitle, b.ProjectID)
}

// projectFieldList は @BQ@gh project field-list@BQ@ の出力を返す。
//
// **Status のほかに single-select でないフィールドも返す。**continuo が
// 名前と型の両方で選んでいることを確かめられるようにする。
//
// b: いまのボード。
// args: サブコマンドより後ろの引数。
func projectFieldList(b *ghBoard, args []string) {
	options := []any{}
	for i, name := range b.StatusOptions {
		options = append(options, map[string]any{"id": "opt" + strconv.Itoa(i), "name": name})
	}
	fields := []any{
		map[string]any{"id": "PVTF_title", "name": "Title", "type": "ProjectV2Field"},
		map[string]any{
			"id": "PVTSSF_status", "name": b.StatusField,
			"type": "ProjectV2SingleSelectField", "options": options,
		},
	}
	if wantJSON(args) {
		printJSON(map[string]any{"fields": fields, "totalCount": len(fields)})
		return
	}
	fmt.Println("Title\tProjectV2Field\tPVTF_title")
	fmt.Println(b.StatusField + "\tProjectV2SingleSelectField\tPVTSSF_status")
}

// projectItemList は @BQ@gh project item-list@BQ@ の出力を返す。
//
// b: いまのボード。
// args: サブコマンドより後ろの引数。
func projectItemList(b *ghBoard, args []string) {
	items := []any{}
	for _, is := range onBoard(b) {
		items = append(items, map[string]any{
			"id":    is.ItemID,
			"title": is.Title,
			"status": is.State,
			"content": map[string]any{
				"type":       "Issue",
				"body":       is.Body,
				"title":      is.Title,
				"number":     is.Number,
				"repository": is.Repo,
				"url":        is.URL,
			},
		})
	}
	if wantJSON(args) {
		printJSON(map[string]any{"items": items, "totalCount": len(items)})
		return
	}
	for _, is := range onBoard(b) {
		fmt.Printf("Issue\t%s\t#%d\t%s\n", is.Title, is.Number, is.State)
	}
}

// projectItemAdd は @BQ@gh project item-add@BQ@ を処理する（**ボードへ載せる**）。
//
// b: いまのボード。書き換える。
// args: サブコマンドより後ろの引数。
func projectItemAdd(b *ghBoard, args []string) {
	url, ok := flagValue(args, "--url")
	if !ok {
		fail("偽の gh: project item-add には --url が要ります")
	}
	for _, is := range b.Issues {
		if is.URL != url {
			continue
		}
		if is.OnBoard {
			fmt.Println("Item already exists on the project")
			return
		}
		is.OnBoard = true
		is.ItemID = "PVTI_item" + strconv.Itoa(b.NextItem)
		b.NextItem++
		// **載せた直後の Status は空である。**画面で選ぶまで値は入らない。
		is.State = ""
		fmt.Println("Added item")
		return
	}
	fail("偽の gh: %s という issue がありません", url)
}

// issueCreate は @BQ@gh issue create@BQ@ を処理する（**ボードには載せない**）。
//
// b: いまのボード。書き換える。
// args: サブコマンドより後ろの引数。
func issueCreate(b *ghBoard, args []string) {
	repo, ok := flagValue(args, "--repo")
	if !ok {
		fail("偽の gh: issue create には --repo が要ります")
	}
	title, _ := flagValue(args, "--title")
	body, _ := flagValue(args, "--body")
	number := b.NextNumber
	b.NextNumber++
	url := "https://github.com/" + repo + "/issues/" + strconv.Itoa(number)
	b.Issues = append(b.Issues, &ghIssue{
		NodeID:        "I_node" + strconv.Itoa(number),
		Number:        number,
		Title:         title,
		Body:          body,
		Repo:          repo,
		URL:           url,
		DefaultBranch: "main",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	fmt.Println(url)
}

// issueView は @BQ@gh issue view@BQ@ の出力を返す（エージェントが叩く）。
//
// b: いまのボード。
// args: サブコマンドより後ろの引数。
func issueView(b *ghBoard, args []string) {
	target := firstPositional(args)
	is := findByURLOrNumber(b, target, args)
	if is == nil {
		fail("偽の gh: %s という issue がありません", target)
	}
	fmt.Println("title:\t" + is.Title)
	fmt.Println("state:\tOPEN")
	fmt.Println("number:\t" + strconv.Itoa(is.Number))
	fmt.Println("--")
	fmt.Println(is.Body)
	if !hasFlag(args, "--comments") {
		return
	}
	for _, c := range b.Comments[is.NodeID] {
		fmt.Println("--")
		fmt.Println("author:\t" + c.Author)
		fmt.Println(c.Body)
	}
}

// issueComment は @BQ@gh issue comment@BQ@ を処理する（エージェントが叩く）。
//
// b: いまのボード。書き換える。
// args: サブコマンドより後ろの引数。
func issueComment(b *ghBoard, args []string) {
	target := firstPositional(args)
	is := findByURLOrNumber(b, target, args)
	if is == nil {
		fail("偽の gh: %s という issue がありません", target)
	}
	body, ok := flagValue(args, "--body")
	if !ok {
		fail("偽の gh: issue comment には --body が要ります")
	}
	id := "IC_" + strconv.Itoa(b.NextComment)
	b.NextComment++
	b.Comments[is.NodeID] = append(b.Comments[is.NodeID], ghComment{
		ID:        id,
		Body:      body,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Author:    "claude-agent",
	})
	fmt.Println("https://github.com/comment/" + id)
}

// findByURLOrNumber は URL か番号で issue を引く。
//
// b: いまのボード。
// target: URL か issue 番号の文字列。
// args: --repo を読むための引数の並び。
// 戻り値: 見つかった issue。無ければ nil。
func findByURLOrNumber(b *ghBoard, target string, args []string) *ghIssue {
	for _, is := range b.Issues {
		if is.URL == target {
			return is
		}
	}
	number, err := strconv.Atoi(strings.TrimPrefix(target, "#"))
	if err != nil {
		return nil
	}
	repo, _ := flagValue(args, "--repo")
	for _, is := range b.Issues {
		if is.Number == number && (repo == "" || is.Repo == repo) {
			return is
		}
	}
	return nil
}

// onBoard はボードに載っている issue を、載せた順に返す。
//
// b: いまのボード。
// 戻り値: ボードに載っている issue。
func onBoard(b *ghBoard) []*ghIssue {
	var out []*ghIssue
	for _, is := range b.Issues {
		if is.OnBoard {
			out = append(out, is)
		}
	}
	return out
}
`

// fakeGHModule は偽の gh を単独でビルドするための go.mod である。
//
// **continuo のモジュールには足さない。**リポジトリの中に検証専用のパッケージを
// 増やさないため、一時ディレクトリの中だけで完結させる。
const fakeGHModule = "module continuoe2efakegh\n\ngo 1.26\n"

// buildFakeGH は偽の `gh` を一時ディレクトリでビルドして、PATH の先頭へ置く。
//
// **`go run` にしない。**gh は1回の試用で何十回も呼ばれるので、その都度ビルドすると遅い。
//
// t: 呼び出し元のテスト。
// srcDir: ソースを置くディレクトリ。
// binDir: 実行ファイルを置くディレクトリ（PATH の先頭に入れる場所）。
func buildFakeGH(t *testing.T, srcDir, binDir string) {
	t.Helper()
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatalf("偽の gh のソースを置く場所を作れません: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("偽の gh を置く場所を作れません: %v", err)
	}
	source := strings.ReplaceAll(fakeGHSource, backquoteToken, "`")
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("偽の gh のソースを書けません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte(fakeGHModule), 0o600); err != nil {
		t.Fatalf("偽の gh の go.mod を書けません: %v", err)
	}

	cmd := exec.Command(goBinary(t), "build", "-o", filepath.Join(binDir, "gh"), ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("偽の gh をビルドできません: %v\n%s", err, out)
	}
}

// writeFakeGhq は PATH の先頭へ置く偽の `ghq` を作る。
//
// **実物の ghq の置き場所は触らない。**`ghq list -p -e <owner>/<repo>` に対して、
// テストが用意した clone のパスだけを返す。**知らないリポジトリには何も返さない**
// （continuo は0行を「手元に clone が無い」と読む）。
//
// t: 呼び出し元のテスト。
// binDir: 実行ファイルを置くディレクトリ。
// fullName: 答える対象の `<owner>/<repo>`。
// repoDir: その clone の絶対パス。
func writeFakeGhq(t *testing.T, binDir, fullName, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("偽の ghq を置く場所を作れません: %v", err)
	}
	script := "#!/bin/sh\n" +
		"# 偽の ghq。`ghq list -p -e <owner>/<repo>` にだけ答える。\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"" + fullName + "\" ]; then\n" +
		"    echo \"" + repoDir + "\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "ghq"), []byte(script), 0o755); err != nil {
		t.Fatalf("偽の ghq を書けません: %v", err)
	}
}
