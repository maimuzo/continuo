// **上限を超える1行を流し込まれたときの検査である。**
//
// **貼り間違いは、それまでの回答を全部捨てる理由にならない。**長い URL やログの塊を
// 端末へ貼り間違えると、番号を待っている入力に上限を超える1行が入る。
// **そこで黙って終わると、画面には空行が1つ増えるだけで、なぜ終わったのかがどこにも出ない。**
package setup_test

import ()

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
