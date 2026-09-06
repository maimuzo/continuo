package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
)

// KindGitHubProjectsV2 は tracker.kind が受け付ける唯一の値である
// （SPEC.md 11.2 が要求する「exact supported tracker.kind value」の公表）。
const KindGitHubProjectsV2 = "github_projects_v2"

// maxItemPages はカンバンを読むときに辿るページ数の上限である（1ページ100件）。
//
// **上限が無いと、カンバンが育つほど1回の呼び出しのコストが黙って増える。**
// 巡回は30秒ごとに走り、表明の対象1件ごとにもカンバンを読むので、気づかないうちに
// GitHub の API 枠（5,000 point/時。設計 3-31）を食い潰す。
// 2,000件は本番のカンバン（104件）の約20倍であり、超えたときは無言で飲み込まず
// CategoryPagination で落として人間に気づかせる。
const maxItemPages = 20

// Adapter は GitHub Projects v2 を SPEC.md 第11節の Issue Tracker Integration Contract に
// 沿って読み書きするアダプタである。
//
// 使い方の順序は決まっている。
//  1. NewAdapter で作る
//  2. Bootstrap を1回呼ぶ（設計 3-6）。project の ID・Status フィールドの ID・
//     各選択肢の ID を解決し、設定と選択肢名が一致するかを検査する。
//     **これを呼ぶ前は FetchIssuesByStates 等を呼んでも動くが、UpdateStatus は
//     必ず失敗する**（書き込みに要る ID が無いため）。
//  3. FetchIssuesByStates / FetchIssuesByIDs / FetchIssuesByIDsWithoutTimeline /
//     UpdateStatus / FetchComments / PostComment を
//     必要に応じて呼ぶ
//
// **Status の選択肢そのものを書き換える mutation（選択肢の指定が全件置き換えとして扱われ、
// 設定済みの Status を全部消す）を呼ぶメソッドはこのパッケージのどこにも無い**
// （CLAUDE.md の絶対制約。test/internal/tracker がこれをソースの grep で確かめている）。
//
// **複数の goroutine から同時に呼んでよい。**巡回ループ（VerifyStatusOptions）と turn ループ
// （UpdateStatus）は別の goroutine で動き、turn は run が終わるまで生き続けるので、
// 30秒ごとの巡回と重なるのが常態である。Bootstrap で解決した ID の一群は mu が守る。
type Adapter struct {
	gql           *graphqlClient
	owner         string
	projectNumber int
	statusField   string
	logger        *slog.Logger

	// repoTrusted はリポジトリが Claude Code に信頼登録されているかを判定する関数である
	// （設計 3-13 の dispatchable の3条件のうち「リポジトリが信頼済み」）。
	// nil なら全て信頼済みとして扱う。
	repoTrusted RepoTrustFunc

	// mu は Bootstrap / VerifyStatusOptions が解決した値
	// （bootstrapped・projectID・statusFieldID・statusOptionIDs・statusOptionNamesFold）を守る。
	//
	// **2つの map を別々のロックで引いてはならない。**新しい正式名と古い選択肢 ID を
	// 組み合わせると、誤った optionId を updateProjectV2ItemFieldValue へ渡す。
	// 名前と ID の組は必ず同じ世代から取ること（writeTargets）。
	mu sync.RWMutex
	// bootstrapped は Bootstrap が成功したかどうかである。
	bootstrapped bool
	// projectID は project の GraphQL ノード ID である（Bootstrap で解決）。
	projectID string
	// statusFieldID は Status フィールドの GraphQL ノード ID である（Bootstrap で解決）。
	statusFieldID string
	// statusOptionIDs は Status の選択肢名（GitHub の綴りそのまま）から選択肢 ID への対応である
	// （Bootstrap で解決）。
	statusOptionIDs map[string]string
	// statusOptionNamesFold は Status の選択肢名を foldStatus した値から、GitHub 側の
	// 正式な綴りへの対応である。大文字小文字を無視した照合に使う（Bootstrap で解決）。
	statusOptionNamesFold map[string]string
}

// ProjectWorkflow はカンバンの組み込みの自動化1件である（設計 3-32 の見出し語 `自動化`）。
//
// **持てるのは名前と有効かどうかだけである。**`ProjectV2Workflow` が公開しているのは
// `createdAt` / `enabled` / `fullDatabaseId` / `id` / `name` / `number` / `project` /
// `updatedAt` の8つで（2026-09-05 に introspection で確認）、
// **どの Status を書くかを返すフィールドは1つも無い。**
// だから `continuo doctor` は「Status を書きうる」までしか言えない。
type ProjectWorkflow struct {
	// Number はカンバンの中での自動化の番号である（GitHub の画面に出る `#1` など）。
	Number int
	// Name は自動化の名前である（`Pull request linked to issue` など。GitHub の綴りのまま）。
	Name string
	// Enabled はその自動化が有効かどうかである。
	Enabled bool
}

// NewAdapter は Adapter を作る。
//
// cfg: WORKFLOW.md の front matter の tracker セクション（設計 5-2）。
// endpoint: GraphQL API の URL。空文字なら本番の GitHub GraphQL API を使う。
// テストでは httptest.Server の URL を渡して本番のカンバンへ接続しないようにすること。
// **https 以外は受け付けない**（loopback の http だけは例外。トークンを平文で第三者へ
// 送らないため。newGraphQLClient を参照）。既定と違う接続先のときは警告を1行残す。
// token: 認証トークン（ResolveToken で取得した値）。
// httpClient: リクエストを送るクライアント。nil なら接続10秒・全体30秒のクライアントを
// 組み立てて使う。
// logger: ログの出力先。nil なら slog.Default() を使う。
// repoTrusted: リポジトリが Claude Code に信頼登録されているかを判定する関数（設計 3-13）。
// **nil を渡すと全てのリポジトリを信頼済みとして扱う。**信頼の判定を orchestrator 側へ
// 出さないために、ここで受け取って Issue.Dispatchable に畳み込む。
// 戻り値: cfg.Kind が KindGitHubProjectsV2 以外の場合は CategoryUnsupportedKind、
// owner / project_number / status_field のいずれかが未設定の場合、および endpoint が
// https でない（loopback の http でもない）場合は CategoryInvalidConfig の *Error を返す。
func NewAdapter(
	cfg config.TrackerConfig,
	endpoint string,
	token string,
	httpClient *http.Client,
	logger *slog.Logger,
	repoTrusted RepoTrustFunc,
) (*Adapter, error) {
	if cfg.Kind != KindGitHubProjectsV2 {
		return nil, &Error{
			Category: CategoryUnsupportedKind,
			Message: fmt.Sprintf(
				"tracker.kind %q はこのアダプタが対応する種別ではありません（対応するのは %q だけです）",
				cfg.Kind, KindGitHubProjectsV2,
			),
		}
	}
	if cfg.Provider.Owner == "" {
		return nil, &Error{Category: CategoryInvalidConfig, Message: "tracker.provider.owner が空です"}
	}
	if cfg.Provider.ProjectNumber <= 0 {
		return nil, &Error{
			Category: CategoryInvalidConfig,
			Message:  fmt.Sprintf("tracker.provider.project_number が不正です: %d", cfg.Provider.ProjectNumber),
		}
	}
	if cfg.Provider.StatusField == "" {
		return nil, &Error{Category: CategoryInvalidConfig, Message: "tracker.provider.status_field が空です"}
	}

	if logger == nil {
		logger = slog.Default()
	}

	gql, err := newGraphQLClient(endpoint, token, httpClient)
	if err != nil {
		return nil, err
	}
	if gql.endpoint != defaultGraphQLEndpoint {
		// **無人運用で「本物の GitHub ではない相手にトークンを送っている」ことに
		// 気づけるようにする。**差し替えは環境変数1行でできてしまうため、必ず1行残す。
		logger.Warn("GraphQL の接続先が既定と違います（本番の GitHub ではありません）",
			"endpoint", gql.endpoint, "既定", defaultGraphQLEndpoint,
		)
	}

	return &Adapter{
		gql:           gql,
		owner:         cfg.Provider.Owner,
		projectNumber: cfg.Provider.ProjectNumber,
		statusField:   cfg.Provider.StatusField,
		logger:        logger,
		repoTrusted:   repoTrusted,
	}, nil
}

// requiredStatesForBootstrap は「カンバンに実在しなければ起動を止める」Status 名を、
// cfg から重複無く集める。active_states・terminal_states・running_state・dispatch_state・
// failure_state・status_signal_map の遷移先を含める
// （3-6: 「書き込みに要る ID をすべて解決して覚える」）。
//
// **集めるのは `config.KnownStates` の1箇所だけである**（設計 3-57）。**自前で集め直さない。**
// **実行時に「知っている Status か」を判定する一覧**（orchestrator の `knownStates`）
// **とぴったり同じにする。**ずれると、起動時に通した設定が実行時には別の意味になる。
//
// **`automated_state_rewrite` のキーは入れない**（設計 3-57）。
// キーは定義上「continuo が知らない Status」であり、**カンバンに実在しなくてよい。**
// 入れると、**カンバンの自動化をやめて選択肢を消した人が抜け出せなくなる。**
// 設定は正しいままなのに、起動は止まり、走っている continuo は巡回ごとの照合に落ちて
// **カンバン全体の dispatch を飛ばし続ける。**
// **綴りの打ち間違いは、起動を止めずに知らせる**（`missingRewriteKeys`）。
//
// **戻す先（値）も足さない。**`config.Validate` が「戻す先は `active_states` に入っていること」を
// 起動前に要求しているので、足しても1件も増えない。
func requiredStatesForBootstrap(cfg config.TrackerConfig) []string {
	return config.KnownStates(cfg)
}

