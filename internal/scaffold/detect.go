package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultDetectTimeout は gh の呼び出し1回あたりの制限時間である。
//
// 期限を付けないと、gh がネットワークの応答を待ったまま `continuo init` が固まる。
// 埋められなくても雛形は書けるので、待ち続けるより「引けませんでした」と出したほうがよい。
const DefaultDetectTimeout = 15 * time.Second

// ghProjectListLimit は `gh project list` に渡す取得件数の上限である。
// gh の既定は30件なので、ボードが多い人の分まで引けるように明示する。
const ghProjectListLimit = "100"

// OwnerKey は雛形の中で owner を書くキーのパスである。報告の見出しに使う。
const OwnerKey = "tracker.provider.owner"

// ProjectKey は雛形の中で project_number を書くキーのパスである。報告の見出しに使う。
const ProjectKey = "tracker.provider.project_number"

// GHRunner は gh コマンドを1回実行して標準出力を返す関数の型である。
//
// テストで本物の gh を叩かずに済むよう、グローバル変数ではなく引数で差し替える
// （internal/tracker の GHAuthTokenFunc と同じ形にしてある）。
type GHRunner func(ctx context.Context, args ...string) ([]byte, error)

// ErrGHNotFound は gh コマンドそのものが見つからなかったことを表す。
var ErrGHNotFound = errors.New("gh コマンドが見つかりません")

// Project は `gh project list` が返したボードの候補である。
type Project struct {
	// Number はボードの番号である（tracker.provider.project_number に書く値）。
	Number int
	// Title はボードの表示名である。人が候補を選ぶために出す。
	Title string
	// URL はボードの URL である。人が候補を選ぶために出す。
	URL string
}

// Field は雛形の1つのキーについて、値をどう決めたかを表す。
//
// 埋まったかどうかにかかわらず1件ずつ作る。`continuo init` はこれをそのまま出力にする。
type Field struct {
	// Key は設定のキーのパスである（OwnerKey / ProjectKey）。
	Key string
	// Value は雛形へ書き込んだ値の文字列表現である。埋まらなかった場合は空文字。
	Value string
	// Filled は雛形へ書き込めたかどうかである。
	Filled bool
	// Reason は、埋めた根拠または埋められなかった理由の1行である。
	Reason string
	// Advice は、埋められなかったときに人が何をすればよいかである。1行に1つ。
	Advice []string
	// Candidates はボードの候補が複数あったときの一覧である。それ以外では空。
	Candidates []Project
}

// Detection は Detect が引いた結果である。
type Detection struct {
	// Values は雛形へ書き込む値である。決まらなかったものはゼロ値のまま。
	Values Values
	// Fields はキーごとの決め方である。OwnerKey、ProjectKey の順に並ぶ。
	Fields []Field
}

// AllFilled は全てのキーが埋まったかどうかを返す。
//
// 戻り値: Fields が空でなく、その全てが Filled なら真。
func (d Detection) AllFilled() bool {
	if len(d.Fields) == 0 {
		return false
	}
	for _, f := range d.Fields {
		if !f.Filled {
			return false
		}
	}
	return true
}

// DetectOptions は Detect の入力である。
type DetectOptions struct {
	// Owner は `--owner` で明示された user / organization 名である。
	// 空でなければ gh を叩かずにこの値を使う。
	Owner string
	// ProjectNumber は `--project` で明示されたボードの番号である。
	// 0 より大きければ gh を叩かずにこの値を使う。
	ProjectNumber int
	// RunGH は gh を実行する関数である。nil なら RunGH（本物のコマンド実行）を使う。
	RunGH GHRunner
	// Timeout は gh の呼び出し1回あたりの制限時間である。0 以下なら DefaultDetectTimeout を使う。
	Timeout time.Duration
}

// RunGH は実際に gh コマンドを実行し、標準出力をそのまま返す。
//
// ctx: 実行に適用するコンテキスト。
// args: gh に渡す引数。
// 戻り値: 標準出力。gh が見つからない場合は ErrGHNotFound を包んだエラー、
// それ以外の失敗では標準エラー出力を添えたエラーを返す。
func RunGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w", ErrGHNotFound)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("`gh %s` の実行に失敗しました: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("`gh %s` の実行に失敗しました: %w: %s", strings.Join(args, " "), err, firstLine(msg))
	}
	return out, nil
}

// Detect は雛形に書き込む owner と project_number を決める。
//
// **失敗しても error を返さない。**gh が無くても、認証が無くても、候補が0件でも、
// 雛形そのものは書けるからである。決まらなかった理由と直し方は Field に入れて返す。
//
// ctx: 呼び出し全体に適用するコンテキスト。
// opts: 明示された値と、gh の実行関数。
// 戻り値: 書き込む値と、キーごとの決め方。
func Detect(ctx context.Context, opts DetectOptions) Detection {
	run := opts.RunGH
	if run == nil {
		run = RunGH
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultDetectTimeout
	}

	owner := detectOwner(ctx, opts, run, timeout)
	project, number := detectProject(ctx, opts, run, timeout, owner.Value)

	d := Detection{Fields: []Field{owner, project}}
	if owner.Filled {
		d.Values.Owner = owner.Value
	}
	if project.Filled {
		d.Values.ProjectNumber = number
	}
	return d
}

