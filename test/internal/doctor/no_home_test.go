// **置き場所が決まらないときに doctor が何をするかだけを集めたファイルである。**
//
// **doctor は「前提が揃っているか」を答える道具である**（設計 3-32）。
// **置き場所を決められないことは、検査を1つも実行しない理由にはならない。**
package doctor_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/doctor"
)

// 目的: `HOME` を引けなくても `continuo doctor` が検査を全部行うことを確かめる
// （設計 3-32 / 3-17d）。
//
// **`instance.Resolve("")` の失敗をそのまま終了コード 2 にしていた。**
// そのため `--id` を1文字も渡していない人の `continuo doctor` が、
// **検査を1つも実行しないまま「引数の誤り」として落ちた。**
//
// 与える情報: `HOME` が空の環境と、`--id` を渡していない引数。
// 成功条件: 検査が呼ばれ、終了コードが 2 にならないこと。
// **置き場所を決められなかった理由が doctor へ渡ること**（それが `ロックの場所` の `✗` になる）。
func TestDoctorCLI_HOMEを引けなくても検査を全部行う(t *testing.T) {
	fx := newFixture(t)
	// **fixture が張ったホームディレクトリを外す。**`os.UserHomeDir` が失敗する状態を作る。
	t.Setenv("HOME", "")
	t.Setenv("CONTINUO_GITHUB_GRAPHQL_ENDPOINT", "")

	var got doctor.Options
	called := false
	deps := cli.Deps{
		DoctorRun: func(_ context.Context, opts doctor.Options) doctor.Report {
			called = true
			got = opts
			return doctor.Report{Results: []doctor.Result{
				{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "読めました"},
			}}
		},
	}
	var stdout, stderr bytes.Buffer

	code := cli.RunWith(deps, []string{"doctor", fx.WorkflowPath},
		strings.NewReader(""), &stdout, &stderr)

	if !called {
		t.Fatalf("検査が1つも呼ばれていない:\n%s", stderr.String())
	}
	if code == 2 {
		t.Fatalf("引数の誤りとして落ちた（--id は1文字も渡していない）:\n%s", stderr.String())
	}
	if got.InstanceErr == nil {
		t.Fatal("置き場所を決められなかった理由が doctor へ渡っていない")
	}
}

// 目的: `HOME` を引けなくても `continuo doctor --missing-keys-patch` が動くことを確かめる
// （設計 3-75）。
//
// **これは「外部と1度も通信しない」と説明されている口である。**
// 置き場所を決められないことと、雛形との突き合わせは何の関係も無い。
//
// 与える情報: `HOME` が空の環境と、雛形どおりに書かれた `WORKFLOW.md`。
// 成功条件: 終了コードが 0 になること（足す項目が1つも無い）。
func TestDoctorCLI_HOMEを引けなくてもmissingKeysPatchが動く(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOME", "")
	t.Setenv("CONTINUO_GITHUB_GRAPHQL_ENDPOINT", "")

	var stdout, stderr bytes.Buffer
	code := cli.RunWith(cli.Deps{}, []string{"doctor", "--missing-keys-patch", fx.WorkflowPath},
		strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("差分だけを求めたのに終了コードが %d だった:\n%s", code, stderr.String())
	}
}

// 目的: 置き場所を決められなくても、残りの検査を全部行うことを確かめる（設計 3-32）。
//
// **決まらなかったことは、見出し語 `ロックの場所` の結果として報告する。**
// **doctor 側にはその仕組みが元からあったが、CLI からは1度も通っていなかった。**
//
// 与える情報: 置き場所を決められなかった理由を渡した入力。
// 成功条件: 見出し語が1つも欠けず、`ロックの場所` が `✗` になること。
func TestDoctor_置き場所が決まらなくても残りの検査を全部行う(t *testing.T) {
	fx := newFixture(t)

	opts := fx.Options()
	opts.Instance = nil
	opts.InstanceErr = errors.New("ホームディレクトリを取得できません")

	report := doctor.Run(t.Context(), opts)

	if len(report.Results) != len(wantLabels) {
		t.Fatalf("置き場所が決まらないだけで検査が欠けた（結果が %d件／期待 %d件）",
			len(report.Results), len(wantLabels))
	}
	for i, want := range wantLabels {
		if report.Results[i].Label != want {
			t.Fatalf("見出し語が設計 3-32 と違う: got %v, want %v",
				report.Results[i].Label, want)
		}
	}
	res := assertSymbol(t, report, doctor.LabelLockFile, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "ホームディレクトリを取得できません") {
		t.Fatalf("決まらなかった理由が出ていない: %q", res.Detail)
	}
}