// missingRewriteKeys は `tracker.automated_state_rewrite` のキーのうち、カンバンの Status の
// 選択肢に無いものを返す（設計 3-57）。
//
// **これは起動を止めない。**キーはカンバンに実在しなくてよい（`requiredStatesForBootstrap`）。
// **だが「綴りを打ち間違えた」と「使わなくなったので選択肢を消した」は同じ形に見える。**
// 前者はその行が一度も効かないまま黙って死ぬので、**起動時に1回だけ名前で知らせる。**
//
// **判定は書き直さない。**`config.RewriteKeysOutsideBoard` をそのまま呼ぶ。
// **`continuo doctor` の見出し語 `対応表のキー` も同じ関数を呼んでいる**（設計 3-57）。
// **違うのは出し方だけである**（こちらは起動時の警告、あちらは記号）。
//
// **`Bootstrap` を通したあとに呼ぶこと。**通っていなければ選択肢の一覧を持っていないので、
// 空を返す。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: カンバンに無いキー（設定に書いてある綴りのまま。名前順）。
func (a *Adapter) missingRewriteKeys(cfg config.TrackerConfig) []string {
	a.mu.RLock()
	bootstrapped := a.bootstrapped
	names := make([]string, 0, len(a.statusOptionNamesFold))
	for _, name := range a.statusOptionNamesFold {
		names = append(names, name)
	}
	a.mu.RUnlock()
	// **通っていなければ「全部カンバンに無い」ではなく「1件も無い」を返す。**
	// 選択肢の一覧を持っていないだけで、キーが実在しないと分かったわけではない。
	if !bootstrapped {
		return nil
	}
	return config.RewriteKeysOutsideBoard(cfg, names)
}

// Bootstrap は起動時の検査を行う（設計 3-6）。
//
// project の ID・Status フィールドの ID・各選択肢の ID を1リクエストで解決して Adapter に
// 覚えさせ、同じリクエストで得た選択肢名が cfg の Status 名（active_states・terminal_states・
// dispatch_state・failure_state・status_signal_map の遷移先）とすべて一致するかを照合する。
//
// **これを怠ると、選択肢名が1つでも食い違っている場合に GraphQL がエラーを出さずに
// 0件を返し続け、キューが無言で止まる**（設計 2-2 / 3-6 の最大の落とし穴）。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: project または Status フィールドが見つからない場合、あるいは Status の選択肢名が
// 1つでも cfg と一致しない場合は CategoryInvalidConfig の *Error（一致しない名前をすべて
// メッセージに列挙する）。GraphQL 呼び出し自体が失敗した場合はそのエラーをそのまま返す。
func (a *Adapter) Bootstrap(ctx context.Context, cfg config.TrackerConfig) error {
	if err := a.resolveStatusOptions(ctx, cfg, true); err != nil {
		return err
	}
	a.logger.Info("tracker アダプタの起動時検査が完了しました",
		"owner", a.owner,
		"project_number", a.projectNumber,
		"status_options", a.statusOptionCount(),
	)
	// **起動時に1回だけ、カンバンにあって設定に無い Status を名前で出す**（設計 3-50）。
	//
	// **件数だけでは気づけない。**`status_options=11` とだけ出しても、そのうち
	// どれを continuo が扱えないのかが分からない。**扱えない Status へ動かされた issue は
	// worker を止められる**ので、名前を先に見せておく。
	//
	// **巡回ごとの再照合（VerifyStatusOptions）では出さない。**10分に1回同じ行が流れると
	// 他の行が埋もれる。
	if unknown := a.unknownStatusOptions(cfg); len(unknown) > 0 {
		a.logger.Info("カンバンには continuo が知らない Status があります"+
			"（continuo は WORKFLOW.md に書かれた Status だけを扱います。"+
			"知らない Status へ動かされた issue は worker を止めます）",
			"件数", len(unknown),
			"知らない Status", strings.Join(unknown, ", "),
		)
	}
	// **対応表のキーがカンバンに無くても起動は止めない**（設計 3-57）。
	// **だが綴りの打ち間違いと見分けが付かない**ので、起動時に1回だけ名前で知らせる。
	if missing := a.missingRewriteKeys(cfg); len(missing) > 0 {
		a.logger.Warn("tracker.automated_state_rewrite のキーがカンバンの Status の選択肢にありません"+
			"（その行は一度も効きません。綴りの打ち間違いなら直してください。"+
			"その Status をカンバンで使わなくなったのなら、対応表からその行を消してください）",
			"件数", len(missing),
			"カンバンに無いキー", strings.Join(missing, ", "),
		)
	}
	return nil
}

// unknownStatusOptions はカンバンにあって設定に無い Status の選択肢名を返す（設計 3-50）。
//
// **照合の向きが `Bootstrap` の検査と逆である。**`Bootstrap` は「設定の名前がカンバンに
// 在るか」を見る（無ければ起動を止める）。こちらは「カンバンの名前が設定に在るか」を見る
// （無くても止めない。知らせるだけである）。
//
// **`Bootstrap` を通したあとに呼ぶこと。**通っていなければ選択肢の一覧を持っていないので、
// 空を返す。
//
// **対応表のキーは「設定に名前が出てくる」側に数える**（`config.NamedStates`。設計 3-57）。
// **起動を止める照合（`requiredStatesForBootstrap`）とは、そこだけ一覧が違う。**
// **キーは人間が WORKFLOW.md に書いた名前である。**この行は
// 「continuo は WORKFLOW.md に書かれた Status だけを扱います」と言うので、
// **書いてある名前を挙げると嘘になる。**
//
// **「キーの Status では worker が止まらないから」ではない。**書き戻して worker を続けるのは
// **カンバンの自動化がその Status を書いたときだけ**であり（設計 3-54）、
// **人間がキーの Status へ動かしたときは、いままでどおり worker を止める**（設計 3-50）。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: 設定に名前が出てこない選択肢名（カンバンの綴りのまま。名前順）。
func (a *Adapter) unknownStatusOptions(cfg config.TrackerConfig) []string {
	wanted := make(map[string]bool)
	for _, s := range config.NamedStates(cfg) {
		wanted[foldStatus(s)] = true
	}
	a.mu.RLock()
	names := make([]string, 0, len(a.statusOptionIDs))
	for name := range a.statusOptionIDs {
		if !wanted[foldStatus(name)] {
			names = append(names, name)
		}
	}
	a.mu.RUnlock()
	sort.Strings(names)
	return names
}

// VerifyStatusOptions は Status の選択肢名がまだ設定と一致するかを検査し直す（設計 3-6 の
// 「巡回ごとに検査するもの」）。
//
// **起動時に一致していても、人間が GitHub の画面で Status を改名すればその瞬間から
// 食い違う。**食い違ったまま巡回を続けると、GraphQL はエラーを出さずに0件を返し続け、
// 「対象0件」が無言で永久に続く。**巡回のたびにこれを呼び、エラーになったらその巡回の
// dispatch を飛ばすこと**（実行中の照合は止めない）。
//
// Bootstrap と同じ1リクエストで検査する。照合に成功した場合は、解決済みの project ID・
// Status フィールド ID・選択肢 ID をそのとき返ってきた最新の値へ更新する
// （選択肢を作り直すと同じ名前でも ID が変わるため、古い ID を持ち続けると
// UpdateStatus が失敗する）。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: 選択肢名が1つでも一致しない場合は CategoryInvalidConfig の *Error。
// GraphQL 呼び出し自体が失敗した場合はそのエラーをそのまま返す。
func (a *Adapter) VerifyStatusOptions(ctx context.Context, cfg config.TrackerConfig) error {
	// **絞り込みキーの検査は起動時（Bootstrap）だけで行う。**設定の status_field は
	// 実行中に変わらず、フィールドがカンバン側で改名されれば下の field(name:) が
	// 見つからずエラーになるため、巡回のたびに数え直す必要が無い。
	if err := a.resolveStatusOptions(ctx, cfg, false); err != nil {
		return err
	}
	a.logger.Debug("Status の選択肢名がまだ設定と一致することを確認しました",
		"owner", a.owner,
		"project_number", a.projectNumber,
		"status_options", a.statusOptionCount(),
	)
	return nil
}