// detectOwner は tracker.provider.owner に書く値を決める。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: 明示された値。
// run: gh の実行関数。
// timeout: gh の呼び出し1回あたりの制限時間。
// 戻り値: owner についての Field。
func detectOwner(ctx context.Context, opts DetectOptions, run GHRunner, timeout time.Duration) Field {
	f := Field{Key: OwnerKey}

	if opts.Owner != "" {
		f.Value, f.Filled = opts.Owner, true
		f.Reason = "--owner で指定された値です"
		return f
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "api", "user", "--jq", ".login")
	if err != nil {
		f.Reason = ownerFailureReason(err)
		f.Advice = ownerAdvice()
		return f
	}

	login := strings.TrimSpace(string(out))
	switch {
	case login == "":
		f.Reason = "`gh api user` が空を返しました"
		f.Advice = ownerAdvice()
	case !ValidOwner(login):
		f.Reason = fmt.Sprintf("`gh api user` が返した %q は user / organization 名として受け付けられません", login)
		f.Advice = ownerAdvice()
	default:
		f.Value, f.Filled = login, true
		f.Reason = "`gh api user` が返した GitHub のログイン名です"
	}
	return f
}

// ownerFailureReason は owner を引けなかった理由の1行を作る。
//
// err: gh の実行が返したエラー。
// 戻り値: 人が読む1行。
func ownerFailureReason(err error) string {
	if errors.Is(err, ErrGHNotFound) {
		return "gh コマンドが見つかりませんでした"
	}
	return fmt.Sprintf("gh から取得できませんでした（%v）", err)
}

// ownerAdvice は owner を埋められなかったときに出す案内を返す。
//
// 戻り値: 1行に1つずつの案内。
func ownerAdvice() []string {
	return []string{
		"gh を入れて `gh auth login -s project` でログインしてください",
		"または `continuo init --owner <名前>` でもう一度実行してください",
		"https://github.com/maimuzo なら maimuzo の位置が owner です",
	}
}

// detectProject は tracker.provider.project_number に書く値を決める。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: 明示された値。
// run: gh の実行関数。
// timeout: gh の呼び出し1回あたりの制限時間。
// owner: 決まった owner。空文字なら候補を引かない。
// 戻り値: project_number についての Field と、埋めたボードの番号（埋まらなかった場合は 0）。
func detectProject(ctx context.Context, opts DetectOptions, run GHRunner, timeout time.Duration, owner string) (Field, int) {
	f := Field{Key: ProjectKey}

	if opts.ProjectNumber > 0 {
		f.Value, f.Filled = strconv.Itoa(opts.ProjectNumber), true
		f.Reason = "--project で指定された値です"
		return f, opts.ProjectNumber
	}
	if owner == "" {
		f.Reason = "owner が決まらないので、ボードの候補を引けませんでした"
		f.Advice = []string{
			"先に owner を決めてから、もう一度 `continuo init` を実行してください",
			"または `continuo init --project <番号>` でボードの番号を直接指定してください",
		}
		return f, 0
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "project", "list", "--owner", owner, "--format", "json", "--limit", ghProjectListLimit)
	if err != nil {
		if errors.Is(err, ErrGHNotFound) {
			f.Reason = "gh コマンドが見つかりませんでした"
		} else {
			f.Reason = fmt.Sprintf("ボードの一覧を引けませんでした（%v）", err)
		}
		f.Advice = []string{
			"ボードを読むには project の scope が要ります。`gh auth login -s project` でログインし直してください",
			"または `continuo init --project <番号>` でボードの番号を直接指定してください",
		}
		return f, 0
	}

	projects, err := parseProjectList(out)
	if err != nil {
		f.Reason = fmt.Sprintf("`gh project list` の出力を解釈できませんでした（%v）", err)
		f.Advice = []string{"`continuo init --project <番号>` でボードの番号を直接指定してください"}
		return f, 0
	}

	number := 0
	switch len(projects) {
	case 0:
		f.Reason = fmt.Sprintf("%s のボードが1件も見つかりませんでした", owner)
		f.Advice = []string{
			"GitHub の画面でボードを1つ作るか、`gh project create --owner @me --title \"continuo\"` を実行してください",
			"作ったら `continuo init --project <番号>` でもう一度実行してください",
		}
	case 1:
		number = projects[0].Number
		f.Value, f.Filled = strconv.Itoa(number), true
		f.Reason = fmt.Sprintf("`gh project list` の候補が1件だけでした: #%d %s", projects[0].Number, projects[0].Title)
	default:
		f.Reason = fmt.Sprintf("%s のボードの候補が %d 件あります", owner, len(projects))
		f.Candidates = projects
		f.Advice = []string{"`continuo init --project <番号>` で、使うボードを指定してもう一度実行してください"}
	}
	return f, number
}

// parseProjectList は `gh project list --format json` の出力からボードの候補を取り出す。
//
// out: gh の標準出力。
// 戻り値: 閉じていないボードの一覧（gh が返した並び順のまま）。JSON として読めない場合はエラー。
func parseProjectList(out []byte) ([]Project, error) {
	var payload struct {
		Projects []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			URL    string `json:"url"`
			Closed bool   `json:"closed"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}

	projects := make([]Project, 0, len(payload.Projects))
	for _, p := range payload.Projects {
		// 閉じたボードは選ばせない。gh は既定で closed を返さないが、
		// 返ってきたときに「候補が1件」の判定を狂わせないよう、ここでも落とす。
		if p.Closed {
			continue
		}
		projects = append(projects, Project{Number: p.Number, Title: p.Title, URL: p.URL})
	}
	return projects, nil
}

// firstLine は複数行の文字列から1行目だけを返す。
// gh の標準エラー出力をそのまま貼ると報告が読めなくなるので、1行に切り詰める。
//
// s: 元の文字列。
// 戻り値: 最初の改行より前の部分。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
