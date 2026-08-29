// {"RUCM-CFG-SHA256": "762f90189ab19708c063eb0bb16a544257768ec0f393e6a6ea44614891b171da", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
package setup_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/setup"
)

// fieldListJSON は `gh project field-list --format json` の出力を組み立てる。
//
// **本番のボード（project #3）から実際に返ってきた形をそのまま写してある。**
// single-select には options が付き、それ以外のフィールドには付かない。
//
// options: Status フィールドの選択肢の名前。
// 戻り値: gh の標準出力に相当する JSON。
func fieldListJSON(options []string) []byte {
	quoted := make([]string, 0, len(options))
	for i, name := range options {
		quoted = append(quoted, fmt.Sprintf(`{"id":"opt%d","name":%q}`, i, name))
	}
	return []byte(`{"fields":[` +
		`{"id":"PVTF_1","name":"Title","type":"ProjectV2Field"},` +
		`{"id":"PVTSSF_1","name":"Status","options":[` + strings.Join(quoted, ",") + `],"type":"ProjectV2SingleSelectField"},` +
		`{"id":"PVTIF_1","name":"Iteration","type":"ProjectV2IterationField"}` +
		`],"totalCount":3}`)
}

// 目的: ボードの Status フィールドの選択肢を、ボードの並び順のまま読めることを確認する。
// 与える情報: 本番と同じ6個の選択肢を返すテスト用gh mock。
// 成功条件: 選択肢が並び順のまま返ること。**呼んだ gh のサブコマンドが field-list だけであること**
// （ボードを書き換える field-create / item-edit を呼んでいないこと）。
func TestFetchStatusField_選択肢をボードの並び順のまま読む(t *testing.T) {
	var called [][]string
	got, err := setup.FetchStatusField(context.Background(), setup.FetchOptions{
		Owner:         "octocat",
		ProjectNumber: 3,
		RunGH: func(_ context.Context, args ...string) ([]byte, error) {
			called = append(called, args)
			return fieldListJSON(boardOptions), nil
		},
	})
	if err != nil {
		t.Fatalf("選択肢を読めなかった: %v", err)
	}
	if got.Name != "Status" {
		t.Errorf("フィールドの名前が違う: %q（期待 %q）", got.Name, "Status")
	}
	if strings.Join(got.Options, "|") != strings.Join(boardOptions, "|") {
		t.Errorf("選択肢の並びが違う: %v（期待 %v）", got.Options, boardOptions)
	}

	if len(called) != 1 {
		t.Fatalf("gh を呼んだ回数が %d 回だった（期待 1 回）: %v", len(called), called)
	}
	if called[0][0] != "project" || called[0][1] != "field-list" {
		t.Errorf("読み取り以外の gh を呼んでいる: %v", called[0])
	}
}

// 目的: 指定した名前の single-select フィールドがボードに無いときの落ち方を確認する
// （代替フロー「ボードを読めない」の1つ）。
// 与える情報: Status しか持たないボードに対して --status-field 相当で "State" を渡す。
// 成功条件: setup.ErrStatusFieldNotFound を返すこと。
func TestFetchStatusField_名前の合うフィールドが無ければStatusフィールドが無いと返す(t *testing.T) {
	_, err := setup.FetchStatusField(context.Background(), setup.FetchOptions{
		Owner:         "octocat",
		ProjectNumber: 3,
		FieldName:     "State",
		RunGH: func(_ context.Context, _ ...string) ([]byte, error) {
			return fieldListJSON(boardOptions), nil
		},
	})
	if !errors.Is(err, setup.ErrStatusFieldNotFound) {
		t.Fatalf("Status フィールドが無いと返さなかった: %v", err)
	}
}

// 目的: 名前は合っていても single-select でなければ受け付けないことを確認する。
// 与える情報: Iteration フィールドの名前を渡す。
// 成功条件: setup.ErrStatusFieldNotFound を返し、single-select でないことを文言に含むこと。
func TestFetchStatusField_singleSelectでないフィールドは受け付けない(t *testing.T) {
	_, err := setup.FetchStatusField(context.Background(), setup.FetchOptions{
		Owner:         "octocat",
		ProjectNumber: 3,
		FieldName:     "Iteration",
		RunGH: func(_ context.Context, _ ...string) ([]byte, error) {
			return fieldListJSON(boardOptions), nil
		},
	})
	if !errors.Is(err, setup.ErrStatusFieldNotFound) {
		t.Fatalf("single-select でないフィールドを受け付けてしまった: %v", err)
	}
	if !strings.Contains(err.Error(), "single-select") {
		t.Errorf("single-select でないことが文言に無い: %v", err)
	}
}

// {"RUCM-PATH": "P010"}
//
// 目的: gh の落ち方を「直し方が決まる形」へ分類できることを確認する。
// 与える情報: scope 不足とレートリミットのそれぞれの文言を返すテスト用gh mock。
// 成功条件: setup.ErrScopeMissing / setup.ErrRateLimited をそれぞれ返すこと。
func TestFetchStatusField_ghの落ち方を直し方が決まる形へ分類する(t *testing.T) {
	cases := []struct {
		name    string
		ghError string
		want    error
	}{
		{
			name:    "scopeにprojectが無い",
			ghError: "your authentication token is missing required scopes [read:project]",
			want:    setup.ErrScopeMissing,
		},
		{
			name:    "レートリミットに当たった",
			ghError: "API rate limit exceeded for user ID 1234",
			want:    setup.ErrRateLimited,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setup.FetchStatusField(context.Background(), setup.FetchOptions{
				Owner:         "octocat",
				ProjectNumber: 3,
				RunGH: func(_ context.Context, _ ...string) ([]byte, error) {
					return nil, errors.New(tc.ghError)
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("分類が違う: %v（期待 %v）", err, tc.want)
			}
		})
	}
}