// resolveStatusOptions は project / Status フィールド / 選択肢を1リクエストで解決し、
// 設定の Status 名とすべて一致することを確かめてから Adapter に覚えさせる。
// Bootstrap（起動時）と VerifyStatusOptions（巡回ごと）の共通処理である。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: WORKFLOW.md の front matter の tracker セクション。
// checkFilterKey: true なら「status_field を `items(query:)` のキーとして使えているか」も
// 検査する（Bootstrap だけが true を渡す）。
// 戻り値: project / Status フィールドが見つからない、選択肢名が一致しない、または
// status_field を絞り込みのキーとして使えない場合は CategoryInvalidConfig の *Error。
// GraphQL 呼び出しが失敗した場合はそのエラー。
// **エラーを返す場合、Adapter が覚えている値は一切書き換えない。**
func (a *Adapter) resolveStatusOptions(ctx context.Context, cfg config.TrackerConfig, checkFilterKey bool) error {
	var resp bootstrapQueryResponse
	vars := map[string]any{
		"login":              a.owner,
		"number":             a.projectNumber,
		"statusField":        a.statusField,
		"withStatusQuery":    buildHasFieldQuery(a.statusField),
		"withoutStatusQuery": buildNoFieldQuery(a.statusField),
	}
	if err := a.gql.do(ctx, bootstrapQueryTemplate, vars, &resp); err != nil {
		return err
	}

	if resp.RepositoryOwner == nil || resp.RepositoryOwner.ProjectV2 == nil {
		return &Error{
			Category: CategoryInvalidConfig,
			Message: fmt.Sprintf(
				"project が見つかりません（tracker.provider.owner=%q, project_number=%d を確認してください）",
				a.owner, a.projectNumber,
			),
		}
	}
	project := resp.RepositoryOwner.ProjectV2
	if project.Field == nil || project.Field.Typename != "ProjectV2SingleSelectField" {
		return &Error{
			Category: CategoryInvalidConfig,
			Message: fmt.Sprintf(
				"Status フィールド %q が見つからないか、単一選択（single select）ではありません"+
					"（tracker.provider.status_field を確認してください）",
				a.statusField,
			),
		}
	}

	optionIDs := make(map[string]string, len(project.Field.Options))
	optionNamesFold := make(map[string]string, len(project.Field.Options))
	for _, opt := range project.Field.Options {
		optionIDs[opt.Name] = opt.ID
		optionNamesFold[foldStatus(opt.Name)] = opt.Name
	}

	var missing []string
	for _, want := range requiredStatesForBootstrap(cfg) {
		if _, ok := optionNamesFold[foldStatus(want)]; !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &Error{
			Category: CategoryInvalidConfig,
			Message: fmt.Sprintf(
				"Status の選択肢名が設定と一致しません（カンバン側に無いか改名されています。"+
					"GraphQL はエラーを出さずに0件を返し続けるため、ここで起動を止めます）: %s",
				strings.Join(missing, ", "),
			),
		}
	}

	if checkFilterKey {
		if err := a.checkStatusFieldIsFilterKey(project); err != nil {
			return err
		}
	}

	// **5つをまとめて1回のロックで差し替える。**読み手（別 goroutine の UpdateStatus →
	// lookupOptionID）が、新しい正式名と古い選択肢 ID を混ぜて読まないようにするためである。
	a.mu.Lock()
	a.projectID = project.ID
	a.statusFieldID = project.Field.ID
	a.statusOptionIDs = optionIDs
	a.statusOptionNamesFold = optionNamesFold
	a.bootstrapped = true
	a.mu.Unlock()
	return nil
}

// checkStatusFieldIsFilterKey は status_field を `items(query:)` の絞り込みキーとして
// 使えているかを、Bootstrap の応答に載せた3つの件数から検査する。
//
// **フィールドが在ることと、絞り込みのキーにできることは別である。**
// `field(name: $statusField)` が返っても、`items(query:)` のキーとして解決できるとは
// 限らない。しかも解決できないとき GraphQL はエラーを出さず、値付きの条件なら0件を返す
// （`nosuchfield:"Ready"` が0件。2026-08-20 に project #3 で実測）。**そのまま動かすと
// 「対象が無い」と見分けがつかず、キューが無言で永久に止まる**（設計 2-2 / 3-34）。
//
// project: Bootstrap の応答（件数が載っていない場合は検査しない）。
// 戻り値: 絞り込みのキーとして使えていない場合は CategoryInvalidConfig の *Error。
func (a *Adapter) checkStatusFieldIsFilterKey(project *rawProjectForBootstrap) error {
	if project.TotalItems == nil || project.ItemsWithStatus == nil || project.ItemsWithoutStatus == nil {
		// 件数が返っていない（古い偽サーバなど）。検査できないので何もしない。
		return nil
	}
	total := project.TotalItems.TotalCount
	withValue := project.ItemsWithStatus.TotalCount
	withoutValue := project.ItemsWithoutStatus.TotalCount

	if judgeFilterKeyUsable(total, withValue, withoutValue) {
		a.logger.Debug("status_field を絞り込みのキーとして使えることを確認しました",
			"status_field", a.statusField,
			"全件", total, "値あり", withValue, "値なし", withoutValue,
		)
		return nil
	}
	return &Error{
		Category: CategoryInvalidConfig,
		Message: fmt.Sprintf(
			"tracker.provider.status_field %q を候補の絞り込みのキーとして使えません"+
				"（フィールド自体はカンバンにありますが、items(query:) が名前を解決できていません。"+
				"GitHub はこの場合エラーを出さずに0件を返すため、ここで起動を止めます。"+
				"フィールド名の綴り・空白の数を画面の表示と揃えてください）"+
				": 全件=%d, 値あり=%d, 値なし=%d",
			a.statusField, total, withValue, withoutValue,
		),
	}
}

// statusOptionCount は覚えている Status 選択肢の件数を返す（ログ用）。
func (a *Adapter) statusOptionCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.statusOptionIDs)
}

// StatusOptionNames はカンバン側の Status の選択肢名を、GitHub の綴りのまま全部返す。
//
// **設定に書いた名前だけでは、取り違えを見つけられないから公開してある。**
// `Bootstrap` が照合するのは「設定に書いた名前がカンバンに在るか」だけである。
// **カンバンに `In Progress` と `AI In Progress` が並んでいても、片方が設定に在れば通る。**
// `continuo doctor` はここで全部の選択肢名を受け取り、設定の名前と紛らわしい組を警告する。
//
// **Bootstrap（または VerifyStatusOptions）を通してから呼ぶこと。**通っていなければ nil を返す。
//
// 戻り値: 選択肢名の一覧（昇順。出力の順序を安定させるため）。通っていなければ nil。
func (a *Adapter) StatusOptionNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.bootstrapped {
		return nil
	}
	names := make([]string, 0, len(a.statusOptionIDs))
	for name := range a.statusOptionIDs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FetchProjectWorkflows はカンバンの組み込みの自動化を、GitHub が返した順のまま先頭100件まで返す
// （設計 3-32 の見出し語 `自動化`。issue #209）。
//
// **`continuo doctor` だけが呼ぶ。**常駐プロセスはこの値を使わない。
//
// **起動時の検査のクエリへ混ぜてはならない。**あちらは `do`（`allowNotFound` が偽）で
// 送るので、**GraphQL が `errors` を1件でも返した時点で Bootstrap ごと落ちる。**
// `workflows` を読む権限が無いトークンや、この field を持たない GitHub Enterprise Server では、
// **いままで起動していた continuo が起動しなくなる。**
// **別のリクエストにしておけば、落ちても doctor の見出し語 `自動化` が `!` になるだけで済む。**
//
// **戻り値の nil と長さ0を区別すること。**
//
//	nil    … 応答に `workflows` が無かった（読めなかった）。**`✓` にしてはならない**
//	長さ0  … 自動化が1件も無い
//
// **`Bootstrap` を通していなくても呼べる。**このクエリは project を番号で引くだけで、
// Adapter が覚えている値を1つも使わない。
//
// ctx: 呼び出しに適用するコンテキスト。
// **`first: 100` で切ってある。**GitHub の組み込みの自動化は2026-09-05 時点で10個前後なので、
// ページを送る分岐は作っていない。**101件目以降があるカンバンでは、そこを見ていない。**
//
// 戻り値の1つ目: 自動化の一覧（GitHub が返した順。先頭100件まで）。応答に `workflows` が無ければ nil
// （project そのものが返らなかったときも nil である。**その判定は `Bootstrap` が持っている**）。
// 戻り値の2つ目: 呼び出しそのものが失敗した場合のエラー。
func (a *Adapter) FetchProjectWorkflows(ctx context.Context) ([]ProjectWorkflow, error) {
	var resp projectWorkflowsQueryResponse
	vars := map[string]any{"login": a.owner, "number": a.projectNumber}
	if err := a.gql.do(ctx, projectWorkflowsQueryTemplate, vars, &resp); err != nil {
		return nil, err
	}
	// **project が見つからないときも、ここではエラーにしない。**
	// **その判定は `Bootstrap` が持っている**（呼び出し元の `continuo doctor` は
	// 先に `Bootstrap` を通しており、見つからなければ見出し語 `カンバン` が `✗` になる）。
	// **同じことを2箇所で判定すると、直す先を2つ案内することになる。**
	// ここは nil を返し、見出し語 `自動化` を `!`（確かめられなかった）に落とす。
	if resp.RepositoryOwner == nil ||
		resp.RepositoryOwner.ProjectV2 == nil ||
		resp.RepositoryOwner.ProjectV2.Workflows == nil {
		// **「1件も無い」と取り違えると、doctor が確かめていないことを `✓` で通す。**
		return nil, nil
	}
	conn := resp.RepositoryOwner.ProjectV2.Workflows
	out := make([]ProjectWorkflow, 0, len(conn.Nodes))
	for _, w := range conn.Nodes {
		out = append(out, ProjectWorkflow{Number: w.Number, Name: w.Name, Enabled: w.Enabled})
	}
	return out, nil
}

// writeTargets は書き込み（updateProjectV2ItemFieldValue）に要る値を、
// **1回のロックでまとめて**取り出す。
//
// targetState: 書き込む先の Status 名（大文字小文字は無視して照合する）。
// 戻り値の1つ目: project の ID。
// 戻り値の2つ目: Status フィールドの ID。
// 戻り値の3つ目: targetState に対応する選択肢 ID。
// 戻り値の4つ目: Bootstrap（または VerifyStatusOptions）を通っていれば true。
// 戻り値の5つ目: targetState がカンバン側の選択肢に在れば true。
func (a *Adapter) writeTargets(targetState string) (string, string, string, bool, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.bootstrapped {
		return "", "", "", false, false
	}
	canonical, ok := a.statusOptionNamesFold[foldStatus(targetState)]
	if !ok {
		return a.projectID, a.statusFieldID, "", true, false
	}
	optionID, ok := a.statusOptionIDs[canonical]
	return a.projectID, a.statusFieldID, optionID, true, ok
}

