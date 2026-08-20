package setup_test

import "io"

// blockingReader は s を返し終えたあと、合図があるまで入力の終わりを知らせない読み手を作る。
//
// **strings.Reader では中断を試せない。**用意した行を返し切った瞬間に io.EOF になるので、
// Assign は「入力が終わった」で止まり、Ctrl+C の経路（中断）を通らない。
// 端末は行を打つまで返らないので、こちらが本番に近い。
//
// s: 先に返す中身。
// 戻り値の1つ目: 読み手。
// 戻り値の2つ目: 入力を終わらせる合図。**テストが close すること**（閉じないと goroutine が残る）。
func blockingReader(s string) (io.Reader, chan struct{}) {
	done := make(chan struct{})
	pr, pw := io.Pipe()
	go func() {
		// 読み手が受け取るまでここで待つ（io.Pipe は緩衝を持たない）。
		_, _ = io.WriteString(pw, s)
		<-done
		_ = pw.Close()
	}()
	return pr, done
}
