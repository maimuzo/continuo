// 組み込みの指示書が見せるシェルの見本の形の検査である（設計 5-3t）。
//
// **外部へ1回も接続しない。**組み込みの文面を読むだけである。
package prompt_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
)

// heredocStart は、ヒアドキュメントを始める行の末尾に当たる。
// **行末で見る。**説明の文（「`<<'NOW'` の下に書きます」など）は行末ではないので当たらない。
var heredocStart = regexp.MustCompile(`<<'([A-Z]+)'$`)

// 目的: ヒアドキュメントの見本が、全部コード囲みの中で、行頭の終わりの行で閉じることを固定する（設計 5-3t）。
//
// **なぜ要るか。**組み込みの指示書は表示されず、文字列のまま Claude Code へ届く。
// **字下げした見本をそのまま写すと、字下げした終わりの行は終わりと読まれない。**
// 後ろの `gh` まで本文に取り込まれ、何も投稿されないまま終了コード 0 で終わる
// （2026-09-17 に bash と zsh で測った）。成果の報告が出ず、その run は `failure_state` へ落ちる。
//
// 与える情報: prompt.BuiltinRaw() の全文。
// 成功条件: 行末が `<<'語'` の行が、全部コード囲みの中にあり、同じ囲みの中で後ろに、
// その語だけの行（前後の空白も無い行）があること。
func TestTemplate_ヒアドキュメントの見本は囲みの中で閉じる(t *testing.T) {
	lines := strings.Split(prompt.BuiltinRaw(), "\n")
	found := 0
	inFence := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		m := heredocStart.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		found++
		if !inFence {
			t.Errorf("組み込みの %d 行目のヒアドキュメントが、コード囲みの外にあります: %q。"+
				"字下げのまま写されると、終わりの行が終わりと読まれません", i+1, line)
			continue
		}
		closed := false
		for k := i + 1; k < len(lines) && !isFenceLine(lines[k]); k++ {
			if lines[k] == m[1] {
				closed = true
				break
			}
		}
		if !closed {
			t.Errorf("組み込みの %d 行目のヒアドキュメント（%s）が、同じ囲みの中で行頭の %q だけの行で閉じていません",
				i+1, m[1], m[1])
		}
	}
	if found == 0 {
		t.Fatal("組み込みにヒアドキュメントの見本が1つもありません（検査が的を外しています）")
	}
}

// 目的: 組み込みの指示書が、PR の題名を二重引用符の中へ直に書かせないことを固定する（設計 5-3t）。
//
// **なぜ要るか。**二重引用符の中では、題名に書いた backtick がコマンドとして実行される。
// 一重引用符にしても、`don't` のような題名で引用が切れる。**題名はファイルへ書き、`"$(cat "$T")"` で渡す。**
// 展開された中身を、シェルがもう一度実行することは無い。
//
// 与える情報: prompt.BuiltinRaw() の全文。
// 成功条件: `--title` を含む行が、どれも `--title "$(cat ` の形であること。
func TestTemplate_題名は二重引用符の中へ直に書かせない(t *testing.T) {
	for i, line := range strings.Split(prompt.BuiltinRaw(), "\n") {
		if strings.Contains(line, "--title ") && !strings.Contains(line, `--title "$(cat `) {
			t.Errorf("組み込みの %d 行目が、題名を直に渡させています: %q", i+1, line)
		}
		if strings.Contains(line, "--title '") {
			t.Errorf("組み込みの %d 行目が、題名を一重引用符で渡させています: %q。"+
				"`'` を含む題名で引用が切れます", i+1, line)
		}
	}
}

// relativeBodyFile は、worktree の中の決まった名前のファイルへ本文を書かせる形に当たる。
var relativeBodyFile = regexp.MustCompile(`(^|\s)(>>?|--body-file)\s*[A-Za-z0-9_.-]+\.md\b`)

// 目的: 組み込みの指示書が、本文のファイルを worktree の中に作らせないことを固定する（設計 5-3t）。
//
// **なぜ要るか。**エージェントのシェルの作業場所は worktree である。`plan.md` のようなファイルは
// 未追跡のまま残り、片付け（internal/workspace/cleanup.go）が「コミットされていない変更が残っている」として
// worktree を消さず、issue に事実と違う理由を書く。`git add -A` をすれば commit に混ざる。
//
// 与える情報: prompt.BuiltinRaw() の全文。
// 成功条件: `> 名前.md` や `--body-file 名前.md` の形の行が無いこと。
func TestTemplate_本文のファイルをworktreeの中に作らせない(t *testing.T) {
	for i, line := range strings.Split(prompt.BuiltinRaw(), "\n") {
		if relativeBodyFile.MatchString(line) {
			t.Errorf("組み込みの %d 行目が、本文のファイルを作業場所に作らせています: %q。"+
				"`F=$(mktemp)` で作ったファイルへ書かせてください", i+1, line)
		}
	}
}

// 目的: 計画の見本が、本文の1行目にエージェントの印、2行目に計画の印を、行頭から書かせることを固定する
// （設計 5-3q）。
//
// **なぜ要るか。**continuo は、先頭の印の並びに計画の印があるコメントを成果の報告から外す
// （`handoff.StartsAsPlan`）。**見本から計画の印が落ちると、計画を書いた時点で「成果を書いた」と数えられ、**
// turn が途中で終わった run で書かせ直しが飛ぶ。
//
// 与える情報: prompt.BuiltinRaw() の全文。
// 成功条件: `<<'PLAN'` の次の2行が、エージェントの印と計画の印であること。
func TestTemplate_計画の見本は2行目に計画の印を行頭から書かせる(t *testing.T) {
	agentMarker := config.DefaultConfig().Tracker.Comments.Marker
	if agentMarker == "" {
		t.Fatal("既定の tracker.comments.marker が空です（検査が素通りします）")
	}
	lines := strings.Split(prompt.BuiltinRaw(), "\n")
	found := false
	for i, line := range lines {
		if !strings.HasSuffix(line, `<<'PLAN'`) {
			continue
		}
		found = true
		if i+2 >= len(lines) || lines[i+1] != agentMarker || lines[i+2] != config.PlanMarker {
			t.Errorf("計画の見本の本文が、行頭から始まる %q と %q の2行で始まっていません（%q の次の2行）",
				agentMarker, config.PlanMarker, line)
		}
	}
	if !found {
		t.Fatal("組み込みに計画の見本（`<<'PLAN'`）がありません")
	}
}
