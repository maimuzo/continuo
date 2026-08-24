package abandon

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// IssueRef は issue の URL から取り出した宛先である。
//
// **worktree のパスをここから組み立ててはならない。**`workspace.root` や
// `herdr.worktree.branch_template` を変えている環境では、組み立てたパスが空振りする。
// worktree は身元ファイルの `issue_url` で照合して探す（Find を見よ）。
type IssueRef struct {
	// URL は渡された issue の URL をそのまま持つ。
	URL string
	// Host は URL のホスト名である（小文字に揃えてある）。
	Host string
	// Owner はリポジトリの所有者名である。
	Owner string
	// Repo はリポジトリ名である。
	Repo string
	// Number は issue の番号である。
	Number int
}

// Identifier は `<owner>/<repo>#<番号>` の形の人間可読な名前を返す。
//
// **ボードから issue を引く鍵である**（tracker.FetchIssueByIdentifier がこの形を取る）。
//
// 戻り値: `octocat/hello-world#42` の形の文字列。
func (r IssueRef) Identifier() string {
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

// SameIssue は、別の issue の URL が同じ issue を指すかどうかを返す。
//
// **文字列の一致では照合しない。**身元ファイルに書かれた URL は末尾のスラッシュや
// 大文字小文字が揺れうる。GitHub は owner とリポジトリ名の大文字小文字を区別しないので、
// **ホスト・owner・リポジトリ名は大文字小文字を無視して比べ、番号は数として比べる。**
//
// **解釈できない URL は、どれにも一致しない。**解釈できないものを推測で一致させると、
// 別の issue の worktree を消しかねない。**文字列の一致で拾い直すこともしない。**
// この型は `ParseIssueURL` が作るので `URL` は必ず解釈できる形であり、
// **それと同じ文字列なら相手も解釈できる。**拾い直す先が無い。
//
// other: 比べる相手の URL（身元ファイルの issue_url）。
// 戻り値: 同じ issue を指していれば true。
func (r IssueRef) SameIssue(other string) bool {
	trimmed := strings.TrimSpace(other)
	if trimmed == "" {
		return false
	}
	parsed, err := ParseIssueURL(trimmed)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host) &&
		strings.EqualFold(parsed.Owner, r.Owner) &&
		strings.EqualFold(parsed.Repo, r.Repo) &&
		parsed.Number == r.Number
}

// ParseIssueURL は issue の URL を owner / repo / 番号 に分解する。
//
// 受け付ける形は `<scheme>://<host>/<owner>/<repo>/issues/<番号>` である
// （例 `https://github.com/octocat/hello-world/issues/42`）。
// 末尾のスラッシュ・クエリ・フラグメントは落とす。
//
// **`pull` は受け付けない。**pull request の URL を渡されても、continuo が worktree を
// 用意した単位は issue なので、探す鍵にならない。
//
// raw: 渡された URL。
// 戻り値の1つ目: 分解した宛先。
// 戻り値の2つ目: URL として読めない場合・形が合わない場合・番号が正の整数でない場合のエラー。
func ParseIssueURL(raw string) (IssueRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLEmpty)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLUnparsable, trimmed, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLBadScheme, trimmed)
	}
	if u.Host == "" {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLNoHost, trimmed)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "issues" {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLBadShape, trimmed)
	}
	owner, repo := parts[0], parts[1]
	if owner == "" || repo == "" {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLBadShape, trimmed)
	}
	// **`strconv.Atoi` だけでは足りない。**`+42` も `042` も 42 として通ってしまい、
	// **同じ issue を指す URL が何通りもできる。**消す相手を決める鍵なので、
	// **10進の数字だけ・先頭が 0 でない**ことを確かめて1通りに固定する。
	if !isPlainNumber(parts[3]) {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLBadNumber, trimmed, parts[3])
	}
	number, convErr := strconv.Atoi(parts[3])
	if convErr != nil || number <= 0 {
		return IssueRef{}, i18n.Errorf(i18n.KeyAbandonIssueURLBadNumber, trimmed, parts[3])
	}

	return IssueRef{
		URL:    trimmed,
		Host:   strings.ToLower(u.Hostname()),
		Owner:  owner,
		Repo:   repo,
		Number: number,
	}, nil
}

// isPlainNumber は、10進の数字だけで書かれていて先頭が 0 でないかを返す。
//
// **符号も前置ゼロも受け付けない。**`+42` と `042` と `42` を同じ issue として
// 受け付けると、**同じ相手を指す URL が何通りもできる。**消す相手を決める鍵は
// 1通りに固定しておくこと。
//
// s: 調べる文字列。
// 戻り値: `1` 以上の10進整数が前置ゼロ無しで書かれていれば true。
func isPlainNumber(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
