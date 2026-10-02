// {"RUCM-CFG-SHA256": "5c0a174a30e3df14be9fd9c6e9bf7edfe2c1f1488198f00e033bd8428061e181", "SOURCE": "docs/spec/usecases/particular_case/対象リポジトリを信頼登録する.cfg.json"}
//
// **ユースケース記述「対象リポジトリを信頼登録する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/trust"
)

// {"RUCM-PATH": "P021"}
//
// Test_対象リポジトリを信頼登録する_P021_ホームディレクトリを引けなければ書き込まない は、書き込み先の確定を確かめる。
//
// **引けないまま既定値へ落とすと、別の場所を書き換えることになる。**
//
// 目的: ホームディレクトリを引けないとき、何も書き換えずに落ちること。
// 与える情報: 常に失敗する userHomeDir。
// 成功条件: 終了コードが 0 でなく、stderr に理由が出ること。
func Test_対象リポジトリを信頼登録する_P021_ホームディレクトリを引けなければ書き込まない(t *testing.T) {
	deps := cli.Deps{
		UserHomeDir: func() (string, error) { return "", errors.New("ホームを引けません") },
	}

	code, _, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code == 0 {
		t.Error("ホームを引けないのに成功として終わっている")
	}
	if stderr == "" {
		t.Error("なぜ止まったかを出していない")
	}
}

// {"RUCM-PATH": "P021"}
//
// Test_対象リポジトリを信頼登録する_P021_調べられなければ落とす は、Plan の失敗を確かめる。
//
// 目的: 対象を調べられないとき、書き込まずに落ちること。
// 与える情報: 常に失敗する Plan。
// 成功条件: 終了コードが 0 でなく、Apply を呼ばないこと。
func Test_対象リポジトリを信頼登録する_P021_調べられなければ落とす(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return nil, errors.New("git を実行できません")
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{}, nil
	}

	code, _, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code == 0 {
		t.Error("調べられないのに成功として終わっている")
	}
	if applied {
		t.Error("調べられないのに書き込んでいる")
	}
	if !strings.Contains(stderr, "git を実行できません") {
		t.Errorf("なぜ止まったかを出していない: %s", stderr)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_対象リポジトリを信頼登録する_P001_登録の対象があれば書き込んで結果を出す は、`continuo trust` の本筋を確かめる。
//
// 目的: 未登録の項目があるとき、`Apply` を呼んで結果を報告すること。
// 与える情報: 未登録の項目を1件返す Plan と、書き込んだことにする Apply。
// 成功条件: 終了コードが 0 で、Apply が呼ばれ、登録した項目が stdout に出ること。
func Test_対象リポジトリを信頼登録する_P001_登録の対象があれば書き込んで結果を出す(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return &trust.Report{
			ClaudeConfigPath: "/tmp/.claude.json",
			Entries: []trust.Entry{
				{Repository: "octocat/hello-world", ClonePath: "/repos/hello-world",
					TrustKey: "/repos/hello-world", Trusted: false},
			},
		}, nil
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{
			ClaudeConfigPath: "/tmp/.claude.json",
			BackupPath:       "/tmp/.claude.json.backup",
			Changed: []trust.Change{
				{Repository: "octocat/hello-world", TrustKey: "/repos/hello-world"},
			},
		}, nil
	}

	code, stdout, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if !applied {
		t.Error("登録の対象があるのに Apply を呼んでいない")
	}
	if !strings.Contains(stdout, "octocat/hello-world") {
		t.Errorf("登録した項目を報告していない: %s", stdout)
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_対象リポジトリを信頼登録する_P015_dryRunならApplyを呼ばない は、`--dry-run` の約束を確かめる。
//
// 目的: `--dry-run` のとき、対象があっても `Apply` を呼ばないこと。
// 与える情報: 未登録の項目を1件返す Plan。
// 成功条件: Apply が呼ばれないこと。
func Test_対象リポジトリを信頼登録する_P015_dryRunならApplyを呼ばない(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return &trust.Report{
			ClaudeConfigPath: "/tmp/.claude.json",
			Entries: []trust.Entry{
				{Repository: "octocat/hello-world", ClonePath: "/repos/hello-world",
					TrustKey: "/repos/hello-world", Trusted: false},
			},
		}, nil
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{}, nil
	}

	runCLIWith(deps, []string{"trust", "--dry-run", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if applied {
		t.Error("--dry-run なのに Apply を呼んでいる")
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_対象リポジトリを信頼登録する_P015_後ろに書いたdryRunが効く は、`continuo trust` でも並べ替えが効くことを確かめる。
//
// **ここで効かないと、下見のつもりで叩いた `trust <パス> --dry-run` が本当に書き込む。**
//
// 目的: `trust <パス> --dry-run` で Apply を1度も呼ばないこと。
// 与える情報: 未登録の項目を1件返す Plan と、末尾に置いた `--dry-run`。
// 成功条件: Apply が呼ばれず、引数の誤り（2）でも終わらないこと。
func Test_対象リポジトリを信頼登録する_P015_後ろに書いたdryRunが効く(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return &trust.Report{
			ClaudeConfigPath: "/tmp/.claude.json",
			Entries: []trust.Entry{
				{Repository: "octocat/hello-world", ClonePath: "/repos/hello-world",
					TrustKey: "/repos/hello-world", Trusted: false},
			},
		}, nil
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{}, nil
	}

	code, _, stderr := runCLIWith(deps,
		[]string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md"), "--dry-run"}, "")

	if code == 2 {
		t.Fatalf("引数の誤りとして落ちている: stderr=%s", stderr)
	}
	if applied {
		t.Error("後ろに書いた --dry-run が効かず Apply を呼んでいる")
	}
}
