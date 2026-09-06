package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// DefaultDetectTimeout は gh の呼び出し1回あたりの制限時間である。
//
// 期限を付けないと、gh がネットワークの応答を待ったまま `continuo init` が固まる。
// 埋められなくても雛形は書けるので、待ち続けるより「引けませんでした」と出したほうがよい。
const DefaultDetectTimeout = 15 * time.Second

// ghProjectListLimit は `gh project list` に渡す取得件数の上限である。
// gh の既定は30件なので、カンバンが多い人の分まで引けるように明示する。
const ghProjectListLimit = "100"

// OwnerKey は雛形の中で owner を書くキーのパスである。報告の見出しに使う。
const OwnerKey = "tracker.provider.owner"

// ProjectKey は雛形の中で project_number を書くキーのパスである。報告の見出しに使う。
const ProjectKey = "tracker.provider.project_number"

// RepositoriesKey は雛形の中で信頼の対象を並べるキーのパスである。報告の見出しに使う。
const RepositoriesKey = "trust.repositories"

// ghProjectItemListLimit は `gh project item-list` に渡す取得件数の上限である。
//
// **拾うのは「どのリポジトリが載っているか」だけで、issue の中身は使わない。**
// gh の既定は30件で、本番のカンバンは100件を超えるので明示する。
// **この数で頭打ちになったら、その旨を人間に伝える**（拾い漏れたリポジトリがありうる）。
const ghProjectItemListLimit = 500

// GHRunner は gh コマンドを1回実行して標準出力を返す関数の型である。
//
// テストで本物の gh を叩かずに済むよう、グローバル変数ではなく引数で差し替える
// （internal/tracker の GHAuthTokenFunc と同じ形にしてある）。
type GHRunner func(ctx context.Context, args ...string) ([]byte, error)

// ErrGHNotFound は gh コマンドそのものが見つからなかったことを表す。
var ErrGHNotFound = i18n.Sentinel(i18n.KeyScaffoldGHNotFound)

// Project は `gh project list` が返したカンバンの候補である。
type Project struct {
	// Owner はそのカンバンを持つ user / organization の名前である。
	//
	// **`gh api user` はログイン名しか返さない。**organization に置いたカンバンは
	// ログイン名で探しても1件も出ない。実際、GitHub Enterprise で organization に
	// カンバンを置いていた利用者が `continuo setup` で1歩も進めなかった（issue #7）。
	// **候補ごとに、どの owner のものかを持つ。**
	Owner string
	// Number はカンバンの番号である（tracker.provider.project_number に書く値）。
	Number int
	// Title はカンバンの表示名である。人が候補を選ぶために出す。
	Title string
	// URL はカンバンの URL である。人が候補を選ぶために出す。
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
	// Candidates はカンバンの候補が複数あったときの一覧である。それ以外では空。
	Candidates []Project
}

// Detection は Detect が引いた結果である。
type Detection struct {
	// Values は雛形へ書き込む値である。決まらなかったものはゼロ値のまま。
	Values Values
	// Fields はキーごとの決め方である。OwnerKey、ProjectKey、RepositoriesKey の順に並ぶ。
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
	// ProjectNumber は `--project` で明示されたカンバンの番号である。
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
			return nil, i18n.Errorf(i18n.KeyScaffoldGHRunFailed, strings.Join(args, " "), err)
		}
		return nil, i18n.Errorf(i18n.KeyScaffoldGHRunFailedWithStderr, strings.Join(args, " "), err, firstLine(msg))
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
	project, number, boardOwner := detectProject(ctx, opts, run, timeout, owner.Value)

	// **カンバンが別の owner のものだったら、owner を決め直す**（設計 3-32）。
	//
	// ログイン名でカンバンが見つからず、organization で見つかったときに起こる。
	// **決め直さないと、`project_number` は organization のカンバンを指すのに
	// `owner` はログイン名のまま**という、どこにも存在しない組み合わせが書かれる。
	if boardOwner != "" && boardOwner != owner.Value {
		owner.Value, owner.Filled = boardOwner, true
		owner.Reason = i18n.T(i18n.KeyScaffoldDetectOwnerFollowedBoard, number, boardOwner)
		owner.Advice = nil
	}

	repos, repoList := detectRepositories(ctx, run, timeout, owner.Value, number)

	d := Detection{Fields: []Field{owner, project, repos}}
	if owner.Filled {
		d.Values.Owner = owner.Value
	}
	if project.Filled {
		d.Values.ProjectNumber = number
	}
	if repos.Filled {
		d.Values.Repositories = repoList
	}
	return d
}

