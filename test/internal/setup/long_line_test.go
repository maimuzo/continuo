// {"RUCM-CFG-SHA256": "762f90189ab19708c063eb0bb16a544257768ec0f393e6a6ea44614891b171da", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **上限を超える1行を流し込まれたときの検査である。**
//
// **貼り間違いは、それまでの回答を全部捨てる理由にならない。**長い URL やログの塊を
// 端末へ貼り間違えると、番号を待っている入力に上限を超える1行が入る。
// **そこで黙って終わると、画面には空行が1つ増えるだけで、なぜ終わったのかがどこにも出ない。**
package setup_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/setup"
)

// {"RUCM-PATH": "P008"}
//
// TestAssign_長すぎる1行は捨てて同じ役割を尋ね直す は、**無言の終了**を落とす。
//
// 目的: 上限を超える1行が来ても打ち切らず、理由を出して同じ役割を尋ね直し、
// そのあとの正しい行を読めること。
// 与える情報: 5000バイトの1行と、そのあとに続く正しい5つの番号。
// 成功条件: エラーにならず5つとも割り当たり、画面に「読み捨てました」が出ること。
func TestAssign_長すぎる1行は捨てて同じ役割を尋ね直す(t *testing.T) {
	long := strings.Repeat("x", 5000)
	a, err, out := runAssign(t, boardOptions, []string{long, "2", "3", "5", "4", "6"})
	if err != nil {
		t.Fatalf("長い1行で打ち切られた: %v（画面: %s）", err, out)
	}

	st := a.Statuses()
	if !st.Complete() {
		t.Fatalf("5つの役割が埋まっていない: %+v", st)
	}
	if st.Dispatch != "Ready" || st.Done != "Done" {
		t.Errorf("長い1行のあとの回答がずれている: %+v", st)
	}
	if !strings.Contains(out, "読み捨てました") {
		t.Errorf("長すぎる行を捨てたことが画面に出ていない:\n%s", out)
	}
}

// {"RUCM-PATH": "P008"}
//
// TestAssign_長すぎる1行が続いても答え終えられる は、**1回きりの回復ではない**ことを見る。
//
// **bufio.Scanner は一度上限を超えると、そのあとの正しい行も1行も読めなくなる。**
// 貼り間違いは繰り返し起こりうるので、何度でも尋ね直せなければならない。
//
// 目的: 上限を超える行が2回来ても、そのあとの回答で最後まで進めること。
// 与える情報: 長い1行を2回はさんだ、正しい5つの番号。
// 成功条件: エラーにならず、5つとも割り当たること。
func TestAssign_長すぎる1行が続いても答え終えられる(t *testing.T) {
	long := strings.Repeat("y", 9000)
	a, err, out := runAssign(t, boardOptions, []string{long, "2", long, "3", "5", "4", "6"})
	if err != nil {
		t.Fatalf("長い1行で打ち切られた: %v（画面: %s）", err, out)
	}
	if !a.Statuses().Complete() {
		t.Fatalf("5つの役割が埋まっていない: %+v", a.Statuses())
	}
}

// TestAssign_読めなかった理由は必ず画面に出す は、**理由の無い終了**を落とす。
//
// **RUCM に対応するパスが無い。**「入力そのものを読めない」は結末として書かれていないが、
// 起こりうるし、起きたときに何も出さないのは誤りである。
//
// **呼び出し側（cmd/continuo）は「なぜ止まったかは Assign が出し終えている」前提で、
// 何も出さずに終了コード 1 を返す。**ここで黙ると、利用者の画面には何も残らない。
//
// 目的: 入力を読めなかったとき、その理由を画面に出してから返すこと。
// 与える情報: 必ず読み取りに失敗する入力。
// 成功条件: その理由が返り、画面に「入力を読めなかった」旨が出ること。
func TestAssign_読めなかった理由は必ず画面に出す(t *testing.T) {
	want := errors.New("読み取りに失敗しました")
	var out strings.Builder
	_, err := setup.Assign(context.Background(), setup.AssignOptions{
		FieldName: "Status",
		Options:   boardOptions,
		In:        failingReader{err: want},
		Out:       &out,
	})
	if !errors.Is(err, want) {
		t.Fatalf("読めなかった理由が返っていない: %v", err)
	}
	if !strings.Contains(out.String(), "入力を読めなかった") {
		t.Errorf("読めなかった理由が画面に出ていない:\n%s", out.String())
	}
}

// failingReader は必ず読み取りに失敗する入力である。
type failingReader struct {
	// err は Read が返すエラーである。
	err error
}

// Read は必ず err を返す。
//
// _: 読み込み先（使わない）。
// 戻り値: 常に 0 と、組み立てたときのエラー。
func (r failingReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
