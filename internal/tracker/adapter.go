package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
)

// KindGitHubProjectsV2 は tracker.kind が受け付ける唯一の値である
// （SPEC.md 11.2 が要求する「exact supported tracker.kind value」の公表）。
const KindGitHubProjectsV2 = "github_projects_v2"

// Adapter は GitHub Projects v2 を SPEC.md 第11節の Issue Tracker Integration Contract に
// 沿って読み書きするアダプタである。
//
// 使い方の順序は決まっている。
//  1. NewAdapter で作る
//  2. Bootstrap を1回呼ぶ（設計 3-6）。project の ID・Status フィールドの ID・
//     各選択肢の ID を解決し、設定と選択肢名が一致するかを検査する。
//     **これを呼ぶ前は FetchIssuesByStates 等を呼んでも動くが、UpdateStatus は
//     必ず失敗する**（書き込みに要る ID が無いため）。
//  3. FetchIssuesByStates / FetchIssuesByIDs / UpdateStatus / FetchComments / PostComment を
//     必要に応じて呼ぶ
//
// **Status の選択肢そのものを書き換える mutation（選択肢の指定が全件置き換えとして扱われ、
// 設定済みの Status を全部消す）を呼ぶメソッドはこのパッケージのどこにも無い**
// （CLAUDE.md の絶対制約。test/internal/tracker がこれをソースの grep で確かめている）。
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

// NewAdapter は Adapter を作る。
//
// cfg: WORKFLOW.md の front matter の tracker セクション（設計 5-2）。
// endpoint: GraphQL API の URL。空文字なら本番の GitHub GraphQL API を使う。
// テストでは httptest.Server の URL を渡して本番のボードへ接続しないようにすること。
// token: 認証トークン（ResolveToken で取得した値）。
// httpClient: リクエストを送るクライアント。nil なら http.DefaultClient を使う。
// logger: ログの出力先。nil なら slog.Default() を使う。
// repoTrusted: リポジトリが Claude Code に信頼登録されているかを判定する関数（設計 3-13）。
// **nil を渡すと全てのリポジトリを信頼済みとして扱う。**信頼の判定を orchestrator 側へ
// 出さないために、ここで受け取って Issue.Dispatchable に畳み込む。
// 戻り値: cfg.Kind が KindGitHubProjectsV2 以外の場合は CategoryUnsupportedKind、
// owner / project_number / status_field のいずれかが未設定の場合は CategoryInvalidConfig の
// *Error を返す。
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

	return &Adapter{
		gql:           newGraphQLClient(endpoint, token, httpClient),
		owner:         cfg.Provider.Owner,
		projectNumber: cfg.Provider.ProjectNumber,
		statusField:   cfg.Provider.StatusField,
		logger:        logger,
		repoTrusted:   repoTrusted,
	}, nil
}

// requiredStatesForBootstrap は Bootstrap が照合すべき Status 名の一覧を、
// cfg から重複無く集める。active_states・terminal_states・dispatch_state・failure_state・
// status_signal_map の遷移先をすべて含める（3-6: 「書き込みに要る ID をすべて解決して覚える」）。
func requiredStatesForBootstrap(cfg config.TrackerConfig) []string {
	seen := make(map[string]bool)
	var result []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		result = append(result, s)
	}
	for _, s := range cfg.ActiveStates {
		add(s)
	}
	for _, s := range cfg.TerminalStates {
		add(s)
	}
	add(cfg.DispatchState)
	add(cfg.FailureState)
	for _, target := range cfg.StatusSignalMap {
		if target != nil {
			add(*target)
		}
	}
	return result
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
	if err := a.resolveStatusOptions(ctx, cfg); err != nil {
		return err
	}
	a.logger.Info("tracker アダプタの起動時検査が完了しました",
		"owner", a.owner,
		"project_number", a.projectNumber,
		"status_options", len(a.statusOptionIDs),
	)
	return nil
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
	if err := a.resolveStatusOptions(ctx, cfg); err != nil {
		return err
	}
	a.logger.Debug("Status の選択肢名がまだ設定と一致することを確認しました",
		"owner", a.owner,
		"project_number", a.projectNumber,
		"status_options", len(a.statusOptionIDs),
	)
	return nil
}