// VerifyKnownStates は states がすべてカンバン側の Status 選択肢に存在するかを確かめる。
//
// **書き込む前に、書き込む先の名前を確かめるために公開してある。**`UpdateStatus` は
// 選択肢に無い名前を渡されると失敗するが、**それを知るのは書きにいったときである。**
// `continuo abandon` は worktree と branch を消したあとに Status を動かすので、
// **消す前にここで確かめないと、綴り違いのときに worktree だけを失う。**
//
// **Bootstrap（または VerifyStatusOptions）を通してから呼ぶこと。**通っていなければ
// 照合に使う選択肢の一覧をまだ持っていないので、何も検査せずに nil を返す。
//
// states: 検査する Status 名の一覧（大文字小文字は無視して照合する）。
// 戻り値: カンバン側に無い名前が1つでもあれば CategoryInvalidConfig の *Error
// （見つからなかった名前をすべて列挙する）。
func (a *Adapter) VerifyKnownStates(states []string) error {
	return a.verifyKnownStates(states)
}

// verifyKnownStates は states がすべてカンバン側の Status 選択肢に存在するかを確かめる。
//
// **Bootstrap（または VerifyStatusOptions）を通っていないときは何も検査しない。**
// 照合に使う選択肢の一覧をまだ持っていないためである。
//
// states: 検査する Status 名の一覧。
// 戻り値: カンバン側に無い名前が1つでもあれば CategoryInvalidConfig の *Error
// （見つからなかった名前をすべて列挙する）。
func (a *Adapter) verifyKnownStates(states []string) error {
	a.mu.RLock()
	bootstrapped := a.bootstrapped
	var unknown []string
	if bootstrapped {
		for _, s := range states {
			if _, ok := a.statusOptionNamesFold[foldStatus(s)]; !ok {
				unknown = append(unknown, s)
			}
		}
	}
	a.mu.RUnlock()

	if !bootstrapped {
		return nil
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return &Error{
		Category: CategoryInvalidConfig,
		Message: fmt.Sprintf(
			"カンバンに無い Status 名が指定されました（GraphQL はエラーを出さずに0件を返すため、"+
				"0件ではなくエラーとして返します。カンバン側で改名された可能性があります）: %s",
			strings.Join(unknown, ", "),
		),
	}
}

// FetchIssuesByStates は states に含まれる Status を持つ issue を、カンバンの並び順のまま
// すべて取得する（SPEC.md 11.1 の fetch_issues_by_states。設計「その2」）。
//
// **呼び出し側は active_states に入っている Status をすべて渡すこと。**
// `status:Ready` のように一部の状態だけに絞ってはならない。`In Progress` を含めないと、
// 再起動後に取り残された issue を誰も拾えなくなる（設計 4-2）。
//
// **返ってきた配列の順序をそのまま使う。**自前で並べ替えない（設計 4-2: 「並び順だけを使う」）。
// **Priority は読まない。**送るクエリに Priority 相当のフィールドを含めていない（設計 4-2）。
//
// ctx: 呼び出しに適用するコンテキスト。
// states: 対象にする Status 名の一覧。空なら GraphQL へリクエストを送らず、空の結果を返す
// （SPEC.md 11.1: "An empty state_names list MUST return an empty result without a provider
// request."）。
// 戻り値: 正規化した Issue のスライス。Status 未設定の item・archive 済みの item・
// content が Issue でも DraftIssue でもない item は、この呼び出しでは省いてログに残す
// （SHOULD。SPEC.md 11.1 / 設計 3-13）。draft issue は Dispatchable=false のまま含める
// （省かない）。states にカンバン側へ存在しない Status 名が含まれている場合は
// CategoryInvalidConfig の *Error を返す（**0件を返さない。**下記の理由による）。
// GraphQL 呼び出し自体が失敗した場合はそのエラーを返す。
//
// **カンバンに無い Status 名を渡されたら、0件ではなくエラーにする。**GitHub の検索は
// 綴りが違うだけで黙って0件を返すため、そのまま返すと「対象が無い」と区別がつかず、
// キューが無言で止まる（設計 2-2 / 3-6 の最大の落とし穴）。照合には Bootstrap（または
// VerifyStatusOptions）で覚えたカンバン側の選択肢名を使うので、**それらを一度も呼んで
// いない場合はこの検査は行わない。**
func (a *Adapter) FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error) {
	if len(states) == 0 {
		return nil, nil
	}
	if err := a.verifyKnownStates(states); err != nil {
		return nil, err
	}

	// **キーは status_field の値を使う。**`status:` の決め打ちにすると、専用フィールドを
	// 設定していても組み込みの `Status` を絞り込んでしまう（設計 3-34）。
	q := buildStatusSearchQuery(a.statusField, states)
	var result []Issue
	after := ""
	for page := 1; ; page++ {
		if page > maxItemPages {
			return nil, &Error{
				Category: CategoryPagination,
				Message: fmt.Sprintf(
					"カンバンのページ数が上限 %d を超えました（1ページ100件。カンバンが想定外に育っています）",
					maxItemPages,
				),
			}
		}
		var resp candidateQueryResponse
		vars := map[string]any{
			"login":       a.owner,
			"number":      a.projectNumber,
			"statusField": a.statusField,
			"q":           q,
		}
		if after != "" {
			vars["after"] = after
		} else {
			vars["after"] = nil
		}

		if err := a.gql.do(ctx, candidateQueryTemplate, vars, &resp); err != nil {
			return nil, err
		}
		if resp.RepositoryOwner == nil || resp.RepositoryOwner.ProjectV2 == nil {
			return nil, &Error{
				Category: CategoryInvalidConfig,
				Message: fmt.Sprintf(
					"project が見つかりません（tracker.provider.owner=%q, project_number=%d を確認してください）",
					a.owner, a.projectNumber,
				),
			}
		}

		conn := resp.RepositoryOwner.ProjectV2.Items
		for i := range conn.Nodes {
			raw := &conn.Nodes[i]
			mapped := mapRawItemToIssue(raw, a.statusField, a.repoTrusted, a.projectNumber)
			if !mapped.Ok {
				a.logger.Warn("候補の一覧から除外しました",
					"item_id", raw.ID, "理由", mapped.Reason,
				)
				continue
			}
			if !mapped.Issue.Dispatchable {
				// 設計 3-13: dispatchable=false の issue も候補には残す（最後の絞り込みは
				// scheduler が持つ）。ただし理由はログに残す。
				a.logger.Info("dispatch できない issue を候補に含めました",
					"item_id", raw.ID, "identifier", mapped.Issue.Identifier, "理由", mapped.NotDispatchableReason,
				)
			}
			if mapped.LinkedBranchIgnoredReason != "" {
				// 設計 3-22d: リンクされた branch を起点に使えなかった。**dispatch は止めない**
				// （既定 branch へ倒して作業は進む）。**だが黙って倒すと、リンクした人は
				// 「リンクしたのに既定 branch から始まった」を手掛かり無しで見ることになる。**
				a.logger.Warn("リンクされた branch を worktree の起点に使いませんでした"+
					"（既定 branch から始めます）",
					"item_id", raw.ID, "identifier", mapped.Issue.Identifier,
					"理由", mapped.LinkedBranchIgnoredReason,
				)
			}
			result = append(result, mapped.Issue)
		}

		if !conn.PageInfo.HasNextPage {
			break
		}
		if conn.PageInfo.EndCursor == "" {
			return nil, &Error{
				Category: CategoryPagination,
				Message:  "hasNextPage が真なのに endCursor が空です（provider 側の異常）",
			}
		}
		after = conn.PageInfo.EndCursor
	}

	return a.dropUnrequestedStates(result, states, q)
}

// filterMismatchMinSample は「絞り込みのキーの解決に失敗した」と判断してよい最小の件数である。
//
// **これより少ない件数では、設定の誤りと反映待ちを見分けられない。**見分けられないなら
// 外れた item を落として続けるほうが安全である（落とせば着手はされないし、
// 他の issue の dispatch も止まらない）。
const filterMismatchMinSample = 4

