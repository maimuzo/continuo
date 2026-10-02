// {"RUCM-CFG-SHA256": "8d589fc83c8eb1ce6e5f20c0184ac0229dfc87928b48f183b4273b070eb02f1e", "SOURCE": "docs/spec/usecases/particular_case/既存のボードのStatusを割り当てる.cfg.json"}
//
// **ユースケース記述「既存のボードのStatusを割り当てる」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// 関数を直に呼ぶテストは経路の一部だけを通すので、いちばん近い経路の番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
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
// 目的: 基本フローが最後まで通ると、5つの役割それぞれに選択肢が1つ割り当てられることを確認する。
// 与える情報: 本番と同じ6個の選択肢と、5つの番号（Ready / In Progress / In Review / Blocked / Done）。
// 成功条件: エラーにならず、5つの役割の割り当てが番号のとおりであること。
// 画面には役割の名前より先に「continuo が何をするか」の説明が出ていること。
func Test_既存のボードのStatusを割り当てる_P008_5つの役割それぞれに選択肢が1つ割り当てられる(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6", "0"})
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

// {"RUCM-PATH": "P001"}
//
// 目的: direct chat の役割へ選択肢を割り当てられることを確認する（設計 3-83）。
//
// 与える情報: 本番と同じ6個の選択肢と、6つの番号（最後は Ice Box を direct chat に当てる）。
// 成功条件: エラーにならず、direct_chat_state に選択肢名が入ること。
func Test_既存のボードのStatusを割り当てる_P001_directChatの役割へ選択肢を割り当てられる(t *testing.T) {
	a, err, _ := runAssign(t, boardOptions, []string{"2", "3", "5", "4", "6", "1"})
	if err != nil {
		t.Fatalf("割り当てが最後まで進まなかった: %v", err)
	}
	if got, want := a.Statuses().DirectChat, "Ice Box"; got != want {
		t.Errorf("direct_chat_state の割り当てが違う: %q（期待 %q）", got, want)
	}
}

// {"RUCM-PATH": "P007"}
//
// 目的: 同じ選択肢を2つの役割へ割り当てようとしたら拒否し、同じ役割を尋ね直すことを確認する
// （代替フロー「二重割り当て」。同じ役割の説明を出す段へ戻る）。
// 与える情報: 作業中に、着手待ちで使った番号2 を入れてから、番号3 を入れる。
// 成功条件: 打ち切らずに最後まで進み、着手待ちが Ready、作業中が In Progress になること。
// 画面には「既に 着手待ち に割り当て済み」と出ており、作業中の説明が2回出ていること。
func Test_既存のボードのStatusを割り当てる_P007_同じ選択肢を2つの役割へ割り当てようとしたら拒否して尋ね直す(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"2", "2", "3", "5", "4", "6", "0"})
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

