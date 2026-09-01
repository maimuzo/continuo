package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// pushToAnotherNameCommand は、別の名前へ push するときのコマンドの形である。
// **`-u` が付いた形しか本文に載せない。**
const pushToAnotherNameCommand = "git push -u origin HEAD:"

// pushWithoutUpstreamFlag は、`-u` を落とした push の形である。
// **本文に1つでもあってはならない。**エージェントはコマンドをそのまま写す。
const pushWithoutUpstreamFlag = "git push origin HEAD"

// 目的: 雛形の本文が、別の名前へ push するときにも `-u` を付けさせることを固定する
// （#144（worktree の branch は変えず push 先だけ分ける）。設計 3-9 の手順2b と 5-3）。
//
// **なぜ要るか。**`git push origin HEAD:<別名>` は upstream を張り替えない。
// 片付けの段2（`git rev-list --count @{u}..HEAD`）が実態と違う数を出すので、
// 見送りの理由が「push されていない commit が n 件残っている」と誤って書かれる。
//
// 与える情報: scaffold.Template() の本文。
// 成功条件: 別の名前へ push する例が `-u` 付きで載っており、
// `-u` を落とした push の例が1つも無いこと。
func TestTemplate_雛形は別名へのpushにもuを付けさせる(t *testing.T) {
	body := bodyOf(t, "雛形", scaffold.Template())

	if !strings.Contains(body, pushToAnotherNameCommand) {
		t.Errorf("雛形の本文に別の名前へ push する例 %q がありません。"+
			"`-u` を落とすと upstream が張り替わらず、片付けが誤った理由を出します",
			pushToAnotherNameCommand)
	}

	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, pushWithoutUpstreamFlag) {
			t.Errorf("雛形の本文の %d 行目に `-u` の無い push があります。"+
				"エージェントはコマンドをそのまま写すので、例に載せてはいけません\n  その行: %q",
				i+1, line)
		}
	}
}
