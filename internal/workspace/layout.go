package workspace

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/maimuzo/continuo/internal/normalize"
)

// DefaultHost は issue の URL が空のときに置き場所の <host> として使うホスト名である（3-22）。
// GitHub Enterprise で使うときは issue の URL のホスト部が別の値になる。
const DefaultHost = "github.com"

// rootDirPerm は workspace.root を作るときのパーミッションである（3-20 の段1）。
// 他人に読ませない 0700 にする。worktree の中には作業中のソースが入る。
const rootDirPerm os.FileMode = 0o700

// EnsureRoot は workspace.root を 0700 で作り、シンボリックリンクを解決した絶対パスを返す
// （3-20 の段1・段2）。
//
// **封じ込め検査より前に呼ぶこと。**検査は「解決済みの root」と比較するので、
// root が存在しないと解決そのものができない。
//
// root: 設定の workspace.root（チルダ・環境変数の展開を済ませた値を渡すこと）。
// 戻り値: シンボリックリンクを解決した絶対パス。root が空文字・相対パスの場合、
// ディレクトリを作れない場合、解決に失敗した場合はエラーを返す。
func EnsureRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("workspace.root が空です（worktree の置き場所を決められません）")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf(
			"workspace.root %q が絶対パスではありません（相対パスだと continuo を起動した"+
				"ディレクトリによって worktree の置き場所が変わる）", root)
	}
	if err := os.MkdirAll(root, rootDirPerm); err != nil {
		return "", fmt.Errorf("workspace.root %q を作成できません: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("workspace.root %q のシンボリックリンクを解決できません: %w", root, err)
	}
	return filepath.Clean(resolved), nil
}

// CheckContainment は path が resolvedRoot の内側にあることを確かめる（3-20）。
//
// 仕様が「最も重要な移植性の制約」と呼ぶ不変条件である
// （SPEC.md 9.5 Invariant 2: worktree のパスは置き場所の内側に留まらなければならない）。
// **path 自体はシンボリックリンクを解決しない。**worktree を作る直前はまだ存在せず
// 解決できないためである。作ったあとの解決し直しは CheckContainmentResolved で行う。
//
// resolvedRoot: EnsureRoot が返した解決済みの置き場所。
// path: 検査対象の worktree の絶対パス。
// 戻り値: 内側に無い場合・path が絶対パスでない場合・root と完全に同じ場合にエラーを返す
// （root そのものを worktree にはできない）。
func CheckContainment(resolvedRoot, path string) error {
	if err := checkUnder(resolvedRoot, path, "worktree のパス"); err != nil {
		return fmt.Errorf("%w（SPEC.md 9.5 Invariant 2）", err)
	}
	return nil
}

// checkUnder は path が root の内側にあることを字句だけで確かめる。
//
// **`..` は filepath.Clean が畳むので、`<root>/../etc/passwd` のような値は弾ける。**
// シンボリックリンクは解決しない（解決したうえで比べたい場合は
// CheckContainmentResolved のように、呼び出し側が先に解決してから渡す）。
//
// root: 内側かどうかの基準になるディレクトリ。
// path: 検査対象の絶対パス。
// label: エラー文で path を指すときの呼び名（"worktree のパス" など）。
// 戻り値: path が絶対パスでない場合・root と完全に同じ場合・root の外側にある場合のエラー。
func checkUnder(root, path, label string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s %q が絶対パスではありません（置き場所の内側か判定できない）", label, path)
	}
	cleaned := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if cleaned == cleanRoot {
		return fmt.Errorf("%s %q が置き場所 %q そのものです", label, cleaned, cleanRoot)
	}
	prefix := cleanRoot
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	if !strings.HasPrefix(cleaned, prefix) {
		return fmt.Errorf("%s %q が置き場所 %q の外側にあります", label, cleaned, cleanRoot)
	}
	return nil
}

// CheckContainmentResolved は、既に存在する path のシンボリックリンクを解決したうえで
// 封じ込め検査を行う（3-20 の段4）。
//
// **作ったあとにもう一度比較するための関数である。**解決前と解決後で食い違ったら、
// 置き場所の外側に実体があるということなので、呼び出し側はその worktree を
// **消さずに残して** その issue を失敗として扱う。
//
// resolvedRoot: EnsureRoot が返した解決済みの置き場所。
// path: 検査対象の worktree の絶対パス（既に存在していること）。
// 戻り値の1つ目: シンボリックリンクを解決した path。
// 戻り値の2つ目: 解決できない場合、または解決後のパスが置き場所の外側にある場合のエラー。
func CheckContainmentResolved(resolvedRoot, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("worktree のパス %q のシンボリックリンクを解決できません: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if err := CheckContainment(resolvedRoot, resolved); err != nil {
		return resolved, fmt.Errorf(
			"worktree の実体が置き場所の外側にあります（解決前 %q / 解決後 %q）: %w", path, resolved, err)
	}
	return resolved, nil
}

// HostFromIssueURL は issue の URL のホスト部を返す（3-22）。
//
// rawURL: issue の URL（例 "https://github.com/maimuzo/koetsumugi/issues/188"）。
// 戻り値: ホスト名。**rawURL が空・解析できない・ホスト部が空のときは DefaultHost を返す。**
// GitHub Enterprise では別のホスト名になるので、URL から取ることに意味がある。
func HostFromIssueURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return DefaultHost
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return DefaultHost
	}
	return parsed.Hostname()
}

