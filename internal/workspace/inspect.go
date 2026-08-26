package workspace

import (
	"context"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// Leftover は「この worktree を消したら何が失われるか」の内訳である（3-9 の手順2 と 2b）。
//
// **判定そのものは Cleanup と同じ関数（leftoverReasons）が出す。**
// Inspect が足すのは、人間に見せるための件数だけである。
// **同じ判定を2箇所に書かない**（書けば必ずずれる）。
type Leftover struct {
	// Identity は読み取った身元ファイルである。
	// issue・branch・base・herdr workspace の ID を人間に見せるのに使う。
	Identity *Identity
	// WorktreePath は封じ込め検査（3-20）を通した worktree の絶対パスである。
	WorktreePath string
	// Base は判定に使った base である（呼び出し側が渡した値、無ければ身元ファイルの値）。
	Base string
	// DirtyFiles はコミットされていない変更のファイル数である（未追跡のファイルを含む）。
	// **身元ファイルとその一時ファイルは数から外す**（Cleanup の判定と同じ扱い）。
	DirtyFiles int
	// DirtyFilesTruncated は `git status --porcelain` の読み取りが上限で打ち切られた
	// ことを表す。**真なら DirtyFiles は「これ以上ある」という下限である。**
	// 人間に見せるときは数だけを出さず、「以上」と分かる形にすること。
	DirtyFilesTruncated bool
	// DirtyFilesUnknown は git が答えられず、コミットされていない変更を1件も数えられなかった
	// ことを表す（worktree の `.git` が壊れている場合など。issue #23）。
	// **真なら DirtyFiles は 0 のままだが、それは「変更が無い」という意味ではない。**
	// 人間に見せるときは数を出さず、「数えられなかった」と書くこと。
	DirtyFilesUnknown bool
	// UnpushedUnknown は git が答えられず、push されていない成果があるかを
	// 1つも判定できなかったことを表す。
	// **HasUpstream / UnpushedCommits / DiffFromBase は真ならどれも当てにならない。**
	UnpushedUnknown bool
	// HasUpstream は現在の branch に upstream があるかどうかである。
	HasUpstream bool
	// UnpushedCommits は upstream より先にある commit の件数である。
	// **HasUpstream が偽なら意味を持たない**（数える相手がいない）。
	UnpushedCommits int
	// DiffFromBase は upstream が無いまま base との差分が残っているかどうかである。
	// **HasUpstream が真のとき、または base を決められなかったときは偽である。**
	DiffFromBase bool
	// BaseUnknown は upstream が無く、base も決められなかったことを表す。
	// **このとき「失うものが無い」とは言えない**（判定する材料が無い）。
	BaseUnknown bool
	// Reasons は Cleanup が片付けを見送る理由である（人間が読む文）。
	// **設定（cleanup.require_clean_worktree / require_pushed）に従う**ので、
	// 検査を切ってあれば空になる。
	Reasons []string
	// Undetermined は git が答えられずに調べ切れなかったことである（人間が読む文）。
	//
	// **空でなければ、この Leftover の数と真偽値は当てにならない。**
	// `continuo abandon` が「失うものはありません」と偽らないために、
	// **これが1件でもあれば HasLoss が真になる**（消すには `--force` が要る）。
	//
	// **調べられないことを理由に止まってはならない。**abandon は壊れた状態を
	// 片付ける道具であり、git が答えられない worktree こそ片付けたい相手である
	// （issue #23）。だから Inspect はエラーを返さず、ここへ理由を積んで返す。
	Undetermined []string
}

// HasLoss は、消すと失われるものがあるかどうかを返す。
//
// **Reasons では判定しない。**Reasons は設定（cleanup.require_clean_worktree /
// require_pushed）に従うので、検査を切ってある環境では「失うものがあるのに空」になる。
// `continuo abandon` は人間が手で消す道具であり、**設定で検査を切ってあっても、
// 失うものがあるなら黙って消してはならない。**
//
// 戻り値: コミットされていない変更がある・push されていない commit がある・
// upstream が無いまま base との差分がある・判定する材料が無い、のいずれかなら true。
func (l *Leftover) HasLoss() bool {
	if l == nil {
		return false
	}
	// **調べ切れなかったものは「失うものがある」側に倒す。**中身が分からないものを
	// 黙って消してはならない（issue #23）。
	if len(l.Undetermined) > 0 {
		return true
	}
	if l.DirtyFiles > 0 {
		return true
	}
	if l.HasUpstream {
		return l.UnpushedCommits > 0
	}
	return l.DiffFromBase || l.BaseUnknown
}

// Inspect は worktree を消さずに、失われるものだけを調べる（3-9 の手順2 と 2b）。
//
// **Cleanup から判定を切り出して呼ぶ。**`continuo abandon --dry-run` と、
// 消す前に人間へ見せる一覧がこれを使う。**Cleanup の挙動は変えない。**
//
// **cleanup.enabled は見ない。**「continuo が自動で片付けるか」の設定であって、
// 「いま何が失われるか」とは別の話である。
//
// **git が答えられなくても止まらない**（issue #23）。worktree の `.git` は
// `gitdir: …` と書かれただけのファイルであり、壊れると `git -C <worktree> …` が
// 1つも通らない。**そこで止まると、まさに片付けたい相手を片付けられなくなる。**
// 調べられなかったことは Leftover.Undetermined に積んで返し、**「失うものが無い」とは
// 決して答えない**（HasLoss が真になる）。
//
// ctx: 実行に適用するコンテキスト。
// req: 調べる worktree と、その worktree を作ったときの base（空なら身元ファイルから補う）。
// 戻り値の1つ目: 失われるものの内訳。
// 戻り値の2つ目: 封じ込め検査（3-20）に落ちた場合・身元ファイルを読めない場合のエラー。
// **git を実行できないことはエラーにしない**（Undetermined に入る）。
func (m *Manager) Inspect(ctx context.Context, req CleanupRequest) (*Leftover, error) {
	resolvedPath, err := CheckContainmentResolved(m.resolvedRoot, req.WorktreePath)
	if err != nil {
		return nil, err
	}
	req.WorktreePath = resolvedPath

	identity, err := m.ReadIdentity(resolvedPath)
	if err != nil {
		return nil, err
	}
	req.Base = m.effectiveBase(req.Base, identity)

	reasons, _ := m.leftoverReasons(ctx, req)
	result := &Leftover{
		Identity:     identity,
		WorktreePath: resolvedPath,
		Base:         req.Base.String(),
		// **見送りの理由は Cleanup と同じ関数に出させる。**
		// **git が答えられたかどうかはここでは使わない**（Inspect は Undetermined で
		// 「調べ切れなかった」を別に持っている）。
		Reasons: reasons,
	}

	// **件数は設定に関係なく数える。**人間に見せるための数であり、
	// 「検査を切ってあるから数えない」では、何を失うのかを判断できない。
	status, truncated, err := gitStatusPorcelain(ctx, resolvedPath, m.identityStatusExcludes()...)
	if err != nil {
		// **0 ファイルに丸めない。**丸めると、成果の残った worktree が
		// 「コミットされていない変更: 0 ファイル」と表示されたまま消える。
		result.DirtyFilesUnknown = true
		result.UnpushedUnknown = true
		result.Undetermined = append(result.Undetermined,
			i18n.T(i18n.KeyWorkspaceUndeterminedDirty, err),
			i18n.T(i18n.KeyWorkspaceUndeterminedUnpushed, err))
		return result, nil
	}
	result.DirtyFiles = countPorcelainLines(status)
	// **打ち切られたことを落とさない。**落とすと、数千ファイルを失う worktree が
	// 「200 ファイル」に見える。**見せた数より多く失う**のが、いちばん困る誤りである。
	result.DirtyFilesTruncated = truncated

	hasUpstream, err := gitHasUpstream(ctx, resolvedPath)
	if err != nil {
		result.UnpushedUnknown = true
		result.Undetermined = append(result.Undetermined,
			i18n.T(i18n.KeyWorkspaceUndeterminedUnpushed, err))
		return result, nil
	}
	result.HasUpstream = hasUpstream
	if hasUpstream {
		ahead, err := gitAheadOfUpstream(ctx, resolvedPath)
		if err != nil {
			result.HasUpstream = false
			result.UnpushedUnknown = true
			result.Undetermined = append(result.Undetermined,
				i18n.T(i18n.KeyWorkspaceUndeterminedUnpushed, err))
			return result, nil
		}
		result.UnpushedCommits = ahead
		return result, nil
	}

	if req.Base == "" {
		result.BaseUnknown = true
		return result, nil
	}
	noDiff, err := gitNoDiffFromBase(ctx, resolvedPath, req.Base)
	if err != nil {
		// **判定できないことを「差分が無い」に丸めない。**丸めると、成果の残った
		// worktree が「失うものはありません」と表示されたまま消える。
		result.BaseUnknown = true
		return result, nil
	}
	result.DiffFromBase = !noDiff
	return result, nil
}

// countPorcelainLines は `git status --porcelain` の出力の行数を数える。
//
// **1行が1つの対象である**（変更されたファイル、または未追跡のファイル）。
//
// status: gitStatusPorcelain の出力（前後の空白は落としてある）。
// 戻り値: 行数。空文字なら 0。
func countPorcelainLines(status string) int {
	if strings.TrimSpace(status) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(status, "\n"), "\n"))
}
