// `continuo trust` が「何をしたか」を人間へ見せる部分の検査である。
//
// **信頼登録は `~/.claude.json` を書き換える操作である。**
// 何を書き換えたのか・バックアップはどこかが読み取れなければ、人間は元に戻せない。
package trust_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/trust"
)

// TestWriteApplyResult_何も変えなかったときはそう書く は、書き換えが無い場合を確かめる。
//
// **「登録しました」とだけ出すと、人間は書き換えられたと誤解する。**
//
// 目的: 変えるものが無かったとき、書き換えていないと明記すること。
// 与える情報: Changed が空で、Skipped だけがある結果。
// 成功条件: 「書き換えていません」と、既に信頼済みだったリポジトリ名が出ること。
// **バックアップの案内は出さないこと**（取っていないため）。
func TestWriteApplyResult_何も変えなかったときはそう書く(t *testing.T) {
	var out strings.Builder
	err := trust.WriteApplyResult(&out, &trust.ApplyResult{
		ClaudeConfigPath: "/home/octocat/.claude.json",
		Skipped: []trust.Change{
			{Repository: "octocat/hello-world", TrustKey: "/home/octocat/ghq/github.com/octocat/hello-world"},
		},
	})
	if err != nil {
		t.Fatalf("WriteApplyResult が失敗した: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "書き換えていません") {
		t.Errorf("書き換えていないことを明記していない: %s", got)
	}
	if !strings.Contains(got, "octocat/hello-world") {
		t.Errorf("既に信頼済みだったリポジトリを示していない: %s", got)
	}
	if strings.Contains(got, "バックアップ") {
		t.Errorf("取っていないバックアップを案内している: %s", got)
	}
}

// TestWriteApplyResult_書き換えたらバックアップの場所を必ず出す は、元に戻せることを確かめる。
//
// **`~/.claude.json` は Claude Code 自身が使うファイルである。**
// 書き換えた以上、どこに写しがあるかを人間へ渡さなければならない。
//
// 目的: 書き換えたとき、バックアップのパスと、登録した項目を出すこと。
// 与える情報: Changed が1件ある結果。
// 成功条件: バックアップのパス・「消しません」の断り・リポジトリ名と信頼の鍵が出ること。
func TestWriteApplyResult_書き換えたらバックアップの場所を必ず出す(t *testing.T) {
	var out strings.Builder
	err := trust.WriteApplyResult(&out, &trust.ApplyResult{
		ClaudeConfigPath: "/home/octocat/.claude.json",
		BackupPath:       "/home/octocat/.claude.json.continuo-backup-2026-08-21T10:00:00+09:00",
		Changed: []trust.Change{
			{Repository: "octocat/hello-world", TrustKey: "/home/octocat/ghq/github.com/octocat/hello-world"},
		},
	})
	if err != nil {
		t.Fatalf("WriteApplyResult が失敗した: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"continuo-backup-",
		"消しません",
		"octocat/hello-world",
		"/home/octocat/ghq/github.com/octocat/hello-world",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない: %s", want, got)
		}
	}
}

// TestWriteApplyResult_確認できなかった項目を隠さない は、書き込み後の検算を確かめる。
//
// **書き込んだのに読み直せない場合がある**（別のプロセスが同時に書き換えた等）。
// **黙って成功として見せると、人間は信頼登録できたと思い込んで先へ進む。**
//
// 目的: 検算で問題が出た項目を必ず出すこと。
// 与える情報: Changed が1件、VerifyProblems が1件ある結果。
// 成功条件: 問題の内容が出ること。
func TestWriteApplyResult_確認できなかった項目を隠さない(t *testing.T) {
	var out strings.Builder
	err := trust.WriteApplyResult(&out, &trust.ApplyResult{
		ClaudeConfigPath: "/home/octocat/.claude.json",
		BackupPath:       "/home/octocat/.claude.json.continuo-backup-2026-08-21T10:00:00+09:00",
		Changed: []trust.Change{
			{Repository: "octocat/hello-world", TrustKey: "/home/octocat/ghq/github.com/octocat/hello-world"},
		},
		VerifyProblems: []string{"octocat/hello-world: 書き込んだのに hasTrustDialogAccepted が真になっていません"},
	})
	if err != nil {
		t.Fatalf("WriteApplyResult が失敗した: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "hasTrustDialogAccepted") {
		t.Errorf("検算の問題を隠している: %s", got)
	}
}

// TestWriteApplyResult_複数件をすべて並べる は、件数が増えても落とさないことを確かめる。
//
// 目的: 登録した項目と、触らなかった項目の両方を全件出すこと。
// 与える情報: Changed 2件・Skipped 2件。
// 成功条件: 4つのリポジトリ名がすべて出ること。
func TestWriteApplyResult_複数件をすべて並べる(t *testing.T) {
	var out strings.Builder
	err := trust.WriteApplyResult(&out, &trust.ApplyResult{
		ClaudeConfigPath: "/home/octocat/.claude.json",
		BackupPath:       "/home/octocat/.claude.json.backup",
		Changed: []trust.Change{
			{Repository: "octocat/alpha", TrustKey: "/repos/alpha"},
			{Repository: "octocat/bravo", TrustKey: "/repos/bravo"},
		},
		Skipped: []trust.Change{
			{Repository: "octocat/charlie", TrustKey: "/repos/charlie"},
			{Repository: "octocat/delta", TrustKey: "/repos/delta"},
		},
	})
	if err != nil {
		t.Fatalf("WriteApplyResult が失敗した: %v", err)
	}
	got := out.String()
	for _, want := range []string{"octocat/alpha", "octocat/bravo", "octocat/charlie", "octocat/delta"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が落ちている: %s", want, got)
		}
	}
}
