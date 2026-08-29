package scaffold_test

import (
	"os"
	"testing"

	"github.com/maimuzo/continuo/test/testlang"
)

// TestMain は、この package のテストを正の言語（日本語）で走らせる。
//
// **ここの検査は、画面に出る文言の日本語の原文を相手にしている。**
// `continuo init` が出す埋めた根拠と案内（internal/scaffold の Detect）は i18n から引くので、
// 何もしないと英語の訳文が返り、検査が空振りする（理由は testlang の説明にある）。
func TestMain(m *testing.M) { os.Exit(testlang.Run(m)) }