// detectRepositories は trust.repositories へ並べる owner/repo をカンバンから拾う（3-33）。
//
// **拾うだけである。信頼は登録しない。**登録するのは `continuo trust` であり、その対象は
// 人間がこの一覧から要らない行を消したあとに残ったものである。**カンバンは他人が編集できる**
// ので、拾った一覧をそのまま信頼させてはならない。
//
// **draft issue は数えない。**リポジトリに属していないため、信頼させる対象が存在しない。
//
// ctx: 呼び出しに適用するコンテキスト。
// run: gh の実行関数。
// timeout: gh の呼び出し1回あたりの制限時間。
// owner: 決まった owner。空文字なら引かない。
// number: 決まったカンバンの番号。0 以下なら引かない。
// 戻り値の1つ目: trust.repositories についての Field。
// 戻り値の2つ目: 拾った owner/repo（辞書順・重複なし）。拾えなかった場合は nil。
func detectRepositories(ctx context.Context, run GHRunner, timeout time.Duration, owner string, number int) (Field, []string) {
	f := Field{Key: RepositoriesKey}

	if owner == "" || number <= 0 {
		f.Reason = i18n.T(i18n.KeyScaffoldDetectRepositoriesNoOwnerOrProject)
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceDecideFirst),
			i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceWriteByHandOptional),
		}
		return f, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "project", "item-list", strconv.Itoa(number),
		"--owner", owner, "--format", "json", "--limit", strconv.Itoa(ghProjectItemListLimit))
	if err != nil {
		if errors.Is(err, ErrGHNotFound) {
			f.Reason = i18n.T(i18n.KeyScaffoldDetectGHNotFound)
		} else {
			f.Reason = i18n.T(i18n.KeyScaffoldDetectRepositoriesItemListFailed, err)
		}
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectAdviceProjectScope),
			i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceWriteOwnerRepoOptional),
		}
		return f, nil
	}

	repos, itemCount, err := parseItemRepositories(out)
	if err != nil {
		f.Reason = i18n.T(i18n.KeyScaffoldDetectRepositoriesItemListUnparsable, err)
		f.Advice = []string{i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceWriteOwnerRepo)}
		return f, nil
	}

	if len(repos) == 0 {
		f.Reason = i18n.T(i18n.KeyScaffoldDetectRepositoriesNoIssue, number)
		f.Advice = []string{i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceWriteWanted)}
		return f, nil
	}

	f.Value, f.Filled = strings.Join(repos, ", "), true
	f.Reason = i18n.T(i18n.KeyScaffoldDetectRepositoriesListed, number, len(repos))
	f.Advice = []string{
		i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceRemoveUnneeded),
		i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceDryRun),
	}
	if itemCount >= ghProjectItemListLimit {
		f.Advice = append(f.Advice,
			i18n.T(i18n.KeyScaffoldDetectRepositoriesAdviceTruncated, ghProjectItemListLimit))
	}
	return f, repos
}

