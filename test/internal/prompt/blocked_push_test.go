package prompt_test

import (
	"strings"
	"testing"
)

// pushDemandMarker は「人間へ渡す前に push させる」文を見分ける目印である。
// 文中の Status 名（`review` / `blocked`）を含まない部分だけを使う。
// Status 名まで含めて探すと、片方が消えたときにこの行そのものが見つからなくなり、
// 「文が無い」と「文はあるが blocked が抜けた」の区別がつかなくなる。
// **句点は含めない。**文面は「…push してください**（commit するものが無ければ要りません）。」で、
// 強調の閉じと括弧の注記が句点の前に入る。
const pushDemandMarker = "を出す前に、必ず commit して push してください"

// pushCommand は組み込みのプロンプトがエージェントに叩かせる push のコマンドである。
// この形で足りることは docs/evidence/push_u_origin_head.md で確かめてある。
const pushCommand = "git push -u origin HEAD"

// 目的: continuo が送る組み込みのプロンプト が、`blocked` を出す前にも push を求めることを固定する
// （issue #64。設計 3-9 の「— その前提」と 5-3）。
//
// **この条件は、組み込みのプロンプトだけを読んで確かめる。**
//
// **なぜ `blocked` が要るか。**`blocked` は人間へ渡す合図であり、そこから先この worktree で
// 作業が続く保証が無い。`continuo abandon` や片付けで branch ごと消えると、
// push していない commit は取り戻せない。`review` と同じ理由がそのまま当てはまる。
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: push を求める文が1つだけあり、その文が `review` と `blocked` の両方を名指しし、
// 本文が push のコマンドをそのまま載せていること。
func TestTemplate_組み込みのプロンプトはblockedを出す前にもpushを求める(t *testing.T) {
	body := instructionsText(t)

	var found []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, pushDemandMarker) {
			found = append(found, line)
		}
	}

	if len(found) == 0 {
		t.Fatalf("組み込みのプロンプトに push を求める文がありません（%q を含む行が1行もない）。"+
			"人間へ渡す前に push させないと、worktree の片付けで作業が失われます", pushDemandMarker)
	}
	if len(found) > 1 {
		t.Fatalf("組み込みのプロンプトに push を求める文が %d 行あります。"+
			"複数あると、片方だけ直したときに指示が食い違います:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}

	line := found[0]
	for _, status := range []string{"review", "blocked"} {
		if !strings.Contains(line, "`"+status+"`") {
			t.Errorf("push を求める文が `%s` を名指ししていません（issue #64）。"+
				"%s は人間か continuo へ渡す合図なので、その前に push させること\n  その行: %q",
				status, status, line)
		}
	}

	if !strings.Contains(body, pushCommand) {
		t.Errorf("組み込みのプロンプトに push のコマンド %q がありません。"+
			"branch 名を自分で決めさせると、片付けが見る upstream と食い違います", pushCommand)
	}
}
