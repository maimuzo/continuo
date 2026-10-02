// {"RUCM-CFG-SHA256": "aabc96cf347649c45020e53dc347244a1e1eac8a57051a6455c70f2f64e0d39f", "SOURCE": "docs/spec/usecases/particular_case/設定ファイルを作る.cfg.json"}
//
// **ユースケース記述「設定ファイルを作る」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **終端フローごとに代表を対応づける。**18本の経路の全部には書かない。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストは、
// 同じディレクトリの別のファイルへ足す。
package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// {"RUCM-PATH": "P015"}
//
// Test_設定ファイルを作る_P015_ディレクトリでなければエラーを返す は、置き場所の種類を確かめる。
//
// 目的: ファイルを指されたら `ErrNotADirectory` を返すこと。
// 与える情報: ファイルのパス。
// 成功条件: そのエラーで返ること。
func Test_設定ファイルを作る_P015_ディレクトリでなければエラーを返す(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("ファイルを作れません: %v", err)
	}

	_, err := scaffold.WriteTemplate(file, false)
	if !errors.Is(err, scaffold.ErrNotADirectory) {
		t.Fatalf("ErrNotADirectory でない: %v", err)
	}
}

// {"RUCM-PATH": "P016"}
//
// Test_設定ファイルを作る_P016_ディレクトリが無ければエラーを返す は、置き場所の検査を確かめる。
//
// 目的: 存在しないディレクトリを指されたら `ErrDirNotFound` を返すこと。
// 与える情報: 存在しないパス。
// 成功条件: そのエラーで返ること。
func Test_設定ファイルを作る_P016_ディレクトリが無ければエラーを返す(t *testing.T) {
	_, err := scaffold.WriteTemplate(filepath.Join(t.TempDir(), "no-such-dir"), false)
	if !errors.Is(err, scaffold.ErrDirNotFound) {
		t.Fatalf("ErrDirNotFound でない: %v", err)
	}
}
