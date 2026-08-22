// continuo の実行ファイルの入口である。
//
// **ここには何も実装しない。**`package main` の関数は `test/` から呼べないので、
// 引数の受け取り方も終了コードも検査できなくなる（設計 6-4）。
// **実体は internal/cli にある。**
package main

import (
	"os"

	"github.com/maimuzo/continuo/internal/cli"
)

// main は continuo を起動する。
//
// **os.Exit を呼ぶのはここだけである。**cli.Run は終了コードを返すだけなので、
// 検査から呼んでもプロセスが落ちない。
func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