// dropUnrequestedStates は、頼んだ Status に入っていない item を結果から落とす（設計 3-34）。
//
// **なぜ落として続けるのか。**GitHub の `items(query:)` はサーバ側の検索であり、
// 絞り込みに使われる索引と、同じ応答の `fieldValueByName` が返す値は一致するとは限らない。
// continuo が Status を書いた直後に同じ巡回で候補を取り直すと、**自分が書いた値が原因で
// 1件だけ食い違う。**これで一覧ごとエラーにすると、正しく絞り込めていた他の issue の
// dispatch までその巡回で止まる。
//
// **「設定の誤り」との見分け方。**絞り込みのキーを解決できていない場合、GitHub は
// 条件ごと無かったことにして**カンバンのほぼ全件**を返す。つまり外れる item は多数派になる。
// 反映待ちで外れるのは continuo 自身が直前に書いた item だけで、必ず少数派である。
// そこで、**外れた item が過半数を占め、かつ見分けがつく件数がある場合に限って**
// 設定の誤りとしてエラーにする。
//
// **キーを解決できているかは、そもそも起動時（Bootstrap）に直接測っている**
// （checkStatusFieldIsFilterKey）。ここは推測でしかないので、直接の測定を上書きしない。
//
// result: 正規化済みの item の一覧。
// states: 頼んだ Status の一覧。
// q: 送った検索クエリ（エラー文に載せる）。
// 戻り値の1つ目: 頼んだ Status に入っている item だけの一覧。
// 戻り値の2つ目: 外れた item が多数派で、件数も足りている場合の CategoryResponse の *Error。
func (a *Adapter) dropUnrequestedStates(result []Issue, states []string, q string) ([]Issue, error) {
	kept := make([]Issue, 0, len(result))
	var dropped []Issue
	for i := range result {
		if containsFoldedStatus(states, result[i].State) {
			kept = append(kept, result[i])
			continue
		}
		dropped = append(dropped, result[i])
	}
	if len(dropped) == 0 {
		return kept, nil
	}

	if len(result) >= filterMismatchMinSample && len(dropped)*2 > len(result) {
		return nil, &Error{
			Category: CategoryResponse,
			Message: fmt.Sprintf(
				"絞り込みが効いていません（頼んだ Status に無い item が大半を占めています）: "+
					"外れた件数=%d/%d, 最初の item=%s, 返ってきた Status=%q, 頼んだ Status=%s, "+
					"送ったクエリ=%s。tracker.provider.status_field %q が候補の絞り込みのキーとして"+
					"正しく解決されているか確認してください",
				len(dropped), len(result), dropped[0].ID, dropped[0].State,
				strings.Join(states, ", "), q, a.statusField,
			),
		}
	}

	for i := range dropped {
		a.logger.Warn("頼んだ Status に無い item を候補から落としました"+
			"（絞り込みの索引がまだ追いついていない可能性があります）",
			"item_id", dropped[i].ID, "identifier", dropped[i].Identifier,
			"返ってきた Status", dropped[i].State, "頼んだ Status", strings.Join(states, ", "),
		)
	}
	return kept, nil
}

// FetchIssuesByIDs は指定した project item ID の現在のスナップショットを取り直す
// （SPEC.md 11.1 の fetch_issues_by_ids。設計「その3」）。実行中 issue の照合に使う。
//
// **「いまの Status を書いたのは誰か」（timeline）も一緒に取る**（設計 3-54）。
// **読まない呼び出し元は `FetchIssuesByIDsWithoutTimeline` を呼ぶ**（設計 3-61）。
// **どの呼び出し元がどちらを使うかの一覧は `orchestrator.Tracker` のコメントにある。**
//
// **見つからない ID は「もう見えない」として扱い、結果から省く。**合成した状態を作らない
// （SPEC.md: "IDs no longer visible in the configured scope are omitted; the orchestrator
// treats omission as 'no longer visible' rather than inventing a synthetic state."）。
//
// **候補の集合に居ない item も同じく省く**（archive 済み・Status 未設定・Issue でも
// DraftIssue でもない content）。どれも候補の取得（`items(...)`）は最初から返さないのに、
// こちらの `nodes(ids:)` はそのまま返す。**ここで弾かないと「まだ作業中の状態にある」と
// 誤認し続ける。**
//
// **一方、provider 側の異常（content が空・Issue なのに repository が無い・
// nameWithOwner の形が壊れている）はエラーにする。**黙って省いてはならない
// （SPEC.md 11.1: "An ID-refresh call MUST fail instead of silently omitting a malformed
// requested record, because omission is meaningful."）。
//
// **1件を省くために全件を捨ててはならない。**この呼び出しには複数の run の item が同時に
// 乗る。1件をエラーにすると、実行中 issue の照合・取り残された worktree の照合・
// 再起動時の復元が丸ごと飛び、**残りの run が全部巻き添えになる。**
//
// ctx: 呼び出しに適用するコンテキスト。
// ids: 取り直す project item ID の一覧（Issue.ID）。空なら GraphQL へリクエストを送らず、
// 空の結果を返す。
// 戻り値: 正規化した Issue のスライス（順序は保証しない。SPEC.md 11.1: "Output order is not
// significant"）。いずれかの ID が見つかったのに正規化できなかった場合は CategoryResponse の
// *Error を返す。GraphQL 呼び出し自体が失敗した場合はそのエラーを返す。
func (a *Adapter) FetchIssuesByIDs(ctx context.Context, ids []string) ([]Issue, error) {
	return a.fetchIssuesByIDs(ctx, ids, true)
}

// FetchIssuesByIDsWithoutTimeline は ID 指定の取り直しのうち、
// **「いまの Status を書いたのは誰か」（timeline）を取らない**ものである（設計 3-61）。
//
// **省くのはその1点だけである。**見つからない ID を省く扱いも、候補の集合に居ない item
// （archive 済み・Status 未設定・Issue でも DraftIssue でもない content）を省く扱いも、
// provider 側の異常をエラーにする扱いも `FetchIssuesByIDs` と同じである。
// **`Issue.StatusChangedBy` と `Issue.StatusChangedByAutomation` はゼロ値になる。**
//
// **なぜ2本に分けるか。**timeline はネストした connection を1本ぶら下げるので、
// **使わなくても返る node の数だけ点数が増える**（設計 3-31）。取り直しは巡回ごと・
// turn ごと・着手ごとに走るので、読まない側を軽い経路へ寄せる。
//
// **どの呼び出し元がどちらを使うかの一覧は `orchestrator.Tracker` のコメントにある。**
//
// ctx: 呼び出しに適用するコンテキスト。
// ids: 取り直す project item ID の一覧（Issue.ID）。空なら GraphQL へリクエストを送らず、
// 空の結果を返す。
// 戻り値: FetchIssuesByIDs と同じ（timeline から埋める2つのフィールドだけがゼロ値になる）。
func (a *Adapter) FetchIssuesByIDsWithoutTimeline(ctx context.Context, ids []string) ([]Issue, error) {
	return a.fetchIssuesByIDs(ctx, ids, false)
}

// fetchIssuesByIDs は ID 指定の取り直しの本体である。
//
// ctx: 呼び出しに適用するコンテキスト。
// ids: 取り直す project item ID の一覧。
// withTimeline: 「いまの Status を書いたのは誰か」も取るかどうか。
// **偽で呼ぶのは2つである。**Status を書く前の取り直し（`UpdateStatus`）と、
// 記録を読まない呼び出し元のための `FetchIssuesByIDsWithoutTimeline` である。
// 偽のとき `Issue.StatusChangedBy` と `Issue.StatusChangedByAutomation` はゼロ値になる。
// 戻り値: FetchIssuesByIDs と同じ。
func (a *Adapter) fetchIssuesByIDs(ctx context.Context, ids []string, withTimeline bool) ([]Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	query := byIDsQueryTemplate
	if !withTimeline {
		query = byIDsWithoutTimelineQueryTemplate
	}
	var resp byIDsQueryResponse
	vars := map[string]any{
		"statusField": a.statusField,
		"ids":         ids,
	}
	// **`NOT_FOUND` は「その ID がもう見えない」ことを意味するので、部分的な成功として扱う。**
	// 消えた ID は `data.nodes` に `null` で入るので、下のループが省く。
	if err := a.gql.doAllowingNotFound(ctx, query, vars, &resp); err != nil {
		return nil, err
	}

	result := make([]Issue, 0, len(ids))
	for _, raw := range resp.Nodes {
		if raw == nil {
			// もう見えない。合成した状態を作らず、単に省く。
			continue
		}
		if raw.Typename != "ProjectV2Item" {
			return nil, &Error{
				Category: CategoryResponse,
				Message:  fmt.Sprintf("想定外の node 型です（ProjectV2Item ではない）: %s", raw.Typename),
			}
		}
		mapped := mapRawItemToIssue(raw, a.statusField, a.repoTrusted, a.projectNumber)
		if !mapped.Ok && mapped.Gone {
			// 候補の集合に居ない（archive 済み・Status 未設定・Issue でも DraftIssue でも
			// ない content）。**候補の取得（items）はどれも返さないのに、nodes(ids:) は
			// そのまま返す。**ここで省かないと「まだ作業中の状態にある」と誤認し続ける。
			// 合成した状態は作らず、単に省く。**エラーにもしない**（同じ呼び出しに乗った
			// 他の run を巻き添えにする）。
			a.logger.Warn("ID 指定の取り直しから除外しました（もう見えません）",
				"item_id", raw.ID, "理由", mapped.Reason,
			)
			continue
		}
		if !mapped.Ok {
			return nil, &Error{
				Category: CategoryResponse,
				Message:  fmt.Sprintf("item %s を正規化できません: %s", raw.ID, mapped.Reason),
			}
		}
		result = append(result, mapped.Issue)
	}
	return result, nil
}

// StatusWrite は UpdateStatus が何をしたかを返す。
//
// **`bool` を2つ並べない。**呼び出し側で取り違える。
type StatusWrite struct {
	// Reached は「目的の Status になっているか」である。
	// **取り直した値が既に目的の値で、書き込みを省いた場合も true になる。**
	Reached bool
	// Wrote は「書き込みの mutation を実際に呼んだか」である。
	//
	// **issue へ「Status を動かした」と書いてよいのは、これが真のときだけである。**
	// Reached が真でも Wrote が偽なら、カンバンは何も動いていないので書くことがない。
	Wrote bool
	// Previous は書き込む直前に ID 指定で取り直した Status である。
	//
	// **巡回で読んだ値ではない。**item がもう見えなかったときだけ空になる。
	Previous string
}

