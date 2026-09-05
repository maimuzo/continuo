// {"RUCM-CFG-SHA256": "762f90189ab19708c063eb0bb16a544257768ec0f393e6a6ea44614891b171da", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// Package setup_test は internal/setup の対話を、公開 API（setup.Assign）を通して検証する。
//
// 確かめたいことは RUCM「既存のカンバンの Status を割り当てる」の基本フローと代替フローである。
// docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md
//
// **カンバンは1回も読まない。**選択肢は引数で渡すので、本番のカンバンにも GitHub にも触れない。
package setup_test

import (
	"context"
	"errors"
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

// {"RUCM-PATH": "P001"}
//
// 目的: 基本フローが最後まで通ると、5つの役割それぞれに選択肢が1つ割り当てられることを確認する。
// 与える情報: 本番と同じ6個の選択肢と、5つの番号（Ready / In Progress / In Review / Blocked / Done）。
// 成功条件: エラーにならず、5つの役割の割り当てが番号のとおりであること。
// 画面には役割の名前より先に「continuo が何をするか」の説明が出ていること。
func TestAssign_5つの役割それぞれに選択肢が1つ割り当てられる(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6"})
	if err != nil {
		t.Fatalf("割り当てが最後まで進まなかった: %v", err)
	}

	st := a.Statuses()
	if !st.Complete() {
		t.Fatalf("5つの役割が埋まっていない: %+v", st)
	}
	if st.Dispatch != "Ready" {
		t.Errorf("着手待ちの割り当てが違う: %q（期待 %q）", st.Dispatch, "Ready")
	}
	if st.Running != "In Progress" {
		t.Errorf("作業中の割り当てが違う: %q（期待 %q）", st.Running, "In Progress")
	}
	if st.Review != "In Review" {
		t.Errorf("レビュー待ちの割り当てが違う: %q（期待 %q）", st.Review, "In Review")
	}
	if st.Blocked != "Blocked" {
		t.Errorf("保留の割り当てが違う: %q（期待 %q）", st.Blocked, "Blocked")
	}
	if st.Done != "Done" {
		t.Errorf("完了の割り当てが違う: %q（期待 %q）", st.Done, "Done")
	}

	// **設定のキー名と説明が両方出ていること。**Status 名で尋ねると、初見の利用者は
	// 名前の似た選択肢を役割の意味と無関係に選ぶ。**キー名を出すのは、答えたあとに
	// WORKFLOW.md のどの行が変わったかを自分で確かめられるようにするためである。**
	if !strings.Contains(out, "dispatch_state: continuo が自動的に処理を開始する Status は何番ですか?") {
		t.Errorf("dispatch_state の質問が画面に出ていない:\n%s", out)
	}
	if !strings.Contains(out, "terminal_states: 人間がここへissueを移動したら作業完了とみなしgit worktreeを削除する Status は何番ですか?") {
		t.Errorf("terminal_states の質問が画面に出ていない:\n%s", out)
	}
}

// 目的: 選択肢を番号付きで並べてから尋ねることを確認する（基本フロー6）。
// 与える情報: 本番と同じ6個の選択肢と、通る5つの番号。
// 成功条件: 6個すべてが「番号  名前」の形で画面に出ていること。
func TestAssign_選択肢を番号付きで並べる(t *testing.T) {
	_, err, out := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6"})
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

// {"RUCM-PATH": "P006"}
//
// 目的: 同じ選択肢を2つの役割へ割り当てようとしたら拒否し、同じ役割を尋ね直すことを確認する
// （代替フロー「二重割り当て」／RESUME STEP 8）。
// 与える情報: 作業中に、着手待ちで使った番号2 を入れてから、番号3 を入れる。
// 成功条件: 打ち切らずに最後まで進み、着手待ちが Ready、作業中が In Progress になること。
// 画面には「既に 着手待ち に割り当て済み」と出ており、作業中の説明が2回出ていること。
func TestAssign_同じ選択肢を2つの役割へ割り当てようとしたら拒否して尋ね直す(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "2", "3", "5", "4", "6"})
	if err != nil {
		t.Fatalf("二重割り当てで打ち切ってしまった: %v", err)
	}

	st := a.Statuses()
	if st.Dispatch != "Ready" {
		t.Errorf("着手待ちの割り当てが違う: %q（期待 %q）", st.Dispatch, "Ready")
	}
	if st.Running != "In Progress" {
		t.Errorf("作業中の割り当てが違う: %q（期待 %q）", st.Running, "In Progress")
	}

	// **どの役割と衝突したかを出す。**出さないと、利用者はどれを選び直せばよいか分からない。
	if !strings.Contains(out, "既に dispatch_state に割り当て済みです") {
		t.Errorf("衝突した相手のキー名が画面に出ていない:\n%s", out)
	}
	// **同じ役割を尋ね直す。**running_state の質問が2回出ていることで確かめる。
	askedRunning := strings.Count(out, "running_state: continuo が処理を開始したときに移動する Status は何番ですか?")
	if askedRunning != 2 {
		t.Errorf("作業中を尋ねた回数が %d 回だった（期待 2 回）:\n%s", askedRunning, out)
	}
}