// parseItemRepositories は `gh project item-list --format json` の出力から owner/repo を取り出す。
//
// out: gh の標準出力。
// 戻り値の1つ目: 重複を除いて辞書順に並べた owner/repo。
// 戻り値の2つ目: 読んだ項目の件数（打ち切りに達したかの判定に使う）。
// 戻り値の3つ目: JSON として読めない場合のエラー。
func parseItemRepositories(out []byte) ([]string, int, error) {
	var payload struct {
		Items []struct {
			Content struct {
				Repository string `json:"repository"`
			} `json:"content"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, 0, err
	}

	seen := map[string]struct{}{}
	repos := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		// draft issue には repository が無い。空のまま並べると config の検査で落ちる。
		name := strings.TrimSpace(item.Content.Repository)
		if name == "" || !validOwnerRepo(name) {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		repos = append(repos, name)
	}
	sort.Strings(repos)
	return repos, len(payload.Items), nil
}

// validOwnerRepo は gh が返した文字列が "owner/repo" として受け付けられる形かを返す。
//
// **config の trust.repositories の検査と同じ形にそろえる**（internal/config の
// trustRepositoryPattern）。ここを緩くすると、雛形へ書いた値を continuo が起動時に弾く。
//
// name: 検査する文字列。
// 戻り値: 受け付けられる形なら真。
func validOwnerRepo(name string) bool {
	owner, repo, ok := strings.Cut(name, "/")
	if !ok {
		return false
	}
	return ValidOwner(owner) && repoPattern.MatchString(repo)
}

// repoPattern は owner/repo の repo の部分として受け付ける文字の範囲である。
// GitHub のリポジトリ名は英数字・ハイフン・アンダースコア・ドットの100文字以内である。
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

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
		f.Reason = i18n.T(i18n.KeyScaffoldDetectOwnerFromFlag)
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
		f.Reason = i18n.T(i18n.KeyScaffoldDetectOwnerAPIEmpty)
		f.Advice = ownerAdvice()
	case !ValidOwner(login):
		f.Reason = i18n.T(i18n.KeyScaffoldDetectOwnerAPIInvalid, login)
		f.Advice = ownerAdvice()
	default:
		f.Value, f.Filled = login, true
		f.Reason = i18n.T(i18n.KeyScaffoldDetectOwnerAPILogin)
	}
	return f
}

// ownerFailureReason は owner を引けなかった理由の1行を作る。
//
// err: gh の実行が返したエラー。
// 戻り値: 人が読む1行。
func ownerFailureReason(err error) string {
	if errors.Is(err, ErrGHNotFound) {
		return i18n.T(i18n.KeyScaffoldDetectGHNotFound)
	}
	return i18n.T(i18n.KeyScaffoldDetectOwnerGHFailed, err)
}

// ownerAdvice は owner を埋められなかったときに出す案内を返す。
//
// 戻り値: 1行に1つずつの案内。
func ownerAdvice() []string {
	return []string{
		i18n.T(i18n.KeyScaffoldDetectOwnerAdviceLogin),
		i18n.T(i18n.KeyScaffoldDetectOwnerAdviceFlag),
		i18n.T(i18n.KeyScaffoldDetectOwnerAdviceWhere),
	}
}

// detectProject は tracker.provider.project_number に書く値を決める。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: 明示された値。
// run: gh の実行関数。
// timeout: gh の呼び出し1回あたりの制限時間。
// owner: 決まった owner。空文字なら候補を引かない。
// 戻り値: project_number についての Field と、埋めたカンバンの番号（埋まらなかった場合は 0）。
func detectProject(ctx context.Context, opts DetectOptions, run GHRunner, timeout time.Duration, owner string) (Field, int, string) {
	f := Field{Key: ProjectKey}

	if opts.ProjectNumber > 0 {
		f.Value, f.Filled = strconv.Itoa(opts.ProjectNumber), true
		f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectFromFlag)
		// **`--project` を渡されたら owner は決め直さない。**
		// 利用者が `--owner` と一緒に指定していることも、していないこともある。
		return f, opts.ProjectNumber, ""
	}
	if owner == "" {
		f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectNoOwner)
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceOwnerFirst),
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceFlag),
		}
		return f, 0, ""
	}

	projects, err := listProjects(ctx, run, timeout, owner)
	if err != nil {
		if errors.Is(err, ErrGHNotFound) {
			f.Reason = i18n.T(i18n.KeyScaffoldDetectGHNotFound)
		} else {
			f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectListFailed, err)
		}
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectAdviceProjectScope),
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceFlag),
		}
		return f, 0, ""
	}

	// **ログイン名のカンバンが1件も無ければ、所属する organization も探す**（設計 3-32）。
	//
	// `gh api user` はログイン名しか返さないので、**organization に置いたカンバンは
	// ログイン名で探しても1件も出ない。**実際、GitHub Enterprise で organization に
	// カンバンを置いていた利用者が、`continuo setup` で1歩も進めなかった（issue #7）。
	searched := []string{owner}
	if len(projects) == 0 {
		for _, org := range listOrgs(ctx, run, timeout) {
			if org == owner {
				continue
			}
			searched = append(searched, org)
			found, err := listProjects(ctx, run, timeout, org)
			if err != nil {
				// **1つの organization で失敗しても、残りを探す。**
				// 権限が無い organization に所属していることは珍しくない。
				continue
			}
			projects = append(projects, found...)
		}
	}

	number := 0
	boardOwner := ""
	switch len(projects) {
	case 0:
		// **探した owner を全部見せる。**「見つからない」だけでは、
		// どこを探したのかが分からず、利用者は次の手を打てない。
		f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectNone, strings.Join(searched, ", "))
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceCreate),
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceOtherOwner),
			i18n.T(i18n.KeyScaffoldDetectProjectAdviceRerun),
		}
	case 1:
		number = projects[0].Number
		boardOwner = projects[0].Owner
		f.Value, f.Filled = strconv.Itoa(number), true
		f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectSingle,
			projects[0].Owner, projects[0].Number, projects[0].Title)
	default:
		f.Reason = i18n.T(i18n.KeyScaffoldDetectProjectMultiple,
			len(projects), strings.Join(searched, ", "))
		f.Candidates = projects
		f.Advice = []string{
			i18n.T(i18n.KeyScaffoldDetectProjectAdvicePick),
			i18n.T(i18n.KeyScaffoldDetectProjectAdvicePickOwner),
		}
	}
	return f, number, boardOwner
}

// parseProjectList は `gh project list --format json` の出力からカンバンの候補を取り出す。
//
// out: gh の標準出力。
// 戻り値: 閉じていないカンバンの一覧（gh が返した並び順のまま）。JSON として読めない場合はエラー。
// listProjects は、その owner のカンバンの候補を引く。
//
// ctx: 実行に適用するコンテキスト。
// run: gh を起動する関数。
// timeout: 1回の呼び出しの上限。
// owner: user / organization の名前。
// 戻り値: 閉じていないカンバンの候補と、失敗したときのエラー。
func listProjects(ctx context.Context, run GHRunner, timeout time.Duration, owner string) ([]Project, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "project", "list", "--owner", owner, "--format", "json", "--limit", ghProjectListLimit)
	if err != nil {
		return nil, err
	}
	return parseProjectList(out, owner)
}

// listOrgs は、利用者が所属する organization の名前を引く。
//
// **`gh api user` はログイン名しか返さない。**organization に置いたカンバンを見つけるには、
// 所属を別に引く必要がある。
//
// **失敗しても空を返す。**organization に所属していないことも、
// 認証の scope が足りないことも、どちらも「探せなかった」だけである。
// **ここで止めると、ログイン名のカンバンだけで足りる人まで巻き添えになる。**
//
// ctx: 実行に適用するコンテキスト。
// run: gh を起動する関数。
// timeout: 呼び出しの上限。
// 戻り値: organization の名前。引けなければ空。
func listOrgs(ctx context.Context, run GHRunner, timeout time.Duration) []string {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(callCtx, "api", "user/orgs", "--jq", ".[].login")
	if err != nil {
		return nil
	}
	var orgs []string
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || !ValidOwner(name) {
			continue
		}
		orgs = append(orgs, name)
	}
	return orgs
}

// parseProjectList は `gh project list` の出力を候補の並びに直す。
//
// out: `gh project list --format json` の出力。
// owner: そのカンバンを持つ user / organization の名前。**候補ごとに持たせる**
// （ログイン名と organization の候補が混ざりうるため）。
// 戻り値: 閉じていないカンバンの候補。
func parseProjectList(out []byte, owner string) ([]Project, error) {
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
		// 閉じたカンバンは選ばせない。gh は既定で closed を返さないが、
		// 返ってきたときに「候補が1件」の判定を狂わせないよう、ここでも落とす。
		if p.Closed {
			continue
		}
		projects = append(projects, Project{Owner: owner, Number: p.Number, Title: p.Title, URL: p.URL})
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