// UpdateStatus は project item の Status を書き換える（SPEC.md 11.5 が言う
// provider-native tool 相当の操作。設計「その4」/ 3-25 / 4-1）。
//
// **Status の選択肢そのものを書き換える mutation は使わない。**
// 1件の Status 値だけを書く `updateProjectV2ItemFieldValue` を使う。
//
// **書き込む前に、必ず ID 指定でその item を取り直す。**取り直した結果の State が
// blockedStates に含まれていたら書かない（設計 3-4）。エージェントが自分で `gh` を叩いて
// 先に Status を動かしていた場合に、それを上書き・巻き戻ししないためである。
//
// **許可リストではなく拒否リストである。**グループの issue は Ice Box に置かれるので
// （設計 3-26）、active_states で絞ると表明が1件も反映されない。
//
// **取り直した値が既に targetState と同じなら、書き込みの mutation を送らない。**
// GitHub は同じ値の書き込みを timeline に残さないので、送っても continuo のログにだけ
// 「Status を書き込みました」が出て、issue には何も現れない。あとから「誰がいつ Status を
// 動かしたか」を突き合わせるときに、**continuo が書いたはずの時刻に記録が無い**という
// 食い違いになる。API の呼び出しも1回無駄に増える。
// **送らずに「目的の Status になっている」（Reached が真）を返し、Wrote は偽のままにする。**
//
// ctx: 呼び出しに適用するコンテキスト。
// itemID: 書き込む対象の project item ID（Issue.ID）。
// targetState: 書き込む先の Status 名。Bootstrap で解決した選択肢名と大文字小文字を
// 無視して照合する。
// blockedStates: 「この状態なら書かない」Status の一覧。呼び出し側は terminal_states を渡す。
// 戻り値の1つ目: 何をしたか（StatusWrite）。
// **Reached は「書き込みの API を呼んだかどうか」ではなく「目的の Status になっているか」
// である。**取り直した値が既に targetState と同じだった場合は、書き込みを省いたうえで
// Reached を真にする（呼び出し側は「Status を動かせた」として先へ進んでよい）。
// **Reached が偽なのはエラーではなく、「item がもう見えない」または「取り直した結果、
// blockedStates に入っていたので書かなかった」のいずれかを意味する**
// （呼び出し側はログに残すだけでよい）。
// **Wrote は mutation を実際に呼んだときだけ真になる。**issue へ「何から何へ動かした」と
// 書いてよいのはこのときだけである。
// **Previous には書き込む直前に取り直した Status が入る。**「何から動かしたか」を
// issue へ書くのはこの値である（設計 3-29）。
// 戻り値の2つ目: Bootstrap が未実行の場合は CategoryInvalidConfig、targetState が
// Bootstrap で解決した選択肢に無い場合も CategoryInvalidConfig。取り直しや書き込みの
// GraphQL 呼び出しが失敗した場合はそのエラーを返す。
func (a *Adapter) UpdateStatus(
	ctx context.Context,
	itemID string,
	targetState string,
	blockedStates []string,
) (StatusWrite, error) {
	// **project ID・フィールド ID・選択肢 ID を1回のロックでまとめて取る。**
	// 巡回ループが VerifyStatusOptions で選択肢を差し替えている最中に別々に読むと、
	// 新しい正式名と古い選択肢 ID の組み合わせを書き込みかねない。
	projectID, statusFieldID, optionID, bootstrapped, ok := a.writeTargets(targetState)
	if !bootstrapped {
		return StatusWrite{}, &Error{
			Category: CategoryInvalidConfig,
			Message:  "Bootstrap が呼ばれていません（project / Status フィールドの ID が未解決です）",
		}
	}
	if !ok {
		return StatusWrite{}, &Error{
			Category: CategoryInvalidConfig,
			Message:  fmt.Sprintf("Status の選択肢 %q が見つかりません（Bootstrap で解決した選択肢の一覧に無い）", targetState),
		}
	}

	// 書く前に必ず取り直す。
	// **timeline は取らない。**ここで見るのは取り直した `State` だけであり、
	// 「誰が書いたか」は書き込みの判断に1つも使わない（設計 3-54）。
	current, err := a.fetchIssuesByIDs(ctx, []string{itemID}, false)
	if err != nil {
		return StatusWrite{}, err
	}
	if len(current) == 0 {
		a.logger.Warn("Status を書きませんでした（item がもう見えません）", "item_id", itemID, "target_state", targetState)
		return StatusWrite{}, nil
	}
	previous := current[0].State
	if containsFoldedStatus(blockedStates, previous) {
		a.logger.Info("Status を書きませんでした（取り直した結果、書いてはいけない状態に入っていました）",
			"item_id", itemID, "target_state", targetState, "現在の状態", previous,
		)
		return StatusWrite{Previous: previous}, nil
	}
	// **既にその値なら書きに行かない。**書いても GitHub の timeline には何も残らないので、
	// 「書き込みました」のログだけが残って突き合わせができなくなる。
	// 比較は foldStatus で行う（statusOptionNamesFold の作り方と同じ正規化。SPEC.md 11.3）。
	// **選択肢の正式名ではなく targetState と比べる。**
	// **Reached は真、Wrote は偽である。**目的の Status にはなっているので呼び出し側は
	// 先へ進んでよいが、カンバンは動いていないので「何から何へ動かした」を issue へ書かない。
	if foldStatus(previous) == foldStatus(targetState) {
		a.logger.Info("Status は既にその値でした（書き込みを省きました）",
			"item_id", itemID, "target_state", targetState)
		return StatusWrite{Reached: true, Previous: previous}, nil
	}

	var resp updateStatusResponse
	vars := map[string]any{
		"projectId": projectID,
		"itemId":    itemID,
		"fieldId":   statusFieldID,
		"optionId":  optionID,
	}
	if err := a.gql.do(ctx, updateStatusMutation, vars, &resp); err != nil {
		return StatusWrite{Previous: previous}, err
	}

	a.logger.Info("Status を書き込みました",
		"item_id", itemID, "target_state", targetState, "動かす前の状態", previous)
	return StatusWrite{Reached: true, Wrote: true, Previous: previous}, nil
}

// FetchComments は issue に付いたコメントを取得する。
//
// 用途は「エージェントがコメントを書いたかどうかを判別すること」だけである（設計 3-29）。
// 取得した本文をプロンプトへ渡してはならない。issue の中身はエージェントが
// gh の JSON 出力で自分で読む（設計 3-72。テキスト表示は使わせない）。
//
// **エージェントが書いたコメントは markers.Marker の印で判別する**（Comment.IsAgent）。
// **continuo 自身が代筆したコメント（markers.SelfMarker の印）は、次の turn の入力から
// 外すため結果に含めない**（設計 5-2 の comments.self_marker の説明: 「次の turn の入力
// からは外す」）。
//
// **ただし、印だけで「continuo の側が書いた」と決めない**（設計 3-65）。印は本文の
// 先頭に置くただの文字列であり、issue にコメントできる人なら誰でも同じものを書ける。
// **selfLogin が分かっているときは、投稿者がその持ち主であるものだけを印として扱う。**
// 外部の第三者が self_marker で始まるコメントを書いても、それは除外されずに残り、
// エージェントのコメント（IsAgent）としても数えられない。
// **そのコメントには `MarkedByOther` を立てて返す。**呼び出し側が
// 「印はあるが投稿者が違う」を名指しでログに出せるようにするためである。
//
// **取得を止める経路は持たない。**取得しないと「エージェントがコメントを書いていない」と
// 判定され、成功した run も含めて全件が failure_state へ落ちる。
//
// **持ち回りの印（`continuo:bid` / `continuo:hold` / `continuo:released`）が先頭に付いた
// コメントも結果に含めない**（設計 3-77a）。**投稿者は問わない。**
// 入札は巡回のたびに積み上がるので、混ぜるとエージェントへ渡す入力がそれで埋まる。
// **持ち回りの判定そのものは FetchAllComments が読む**（そちらは1件も落とさない）。
//
// **cfg.Max は「判別のために何件まで遡るか」である**（設計 5-2）。**意味を変えていない。**
// **数えるのは持ち回りの印を外したあとの件数である。**入札は巡回のたびに積み上がるので、
// 印の付いたものを数に入れると、**エージェントが書いた報告が窓から押し出される。**
// **`max` 件が揃うまで、揃わなければ続きが無くなるまで、`after` で取り直す。**
// 入札が積まれていない issue では、いままでどおり1回の問い合わせで終わる。
// GraphQL には降順で要求し、受け取ってから古い順へ並べ替えて返す。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID（Issue.NativeRef["issue_node_id"]）。
// project item の ID ではないことに注意。
// cfg: tracker.provider.comments の設定（GitHub から何件どの順で取るか）。
// cfg.Max が0以下なら50件、100件を超える場合は100件に丸める（警告をログに残す。
// **GitHub の connection は first の上限が100で、101を要求すると
// EXCESSIVE_PAGINATION のエラーになる**。設定の検査でも同じ上限を弾いている）。
// cfg.Order は "oldest_first"（または未設定）だけを受け付ける。
// markers: tracker.comments の設定（マーカー）。空文字のマーカーは判別に使わない。
// selfLogin: continuo が使う gh の持ち主のログイン名（設計 3-65）。
// **空文字なら投稿者を照合せず、印だけで判別する**（持ち主を取れなかったときの動きである）。
// 戻り値: 正規化したコメントの一覧（**古い順**）。**持ち主が書いた** self_marker 付きの
// コメントと、持ち回りの印が付いたコメントは除外済み。cfg.Order が想定外の値の場合は
// CategoryInvalidConfig の *Error。
// GraphQL 呼び出しが失敗した場合はそのエラーを返す。
func (a *Adapter) FetchComments(
	ctx context.Context,
	issueNodeID string,
	cfg config.TrackerProviderCommentsConfig,
	markers config.TrackerCommentsConfig,
	selfLogin string,
) ([]Comment, error) {
	// 想定外の order を黙って無視すると、書いたつもりの設定が効いていないことに気づけない。
	if cfg.Order != "" && cfg.Order != commentsOrderOldestFirst {
		return nil, &Error{
			Category: CategoryInvalidConfig,
			Message: fmt.Sprintf(
				"tracker.provider.comments.order が %q です（%q のみ対応しています）",
				cfg.Order, commentsOrderOldestFirst,
			),
		}
	}

	keep := commentsPerFetch(cfg.Max)
	// **切れたかどうかは捨てる。**この経路は `keep` で狙って打ち切るので、
	// 「古い側を読み切れなかった」は最初から想定どおりである。
	oldestFirst, _, err := a.fetchCommentNodes(ctx, issueNodeID, keep, keep)
	if err != nil {
		return nil, err
	}
	// **持ち回りの印が付いたコメントは、投稿者を問わず外す**（設計 3-77a）。
	// **`self_marker` の判定より先に行う。**入札は継続的に積み上がるので、
	// ここを通すとエージェントへ渡す入力がそれで埋まる。
	// **外したうえで、新しい方から `max` 件だけ残す**（設計 5-2 の「何件まで遡るか」）。
	oldestFirst = keepNewestUnmarked(oldestFirst, keep)

	result := make([]Comment, 0, len(oldestFirst))
	for _, c := range oldestFirst {
		comment := rawCommentToComment(c)
		trimmed := strings.TrimSpace(comment.Body)
		// **印は投稿者と併せて見る**（設計 3-65）。第三者が同じ印を書いても、
		// continuo の側が書いたものとしては扱わない。
		writtenBySelf := comment.WrittenBy(selfLogin)
		marked := (markers.SelfMarker != "" && strings.HasPrefix(trimmed, markers.SelfMarker)) ||
			(markers.Marker != "" && strings.HasPrefix(trimmed, markers.Marker))
		if writtenBySelf && markers.SelfMarker != "" && strings.HasPrefix(trimmed, markers.SelfMarker) {
			// continuo 自身が代筆したコメント。次の turn の入力からは外す。
			continue
		}
		if writtenBySelf && markers.Marker != "" && strings.HasPrefix(trimmed, markers.Marker) {
			comment.IsAgent = true
		}
		// **「印はあるが投稿者が違う」を、落としたことが分かる形で残す**（設計 3-65）。
		// 黙って印を無視すると、人間には「コメントが無い」としか見えない。
		comment.MarkedByOther = marked && !writtenBySelf
		result = append(result, comment)
	}
	return result, nil
}