// {"RUCM-PATH": "P007"}
//
// 目的: 番号 0 が入ったら打ち切ることを確認する（代替フロー「該当する選択肢が無い」）。
// 与える情報: 着手待ちに 2 を入れたあと、作業中に 0 を入れる。
// 成功条件: setup.ErrNoSuitableOption を返し、割り当てが1つも返らないこと。
// 画面には GitHub の画面から足す手順と、API で足すと Status が全部消える警告が出ていること。
func TestAssign_番号0が入ったら打ち切る(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "0"})
	if !errors.Is(err, setup.ErrNoSuitableOption) {
		t.Fatalf("番号 0 で打ち切らなかった: err=%v", err)
	}
	// **それまでに選んだ番号は保存しない。**次回の実行へ持ち越すと、状態の置き場所が1つ増える。
	if a.Statuses().Complete() || a.Name(setup.RoleDispatch) != "" {
		t.Errorf("打ち切ったのに割り当てが残っている: %+v", a.Statuses())
	}
	if !strings.Contains(out, "GitHub の画面でカンバンを開き") {
		t.Errorf("選択肢を GitHub の画面から足す手順が出ていない:\n%s", out)
	}
	if !strings.Contains(out, "設定済みの Status が全部消えます") {
		t.Errorf("API で足すと Status が全部消える警告が出ていない:\n%s", out)
	}
}

// {"RUCM-PATH": "P008"}
//
// 目的: 一覧の範囲外の番号を拒否し、同じ役割を尋ね直すことを確認する（代替フロー「番号が範囲外」）。
// 与える情報: 選択肢が6個のところへ 7、次に数値でない "abc"、最後に正しい番号を入れる。
// 成功条件: 打ち切らずに最後まで進み、着手待ちが Ready になること。
// 画面には選べる番号の範囲が出ていること。
func TestAssign_範囲外の番号と数値でない入力を拒否して尋ね直す(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"7", "abc", "2", "3", "5", "4", "6"})
	if err != nil {
		t.Fatalf("範囲外の入力で打ち切ってしまった: %v", err)
	}
	if a.Statuses().Dispatch != "Ready" {
		t.Errorf("着手待ちの割り当てが違う: %q（期待 %q）", a.Statuses().Dispatch, "Ready")
	}
	if !strings.Contains(out, "選べる番号は 1 から 6 です") {
		t.Errorf("選べる番号の範囲が画面に出ていない:\n%s", out)
	}
	if !strings.Contains(out, `入力 "abc" は番号ではありません`) {
		t.Errorf("番号でない入力を指摘していない:\n%s", out)
	}
}

// {"RUCM-PATH": "P009"}
//
// 目的: 選択肢が5個に満たないときは、尋ねる前に止まることを確認する（代替フロー「選択肢が足りない」）。
// 与える情報: 選択肢を4個だけ渡す。入力は空にする。
// 成功条件: setup.ErrTooFewOptions を返し、役割の説明が1つも画面に出ていないこと。
// 選択肢を足す手順と、API で足す禁止の警告が出ていること。
func TestAssign_選択肢が5個未満なら尋ねる前に止まる(t *testing.T) {
	_, err, out := runAssign(t, []string{"Todo", "In Progress", "Done", "Blocked"}, []string{})
	if !errors.Is(err, setup.ErrTooFewOptions) {
		t.Fatalf("選択肢が足りないのに止まらなかった: err=%v", err)
	}
	if strings.Contains(out, "continuo はここから issue を取ります") {
		t.Errorf("尋ねる前に止まっていない（役割の説明が出た）:\n%s", out)
	}
	if !strings.Contains(out, "GitHub の画面でカンバンを開き") {
		t.Errorf("選択肢を足す手順が出ていない:\n%s", out)
	}
	if !strings.Contains(out, "設定済みの Status が全部消えます") {
		t.Errorf("API で足すと Status が全部消える警告が出ていない:\n%s", out)
	}
}

// {"RUCM-PATH": "P004"}
//
// 目的: 中断すると、割り当てを保存しないことを応答して終わることを確認する
// （GLOBAL ALTERNATIVE FLOW 中断）。
// 与える情報: 4つまで答えたところでコンテキストを取り消す（Ctrl+C に相当）。
// 成功条件: setup.ErrInterrupted を返し、割り当てが1つも返らないこと。
// 画面には「割り当ては保存していません」と出ていること。
func TestAssign_中断したら割り当てを保存しないと応答して終わる(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out strings.Builder

	// 4つ分だけ答えて、5つ目を待たせる。**閉じない読み手を渡す**ことで、
	// 入力の終わり（ErrInputClosed）ではなく中断であることを確かめられる。
	in, done := blockingReader("2\n3\n5\n4\n")
	defer close(done)

	errCh := make(chan error, 1)
	go func() {
		_, err := setup.Assign(ctx, setup.AssignOptions{
			FieldName: "Status",
			Options:   boardOptions,
			In:        in,
			Out:       &out,
		})
		errCh <- err
	}()

	// 5つ目を待っているところで取り消す。取り消しは何度呼んでも同じなので、
	// 到達を待たずに呼んでも「中断で終わる」ことは変わらない。
	cancel()
	err := <-errCh

	if !errors.Is(err, setup.ErrInterrupted) {
		t.Fatalf("中断として終わらなかった: err=%v", err)
	}
	if !strings.Contains(out.String(), "割り当ては保存していません") {
		t.Errorf("割り当てを保存しないことを応答していない:\n%s", out.String())
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: 番号を待っている間に入力が終わったら、割り当てを保存せずに終わることを確認する。
// 与える情報: 3つ分の番号しか無い入力。
// 成功条件: setup.ErrInputClosed を返し、割り当てが返らないこと。
func TestAssign_番号を待つ間に入力が終わったら保存せずに終わる(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "3", "5"})
	if !errors.Is(err, setup.ErrInputClosed) {
		t.Fatalf("入力の終わりで止まらなかった: err=%v", err)
	}
	if a.Statuses().Complete() {
		t.Errorf("入力が終わったのに割り当てが揃っている: %+v", a.Statuses())
	}
	if !strings.Contains(out, "割り当ては保存していません") {
		t.Errorf("割り当てを保存しないことを応答していない:\n%s", out)
	}
}
