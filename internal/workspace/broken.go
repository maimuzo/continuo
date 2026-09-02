package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// 壊れた worktree の見つけ方と、人間への案内（設計 3-49）。
//
// **壊れた worktree とは「continuo が身元を確かめられない worktree」である。**
// 2種類しかない。
//
//	身元ファイルが読めない … JSON が壊れている・通常のファイルでない・上限を超えた
//	身元ファイルが無い     … 着手の段6 の手前で落ちると、worktree だけができる
//
// **どちらも1バイトも消さない。**消してよいかを決めるのは人間であり、continuo ではない。
// continuo がやるのは「何が起きているか」と「次に何をすべきか」を出すことだけである。

// slugNumberProbe は、`branch_template` のどこに issue の番号が入るかを調べるための
// 番号である（issueNumberFromSlug）。
//
// **実在しうる issue 番号と紛れない値にする。**テンプレートの固定部分にたまたま
// 同じ数字が並んでいると切り出しに失敗するので、桁数を大きく取る。
// **これは探索の手掛かりを作るためだけの値であり、この番号で何かを引くことは無い。**
const slugNumberProbe = 1000000007

// BrokenKind は worktree の身元を確かめられない理由の種類である。
type BrokenKind string

const (
	// BrokenIdentityUnreadable は、身元ファイルはあるのに読めないことを表す。
	BrokenIdentityUnreadable BrokenKind = "identity_unreadable"
	// BrokenIdentityMissing は、身元ファイルが無いことを表す。
	//
	// **置き場所の最下層のディレクトリ名が `branch_template` の形をしているものだけが
	// これになる。**形が合わないディレクトリは**人間が置いたもの**として扱い、
	// 壊れた worktree に数えない（数えると、既定の「止める」で continuo が起動しなくなる）。
	BrokenIdentityMissing BrokenKind = "identity_missing"
)

// PathClue は worktree の置き場所のパスだけから引ける手掛かりである（設計 3-49）。
//
// **身元ファイルを1バイトも読まずに作る。**身元ファイルは worktree の直下にあり、
// その worktree ではエージェントが `--permission-mode dontAsk` で動く（3-16 の段9）。
// **パスは封じ込め検査（3-20）を通ったものなので、エージェントには書き換えられない。**
type PathClue struct {
	// Host は置き場所の1階層目である（issue の URL のホスト部）。
	Host string
	// Owner は置き場所の2階層目である。
	Owner string
	// Repo は置き場所の3階層目である。
	Repo string
	// Slug は置き場所の4階層目（最下層のディレクトリ名）である。
	Slug string
	// Number はスラグから切り出せた issue の番号である。
	// **切り出せなければ 0 である**（`branch_template` の形と合わなかった）。
	Number int
	// IssueOwner は、Number を切り出せたときに使った issue の所有者名である。
	//
	// **Owner とは別物である。**Owner は置き場所の2階層目、つまり
	// **コードのリポジトリ**の所有者名であり、issue のリポジトリとは限らない
	// （設計 issue144 の 8）。**Number が 0 なら空文字である。**
	IssueOwner string
	// IssueRepo は、Number を切り出せたときに使った issue のリポジトリ名である。
	// **Number が 0 なら空文字である。**
	IssueRepo string
}

// IssueHintFunc は、worktree のパスから「その worktree はどの issue のものか」を答える。
//
// **置き場所からは引けないので、外から渡してもらう**（設計 issue144 の 8c）。
// 置き場所の2・3階層目はコードのリポジトリなので、
// **issue のリポジトリとコードのリポジトリが違う形では、そこから issue を組み立てられない。**
// いま手掛かりになるのは pane の label（`<owner>/<repo>/issues/<番号>`。3-3）だけである。
//
// worktreePath: worktree の絶対パス。
// 戻り値の1つ目: issue のリポジトリの所有者名。**分からなければ空文字。**
// 戻り値の2つ目: issue のリポジトリ名。**分からなければ空文字。**
type IssueHintFunc func(worktreePath string) (string, string)