// ComposeCommentBody は、continuo が投稿する本文を組み立てる（設計 3-82）。
//
// **`PostComment` から切り出してある。**検査の偽の tracker
// （`test/internal/orchestrator` の `fakeTracker`）も、これを呼ぶ。
// **写して持つと、片方を直したときに黙ってずれる。**
// ずれても orchestrator の検査は通り続けるので、**誰も気づけない。**
//
// body: 素の本文。
// selfMarker: 本文の先頭に付ける印（`tracker.comments.self_marker`）。
// **空文字なら付けない。**持ち回りのコメント（入札・hold・released）は、
// 本文が自分で印を持っているので空文字で渡ってくる。
// 戻り値: 投稿する本文。**`self_marker` の次に `config.AIMarker` が入り、
// 先頭に並ぶ印は1つも動かない。**
func ComposeCommentBody(body, selfMarker string) string {
	// **`self_marker` を足す前に印を足す。**順序を逆にしてはならない。
	//
	// **`self_marker` は利用者が設定で決める文字列であり、形を縛る検査が無い**
	// （`tracker.comments.self_marker`）。`[continuo-self]` のような値にできる。
	// **`withAIMarker` は `<!--` で始まる行だけを印の行とみなす**ので、
	// **そういう値を先に足すと、印がその行より前へ入る。**
	// そうなると `FetchComments` の先頭一致が外れ、
	// **continuo 自身の通知が、次の turn の入力から外れなくなる。**
	// 人間が書いたコメントとして、毎 turn エージェントへ渡り続ける。
	//
	// **先に印を足せば、`self_marker` の形を問わない。**
	// 持ち回りのコメント（入札・hold・released）は `selfMarker` が空で渡ってくるので、
	// **本文が自分で持っている印の後ろへ入る。**そちらは固定の `<!--` の印である。
	full := withAIMarker(body)
	// **`self_marker` が印と同じ値でも、二重にしない。**
	// **`self_marker` は利用者が設定で決める文字列で、形も値も縛られていない。**
	// 同じ値を書かれると、印が2行並んだ本文を毎回投稿することになる。
	// **落としても `FetchComments` の判定は変わらない。**先頭は同じ文字列のままである。
	if selfMarker != "" && strings.TrimSpace(selfMarker) != config.AIMarker {
		full = selfMarker + "\n" + full
	}
	return full
}

// PostComment は continuo 自身が issue へコメントを投稿する。
//
// 投稿するのは人間への引き渡しの通知と、Status を動かした記録の2つだけである（設計 3-29）。
// 成果の要約は書かない。エージェントが成果を書かずに終えた場合は、代筆せずに
// セッションを復元して書かせる（設計 3-25 / 3-29）。
// 自分が書いたものには self_marker の印を付け、次の turn の入力から外せるようにする。
//
// **あわせて `config.AIMarker` を1行足す**（設計 3-82）。**人間ではなく機械が書いた、と
// 名乗るためである。**足す先は「先頭に並ぶ印のいちばん後ろ」であり、
// **`self_marker` も、本文が自分で持っている持ち回りの印も、先頭のまま動かない。**
// **持ち回りのコメント（`selfMarker` が空で、本文が `<!-- continuo:bid -->` などで始まるもの）でも足す。**
// 人間が issue の画面で読むものであり、**印を付ける経路から外す理由が無い。**
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID（Issue.NativeRef["issue_node_id"]）。
// body: コメント本文（マーカーを含まない、素の本文）。
// selfMarker: 本文の先頭に付ける印（tracker.comments.self_marker）。空文字なら
// 印を付けずに投稿する（`config.AIMarker` は、それでも足す）。
// 戻り値: 投稿したコメント（IsSelf は常に true）。GraphQL 呼び出しが失敗した場合、または
// 応答にコメントが含まれていない場合はエラーを返す。
func (a *Adapter) PostComment(ctx context.Context, issueNodeID, body, selfMarker string) (*Comment, error) {
	full := ComposeCommentBody(body, selfMarker)

	var resp addCommentResponse
	vars := map[string]any{"subjectId": issueNodeID, "body": full}
	if err := a.gql.do(ctx, addCommentMutation, vars, &resp); err != nil {
		return nil, err
	}
	if resp.AddComment == nil || resp.AddComment.CommentEdge == nil {
		return nil, &Error{
			Category: CategoryResponse,
			Message:  "コメント投稿の応答にコメントが含まれていません",
		}
	}

	comment := rawCommentToComment(resp.AddComment.CommentEdge.Node)
	comment.IsSelf = true
	return &comment, nil
}

// FetchAllComments は issue に付いたコメントを1件残らず取る（設計 3-77a）。
//
// **1件も落とさない。**持ち回りの印が付いたコメント（入札・hold・released）も、
// continuo 自身が代筆したコメントも、そのまま返す。
// **持ち回りの判定はこれを読む**（誰の担当か・期限が切れているか・誰が勝ったか）。
//
// **`FetchComments` と使い分ける。**あちらは「エージェントへ渡す入力」を作る経路であり、
// **印の付いたものを外す。**外したものを見なければ、持ち回りの判定はできない。
//
// **`tracker.provider.comments.max` は見ない。**あれは「エージェントへ渡す入力を何件まで
// 遡るか」の設定であり（設計 5-2）、**ここが要るのは全件である。**
// **1ページは GitHub の上限いっぱい（100件）で取る。**問い合わせの回数がいちばん少なくなる。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID。
// cfg: tracker.provider.comments の設定。**いまは読まない**（引数は呼び出し側の形に合わせてある）。
// 戻り値の1つ目: 正規化したコメントの一覧（**古い順**）。IsAgent / IsSelf / MarkedByOther は
// 立てない（印の判定は呼び出し側が行う）。
// 戻り値の2つ目: **ページ数の上限で古い側を読み切れなかったら true**（issue #140）。
// **件数では当てられない。**1ページが100件に満たないまま20ページを使い切ると、
// 2000件に届かないまま切れる。逆にちょうど2000件で続きが無いこともある。
// 戻り値の3つ目: エラー。
func (a *Adapter) FetchAllComments(
	ctx context.Context,
	issueNodeID string,
	_ config.TrackerProviderCommentsConfig,
) ([]Comment, bool, error) {
	nodes, truncated, err := a.fetchCommentNodes(ctx, issueNodeID, maxCommentsPerFetch, 0)
	if err != nil {
		return nil, false, err
	}
	out := make([]Comment, 0, len(nodes))
	for _, c := range nodes {
		out = append(out, rawCommentToComment(c))
	}
	return out, truncated, nil
}

// commentsPerFetch は `tracker.provider.comments.max` を、実際に使える件数へ丸める。
//
// max: 設定に書かれた件数。
// 戻り値: 1 以上 maxCommentsPerFetch 以下の件数。
func commentsPerFetch(max int) int {
	if max <= 0 {
		return defaultCommentsPerFetch
	}
	if max > maxCommentsPerFetch {
		return maxCommentsPerFetch
	}
	return max
}