// {"RUCM-PATH": "P013"}
//
// 目的: 番号 0 が入ったら打ち切ることを確認する（代替フロー「該当する選択肢が無い」）。
// 与える情報: 着手待ちに 2 を入れたあと、作業中に 0 を入れる。
// 成功条件: setup.ErrNoSuitableOption を返し、割り当てが1つも返らないこと。
// 画面には GitHub の画面から足す手順と、API で足すと Status が全部消える警告が出ていること。
func Test_既存のボードのStatusを割り当てる_P013_番号0が入ったら打ち切る(t *testing.T) {
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

// {"RUCM-PATH": "P014"}
//
// 目的: 一覧の範囲外の番号を拒否し、同じ役割を尋ね直すことを確認する（代替フロー「番号が範囲外」）。
// 与える情報: 選択肢が6個のところへ 7、次に数値でない "abc"、最後に正しい番号を入れる。
// 成功条件: 打ち切らずに最後まで進み、着手待ちが Ready になること。
// 画面には選べる番号の範囲が出ていること。
func Test_既存のボードのStatusを割り当てる_P014_範囲外の番号と数値でない入力を拒否して尋ね直す(t *testing.T) {
	a, err, out := runAssign(t, boardOptions, []string{"7", "abc", "2", "3", "5", "4", "6", "0"})
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

// {"RUCM-PATH": "P020"}
//
// 目的: 選択肢が5個に満たないときは、尋ねる前に止まることを確認する（代替フロー「選択肢が足りない」）。
// 与える情報: 選択肢を4個だけ渡す。入力は空にする。
// 成功条件: setup.ErrTooFewOptions を返し、役割の説明が1つも画面に出ていないこと。
// 選択肢を足す手順と、API で足す禁止の警告が出ていること。
func Test_既存のボードのStatusを割り当てる_P020_選択肢が5個未満なら尋ねる前に止まる(t *testing.T) {
	_, err, out := runAssign(t, []string{"Todo", "In Progress", "Done", "Blocked"}, []string{})
	if !errors.Is(err, setup.ErrTooFewOptions) {
		t.Fatalf("選択肢が足りないのに止まらなかった: err=%v", err)
	}
	if strings.Contains(out, "[1/") {
		t.Errorf("尋ねる前に止まっていない（役割の質問が出た）:\n%s", out)
	}
	if !strings.Contains(out, "GitHub の画面でカンバンを開き") {
		t.Errorf("選択肢を足す手順が出ていない:\n%s", out)
	}
	if !strings.Contains(out, "設定済みの Status が全部消えます") {
		t.Errorf("API で足すと Status が全部消える警告が出ていない:\n%s", out)
	}
}

// {"RUCM-PATH": "P017"}
//
// 目的: 中断すると、割り当てを保存しないことを応答して終わることを確認する
// （GLOBAL ALTERNATIVE FLOW 中断）。
// 与える情報: 4つまで答えたところでコンテキストを取り消す（Ctrl+C に相当）。
// 成功条件: setup.ErrInterrupted を返し、割り当てが1つも返らないこと。
// 画面には「割り当ては保存していません」と出ていること。
func Test_既存のボードのStatusを割り当てる_P017_中断したら割り当てを保存しないと応答して終わる(t *testing.T) {
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

// {"RUCM-PATH": "P018"}
//
// 目的: 番号を待っている間に入力が終わったら、割り当てを保存せずに終わることを確認する。
// 与える情報: 3つ分の番号しか無い入力。
// 成功条件: setup.ErrInputClosed を返し、割り当てが返らないこと。
func Test_既存のボードのStatusを割り当てる_P018_番号を待つ間に入力が終わったら保存せずに終わる(t *testing.T) {
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

// {"RUCM-PATH": "P021"}
//
// 目的: gh の落ち方を「直し方が決まる形」へ分類できることを確認する。
// 与える情報: scope 不足とレートリミットのそれぞれの文言を返すテスト用gh mock。
// 成功条件: setup.ErrScopeMissing / setup.ErrRateLimited をそれぞれ返すこと。
func Test_既存のボードのStatusを割り当てる_P021_ghの落ち方を直し方が決まる形へ分類する(t *testing.T) {
	cases := []struct {
		name    string
		ghError string
		want    error
	}{
		{
			name:    "scopeにprojectが無い",
			ghError: "your authentication token is missing required scopes [read:project]",
			want:    setup.ErrScopeMissing,
		},
		{
			name:    "レートリミットに当たった",
			ghError: "API rate limit exceeded for user ID 1234",
			want:    setup.ErrRateLimited,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setup.FetchStatusField(context.Background(), setup.FetchOptions{
				Owner:         "octocat",
				ProjectNumber: 3,
				RunGH: func(_ context.Context, _ ...string) ([]byte, error) {
					return nil, errors.New(tc.ghError)
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("分類が違う: %v（期待 %v）", err, tc.want)
			}
		})
	}
}

// {"RUCM-PATH": "P016"}
//
// Test_既存のボードのStatusを割り当てる_P016_長すぎる1行は捨てて同じ役割を尋ね直す は、**無言の終了**を落とす。
//
// 目的: 上限を超える1行が来ても打ち切らず、理由を出して同じ役割を尋ね直し、
// そのあとの正しい行を読めること。
// 与える情報: 5000バイトの1行と、そのあとに続く正しい5つの番号。
// 成功条件: エラーにならず5つとも割り当たり、画面に「読み捨てました」が出ること。
func Test_既存のボードのStatusを割り当てる_P016_長すぎる1行は捨てて同じ役割を尋ね直す(t *testing.T) {
	long := strings.Repeat("x", 5000)
	a, err, out := runAssign(t, boardOptions, []string{long, "2", "3", "5", "4", "6", "0"})
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

// {"RUCM-PATH": "P016"}
//
// Test_既存のボードのStatusを割り当てる_P016_長すぎる1行が続いても答え終えられる は、**1回きりの回復ではない**ことを見る。
//
// **bufio.Scanner は一度上限を超えると、そのあとの正しい行も1行も読めなくなる。**
// 貼り間違いは繰り返し起こりうるので、何度でも尋ね直せなければならない。
//
// 目的: 上限を超える行が2回来ても、そのあとの回答で最後まで進めること。
// 与える情報: 長い1行を2回はさんだ、正しい5つの番号。
// 成功条件: エラーにならず、5つとも割り当たること。
func Test_既存のボードのStatusを割り当てる_P016_長すぎる1行が続いても答え終えられる(t *testing.T) {
	long := strings.Repeat("y", 9000)
	a, err, out := runAssign(t, boardOptions, []string{long, "2", long, "3", "5", "4", "6", "0"})
	if err != nil {
		t.Fatalf("長い1行で打ち切られた: %v（画面: %s）", err, out)
	}
	if !a.Statuses().Complete() {
		t.Fatalf("5つの役割が埋まっていない: %+v", a.Statuses())
	}
}

// {"RUCM-PATH": "P019"}
//
// Test_既存のボードのStatusを割り当てる_P019_読めなかった理由は必ず画面に出す は、**理由の無い終了**を落とす。
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
func Test_既存のボードのStatusを割り当てる_P019_読めなかった理由は必ず画面に出す(t *testing.T) {
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