// IssueURL は手掛かりから組み立てた issue の URL を返す。
//
// **人間への案内に出すためだけの値である。**この URL でボードを引くわけではない
// （引くのは `<owner>/<repo>#<番号>` の識別子である）。
//
// 戻り値: `https://<host>/<owner>/<repo>/issues/<番号>`。
// **番号を切り出せていなければ空文字である。**
func (c *PathClue) IssueURL() string {
	if c == nil || c.Number <= 0 || c.Host == "" || c.IssueOwner == "" || c.IssueRepo == "" {
		return ""
	}
	// **置き場所の owner/repo が issue のものだと言えないなら、URL を組み立てない**
	// （設計 issue144 の 8c）。置き場所の1階層目はコードのリポジトリのホストなので、
	// **issue が別のリポジトリにある形でここを埋めると、実在しない issue を名乗る。**
	if !strings.EqualFold(c.IssueOwner, c.Owner) || !strings.EqualFold(c.IssueRepo, c.Repo) {
		return ""
	}
	return fmt.Sprintf("https://%s/%s/%s/issues/%d", c.Host, c.IssueOwner, c.IssueRepo, c.Number)
}

// Identifier は手掛かりから組み立てた `<owner>/<repo>#<番号>` を返す。
//
// **ボードを1件引くための鍵である**（tracker の FetchIssueByIdentifier に渡す）。
//
// 戻り値: `<owner>/<repo>#<番号>`。**番号を切り出せていなければ空文字である。**
func (c *PathClue) Identifier() string {
	if c == nil || c.Number <= 0 || c.IssueOwner == "" || c.IssueRepo == "" {
		return ""
	}
	// **置き場所の owner/repo ではなく、番号を切り出せたときの issue の owner/repo を使う**
	// （設計 issue144 の 8c）。置き場所の2・3階層目はコードのリポジトリなので、
	// **そこから組み立てると実在しない issue を引きに行く。**
	return fmt.Sprintf("%s/%s#%d", c.IssueOwner, c.IssueRepo, c.Number)
}

// BrokenWorktree は、身元を確かめられない worktree 1件である（設計 3-49）。
type BrokenWorktree struct {
	// Path は worktree の絶対パスである。
	Path string
	// Kind は身元を確かめられない理由の種類である。
	Kind BrokenKind
	// Err は身元ファイルを読めなかった理由である（Kind が BrokenIdentityMissing なら nil）。
	Err error
	// Clue は置き場所のパスから引けた手掛かりである。**引けなければ nil。**
	Clue *PathClue
}

// What は「何が起きているか」を人間が読む1文で返す（設計 3-49）。
//
// identityFileName: 設定の `workspace.identity_file`（文に身元ファイルのパスを出すため）。
// 戻り値: 画面とログに出す1文。
func (b BrokenWorktree) What(identityFileName string) string {
	path := filepath.Join(b.Path, identityFileName)
	if b.Kind == BrokenIdentityMissing {
		return i18n.T(i18n.KeyWorkspaceBrokenIdentityMissing, path, b.Path)
	}
	return i18n.T(i18n.KeyWorkspaceBrokenIdentityUnreadable, path, b.Err)
}

// NextSteps は「次に何をすべきか」を人間が読む3行で返す（設計 3-49）。
//
// **消し方だけを書かない。**壊れた worktree の中には、まだ push していない成果が
// 残っていることがある。**中を調べる → 要るファイルを控える → 消す**の順で書く。
//
// worktreePath: 対象の worktree の絶対パス。
// issueURL: `continuo abandon --force` に渡す issue の URL。**空でもよい。**
// 戻り値: 人間が上から順に実行できる3行。
func NextSteps(worktreePath, issueURL string) []string {
	steps := []string{
		i18n.T(i18n.KeyWorkspaceBrokenStepInspect, worktreePath, worktreePath),
		i18n.T(i18n.KeyWorkspaceBrokenStepBackup, worktreePath),
	}
	if issueURL == "" {
		// **URL を捏造しない。**番号を切り出せていないのに URL を書くと、
		// **人間はその URL の issue（＝別の issue）の worktree を消しに行く。**
		return append(steps, i18n.T(i18n.KeyWorkspaceBrokenStepAbandonUnknown, worktreePath))
	}
	return append(steps, i18n.T(i18n.KeyWorkspaceBrokenStepAbandon, issueURL))
}

// NextSteps は BrokenWorktree について「次に何をすべきか」を返す。
//
// 戻り値: 人間が上から順に実行できる3行。
func (b BrokenWorktree) NextSteps() []string {
	return NextSteps(b.Path, b.Clue.IssueURL())
}

