package trust_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/trust"
)

// otherSettings は、書き換えの対象ではない他のリポジトリの記述である。
//
// **1つも変わってはならない**（設計 3-33）。continuo が知らないキーも含めてある。
const otherSettings = `{
  "numStartups": 41,
  "oauthAccount": {"accountUuid": "0000-1111", "emailAddress": "someone@example.invalid"},
  "projects": {
    "/somewhere/else": {
      "allowedTools": ["Bash(ls:*)"],
      "hasTrustDialogAccepted": true,
      "history": [{"display": "hello", "pastedContents": {}}],
      "mcpServers": {}
    }
  },
  "tipsHistory": {"new-user-warmup": 3}
}
`

// planFor はテスト用に Plan を1回呼ぶ。
//
// t: テストコンテキスト。
// home: テスト用ホームディレクトリ。
// clones: "owner/repo" から clone の絶対パスへの対応。
// repositories: trust.repositories に書かれているとみなす値。
// 戻り値: 調べた結果。
func planFor(t *testing.T, home string, clones map[string]string, repositories ...string) *trust.Report {
	t.Helper()
	opts := optionsFor(home, clones)
	opts.Repositories = repositories
	report, err := trust.Plan(context.Background(), opts)
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	return report
}

// optionsFor はテスト用の Options を組み立てる。
//
// home: テスト用ホームディレクトリ。
// clones: "owner/repo" から clone の絶対パスへの対応。
// 戻り値: Options。
func optionsFor(home string, clones map[string]string) trust.Options {
	return trust.Options{HomeDir: home, ResolveClone: staticClones(clones)}
}

// backupNames はテスト用ホームディレクトリにあるバックアップのファイル名を返す。
//
// t: テストコンテキスト。
// home: テスト用ホームディレクトリ。
// 戻り値: バックアップのファイル名の並び。
func backupNames(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", home, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), trust.BackupPrefix) {
			names = append(names, e.Name())
		}
	}
	return names
}
