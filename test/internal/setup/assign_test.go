// Package setup_test は internal/setup の対話を、公開 API（setup.Assign）を通して検証する。
//
// 確かめたいことは RUCM「既存のボードのStatusを割り当てる」の基本フローと代替フローである。
// docs/spec/usecases/particular_case/既存のボードのStatusを割り当てる.rucm.md
//
// **カンバンは1回も読まない。**選択肢は引数で渡すので、本番のカンバンにも GitHub にも触れない。
package setup_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/setup"
)

// boardOptions は本番のカンバン（project #3）と同じ並びの選択肢である。
//
// **並び順ごと写してある。**番号で選ばせるので、並びが変わると番号の意味が変わる。
var boardOptions = []string{"Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done"}

// runAssign は番号を流し込んで setup.Assign を1回走らせる。
//
// t: 呼び出し元のテスト。
// options: カンバンから読んだことにする選択肢。
// input: 標準入力へ流し込む行（末尾に改行を付けて連結する）。
// 戻り値の1つ目: 決まった割り当て。
// 戻り値の2つ目: Assign が返したエラー。
// 戻り値の3つ目: 画面に出た全文。
func runAssign(t *testing.T, options []string, input []string) (setup.Assignment, error, string) {
	t.Helper()
	var out strings.Builder
	in := strings.NewReader(strings.Join(input, "\n") + "\n")
	a, err := setup.Assign(context.Background(), setup.AssignOptions{
		FieldName: "Status",
		Options:   options,
		In:        in,
		Out:       &out,
	})
	return a, err, out.String()
}

// 目的: 選択肢を番号付きで並べてから尋ねることを確認する（基本フローの、選択肢の一覧を番号付きで応答する段）。
// 与える情報: 本番と同じ6個の選択肢と、通る5つの番号。
// 成功条件: 6個すべてが「番号  名前」の形で画面に出ていること。
func TestAssign_選択肢を番号付きで並べる(t *testing.T) {
	_, err, out := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6", "0"})
	if err != nil {
		t.Fatalf("割り当てが最後まで進まなかった: %v", err)
	}
	for i, name := range boardOptions {
		want := "  " + string(rune('0'+i+1)) + "  " + name
		if !strings.Contains(out, want) {
			t.Errorf("選択肢の行 %q が画面に出ていない:\n%s", want, out)
		}
	}
}

// 目的: direct chat の役割だけは番号 0 で飛ばせて、対話が打ち切られないことを確認する（設計 3-83）。
//
// **他の5つで 0 を入れると打ち切る。**そこは変えていない。
// **direct chat だけを飛ばせるようにしたのは、カンバンにその選択肢が無くても
// continuo は起動するからである。**打ち切ると、この機能を使わない人から
// `continuo setup` そのものを奪うことになる。
//
// 与える情報: 本番と同じ6個の選択肢と、5つの番号のあとに 0。
// 成功条件: エラーにならず、5つの役割が埋まり、direct_chat_state だけが空であること。
// 画面に「飛ばしました」が出ていること。
func TestAssign_directChatの役割は0で飛ばせる(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6", "0"})
	if err != nil {
		t.Fatalf("0 を入れたら打ち切られた: %v", err)
	}
	st := a.Statuses()
	if !st.Complete() {
		t.Fatalf("必ず要る5つの役割が埋まっていない: %+v", st)
	}
	if st.DirectChat != "" {
		t.Errorf("飛ばしたのに direct_chat_state に値が入っている: %q", st.DirectChat)
	}
	if !strings.Contains(out, "飛ばしました") {
		t.Errorf("飛ばしたことが画面に出ていない:\n%s", out)
	}
}

// 目的: direct chat の選択肢が無いカンバン（選択肢ちょうど5個）でも、対話が最後まで通ることを確認する（設計 3-83）。
//
// **必要な選択肢の数を6にすると、ここで1問も尋ねずに終わる。**
// いま5つちょうどで運用している人から `continuo setup` を奪わないための検査である。
//
// 与える情報: 選択肢を5個だけ渡し、5つの番号のあとに 0。
// 成功条件: エラーにならず、必ず要る5つの役割が埋まること。
func TestAssign_選択肢が5個ちょうどでも最後まで通る(t *testing.T) {
	five := []string{"Ready", "In Progress", "In Review", "Blocked", "Done"}
	a, err, _ := runAssign(t, five, []string{"1", "2", "3", "4", "5", "0"})
	if err != nil {
		t.Fatalf("選択肢5個で打ち切られた: %v", err)
	}
	if st := a.Statuses(); !st.Complete() {
		t.Fatalf("5つの役割が埋まっていない: %+v", st)
	}
}