// BrokenWorktreeGuidance は、その worktree について「次に何をすべきか」を返す。
//
// **片付けを見送ったときの案内に使う**（3-9 の手順2。cleanup.go の leftoverReasons が
// git の答えを1つも得られなかったとき）。
//
// worktreePath: 対象の worktree の絶対パス。
// 戻り値: 人間が上から順に実行できる3行。**手掛かりを引けなくても3行返る。**
func (m *Manager) BrokenWorktreeGuidance(worktreePath string) []string {
	// **pane を持たないので、issue の手掛かりは渡せない**（設計 issue144 の 8c）。
	// 置き場所の owner/repo で番号を切り出せなければ `Number` は 0 のままになり、
	// 「URL を捏造しない」分岐（NextSteps）へそのまま落ちる。
	clue, err := m.PathClueOf(worktreePath, "", "")
	if err != nil {
		return NextSteps(worktreePath, "")
	}
	return NextSteps(worktreePath, clue.IssueURL())
}

// PathClueOf は worktree の絶対パスから、issue を引くための手掛かりを組み立てる（設計 3-49）。
//
// **置き場所は `<root>/<host>/<owner>/<repo>/<スラグ>` の固定4階層である**（3-22）。
// **番号はスラグから切り出す。**`branch_template` は `.issue.number` を必ず含む
// （3-37-9d を設定の検査が強制する）ので、テンプレートを「番号だけ差し替えて」
// 描画すれば、番号がスラグのどこに現れるかが分かる。
//
// **切り出した番号を信じてはならない。**呼び出し側は、この番号でボードを引き直し、
// **取れた issue から作り直したスラグが元のディレクトリ名と一致すること**を
// 必ず確かめること（ExpectedSlugFor）。
//
// **番号を切り出すには issue の owner/repo が先に要る。**スラグは issue から作られるが、
// 置き場所の2・3階層目は**コードのリポジトリ**である（設計 issue144 の 8）。
// 2つが違う形（issue が private、コードが public の fork）では突き合わせが必ず外れる。
// **だから pane の label から取った issue の owner/repo を渡してもらう。**
//
// **渡されなければ、置き場所の owner/repo で試す。**コードのリポジトリが
// issue のリポジトリと同じなら（＝リンクを張っていない、いままでどおりの worktree なら）
// それが正しい値である。**違えば突き合わせが外れて `Number` が 0 になるだけで、
// 間違った番号を切り出すことはない**（スラグの固定部分が一致しない）。
//
// worktreePath: worktree の絶対パス（置き場所の内側であること）。
// issueOwner: pane の label から取った issue の所有者名。**分からなければ空文字。**
// issueRepo: pane の label から取った issue のリポジトリ名。**分からなければ空文字。**
// 戻り値の1つ目: 手掛かり。**番号を切り出せなければ Number は 0 である。**
// 戻り値の2つ目: 置き場所の規則に合わない場合のエラー。
func (m *Manager) PathClueOf(worktreePath, issueOwner, issueRepo string) (*PathClue, error) {
	rel, err := filepath.Rel(filepath.Clean(m.resolvedRoot), filepath.Clean(worktreePath))
	if err != nil {
		return nil, i18n.Errorf(
			i18n.KeyWorkspaceOwnerRepoFromWorktreePathRelFailed, worktreePath, m.resolvedRoot, err)
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != scanDepth || parts[0] == ".." {
		return nil, i18n.Errorf(
			i18n.KeyWorkspaceOwnerRepoFromWorktreePathLayoutMismatch, worktreePath)
	}
	clue := &PathClue{Host: parts[0], Owner: parts[1], Repo: parts[2], Slug: parts[3]}
	tryOwner, tryRepo := issueOwner, issueRepo
	if tryOwner == "" || tryRepo == "" {
		tryOwner, tryRepo = clue.Owner, clue.Repo
	}
	clue.Number = issueNumberFromSlug(
		m.cfg.Herdr.Worktree.BranchTemplate, tryOwner, tryRepo, clue.Slug)
	if clue.Number > 0 {
		// **番号を切り出せたということは、この owner/repo でスラグが再現できたということである。**
		// だから `Identifier()` はこの2つで組み立ててよい。
		clue.IssueOwner, clue.IssueRepo = tryOwner, tryRepo
	}
	return clue, nil
}

// issueNumberFromSlug は、置き場所の最下層のディレクトリ名から issue の番号を切り出す。
//
// **やり方は「番号だけ差し替えて描画し、差分を見る」である。**テンプレートを
// `slugNumberProbe` で描画してスラグを作り、その数字が現れる位置の前後を
// 固定部分として使う。**正規表現も、テンプレートの構文解析も要らない。**
//
// **番号が2回以上現れるテンプレートは扱わない。**前後を1通りに切れないためである
// （そのときは 0 を返し、呼び出し側は pane の label など別の手掛かりに頼る）。
//
// branchTemplate: `herdr.worktree.branch_template`。
// owner: 置き場所から引いた所有者名。
// repo: 置き場所から引いたリポジトリ名。
// slug: 置き場所の最下層のディレクトリ名。
// 戻り値: 切り出せた issue の番号。**切り出せなければ 0 である。**
func issueNumberFromSlug(branchTemplate, owner, repo, slug string) int {
	probeBranch, _, err := RenderBranch(branchTemplate, IssueRef{
		Owner: owner, Repo: repo, Number: slugNumberProbe,
	})
	if err != nil {
		return 0
	}
	probeSlug := Slug(probeBranch)
	probe := strconv.Itoa(slugNumberProbe)
	if strings.Count(probeSlug, probe) != 1 {
		return 0
	}
	index := strings.Index(probeSlug, probe)
	prefix, suffix := probeSlug[:index], probeSlug[index+len(probe):]

	rest, ok := strings.CutPrefix(slug, prefix)
	if !ok {
		return 0
	}
	digits, ok := strings.CutSuffix(rest, suffix)
	if !ok || digits == "" {
		return 0
	}
	number, convErr := strconv.Atoi(digits)
	if convErr != nil || number <= 0 {
		return 0
	}
	return number
}

// ScanBroken は置き場所を走査し、**身元を確かめられない worktree** を返す（設計 3-49）。
//
// **Scan と ScanUnidentified の両方が捨てているものを、1つの口にまとめたものである。**
// Scan は身元ファイルの無いディレクトリを結果に含めず、ScanUnidentified は
// 「読めなかった」ものを含めない。どちらの口でも、**壊れているかどうかは分からない。**
//
// **人間が置いた worktree を巻き込まない。**身元ファイルが無いディレクトリのうち、
// 最下層のディレクトリ名から issue の番号を切り出せたものだけを数える。
// 切り出せないものは continuo の命名ではないので、触れないし、数えない。
//
// **1件も消さない。**この関数は読むだけである。
//
// hint: worktree のパスから issue の owner/repo を答える関数（設計 issue144 の 8c）。
// **nil でもよい。**そのときは置き場所の owner/repo で番号を切り出す。
// 戻り値の1つ目: 身元を確かめられない worktree（パスの昇順）。
// 戻り値の2つ目: 置き場所そのものを読めない場合のエラー。
func (m *Manager) ScanBroken(hint IssueHintFunc) ([]BrokenWorktree, error) {
	dirs, err := m.scanLevel(m.resolvedRoot, scanDepth)
	if err != nil {
		return nil, err
	}

	var found []BrokenWorktree
	for _, dir := range dirs {
		_, readErr := m.ReadIdentity(dir)
		if readErr == nil {
			continue
		}
		hintOwner, hintRepo := "", ""
		if hint != nil {
			hintOwner, hintRepo = hint(dir)
		}
		clue, clueErr := m.PathClueOf(dir, hintOwner, hintRepo)
		if clueErr != nil {
			clue = nil
		}
		if errors.Is(readErr, ErrIdentityNotFound) {
			// **身元ファイルが無いだけでは壊れているとは言えない。**
			// 置き場所の命名に一致するものだけを数える（人間が置いた worktree を巻き込まない）。
			if clue == nil || clue.Number <= 0 {
				continue
			}
			found = append(found, BrokenWorktree{
				Path: dir, Kind: BrokenIdentityMissing, Clue: clue,
			})
			continue
		}
		found = append(found, BrokenWorktree{
			Path: dir, Kind: BrokenIdentityUnreadable, Err: readErr, Clue: clue,
		})
	}
	return found, nil
}

// IdentityFileName は設定の `workspace.identity_file` を返す。
//
// **「何が起きているか」の文にファイル名を出すために要る**（BrokenWorktree.What）。
//
// 戻り値: 身元ファイルの名前。
func (m *Manager) IdentityFileName() string { return m.cfg.Workspace.IdentityFile }