// resolveStatusOptions は project / Status フィールド / 選択肢を1リクエストで解決し、
// 設定の Status 名とすべて一致することを確かめてから Adapter に覚えさせる。
// Bootstrap（起動時）と VerifyStatusOptions（巡回ごと）の共通処理である。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: project / Status フィールドが見つからない、または選択肢名が一致しない場合は
// CategoryInvalidConfig の *Error。GraphQL 呼び出しが失敗した場合はそのエラー。
// **エラーを返す場合、Adapter が覚えている値は一切書き換えない。**
func (a *Adapter) resolveStatusOptions(ctx context.Context, cfg config.TrackerConfig) error {
	var resp bootstrapQueryResponse
	vars := map[string]any{
		"login":       a.owner,
		"number":      a.projectNumber,
		"statusField": a.statusField,
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
				"Status の選択肢名が設定と一致しません（ボード側に無いか改名されています。"+
					"GraphQL はエラーを出さずに0件を返し続けるため、ここで起動を止めます）: %s",
				strings.Join(missing, ", "),
			),
		}
	}

	a.projectID = project.ID
	a.statusFieldID = project.Field.ID
	a.statusOptionIDs = optionIDs
	a.statusOptionNamesFold = optionNamesFold
	a.bootstrapped = true
	return nil
}

