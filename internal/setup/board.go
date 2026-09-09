package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// DefaultStatusFieldName は Status を読み書きする single-select フィールドの既定の名前である。
//
// **雛形の `tracker.provider.status_field` と同じ値にする**（internal/scaffold/template.go）。
// setup は WORKFLOW.md を作る側なので、設定からフィールド名を引けない。
const DefaultStatusFieldName = "Status"

// ghFieldListLimit は `gh project field-list` に渡す取得件数の上限である。
//
// gh の既定は30件である。組み込みのフィールドだけで14件あり、そこに人が足した
// single-select が並ぶので、既定のままでは Status が打ち切りの向こう側に落ちうる。
const ghFieldListLimit = 100

// singleSelectFieldType は `gh project field-list` が single-select フィールドに付ける型名である。
const singleSelectFieldType = "ProjectV2SingleSelectField"

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て「直し方」の文言を決める。
var (
	// ErrScopeMissing は gh の scope に project が無いことを表す。
	ErrScopeMissing = errors.New("gh の scope に project がありません")
	// ErrStatusFieldNotFound は、指定した名前の single-select フィールドがカンバンに無いことを表す。
	ErrStatusFieldNotFound = errors.New("カンバンに single-select の Status フィールドがありません")
	// ErrRateLimited は GitHub のレートリミットに当たったことを表す。**一時的である。**
	ErrRateLimited = errors.New("GitHub のレートリミットに当たりました")
)

// StatusField はカンバンから読んだ Status フィールドである。
type StatusField struct {
	// Name はフィールドの名前である（画面に出す）。
	Name string
	// Options は選択肢の名前である。**カンバンの並び順のまま**入れる。
	// 番号で選ばせるので、画面の並びとカンバンの並びを一致させる。
	Options []string
}

// FetchOptions は FetchStatusField の入力である。
type FetchOptions struct {
	// Owner はカンバンの owner（GitHub の user / organization 名）である。
	Owner string
	// ProjectNumber はカンバンの番号である。
	ProjectNumber int
	// FieldName は読む single-select フィールドの名前である。空なら DefaultStatusFieldName。
	FieldName string
	// RunGH は gh を実行する関数である。nil なら scaffold.RunGH（本物のコマンド実行）を使う。
	RunGH scaffold.GHRunner
	// Timeout は gh の呼び出しの制限時間である。0 以下なら scaffold.DefaultDetectTimeout。
	Timeout time.Duration
}

// FetchStatusField はカンバンの Status フィールドの選択肢を読む。
//
// **読み取りだけである。**呼ぶのは `gh project field-list`（GET 相当）だけで、
// カンバンにも item にも1文字も書き込まない。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: カンバンの場所とフィールドの名前、gh の実行関数。
// 戻り値: フィールドの名前と選択肢の一覧。
// エラー: ErrScopeMissing / ErrRateLimited / ErrStatusFieldNotFound を errors.Is で
// 判定できる形で返す。それ以外は gh の出力を添えたエラーを返す。
func FetchStatusField(ctx context.Context, opts FetchOptions) (StatusField, error) {
	name := opts.FieldName
	if name == "" {
		name = DefaultStatusFieldName
	}
	if opts.Owner == "" {
		return StatusField{}, i18n.Errorf(i18n.KeySetupBoardOwnerMissing)
	}
	if opts.ProjectNumber <= 0 {
		return StatusField{}, i18n.Errorf(i18n.KeySetupBoardProjectNumberMissing)
	}

	run := opts.RunGH
	if run == nil {
		run = scaffold.RunGH
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = scaffold.DefaultDetectTimeout
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "project", "field-list", strconv.Itoa(opts.ProjectNumber),
		"--owner", opts.Owner, "--format", "json", "--limit", strconv.Itoa(ghFieldListLimit))
	if err != nil {
		return StatusField{}, classifyGHError(err)
	}

	fields, err := parseFieldList(out)
	if err != nil {
		return StatusField{}, i18n.Errorf(i18n.KeySetupBoardFieldListUnparsable, err)
	}

	for _, f := range fields {
		if f.Name != name {
			continue
		}
		if f.Type != singleSelectFieldType {
			return StatusField{}, i18n.Errorf(i18n.KeySetupBoardFieldNotSingleSelect, ErrStatusFieldNotFound, name, f.Type)
		}
		options := make([]string, 0, len(f.Options))
		for _, o := range f.Options {
			options = append(options, o.Name)
		}
		return StatusField{Name: name, Options: options}, nil
	}
	return StatusField{}, i18n.Errorf(i18n.KeySetupBoardFieldNotFound, ErrStatusFieldNotFound, opts.ProjectNumber, name)
}

// classifyGHError は gh が返したエラーを、直し方が決まる形へ分類する。
//
// **文面で判定する。**gh は終了コードを細かく分けないので、標準エラー出力の文言を見るしかない。
// 見るのは次の2つだけで、当てはまらないものは元のエラーのまま返す（当てずっぽうで
// 直し方を出すより、gh の言い分をそのまま見せたほうがよい）。
//
//   - `missing required scopes` … project の scope が無いときに gh が出す文言
//   - `rate limit` … レートリミットに当たったときに GitHub が返す文言
//
// err: gh の実行が返した非 nil のエラー。
// 戻り値: ErrScopeMissing / ErrRateLimited を包んだエラー、またはそのままの err。
func classifyGHError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "missing required scopes"), strings.Contains(msg, "requires the following scopes"):
		return fmt.Errorf("%w: %v", ErrScopeMissing, err)
	case strings.Contains(msg, "rate limit"):
		return fmt.Errorf("%w: %v", ErrRateLimited, err)
	}
	return err
}

// rawField は `gh project field-list --format json` が返すフィールド1件である。
type rawField struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Options []struct {
		Name string `json:"name"`
	} `json:"options"`
}

// parseFieldList は `gh project field-list --format json` の出力からフィールドを取り出す。
//
// out: gh の標準出力。
// 戻り値: gh が返した並び順のままのフィールドの一覧。JSON として読めない場合はエラー。
func parseFieldList(out []byte) ([]rawField, error) {
	var payload struct {
		Fields []rawField `json:"fields"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}
	return payload.Fields, nil
}
