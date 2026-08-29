package cli_test

import (
	"os"
	"testing"

	"github.com/maimuzo/continuo/test/testlang"
)

// TestMain は、この package のテストを正の言語（日本語）で走らせる。
//
// **ここの検査は、画面に出る文言の日本語の原文を相手にしている。**
// 何もしないと英語の訳文が返り、検査が空振りする（理由は testlang の説明にある）。
//
// **`internal/cli` は環境変数 LANG も読む**（`i18n.FromEnv` / `i18n.Resolve`）。
// **固定しないと、開発者の手元の LANG によって検査の相手の言語が変わる。**
func TestMain(m *testing.M) { os.Exit(testlang.Run(m)) }