// verifyKnownStates は states がすべてボード側の Status 選択肢に存在するかを確かめる。
//
// **Bootstrap（または VerifyStatusOptions）を通っていないときは何も検査しない。**
// 照合に使う選択肢の一覧をまだ持っていないためである。
//
// states: 検査する Status 名の一覧。
// 戻り値: ボード側に無い名前が1つでもあれば CategoryInvalidConfig の *Error
// （見つからなかった名前をすべて列挙する）。
func (a *Adapter) verifyKnownStates(states []string) error {
	if !a.bootstrapped {
		return nil
	}
	var unknown []string
	for _, s := range states {
		if _, ok := a.statusOptionNamesFold[foldStatus(s)]; !ok {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return &Error{
		Category: CategoryInvalidConfig,
		Message: fmt.Sprintf(
			"ボードに無い Status 名が指定されました（GraphQL はエラーを出さずに0件を返すため、"+
				"0件ではなくエラーとして返します。ボード側で改名された可能性があります）: %s",
			strings.Join(unknown, ", "),
		),
	}
}

// lookupOptionID は Status 名（大文字小文字を無視）から選択肢 ID を引く。
func (a *Adapter) lookupOptionID(name string) (string, bool) {
	canonical, ok := a.statusOptionNamesFold[foldStatus(name)]
	if !ok {
		return "", false
	}
	id, ok := a.statusOptionIDs[canonical]
	return id, ok
}

// FetchIssuesByStates は states に含まれる Status を持つ issue を、ボードの並び順のまま
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
// （省かない）。states にボード側へ存在しない Status 名が含まれている場合は
// CategoryInvalidConfig の *Error を返す（**0件を返さない。**下記の理由による）。
// GraphQL 呼び出し自体が失敗した場合はそのエラーを返す。
//
// **ボードに無い Status 名を渡されたら、0件ではなくエラーにする。**GitHub の検索は
// 綴りが違うだけで黙って0件を返すため、そのまま返すと「対象が無い」と区別がつかず、
// キューが無言で止まる（設計 2-2 / 3-6 の最大の落とし穴）。照合には Bootstrap（または
// VerifyStatusOptions）で覚えたボード側の選択肢名を使うので、**それらを一度も呼んで
// いない場合はこの検査は行わない。**
func (a *Adapter) FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error) {
	if len(states) == 0 {
		return nil, nil
	}
	if err := a.verifyKnownStates(states); err != nil {
		return nil, err
	}

	q := buildStatusSearchQuery(states)
	var result []Issue
	after := ""
	for {
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
			mapped := mapRawItemToIssue(raw, a.statusField, a.repoTrusted)
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

	return result, nil
}

// FetchIssuesByIDs は指定した project item ID の現在のスナップショットを取り直す
// （SPEC.md 11.1 の fetch_issues_by_ids。設計「その3」）。実行中 issue の照合や、
// UpdateStatus が書き込み前に行う取り直しに使う。
//
// **見つからない ID は「もう見えない」として扱い、結果から省く。**合成した状態を作らない
// （SPEC.md: "IDs no longer visible in the configured scope are omitted; the orchestrator
// treats omission as 'no longer visible' rather than inventing a synthetic state."）。
// **archive 済みの item も同じく省く。**候補の取得（`items(...)`）は archive 済みを返さない
// のに、こちらの `nodes(ids:)` はそのまま返すため、ここで弾かないと「まだ作業中の状態に
// ある」と誤認し続ける。
//
// **一方、見つかったのに正規化できない item（Status 未設定・content が想定外の型）は
// エラーにする。**一覧取得と違って黙って省いてはならない
// （SPEC.md 11.1: "An ID-refresh call MUST fail instead of silently omitting a malformed
// requested record, because omission is meaningful."）。
//
// ctx: 呼び出しに適用するコンテキスト。
// ids: 取り直す project item ID の一覧（Issue.ID）。空なら GraphQL へリクエストを送らず、
// 空の結果を返す。
// 戻り値: 正規化した Issue のスライス（順序は保証しない。SPEC.md 11.1: "Output order is not
// significant"）。いずれかの ID が見つかったのに正規化できなかった場合は CategoryResponse の
// *Error を返す。GraphQL 呼び出し自体が失敗した場合はそのエラーを返す。
func (a *Adapter) FetchIssuesByIDs(ctx context.Context, ids []string) ([]Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var resp byIDsQueryResponse
	vars := map[string]any{
		"statusField": a.statusField,
		"ids":         ids,
	}
	if err := a.gql.do(ctx, byIDsQueryTemplate, vars, &resp); err != nil {
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
		mapped := mapRawItemToIssue(raw, a.statusField, a.repoTrusted)
		if !mapped.Ok && mapped.Gone {
			// archive 済み。**候補の取得（items）は archive 済みを返さないのに、
			// nodes(ids:) はそのまま返す。**ここで省かないと、人間が archive した issue を
			// 「まだ作業中の状態にある」と誤認し続ける。合成した状態は作らず、単に省く。
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

// UpdateStatus は project item の Status を書き換える（SPEC.md 11.5 が言う
// provider-native tool 相当の操作。設計「その4」/ 3-25 / 4-1）。
//
// **Status の選択肢そのものを書き換える mutation は使わない。**
// 1件の Status 値だけを書く `updateProjectV2ItemFieldValue` を使う。
//
// **書き込む前に、必ず ID 指定でその item を取り直す。**取り直した結果の State が
// activeStates に含まれていなければ書かない（設計 3-25: 「continuo は書く前に必ず
// ボードを取り直すので、既に動いていれば書かない」。エージェントが自分で `gh` を叩いて
// 先に Status を動かしていた場合に、それを上書き・巻き戻ししないためである）。
//
// ctx: 呼び出しに適用するコンテキスト。
// itemID: 書き込む対象の project item ID（Issue.ID）。
// targetState: 書き込む先の Status 名。Bootstrap で解決した選択肢名と大文字小文字を
// 無視して照合する。
// activeStates: 「まだ作業中」とみなす Status の一覧。取り直した結果の State がこれに
// 含まれる場合だけ書き込む。
// 戻り値の1つ目: 実際に書き込んだかどうか。false はエラーではなく、「item がもう見えない」
// または「取り直した結果、既に別の Status へ動いていたので書かなかった」のいずれかを意味する
// （呼び出し側はログに残すだけでよい）。
// 戻り値の2つ目: Bootstrap が未実行の場合は CategoryInvalidConfig、targetState が
// Bootstrap で解決した選択肢に無い場合も CategoryInvalidConfig。取り直しや書き込みの
// GraphQL 呼び出しが失敗した場合はそのエラーを返す。
func (a *Adapter) UpdateStatus(
	ctx context.Context,
	itemID string,
	targetState string,
	activeStates []string,
) (bool, error) {
	if !a.bootstrapped {
		return false, &Error{
			Category: CategoryInvalidConfig,
			Message:  "Bootstrap が呼ばれていません（project / Status フィールドの ID が未解決です）",
		}
	}
	optionID, ok := a.lookupOptionID(targetState)
	if !ok {
		return false, &Error{
			Category: CategoryInvalidConfig,
			Message:  fmt.Sprintf("Status の選択肢 %q が見つかりません（Bootstrap で解決した選択肢の一覧に無い）", targetState),
		}
	}

	// 書く前に必ず取り直す。
	current, err := a.FetchIssuesByIDs(ctx, []string{itemID})
	if err != nil {
		return false, err
	}
	if len(current) == 0 {
		a.logger.Warn("Status を書きませんでした（item がもう見えません）", "item_id", itemID, "target_state", targetState)
		return false, nil
	}
	if !containsFoldedStatus(activeStates, current[0].State) {
		a.logger.Info("Status を書きませんでした（取り直した結果、既に作業中の状態から動いていました）",
			"item_id", itemID, "target_state", targetState, "現在の状態", current[0].State,
		)
		return false, nil
	}

	var resp updateStatusResponse
	vars := map[string]any{
		"projectId": a.projectID,
		"itemId":    itemID,
		"fieldId":   a.statusFieldID,
		"optionId":  optionID,
	}
	if err := a.gql.do(ctx, updateStatusMutation, vars, &resp); err != nil {
		return false, err
	}

	a.logger.Info("Status を書き込みました", "item_id", itemID, "target_state", targetState)
	return true, nil
}

// FetchComments は issue に付いたコメントを取得する。
//
// 用途は「エージェントがコメントを書いたかどうかを判別すること」だけである（設計 3-29）。
// 取得した本文をプロンプトへ渡してはならない。issue の中身はエージェントが
// gh issue view --comments で自分で読む。
//
// **エージェントが書いたコメントは cfg.Marker の印で判別する**（Comment.IsAgent）。
// **continuo 自身が代筆したコメント（cfg.SelfMarker の印）は、次の turn の入力から
// 外すため結果に含めない**（設計 5-2 の comments.self_marker の説明: 「次の turn の入力
// からは外す」）。
//
// **cfg.Max は「新しい方から何件まで遡るか」である**（設計 5-2: 「判別のために何件まで
// 遡るか」）。GraphQL には降順で要求し、受け取ってから古い順へ並べ替えて返す。
// **古い方から max 件を取ると、コメントが max 件を超える issue で最新のコメントが落ちる。**
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID（Issue.NativeRef["issue_node_id"]）。
// project item の ID ではないことに注意。
// cfg: tracker.provider.comments の設定。cfg.Fetch が false の場合は取得せず nil を返す。
// cfg.Max が0以下なら50件、100件を超える場合は100件に丸める（警告をログに残す。
// **GitHub の connection は first の上限が100で、101を要求すると
// EXCESSIVE_PAGINATION のエラーになる**。設定の検査でも同じ上限を弾いている）。
// cfg.Order は "oldest_first"（または未設定）だけを受け付ける。
// 戻り値: 正規化したコメントの一覧（**古い順**。ただし件数が上限を超える場合は、
// 新しい方から max 件を取ったうえでその中を古い順に並べたもの）。self_marker の付いた
// コメントは除外済み。cfg.Order が想定外の値の場合は CategoryInvalidConfig の *Error。
// GraphQL 呼び出しが失敗した場合はそのエラーを返す。
func (a *Adapter) FetchComments(
	ctx context.Context,
	issueNodeID string,
	cfg config.TrackerCommentsConfig,
) ([]Comment, error) {
	if !cfg.Fetch {
		return nil, nil
	}
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

	max := cfg.Max
	if max <= 0 {
		max = defaultCommentsPerFetch
	}
	if max > maxCommentsPerFetch {
		a.logger.Warn("tracker.provider.comments.max が GitHub の上限を超えているため丸めました",
			"設定値", cfg.Max, "使う値", maxCommentsPerFetch,
		)
		max = maxCommentsPerFetch
	}

	var resp commentsQueryResponse
	vars := map[string]any{"issueId": issueNodeID, "first": max}
	if err := a.gql.do(ctx, commentsQueryTemplate, vars, &resp); err != nil {
		return nil, err
	}
	if resp.Node == nil || resp.Node.Comments == nil {
		return nil, nil
	}

	// 降順（新しい順）で受け取ったものを古い順へ戻す。
	nodes := resp.Node.Comments.Nodes
	oldestFirst := make([]rawComment, len(nodes))
	for i, c := range nodes {
		oldestFirst[len(nodes)-1-i] = c
	}

	result := make([]Comment, 0, len(oldestFirst))
	for _, c := range oldestFirst {
		comment := rawCommentToComment(c)
		trimmed := strings.TrimSpace(comment.Body)
		if cfg.SelfMarker != "" && strings.HasPrefix(trimmed, cfg.SelfMarker) {
			// continuo 自身が代筆したコメント。次の turn の入力からは外す。
			continue
		}
		if cfg.Marker != "" && strings.HasPrefix(trimmed, cfg.Marker) {
			comment.IsAgent = true
		}
		result = append(result, comment)
	}
	return result, nil
}

// PostComment は continuo 自身が issue へコメントを投稿する。
//
// 投稿するのは人間への引き渡しの通知だけである（打ち切り・stall・信頼が無い、など）。
// 成果の要約は書かない。エージェントが成果を書かずに終えた場合は、代筆せずに
// セッションを復元して書かせる（設計 3-25 / 3-29）。
// 自分が書いたものには self_marker の印を付け、次の turn の入力から外せるようにする。
//
// ctx: 呼び出しに適用するコンテキスト。
// issueNodeID: 下敷きの GitHub issue のノード ID（Issue.NativeRef["issue_node_id"]）。
// body: コメント本文（マーカーを含まない、素の本文）。
// selfMarker: 本文の先頭に付ける印（tracker.provider.comments.self_marker）。空文字なら
// 印を付けずに投稿する。
// 戻り値: 投稿したコメント（IsSelf は常に true）。GraphQL 呼び出しが失敗した場合、または
// 応答にコメントが含まれていない場合はエラーを返す。
func (a *Adapter) PostComment(ctx context.Context, issueNodeID, body, selfMarker string) (*Comment, error) {
	full := body
	if selfMarker != "" {
		full = selfMarker + "\n" + body
	}

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

// rawCommentToComment は GraphQL の生の応答を Comment へ変換する。
// IsAgent / IsSelf の判定はここでは行わない（呼び出し側がマーカーの設定を知っているため）。
func rawCommentToComment(c rawComment) Comment {
	comment := Comment{ID: c.ID, URL: c.URL, Body: c.Body}
	if c.CreatedAt != nil {
		comment.CreatedAt = *c.CreatedAt
	}
	if c.Author != nil {
		comment.Author = c.Author.Login
	}
	return comment
}