// RenderBranch は herdr.worktree.branch_template を text/template で描画し、
// 正規化した branch 名を返す（3-22 / 3-7）。
//
// 渡す変数はプロンプト（5-3）と同じ `.issue` である（`.issue.owner` / `.issue.repo` /
// `.issue.number`）。**未知の変数は描画を失敗させる**（missingkey=error）。
// 描画に失敗したら、呼び出し側はその issue を失敗として扱う。
//
// tmpl: branch 名のテンプレート文字列。
// issue: 描画に使う issue の情報。
// 戻り値の1つ目: 正規化を通った branch 名。
// 戻り値の2つ目: 正規化で情報が落ちた場合の警告（黙って別名にせず人間に見せる。3-7）。
// 戻り値の3つ目: テンプレートの構文誤り・未知の変数・描画結果が空の場合のエラー。
func RenderBranch(tmpl string, issue IssueRef) (normalize.SafeName, []normalize.Warning, error) {
	parsed, err := template.New("branch").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", nil, fmt.Errorf("herdr.worktree.branch_template を解析できません（%q）: %w", tmpl, err)
	}
	data := map[string]any{
		"issue": map[string]any{
			"owner":  issue.Owner,
			"repo":   issue.Repo,
			"number": issue.Number,
		},
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return "", nil, fmt.Errorf("herdr.worktree.branch_template を描画できません（%q）: %w", tmpl, err)
	}
	rendered := strings.TrimSpace(buf.String())
	if rendered == "" {
		return "", nil, fmt.Errorf("herdr.worktree.branch_template の描画結果が空です（%q）", tmpl)
	}
	name, warnings := normalize.Normalize(rendered)
	return name, warnings, nil
}

// BranchPrefix は herdr.worktree.branch_template の先頭から、最初の `{{` の直前までを返す
// （3-9 の段6b）。既定のテンプレートなら `continuo/` になる。
//
// **continuo が作った branch かどうかを、その名前だけから判定するための手掛かりである。**
// 片付けで `git branch -D` に渡す名前がこの接頭辞で始まらなければ、continuo が作った
// branch ではないので消さない。
//
// tmpl: herdr.worktree.branch_template。
// 戻り値: 接頭辞。**テンプレートに変数が1つも無ければ空文字を返す**
// （その場合は全部の branch が対象になってしまうので、呼び出し側は判定に使ってはならない）。
func BranchPrefix(tmpl string) string {
	index := strings.Index(tmpl, "{{")
	if index < 0 {
		return ""
	}
	return tmpl[:index]
}

// Slug は branch 名から置き場所のディレクトリ名を作る（3-22）。
// スラッシュをハイフンに置き換えるだけである（gwq の naming.sanitize_chars と同じ規則）。
//
// branch: 描画済みの branch 名。
// 戻り値: スラッシュをハイフンに置き換えた文字列。
func Slug(branch normalize.SafeName) string {
	return strings.ReplaceAll(branch.String(), "/", "-")
}

// pathComponent は host / owner / repo を置き場所のディレクトリ名1階層分へ変換する。
//
// 正規化（3-7）を通したうえで、**スラッシュをハイフンに置き換える。**
// normalize.Normalize はスラッシュを許容する（branch 名に必要なため）ので、
// そのまま使うと1つの値が2階層に割れて、4階層固定の走査（3-4 の段2）と食い違う。
//
// raw: 元の文字列（ホスト名・所有者名・リポジトリ名）。
// 戻り値の1つ目: 1階層分として使える文字列。
// 戻り値の2つ目: 正規化で情報が落ちた場合の警告。
func pathComponent(raw string) (string, []normalize.Warning) {
	name, warnings := normalize.Normalize(raw)
	return strings.ReplaceAll(name.String(), "/", "-"), warnings
}

// Location は1つの worktree の置き場所である（3-22 の `<root>/<host>/<owner>/<repo>/<スラグ>`）。
type Location struct {
	// Root はシンボリックリンクを解決した置き場所である。
	Root string
	// Host は issue の URL のホスト部である（空の URL なら DefaultHost）。
	Host string
	// Owner はリポジトリの所有者名である。
	Owner string
	// Repo はリポジトリ名である。
	Repo string
	// Slug は branch 名のスラッシュをハイフンに置き換えたものである。
	Slug string
	// Path は worktree の絶対パスである（Root から Slug までを繋いだもの）。
	Path string
	// Branch は描画・正規化した branch 名である。
	Branch normalize.SafeName
}

// Locate は issue から worktree の置き場所を組み立てる（3-22）。
//
// branch 名の描画・正規化と、置き場所の各階層の組み立てをまとめて行う。
// **封じ込め検査はここでは行わない**（呼び出し側が CheckContainment を呼ぶ）。
//
// resolvedRoot: EnsureRoot が返した解決済みの置き場所。
// branchTemplate: herdr.worktree.branch_template。
// issue: 対象の issue。
// 戻り値の1つ目: 組み立てた置き場所。
// 戻り値の2つ目: 正規化で情報が落ちた場合の警告をすべて集めたもの。
// 戻り値の3つ目: branch 名の描画に失敗した場合のエラー。
func Locate(resolvedRoot, branchTemplate string, issue IssueRef) (*Location, []normalize.Warning, error) {
	branch, warnings, err := RenderBranch(branchTemplate, issue)
	if err != nil {
		return nil, warnings, err
	}

	host, w := pathComponent(HostFromIssueURL(issue.URL))
	warnings = append(warnings, w...)
	owner, w := pathComponent(issue.Owner)
	warnings = append(warnings, w...)
	repo, w := pathComponent(issue.Repo)
	warnings = append(warnings, w...)

	slug := Slug(branch)
	loc := &Location{
		Root:   resolvedRoot,
		Host:   host,
		Owner:  owner,
		Repo:   repo,
		Slug:   slug,
		Path:   filepath.Join(resolvedRoot, host, owner, repo, slug),
		Branch: branch,
	}
	return loc, warnings, nil
}