// keepNewestUnmarked は、持ち回りの印が付いたコメントを外し、新しい方から keep 件だけ残す
// （設計 3-77a / 5-2）。
//
// **`tracker.provider.comments.max` が数えるのは、印を外したあとの件数である。**
// 印の付いたものを数に入れると、入札が積まれた issue で**エージェントが書いた報告が
// 窓から押し出される。**
//
// oldestFirst: 生のコメント（**古い順**）。
// keep: 残す件数。**0 以下なら件数で絞らない**（印だけを外す）。
// 戻り値: 印を外し、新しい方から keep 件だけ残したもの（**古い順**）。
func keepNewestUnmarked(oldestFirst []rawComment, keep int) []rawComment {
	out := make([]rawComment, 0, len(oldestFirst))
	for _, c := range oldestFirst {
		if handoff.IsMarked(c.Body) {
			continue
		}
		out = append(out, c)
	}
	if keep > 0 && len(out) > keep {
		out = out[len(out)-keep:]
	}
	return out
}

// fetchCommentNodes は issue のコメントをページを辿って取り、古い順に並べて返す。
//
// **GraphQL には降順（新しい順）で要求する。**打ち切られたときに落ちるのを
// 古い側にするためである（最新側は判別に要る）。
//
// **`keep` 件が揃ったら、そこで取るのをやめる**（設計 5-2）。数えるのは
// **持ち回りの印が付いていないコメント**である。入札の積まれていない issue では
// 1ページで揃うので、**問い合わせは1回で終わる。**
//
// **ページ数には上限を置く**（maxCommentPages）。荒らされた issue1件で巡回が止まるのを避ける。
// **上限に達したら WARN を1行残す。**黙って途中で切ると、
// 「hold が見えない＝人間が付けた担当」と読み違える。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID。
// perPage: 1ページで要求する件数（1 以上 maxCommentsPerFetch 以下）。
// keep: 印の付いていないコメントがこれだけ揃ったら取るのをやめる。**0 以下なら全ページ取る。**
// 戻り値の1つ目: 生のコメント（**古い順**）。
// 戻り値の2つ目: **ページ数の上限で古い側を読み切れなかったら true**（issue #140）。
// **`keep` で抜けたときは偽である。**狙って止めたので、読み切れなかったのとは違う。
// 戻り値の3つ目: エラー。
func (a *Adapter) fetchCommentNodes(
	ctx context.Context,
	issueNodeID string,
	perPage int,
	keep int,
) ([]rawComment, bool, error) {
	// **新しい順に積む。**最後にまとめて反転して古い順へ戻す。
	var newestFirst []rawComment
	unmarked := 0
	after := ""
	truncated := false
	for page := 0; page < maxCommentPages; page++ {
		var resp commentsQueryResponse
		vars := map[string]any{"issueId": issueNodeID, "first": perPage}
		if after != "" {
			vars["after"] = after
		}
		if err := a.gql.do(ctx, commentsQueryTemplate, vars, &resp); err != nil {
			return nil, false, err
		}
		if resp.Node == nil || resp.Node.Comments == nil {
			break
		}
		for _, c := range resp.Node.Comments.Nodes {
			newestFirst = append(newestFirst, c)
			if !handoff.IsMarked(c.Body) {
				unmarked++
			}
		}
		if keep > 0 && unmarked >= keep {
			// **要る件数が揃った。**これ以上遡らない（設計 5-2）。
			break
		}
		info := resp.Node.Comments.PageInfo
		if info == nil || !info.HasNextPage || info.EndCursor == "" {
			// **`pageInfo` を返さない応答は「続きは無い」として扱う。**
			// 返らないものを待つと、同じページを永久に取り直すことになる。
			break
		}
		after = info.EndCursor
		if page == maxCommentPages-1 {
			// **続きの cursor を持ったまま、ページ数の上限で抜ける。**
			// **これが「古い側を読み切れなかった」の唯一の条件である**（issue #140）。
			truncated = true
			a.logger.Warn("コメントが多すぎるので途中まででやめました（古いコメントは読めていません）",
				"issue_node_id", issueNodeID, "読んだ件数", len(newestFirst), "ページ数の上限", maxCommentPages)
		}
	}

	oldestFirst := make([]rawComment, len(newestFirst))
	for i, c := range newestFirst {
		oldestFirst[len(newestFirst)-1-i] = c
	}
	return oldestFirst, truncated, nil
}

// FetchViewer は、いま使っているトークンの持ち主を返す（設計 3-77b）。
//
// **ノード ID とログイン名の両方を返す。**担当者を書き足すにはノード ID が要り、
// 「自分の担当か」の照合はログイン名で行う。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: 持ち主。応答に持ち主が入っていなければ CategoryResponse の *Error。
func (a *Adapter) FetchViewer(ctx context.Context) (Assignee, error) {
	var resp viewerResponse
	if err := a.gql.do(ctx, viewerQuery, map[string]any{}, &resp); err != nil {
		return Assignee{}, err
	}
	if resp.Viewer == nil || resp.Viewer.ID == "" || resp.Viewer.Login == "" {
		return Assignee{}, &Error{
			Category: CategoryResponse,
			Message:  "viewer の応答に持ち主の ID とログイン名が入っていません",
		}
	}
	return Assignee{ID: resp.Viewer.ID, Login: resp.Viewer.Login}, nil
}

// AddAssignees は issue に担当者を書き足す（設計 3-77b）。
//
// **書き足しであって、置き換えではない。**既にいる担当者は消さないので、
// **人間が付けた担当を巻き込んで消すことがない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID。
// assigneeIDs: 足す GitHub ユーザーのノード ID。**空なら1バイトも書かずに返る。**
// 戻り値: 書き足したあとの担当者の全員。
func (a *Adapter) AddAssignees(ctx context.Context, issueNodeID string, assigneeIDs []string) ([]Assignee, error) {
	return a.changeAssignees(ctx, addAssigneesMutation, issueNodeID, assigneeIDs)
}

// RemoveAssignees は issue から担当者を外す（設計 3-77c）。
//
// **名指しした1人だけを外す。**人間が同じ issue に別の担当者を足していたら、その人は残る。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID。
// assigneeIDs: 外す GitHub ユーザーのノード ID。**空なら1バイトも書かずに返る。**
// 戻り値: 外したあとの担当者の全員。
func (a *Adapter) RemoveAssignees(ctx context.Context, issueNodeID string, assigneeIDs []string) ([]Assignee, error) {
	return a.changeAssignees(ctx, removeAssigneesMutation, issueNodeID, assigneeIDs)
}

// changeAssignees は担当者の書き足し／取り外しに共通の呼び出しである。
//
// ctx: 呼び出しに適用するコンテキスト。
// mutation: 実行するミューテーション。
// issueNodeID: 下敷きの GitHub issue のノード ID。
// assigneeIDs: 対象の GitHub ユーザーのノード ID。
// 戻り値: 書き換えたあとの担当者の全員。
func (a *Adapter) changeAssignees(
	ctx context.Context,
	mutation string,
	issueNodeID string,
	assigneeIDs []string,
) ([]Assignee, error) {
	ids := make([]string, 0, len(assigneeIDs))
	for _, id := range assigneeIDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if issueNodeID == "" || len(ids) == 0 {
		// **空で呼ばれたら書かない。**空の配列を送っても GitHub は成功を返すが、
		// **呼んだ側は「書けた」と読む。**書いていないことを、書いていないと返す。
		return nil, nil
	}

	// **書き足しと取り外しで応答のキーが違う。**両方を持つ入れ物へ読み込み、
	// 埋まっているほうを使う（片方は必ず nil になる）。
	var resp struct {
		addAssigneesResponse
		removeAssigneesResponse
	}
	vars := map[string]any{"assignableId": issueNodeID, "assigneeIds": ids}
	if err := a.gql.do(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}
	var assignable *rawAssignable
	switch {
	case resp.AddAssignees != nil:
		assignable = resp.AddAssignees.Assignable
	case resp.RemoveAssignees != nil:
		assignable = resp.RemoveAssignees.Assignable
	}
	if assignable == nil || assignable.Assignees == nil {
		// **応答の形が想定と違っても、書き込みそのものは通っている。**
		// エラーにせず、担当者の一覧を返せないことだけを伝える（呼び出し側は取り直す）。
		return nil, nil
	}
	out := make([]Assignee, 0, len(assignable.Assignees.Nodes))
	for _, n := range assignable.Assignees.Nodes {
		out = append(out, Assignee{ID: n.ID, Login: n.Login})
	}
	return out, nil
}

// rawCommentToComment は GraphQL の生の応答を Comment へ変換する。
// IsAgent / IsSelf の判定はここでは行わない（呼び出し側がマーカーの設定を知っているため）。
//
// **`updatedAt` が nil のときは、ゼロ値のまま渡す**（設計 5-3k）。
// **ここで CreatedAt を埋め戻さない。**「編集されていない」と「取れなかった」は別の状態であり、
// **埋め戻すと、応答からフィールドが落ちたことに誰も気づけなくなる。**
// 新しいほうを採るのは、判定する側（`handoff.CommentView.LastTouched`）の仕事である。
func rawCommentToComment(c rawComment) Comment {
	comment := Comment{ID: c.ID, URL: c.URL, Body: c.Body}
	if c.CreatedAt != nil {
		comment.CreatedAt = *c.CreatedAt
	}
	if c.UpdatedAt != nil {
		comment.UpdatedAt = *c.UpdatedAt
	}
	if c.Author != nil {
		comment.Author = c.Author.Login
	}
	return comment
}
