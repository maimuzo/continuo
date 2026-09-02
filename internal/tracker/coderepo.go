package tracker

import (
	"net/url"
	"strings"
)

// codeRepoResolution は「コードのリポジトリはどれか」「base に使う branch はどれか」の答えである
// （設計 issue144 の 11a）。
//
// **リンクが0本なら、issue のリポジトリがそのまま入る。**今までと1文字も変わらない。
type codeRepoResolution struct {
	// NameWithOwner はコードのリポジトリである（`<owner>/<repo>`）。**空にしない。**
	NameWithOwner string
	// Host はコードのリポジトリの URL のホスト部である。**空にしない。**
	Host string
	// DefaultBranch はコードのリポジトリの既定 branch である。取れなければ空。
	DefaultBranch string
	// PRTarget は PR の宛先である（fork なら派生元）。**空にしない。**
	PRTarget string
	// BranchName は base に使うリンクされた branch の名前である。
	// **リンクがちょうど1本のときだけ入る。**
	BranchName *string
	// Links はリンクされた branch の一覧である（人間へ見せる文面のため）。
	Links []LinkedBranchRef
	// Undecided は、コードのリポジトリを1つに決められなかったことを表す。
	Undecided bool
}

// resolveCodeRepo は、Development のリンクからコードのリポジトリと base を決める
// （設計 issue144 の 11a）。
//
// **決め方はリンクの本数だけで決まる。推測はしない。**
//
//	0本            … issue のリポジトリ。base は今までどおり（BranchName は nil）
//	1本            … そのリンクのリポジトリ。base はそのリンクの branch
//	2本以上・全部同じ … そのリポジトリ。base は既定へ倒す（BranchName は nil）
//	2本以上・別々    … 決めない（Undecided）。着手しない
//	totalCount > 窓 … 決めない（Undecided）。窓の外が別のリポジトリでないと言えない
//
// conn: `linkedBranches` の応答。**nil なら0本として扱う。**
// issueOwner: issue のリポジトリの所有者名。
// issueRepo: issue のリポジトリ名。
// issueRepository: issue のリポジトリの応答（既定 branch を取る）。**nil でもよい。**
// issueURL: issue の URL（ホスト部を取る）。
// 戻り値: 決まった値。**Undecided が真でも、各フィールドは issue のリポジトリで埋めてある**
// （下流が空文字を掴まないようにするため。着手しないので実際には使われない）。
func resolveCodeRepo(
	conn *rawLinkedBranchConn,
	issueOwner, issueRepo string,
	issueRepository *rawRepository,
	issueURL string,
) codeRepoResolution {
	fallback := codeRepoResolution{
		NameWithOwner: issueOwner + "/" + issueRepo,
		Host:          hostFromURL(issueURL),
	}
	fallback.PRTarget = fallback.NameWithOwner
	if issueRepository != nil && issueRepository.DefaultBranchRef != nil {
		fallback.DefaultBranch = issueRepository.DefaultBranchRef.Name
	}

	if conn == nil {
		return fallback
	}

	links := make([]LinkedBranchRef, 0, len(conn.Nodes))
	refs := make([]*rawLinkedRef, 0, len(conn.Nodes))
	for i := range conn.Nodes {
		ref := conn.Nodes[i].Ref
		if ref == nil || ref.Name == "" {
			continue
		}
		nameWithOwner := fallback.NameWithOwner
		if ref.Repository != nil && ref.Repository.NameWithOwner != "" {
			nameWithOwner = ref.Repository.NameWithOwner
		}
		links = append(links, LinkedBranchRef{NameWithOwner: nameWithOwner, Branch: ref.Name})
		refs = append(refs, ref)
	}
	fallback.Links = links

	if len(refs) == 0 {
		return fallback
	}

	// **窓に収まらない本数がリンクされていたら決めない。**先頭が全部同じリポジトリでも、
	// 見えていない6本目が別のリポジトリを指しているかもしれない。
	if conn.TotalCount > len(conn.Nodes) {
		fallback.Undecided = true
		return fallback
	}

	// **別々のリポジトリを指す2本があったら決めない。**勝手に選ぶと、
	// 別のリポジトリで作業を始めてしまう。
	for _, l := range links[1:] {
		if !strings.EqualFold(l.NameWithOwner, links[0].NameWithOwner) {
			fallback.Undecided = true
			return fallback
		}
	}

	resolved := fallback
	resolved.NameWithOwner = links[0].NameWithOwner
	if repository := refs[0].Repository; repository != nil {
		if host := hostFromURL(repository.URL); host != "" {
			resolved.Host = host
		}
		if repository.DefaultBranchRef != nil && repository.DefaultBranchRef.Name != "" {
			resolved.DefaultBranch = repository.DefaultBranchRef.Name
		} else {
			resolved.DefaultBranch = ""
		}
		if repository.Parent != nil && repository.Parent.NameWithOwner != "" {
			resolved.PRTarget = repository.Parent.NameWithOwner
		} else {
			resolved.PRTarget = resolved.NameWithOwner
		}
	} else {
		// リポジトリまで答えが返っていない。**issue のリポジトリと同じだと決めつけない**
		// 材料が無いので、名前以外は issue の側の値をそのまま使う。
		resolved.PRTarget = resolved.NameWithOwner
	}

	if len(refs) == 1 {
		// **base に使えるのは、リンクがちょうど1本のときだけである。**
		name := refs[0].Name
		resolved.BranchName = &name
	}
	return resolved
}

// hostFromURL は URL のホスト部を返す。
//
// **読めなければ空文字を返す。**呼び出し側が「取れなかった」として倒せるようにする。
//
// raw: URL の文字列。
// 戻り値: ホスト部。読めなければ空文字。
func hostFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// splitNameWithOwner は `<owner>/<repo>` を所有者名とリポジトリ名に割る。
//
// **最初の `/` 1つだけで割る。**GitHub のリポジトリ名にスラッシュは入らないので、
// 残りが出るならそれはこの形の値ではない。**そのときは両方とも空文字を返す。**
//
// nameWithOwner: `<owner>/<repo>` の文字列。
// 戻り値の1つ目: 所有者名。**割れなければ空文字。**
// 戻り値の2つ目: リポジトリ名。**割れなければ空文字。**
func splitNameWithOwner(nameWithOwner string) (string, string) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(nameWithOwner), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", ""
	}
	return owner, repo
}

// CodeOwnerRepo はコードのリポジトリを所有者名とリポジトリ名に割って返す。
//
// **`<owner>/<repo>` を1本の文字列で持っているのは、写しで分解を忘れたときに
// 置き場所が壊れるのを名前で防ぐためである**（設計 issue144 の 11）。
// **割れなければ両方とも空文字を返す。**呼び出し側は issue のリポジトリへ倒すこと。
//
// 戻り値の1つ目: コードのリポジトリの所有者名。
// 戻り値の2つ目: コードのリポジトリ名。
func (i Issue) CodeOwnerRepo() (string, string) {
	return splitNameWithOwner(i.CodeRepoNameWithOwner)
}
