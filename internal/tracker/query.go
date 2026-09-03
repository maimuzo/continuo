package tracker

import (
	"fmt"
	"strings"
	"time"
)

// itemFieldsFragment は project item 1件を取得するときに共通して要る GraphQL フィールドの
// 断片である。候補の取得（fetch_issues_by_states）と ID 指定の取り直し
// （fetch_issues_by_ids）の両方で使い回す。
//
// **Priority は意図的にここに含めない。**continuo は Priority を読まないと決めている
// （設計 4-2）。この断片にどんな名前であれ "riority" を含む文字列を足さないこと。
// テスト（TestFetchIssuesByStates_Priorityを読まない）はこの断片を経由したクエリの
// 送信内容を検査して確認している。
//
// $statusField は呼び出し側が変数として渡す（tracker.provider.status_field。既定 "Status"）。
//
// **item そのものの `type` と `updatedAt` は要求しない。**item の種別は
// `content.__typename` で判別でき、更新時刻は `content.updatedAt` を使うため、
// どちらも読み手がいなかった。**取るのに使わない値は最初から要求しない。**
//
// **`isArchived` は archive 済みの item を弾くために読む**（mapRawItemToIssue）。
// 候補の取得（`items(...)`）は既定の archivedStates が `[NOT_ARCHIVED]` なので archive 済みは
// そもそも返らないが、ID 指定の取り直し（`nodes(ids:)`）にはその既定が効かない
// （2026-08-18 に introspection で確認）。
//
// **`%s` には Issue の断片へ足す追加のフィールドが入る。**いまそこへ入るのは
// `statusChangedTimelineFragment`（誰が Status を書いたか）だけで、
// **足すのは ID 指定の取り直しのクエリにだけである**（itemFieldsFragment を参照）。
const itemFieldsFragmentTemplate = `
  id
  isArchived
  fieldValueByName(name: $statusField) {
    __typename
    ... on ProjectV2ItemFieldSingleSelectValue { name optionId }
  }
  content {
    __typename
    ... on Issue {
      id
      number
      title
      body
      url
      state
      createdAt
      updatedAt
      repository { nameWithOwner isPrivate defaultBranchRef { name } }
      labels(first: 50) { nodes { name } }
      assignees(first: 10) { totalCount nodes { id login } }
      blockedBy(first: 20) { nodes { id number state repository { nameWithOwner } } }
      linkedBranches(first: 5) { totalCount nodes { ref { name repository { nameWithOwner } } } }
      comments { totalCount }%s
    }
    ... on DraftIssue {
      id
      title
      body
      createdAt
      updatedAt
      assignees(first: 10) { totalCount nodes { id login } }
    }
  }
`

// statusChangedTimelineFragment は「いまの Status を書いたのは誰か」を読む断片である
// （設計 2-6 / 3-54）。
//
// **`wasAutomated` だけでは見分けられない。**組み込みの自動化
// （`Pull request linked to issue` など）が動かしたときでも `false` が返る（設計 2-6 の実測）。
// **見分けに使うのは `actor.__typename` である**（自動化は `Bot`、人間と continuo 自身は `User`）。
// **`wasAutomated` も一緒に読む。**同じ応答に載るので費用が増えず、GitHub が将来直せば自動で効く。
//
// **`project { number }` を必ず読む。**1つの issue が複数のボードに載っていると、
// 他のボードのイベントが同じ配列で返る（設計 2-6 の実測）。絞り込みに要る。
//
// **`last: 50` である。**要るのは「いまの Status を書いた最後の1件」だけだが、
// **窓を絞るのはボードで絞り込む前である。**`timelineItems` に「どのボードのイベントか」で
// 絞る引数は無いので、**別のボードで Status が何度も動くと、自分のボードのイベントが
// 窓から押し出される**（`judgeStatusAuthor` が絞るのは、返ってきた50件の中だけである）。
// **押し出されると「誰が書いたか分からない」になり、自動化の書き戻しが効かないまま
// worker が止まる。**1つの issue が載るボードの数だけ余裕を持たせる。
//
// **ネストした connection が1本増えるので、この断片を候補の取得（100件返る）へ
// 足してはならない。**足すのは ID 指定の取り直しだけで、しかも
// **「誰が書いたか」を読む呼び出し元のための取り直しにしか足さない**
// （`byIDsQueryTemplate` と `byIDsWithoutTimelineQueryTemplate` の使い分け。設計 3-61）。
const statusChangedTimelineFragment = `
      timelineItems(last: 50, itemTypes: [PROJECT_V2_ITEM_STATUS_CHANGED_EVENT]) {
        nodes {
          ... on ProjectV2ItemStatusChangedEvent {
            createdAt
            status
            wasAutomated
            actor { __typename login }
            project { number }
          }
        }
      }`

// itemFieldsFragment は候補の取得（fetch_issues_by_states）と識別子での照合が使う断片である。
// **timeline を含まない。**どちらも100件単位でボードを読むので、1件ずつにしか意味の無い
// timeline を足すと費用だけが増える。
var itemFieldsFragment = fmt.Sprintf(itemFieldsFragmentTemplate, "")

// itemFieldsWithTimelineFragment は ID 指定の取り直し（fetch_issues_by_ids）が使う断片である。
// **こちらにだけ timeline を足す**（設計 3-54）。取り直すのは担当中の issue だけなので、
// 件数が少なく、**Status の値と同じ1リクエストで返る**（設計 2-6 の実測）。
var itemFieldsWithTimelineFragment = fmt.Sprintf(itemFieldsFragmentTemplate, statusChangedTimelineFragment)

// candidateQueryTemplate は fetch_issues_by_states が使うクエリである。
// `items(query: $q)` のサーバ側フィルタで Status を絞り込み、返ってきた順序をそのまま使う
// （自前で並べ替えない。設計 4-2）。
//
// **`orderBy: { field: POSITION, direction: ASC }` を明示的に渡す。**POSITION の昇順は
// 「人間がボード上でドラッグして決めた並び順」そのものであり、continuo は実行順序の全部を
// この順序に賭けている（設計 4-2 / 4-4）。**省略してもいまは同じ既定値になる**
// （2026-08-18 の introspection で `{field: POSITION, direction: ASC}` を実測）**が、
// 既定値は provider 側の都合で変わりうる。**黙って実行順序が変わるのを防ぐため、
// 明示して固定する。
//
// **owner が organization か user かをこちらで判定する必要が無いよう、
// `repositoryOwner` を使う。**Organization / User はどちらも ProjectV2Owner インターフェースを
// 実装しているため、`... on ProjectV2Owner` のフラグメントで両対応できる
// （2026-08-18 に project #3 で読み取り専用の introspection とクエリで実測確認済み）。
var candidateQueryTemplate = `
query($login: String!, $number: Int!, $statusField: String!, $q: String!, $after: String) {
  repositoryOwner(login: $login) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        items(first: 100, after: $after, query: $q, orderBy: { field: POSITION, direction: ASC }) {
          pageInfo { hasNextPage endCursor }
          nodes {` + itemFieldsFragment + `
          }
        }
      }
    }
  }
}
`

// byIDsQueryTemplate は fetch_issues_by_ids が使うクエリである。
// `nodes(ids:)` は見つからない ID に対して null を返す（削除・archive 等で
// 「もう見えない」ID を、エラーにせず「見えなくなった」として扱える。2026-08-18 に実測確認）。
//
// **timeline を足してあるのはこのクエリだけである**（設計 3-54）。取り直すのは担当中の
// issue だけなので件数が少なく、リクエストは1本のままである。
var byIDsQueryTemplate = `
query($statusField: String!, $ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    ... on ProjectV2Item {` + itemFieldsWithTimelineFragment + `
    }
  }
}
`

// byIDsWithoutTimelineQueryTemplate は **「誰が Status を書いたか」を読まない取り直し**が
// 使うクエリである（`UpdateStatus` と `FetchIssuesByIDsWithoutTimeline`。設計 3-61）。
//
// **timeline を取らない。**これらの経路が timeline から読むものは1つも無い
// （見るのは取り直した `State` だけである）。
// **Status は turn ごと・巡回ごとに書き、取り直しは着手ごと・巡回ごとにも走るので、
// この経路がいちばん多く呼ばれる。**ネストした connection を1本ぶら下げたままにすると、
// **使わない50件のイベントを毎回読む**ことになる
// （GraphQL の点数は返す node の数で決まる。設計 3-31）。
var byIDsWithoutTimelineQueryTemplate = `
query($statusField: String!, $ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    ... on ProjectV2Item {` + itemFieldsFragment + `
    }
  }
}
`

// bootstrapQueryTemplate は起動時の検査（Bootstrap）が使うクエリである。
// project の ID・Status フィールドの ID・各選択肢の ID と名前を1リクエストで取る
// （設計 3-6 / 2-2: 「選択肢名の照合と同じリクエストで取れる」）。
//
// **同じリクエストで「status_field を絞り込みのキーとして使えるか」も測る。**
// `field(name:)` が返ることは「フィールドが在る」ことしか示さない。`items(query:)` の
// キーとして解決できるかは別問題であり、解決できなくても GraphQL はエラーを出さない
// （3-34）。そこで item の件数を3つ取り、下の対応で判定する（judgeFilterKeyUsable）。
//
//	totalItems         全件
//	itemsWithStatus    `-no:"<status_field>"`（値が入っている件数）
//	itemsWithoutStatus `no:"<status_field>"`（値が空の件数）
//
// **`first: 0` を渡して node を1件も要求しない。**件数だけが要るので、
// GraphQL の点数計算（設計 3-31）にほとんど乗らない
// （2026-08-20 に project #3 への読み取り専用クエリで `first: 0` が通ることを実測）。
const bootstrapQueryTemplate = `
query($login: String!, $number: Int!, $statusField: String!, $withStatusQuery: String!, $withoutStatusQuery: String!) {
  repositoryOwner(login: $login) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        id
        field(name: $statusField) {
          __typename
          ... on ProjectV2SingleSelectField {
            id
            options { id name }
          }
        }
        totalItems: items(first: 0) { totalCount }
        itemsWithStatus: items(first: 0, query: $withStatusQuery) { totalCount }
        itemsWithoutStatus: items(first: 0, query: $withoutStatusQuery) { totalCount }
      }
    }
  }
}
`

// updateStatusMutation は Status を書き込むミューテーションである（設計「その4」）。
// 1件の Status 値だけを書く `updateProjectV2ItemFieldValue` を使う。
// Status の選択肢そのものを書き換える mutation（選択肢の指定が全件置き換えとして扱われ、
// 設定済みの Status を全部消す）は使わない。
const updateStatusMutation = `
mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String!) {
  updateProjectV2ItemFieldValue(
    input: {
      projectId: $projectId
      itemId: $itemId
      fieldId: $fieldId
      value: { singleSelectOptionId: $optionId }
    }
  ) {
    projectV2Item { id }
  }
}
`

// commentsQueryTemplate は作業開始時に既存コメントを取るクエリである（設計「その7」/ 3-77a）。
// IssueCommentOrderField の取りうる値は UPDATED_AT のみ（2026-08-18 に introspection で確認）。
//
// **降順（DESC）で取る。**昇順で先頭から取ると、続きを取り切れなかったときに
// **最新のコメントが落ちて最古のコメントだけが残る。**代筆の要否（エージェントが直近の
// turn でコメントを書いたか）の判別には最新側が要る。
// **Go 側で受け取ってから逆順に並べ替え、古い順（oldest_first）にして返す**（FetchComments）。
//
// **`comments.max` は「1回の取得で何件ずつ取るか」である**（設計 3-77a）。
// **打ち切りの件数ではない。**`pageInfo` を読んで、続きがある限り `after` で取り直す。
// **持ち回りを始めると入札のコメントが積み上がるので、新しい方から数十件だけを見ていると
// エージェントが書いた報告が窓から押し出される。**
//
// **`updatedAt` も取る**（設計 5-3k）。エージェントは進捗の報告を新しいコメントにせず、
// **いちばん下にある自分の進捗報告へ書き足す**（設計 5-3j）。
// **本文を編集しても `createdAt` は動かない**（2026-09-03 に実測）ので、
// **これを取らないと、書き続けている機械の持ち回りの期限が1秒も進まない。**
const commentsQueryTemplate = `
query($issueId: ID!, $first: Int!, $after: String) {
  node(id: $issueId) {
    __typename
    ... on Issue {
      comments(first: $first, after: $after, orderBy: { field: UPDATED_AT, direction: DESC }) {
        pageInfo { hasNextPage endCursor }
        nodes { id url body createdAt updatedAt author { login } }
      }
    }
  }
}
`

// maxCommentPages はコメントの取得で辿るページ数の上限である。
//
// **上限を置かないと、荒らされた issue1件で巡回が止まる。**1ページ100件なので
// 20ページで2000件まで読める。**ここに達したら、読めた分で判定し、WARN を1行残す。**
const maxCommentPages = 20

// viewerQuery は「いまのトークンの持ち主」を取るクエリである（設計 3-77b）。
//
// **ノード ID が要る。**担当者を書き足す `addAssigneesToAssignable` はログイン名を受け付けず、
// **ノード ID しか受け付けない。**
//
// **`gh api user` ではなく GraphQL で取る。**ボードの読み書きと同じ経路・同じ認証で取れるので、
// **「ボードは読めるのに持ち主だけ取れない」という食い違いが起きない。**
const viewerQuery = `
query {
  viewer { id login }
}
`

// addAssigneesMutation は issue に担当者を書き足すミューテーションである（設計 3-77b）。
//
// **書き足しであって、置き換えではない。**`addAssigneesToAssignable` は既にいる担当者を
// 消さないので、**人間が付けた担当を巻き込んで消すことがない。**
//
// **いまの `gh` の認証（scope に `repo`）でそのまま呼べる**（2026-08-29 に `viewerCanAssign: true` を確認）。
const addAssigneesMutation = `
mutation($assignableId: ID!, $assigneeIds: [ID!]!) {
  addAssigneesToAssignable(input: { assignableId: $assignableId, assigneeIds: $assigneeIds }) {
    assignable {
      ... on Issue { id assignees(first: 10) { totalCount nodes { id login } } }
    }
  }
}
`

// removeAssigneesMutation は issue から担当者を外すミューテーションである（設計 3-77c）。
//
// **名指しした1人だけを外す。**全員を置き換える呼び方はしない。
// **人間が同じ issue に別の担当者を足していたら、その人は残す。**
const removeAssigneesMutation = `
mutation($assignableId: ID!, $assigneeIds: [ID!]!) {
  removeAssigneesFromAssignable(input: { assignableId: $assignableId, assigneeIds: $assigneeIds }) {
    assignable {
      ... on Issue { id assignees(first: 10) { totalCount nodes { id login } } }
    }
  }
}
`

// maxCommentsPerFetch は1回のコメント取得で要求できる件数の上限である。
//
// **GitHub の connection は `first` の上限が 100 である。**101 を要求すると
// EXCESSIVE_PAGINATION のエラーになる（2026-08-18 に実測。
// "Requesting 101 records on the `comments` connection exceeds the `first` limit of
// 100 records."）。設定に 200 と書けてしまうと、起動時ではなく毎回のコメント取得で失敗するため、
// 設定の検査（internal/config の validate）と FetchComments の両方でここへ丸める。
const maxCommentsPerFetch = 100

// defaultCommentsPerFetch は comments.max が未設定（0以下）のときに使う件数である
// （設計 5-2 の設定例の `max: 50`）。
const defaultCommentsPerFetch = 50

// commentsOrderOldestFirst は tracker.provider.comments.order が受け付ける唯一の値である
// （設計 5-2）。他の値を黙って無視すると、書いたつもりの設定が効かないことに気づけない。
const commentsOrderOldestFirst = "oldest_first"

// addCommentMutation は continuo がコメントを代筆するときに使うミューテーションである
// （設計「その7」/ 3-25）。subjectId には Issue.NativeRef["issue_node_id"] を渡す
// （project item の ID ではなく、下敷きの GitHub issue のノード ID が要る）。
//
// **`updatedAt` も取る。**応答は `rawComment` へ読み込むので、
// **コメント取得のクエリと同じ形にしておかないと、投稿の直後だけ更新時刻が空になる。**
const addCommentMutation = `
mutation($subjectId: ID!, $body: String!) {
  addComment(input: { subjectId: $subjectId, body: $body }) {
    commentEdge {
      node { id url body createdAt updatedAt author { login } }
    }
  }
}
`

// ===== 応答の wire format =====

type rawRef struct {
	Name string `json:"name"`
	// Repository はその ref が在るリポジトリである。
	//
	// **linkedBranches の ref でだけ埋まる。**`defaultBranchRef` は `name` しか
	// 要求していないので nil のままである。
	Repository *rawRepository `json:"repository"`
}

type rawLinkedBranch struct {
	Ref *rawRef `json:"ref"`
}

type rawLinkedBranchConn struct {
	// TotalCount はリンクされた branch の総数である。
	//
	// **`Nodes` の長さと一致するとは限らない。**取得の窓（`linkedBranches(first: 5)`）に
	// 収まらなかった分はここにしか出ない。**総数を見ないと「窓の外に6本目がある」ことに
	// 気づけず、1本だけリンクされていると誤って判定する。**
	TotalCount int               `json:"totalCount"`
	Nodes      []rawLinkedBranch `json:"nodes"`
}

type rawLabel struct {
	Name string `json:"name"`
}

type rawLabelConn struct {
	Nodes []rawLabel `json:"nodes"`
}

type rawUser struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// rawUserConn は担当者の connection である。
//
// **`totalCount` も読む**（設計 3-77b）。担当者が2人以上いる issue には continuo は触らない
// ので、**「返ってきた件数」ではなく「本当に何人いるか」を知る必要がある。**
// `first` の窓に収まらないほど付いていても、件数だけは正しく返る。
type rawUserConn struct {
	TotalCount int       `json:"totalCount"`
	Nodes      []rawUser `json:"nodes"`
}

type rawRepository struct {
	NameWithOwner string `json:"nameWithOwner"`
	// IsPrivate はリポジトリが非公開かどうかである（設計 3-64）。
	//
	// **ポインタで持つ。**GraphQL の `isPrivate` は非 null だが、
	// **この構造体は `blockedBy` の中の `repository`（`nameWithOwner` しか
	// 要求していない）にも使い回される。**値型にすると、要求していない応答が
	// 「公開である」と読めてしまう。**nil は「取れなかった」である。**
	IsPrivate        *bool   `json:"isPrivate"`
	DefaultBranchRef *rawRef `json:"defaultBranchRef"`
}

type rawBlockerIssue struct {
	ID         string         `json:"id"`
	Number     int            `json:"number"`
	State      string         `json:"state"`
	Repository *rawRepository `json:"repository"`
}

type rawBlockerConn struct {
	Nodes []rawBlockerIssue `json:"nodes"`
}

type rawCommentsCount struct {
	TotalCount int `json:"totalCount"`
}

// rawActor は timeline のイベントを起こした主体である（設計 2-6）。
//
// **`__typename` で自動化と人間を見分ける。**`Bot` なら GitHub App（組み込みの自動化を含む）、
// `User` なら人間か continuo 自身（`gh auth token` の持ち主）である。
type rawActor struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

// rawEventProject は timeline のイベントが指すボードである。番号での絞り込みにだけ使う。
type rawEventProject struct {
	Number int `json:"number"`
}

// rawStatusChangedEvent は ProjectV2ItemStatusChangedEvent 1件の応答である（設計 2-6）。
type rawStatusChangedEvent struct {
	CreatedAt *time.Time `json:"createdAt"`
	// Status はこのイベントで書き込まれた Status 名である。
	Status string `json:"status"`
	// WasAutomated は GraphQL の "Did this event result from workflow automation?"
	// （訳: このイベントは workflow の自動化から生じたものか？）である。
	// **組み込みの自動化でも false が返る**ので、これ単独では判定に使えない（設計 2-6）。
	WasAutomated bool             `json:"wasAutomated"`
	Actor        *rawActor        `json:"actor"`
	Project      *rawEventProject `json:"project"`
}

// rawTimelineItems は timelineItems 接続の応答である。
type rawTimelineItems struct {
	Nodes []rawStatusChangedEvent `json:"nodes"`
}

// rawContent は ProjectV2Item.content の中身である。Issue と DraftIssue のフィールドを
// すべて1つの構造体に平らに持つ（GraphQL のインラインフラグメントは同じ JSON オブジェクトに
// マージされる）。どちらの型のフィールドかは Typename で判別する。
type rawContent struct {
	Typename       string               `json:"__typename"`
	ID             string               `json:"id"`
	Number         int                  `json:"number"`
	Title          string               `json:"title"`
	Body           string               `json:"body"`
	URL            string               `json:"url"`
	State          string               `json:"state"`
	CreatedAt      *time.Time           `json:"createdAt"`
	UpdatedAt      *time.Time           `json:"updatedAt"`
	Repository     *rawRepository       `json:"repository"`
	Labels         *rawLabelConn        `json:"labels"`
	Assignees      *rawUserConn         `json:"assignees"`
	BlockedBy      *rawBlockerConn      `json:"blockedBy"`
	LinkedBranches *rawLinkedBranchConn `json:"linkedBranches"`
	Comments       *rawCommentsCount    `json:"comments"`
	// TimelineItems は「誰が Status を書いたか」のイベントである（設計 3-54）。
	// **ID 指定の取り直しのクエリでだけ埋まる。**候補の取得では常に nil である。
	TimelineItems *rawTimelineItems `json:"timelineItems"`
}

// rawStatusValue は fieldValueByName(name: "Status") の応答である。
// __typename が ProjectV2ItemFieldSingleSelectValue でない場合（Status が単一選択でない
// 設定になっている等）は Name が空のまま届く。
type rawStatusValue struct {
	Typename string `json:"__typename"`
	Name     string `json:"name"`
	OptionID string `json:"optionId"`
}

// rawItem は ProjectV2Item 1件の応答である。fetch_issues_by_states と fetch_issues_by_ids の
// 両方で共通して使う。
type rawItem struct {
	// Typename は fetch_issues_by_ids（nodes(ids:) 経由）でだけ埋まる。
	// items() 経由（fetch_issues_by_states）では常に ProjectV2Item なので送っていない。
	Typename string `json:"__typename"`
	ID       string `json:"id"`
	// IsArchived は item がボード上で archive されているかどうかである。
	// **archive 済みの item は「もう見えない」として扱う**（mapRawItemToIssue）。
	IsArchived       bool            `json:"isArchived"`
	FieldValueByName *rawStatusValue `json:"fieldValueByName"`
	Content          *rawContent     `json:"content"`
}

type rawItemConnection struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []rawItem `json:"nodes"`
}

type rawProjectForItems struct {
	Items rawItemConnection `json:"items"`
}

type rawRepositoryOwnerForItems struct {
	ProjectV2 *rawProjectForItems `json:"projectV2"`
}

type candidateQueryResponse struct {
	RepositoryOwner *rawRepositoryOwnerForItems `json:"repositoryOwner"`
}

type byIDsQueryResponse struct {
	Nodes []*rawItem `json:"nodes"`
}

type rawOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type rawStatusField struct {
	Typename string      `json:"__typename"`
	ID       string      `json:"id"`
	Options  []rawOption `json:"options"`
}

// rawItemCount は件数だけを取る items 接続の応答である。
type rawItemCount struct {
	TotalCount int `json:"totalCount"`
}

type rawProjectForBootstrap struct {
	ID    string          `json:"id"`
	Field *rawStatusField `json:"field"`
	// 以下の3つは絞り込みキーの検査に使う（judgeFilterKeyUsable）。
	// **ポインタにしてある。**応答に含まれていなければ nil になり、検査を飛ばせる。
	TotalItems         *rawItemCount `json:"totalItems"`
	ItemsWithStatus    *rawItemCount `json:"itemsWithStatus"`
	ItemsWithoutStatus *rawItemCount `json:"itemsWithoutStatus"`
}

type rawRepositoryOwnerForBootstrap struct {
	ProjectV2 *rawProjectForBootstrap `json:"projectV2"`
}

type bootstrapQueryResponse struct {
	RepositoryOwner *rawRepositoryOwnerForBootstrap `json:"repositoryOwner"`
}

type updateStatusResponse struct {
	UpdateProjectV2ItemFieldValue *struct {
		ProjectV2Item *struct {
			ID string `json:"id"`
		} `json:"projectV2Item"`
	} `json:"updateProjectV2ItemFieldValue"`
}

type rawComment struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Body      string     `json:"body"`
	CreatedAt *time.Time `json:"createdAt"`
	// UpdatedAt は本文が最後に編集された時刻である（設計 5-3k）。
	//
	// **編集しても `createdAt` は動かない**（2026-09-03 に実測）。
	// **進捗の報告は、いちばん下にある自分のコメントへ書き足す**（設計 5-3j）ので、
	// **これが無いと、書き続けている機械の持ち回りの期限が進まない。**
	//
	// **取れなければ nil である。**古い応答を返す偽サーバや、
	// フィールドを要求していない経路がそうなる。
	UpdatedAt *time.Time `json:"updatedAt"`
	Author    *rawUser   `json:"author"`
}

type rawCommentConn struct {
	PageInfo *rawPageInfo `json:"pageInfo"`
	Nodes    []rawComment `json:"nodes"`
}

// rawPageInfo は connection の続きの有無である。
//
// **nil は「続きは無い」として扱う。**`pageInfo` を返さない応答（テストの偽サーバ）で
// 取得が止まらなくなるのを避ける。
type rawPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type rawIssueForComments struct {
	Typename string          `json:"__typename"`
	Comments *rawCommentConn `json:"comments"`
}

type commentsQueryResponse struct {
	Node *rawIssueForComments `json:"node"`
}

type addCommentResponse struct {
	AddComment *struct {
		CommentEdge *struct {
			Node rawComment `json:"node"`
		} `json:"commentEdge"`
	} `json:"addComment"`
}

type viewerResponse struct {
	Viewer *rawUser `json:"viewer"`
}

// rawAssignable は担当者を書き換えたあとの issue である。
type rawAssignable struct {
	ID        string       `json:"id"`
	Assignees *rawUserConn `json:"assignees"`
}

type addAssigneesResponse struct {
	AddAssignees *struct {
		Assignable *rawAssignable `json:"assignable"`
	} `json:"addAssigneesToAssignable"`
}

type removeAssigneesResponse struct {
	RemoveAssignees *struct {
		Assignable *rawAssignable `json:"assignable"`
	} `json:"removeAssigneesFromAssignable"`
}

// ===== クエリの組み立て =====

// buildStatusSearchQuery は active_states / terminal_states の一覧から、
// `items(query:)` に渡す検索クエリ文字列を組み立てる。
//
// **キーは `status:` 決め打ちではなく、tracker.provider.status_field の値を使う。**
// 決め打ちにすると、専用フィールド（例: `continuo Status`）を設定していても
// 組み込みの `Status` を絞り込んでしまう。しかも**その失敗は無言である**
// （下記の実測。設計 3-34）。
//
// **キーはダブルクオートで囲む。空白を含むフィールド名はこれで使える。**
// 2026-08-20 に project #3（105件）への読み取り専用クエリで実測した結果は次のとおり。
//
//	"status":"Ice Box"            → 93件（引用符付きのキーは値付きの絞り込みでも通る）
//	'status':'Ice Box'            →  0件（**シングルクオートはキーには使えない**）
//	-no:"parent issue" / no:…     →  0件 / 105件（**空白入りの名前は引用符付きなら通る**）
//	no:parent issue               →  0件（引用符なしはクエリごと壊れる）
//	-no:parentissue               → 105件（空白を詰めた綴りは別名として扱われず、無視される）
//	-no:"parent  issue"           → 105件（空白の数まで一致していないと通らない）
//	STATUS:"ice BOX"              → 93件（キーも値も大文字小文字を区別しない）
//	nosuchfield:"Ready"           →  0件（**存在しないキーはエラーにならず0件を返す**）
//
// 同じキーの複数の値をカンマ区切りで並べると OR になる（`"status":"A","B"` で A または B）。
// **キーを分けて書くと AND になり0件になる**（`status:"Done" status:"In Review"` → 0件）。
// **カンマは引用符の中では区切りにならない**（`status:"Done,In Review"` → 0件）ので、
// 選択肢名にカンマが含まれていても OR に化けない。
//
// statusField: 絞り込みのキーにするフィールド名（tracker.provider.status_field）。
// states: 対象にする Status 名の一覧。
// 戻り値: `"<statusField>":"A","B",...` の形の検索クエリ文字列。states が空なら空文字を返す
// （呼び出し側は states が空の時点でリクエストを送らないため、実際には呼ばれない）。
func buildStatusSearchQuery(statusField string, states []string) string {
	if len(states) == 0 {
		return ""
	}
	quoted := make([]string, len(states))
	for i, s := range states {
		quoted[i] = quoteSearchValue(s)
	}
	return quoteSearchKey(statusField) + ":" + strings.Join(quoted, ",")
}

// buildHasFieldQuery は「そのフィールドに値が入っている item」を選ぶ検索クエリを組み立てる
// （`-no:"<field>"`）。絞り込みキーの検査にだけ使う（judgeFilterKeyUsable）。
func buildHasFieldQuery(field string) string {
	return "-no:" + quoteSearchKey(field)
}

// buildNoFieldQuery は「そのフィールドの値が空の item」を選ぶ検索クエリを組み立てる
// （`no:"<field>"`）。絞り込みキーの検査にだけ使う（judgeFilterKeyUsable）。
func buildNoFieldQuery(field string) string {
	return "no:" + quoteSearchKey(field)
}

// judgeFilterKeyUsable は Bootstrap で数えた3つの件数から、status_field を
// `items(query:)` のキーとして使えているかを判定する。
//
// **判定の理屈。**GitHub は知らないキーを見ると、その条件ごと無かったことにする。
// `no:` と `-no:` は本来たがいに排他なので、キーを解決できていれば
// 「値がある件数」＋「値が空の件数」＝「全件」になる。解決できていなければ
// どちらも全件を返す。2026-08-20 に project #3（全105件）で実測した値は次のとおり。
//
//	-no:"status"        → 100件、no:"status"        →   5件（合計105。解決できている）
//	-no:"nosuchfield"   → 105件、no:"nosuchfield"   → 105件（どちらも全件。解決できていない）
//
// **判定は「両方が全件と一致するか」だけで行い、合計との差では見ない。**
// 数えている最中に人間がボードへ item を足すと合計が1件ずれることがあり、
// 差で見ると誤検知する。両方が全件に一致するのは、解決できていない場合だけである
// （解決できていて全件に値が入っているなら、空の件数は0になる）。
//
// total: ボード上の item の全件数。
// withValue: `-no:"<status_field>"` の件数。
// withoutValue: `no:"<status_field>"` の件数。
// 戻り値: 絞り込みのキーとして使えていれば true。
// **全件が0（item が1件も無いボード）のときは判定できないので true を返す。**
func judgeFilterKeyUsable(total, withValue, withoutValue int) bool {
	if total <= 0 {
		return true
	}
	return !(withValue == total && withoutValue == total)
}

// quoteSearchKey は GitHub Projects の検索構文向けに、フィールド名を絞り込みのキーとして
// 書ける形（ダブルクオート囲み）にする。
//
// **シングルクオートではなくダブルクオートを使う。**値はどちらでも通るが、
// キーはダブルクオートでないと解決されない（`'status':'Ice Box'` は0件。
// 2026-08-20 に project #3 で実測）。
func quoteSearchKey(s string) string {
	return quoteSearchValue(s)
}

// quoteSearchValue は GitHub Projects の検索構文向けに値をダブルクオートで囲む。
// 値の中にダブルクオートが含まれる場合はバックスラッシュでエスケープする
// （Status の選択肢名にダブルクオートを使う運用は無いはずだが、含まれていても
// 壊れたクエリを送らないようにする）。
func quoteSearchValue(s string) string {
	escaped := strings.ReplaceAll(s, `"`, `\"`)
	return `"` + escaped + `"`
}

// foldStatus は Status 名の比較用に、前後の空白を落として小文字にする
// （SPEC.md 11.3: "Compare states after trimming surrounding whitespace and applying
// lowercase."）。**表示用の値はこの関数を通さず、GitHub の綴りをそのまま使う**（3-13）。
func foldStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// actorTypeBot は、自動化が起こしたイベントの `actor.__typename` である（設計 2-6）。
//
// **組み込みの自動化（`Pull request linked to issue` など）は `Bot` になる。**
// **continuo 自身の書き込みは `User` になる**（`gh auth token` の持ち主として書くため）。
// 人間も `User` なので、continuo は自分の書き込みを自動化と取り違えない。
const actorTypeBot = "Bot"

// statusAuthor は「いまの Status を書いたのは誰か」の判定結果である（設計 3-54）。
type statusAuthor struct {
	// Automated は、書いたのがボードの自動化（`Bot`）だったかどうかである。
	// **イベントが1件も引けなければ false である**（分からないなら「自動化ではない」に倒す）。
	Automated bool
	// Login は書いた主体のログイン名である。ログと issue のコメントに出すためだけに持つ。
	// 取れなければ空文字。
	Login string
}

// judgeStatusAuthor は timeline のイベントから「いまの Status を書いたのは誰か」を決める
// （設計 2-6 / 3-54）。
//
// **自分のボードのイベントだけを見る。**1つの issue が複数のボードに載っていると、
// 他のボードのイベントが同じ配列で返る（設計 2-6 の実測）。**絞り込まないと、
// 別のボードで自動化が動いただけで「自動化が動かした」と判定してしまう。**
//
// **いまの Status と一致する、いちばん新しいイベントを採る。**古いイベントを採ると、
// そのあと誰かが上書きしても、最初の書き手を指し続ける。
//
// **判定は「`actor.__typename` が `Bot`、または `wasAutomated` が真」である。**
// `wasAutomated` は組み込みの自動化でも `false` を返すので、これ単独では使えない（設計 2-6）。
//
// items: timelineItems の応答（nil なら判定しない）。
// state: いまの Status 名（大文字小文字と前後の空白は無視して照合する）。
// projectNumber: 自分のボードの番号。**0 以下なら絞り込まない**（テストの都合で番号を
// 持たない偽サーバに合わせるためではなく、番号が未設定のアダプタを作れないので実際には起きない）。
// 戻り値: 判定結果。一致するイベントが1件も無ければゼロ値。
func judgeStatusAuthor(items *rawTimelineItems, state string, projectNumber int) statusAuthor {
	if items == nil {
		return statusAuthor{}
	}
	folded := foldStatus(state)
	var latest *rawStatusChangedEvent
	for i := range items.Nodes {
		ev := &items.Nodes[i]
		if projectNumber > 0 && (ev.Project == nil || ev.Project.Number != projectNumber) {
			continue
		}
		if foldStatus(ev.Status) != folded {
			continue
		}
		if latest == nil {
			latest = ev
			continue
		}
		// **時刻が取れないイベントは、あとから来たものを新しいとみなす。**
		// `timelineItems(last:)` は古い順に返るので、並びの後ろほど新しい。
		if latest.CreatedAt == nil || ev.CreatedAt == nil || !ev.CreatedAt.Before(*latest.CreatedAt) {
			latest = ev
		}
	}
	if latest == nil {
		return statusAuthor{}
	}
	author := statusAuthor{Automated: latest.WasAutomated}
	if latest.Actor != nil {
		author.Login = latest.Actor.Login
		if latest.Actor.Typename == actorTypeBot {
			author.Automated = true
		}
	}
	return author
}

// containsFoldedStatus は states の中に target と（foldStatus で比較して）一致するものが
// あるかどうかを判定する。
func containsFoldedStatus(states []string, target string) bool {
	folded := foldStatus(target)
	for _, s := range states {
		if foldStatus(s) == folded {
			return true
		}
	}
	return false
}

// linkedBranchForBase は「作業の base に使ってよいリンクされた branch」を1本だけ返す
// （設計 3-22d）。**使ってよい形でなければ nil を返す。**推測で1本目を選ばない。
//
// **使ってよいのは、次を全部満たすときだけである。**
//
//   - リンクがちょうど1本であること（`totalCount` が 1）。**2本以上は、どれを選ぶか
//     決められない。**1本目を勝手に採ると、人間が後から足したリンクで base が変わる
//   - その1本が issue と同じリポジトリの branch であること。**別のリポジトリを指す
//     リンクは無視する。**issue のリポジトリの clone には `origin/<その名前>` が
//     存在しないので、base に据えると `git worktree add` が必ず落ちる
//   - branch の名前が空でないこと
//
// **`totalCount` を見る理由。**`nodes` は `first: 5` の窓なので、6本目が別のリポジトリを
// 指していても `nodes` には出ない。**件数を `len(nodes)` で数えると、窓の外を見落とす。**
//
// **捨てたときは理由も返す。**捨てた事実がどこにも出ないと、別のリポジトリの branch を
// リンクした人は「リンクしたのに既定 branch から始まった」を、手掛かりが1つも無いまま
// 見ることになる。**呼び出し側（adapter.go）がこの理由をログへ出す。**
// 症状と対処は [docs/FAQ.md](../../docs/FAQ.md) にも載せてある。
//
// conn: GraphQL が返した linkedBranches の connection（nil でよい）。
// issueNameWithOwner: issue が在るリポジトリの `<owner>/<repo>`。
// 戻り値の1つ目: base に使ってよい branch の名前。使ってよい形でなければ nil。
// 戻り値の2つ目: リンクが在るのに捨てたときの理由（人間可読）。
// **リンクがそもそも無いときは空文字である**（捨てていないので出すことが無い）。
func linkedBranchForBase(conn *rawLinkedBranchConn, issueNameWithOwner string) (*string, string) {
	if conn == nil || conn.TotalCount == 0 {
		return nil, ""
	}
	if conn.TotalCount != 1 {
		return nil, fmt.Sprintf(
			"リンクされた branch が %d 本あるので、どれを起点にするか決められません"+
				"（1本だけリンクしてください）", conn.TotalCount)
	}
	if len(conn.Nodes) != 1 {
		// **`totalCount` は 1 なのに窓が返らなかった。**権限で見えない branch などが
		// これに当たる。**「2本以上ある」と同じ文面にしてはならない。**直し方が違う。
		return nil, fmt.Sprintf(
			"リンクされた branch が1本あることになっていますが、その中身が返りませんでした"+
				"（返ってきた件数: %d）", len(conn.Nodes))
	}
	ref := conn.Nodes[0].Ref
	if ref == nil || ref.Name == "" {
		return nil, "リンクされた branch の名前が空です（provider 側の異常）"
	}
	// **リポジトリ名が取れなかったときも無視する。**「同じである」と言い切れない以上、
	// 今までどおり（既定 branch を base にする）へ倒すほうが安全である。
	if ref.Repository == nil {
		return nil, "リンクされた branch がどのリポジトリのものか分からないので起点に使いません"
	}
	if ref.Repository.NameWithOwner != issueNameWithOwner {
		return nil, fmt.Sprintf(
			"リンクされた branch が別のリポジトリ（%s）のものなので起点に使いません"+
				"（issue は %s に在ります）",
			ref.Repository.NameWithOwner, issueNameWithOwner)
	}
	name := ref.Name
	return &name, ""
}

// mapAssignees は GraphQL の担当者の connection を、正規化した形へ移す（設計 3-77b）。
//
// **`native_ref` の `assignee_login` は先頭の1人のままにする。**既にそこを読んでいる
// 経路（プロンプトのテンプレート）があり、**意味を変えると出力が黙って変わる。**
//
// conn: GraphQL が返した担当者の connection（nil でよい）。
// nativeRef: 先頭の担当者のログイン名を書き込む先。
// 戻り値の1つ目: 先頭の担当者のノード ID（いなければ nil）。
// 戻り値の2つ目: 担当者の全員（いなければ空）。
// 戻り値の3つ目: 担当者の人数（`totalCount`。返らなければ返ってきた件数）。
func mapAssignees(conn *rawUserConn, nativeRef map[string]any) (*string, []Assignee, int) {
	if conn == nil || len(conn.Nodes) == 0 {
		// **`totalCount` だけが返って node が空、ということは起きうる**（窓が 0 のとき）。
		// **人数は落とさない。**落とすと「担当者がいない」と読まれて入札してしまう。
		if conn != nil {
			return nil, nil, conn.TotalCount
		}
		return nil, nil, 0
	}
	assignees := make([]Assignee, 0, len(conn.Nodes))
	for _, n := range conn.Nodes {
		assignees = append(assignees, Assignee{ID: n.ID, Login: n.Login})
	}
	count := conn.TotalCount
	if count < len(assignees) {
		// **`totalCount` を要求していない古い応答**（テストの偽サーバを含む）では 0 が入る。
		// **返ってきた件数のほうが確かなので、そちらを採る。**
		count = len(assignees)
	}
	id := assignees[0].ID
	nativeRef["assignee_login"] = assignees[0].Login
	return &id, assignees, count
}

// ===== 正規化（設計 3-13 / SPEC.md 11.3） =====

// normalizeLabels はラベルの一覧を正規化する。
// 前後の空白を落として小文字にし、空のラベルは捨て、重複は取り除く（3-13 / SPEC.md 11.3）。
// 順序は最初に現れた順を保つ（決定的な出力にするため、map の反復順に頼らない）。
func normalizeLabels(names []string) []string {
	seen := make(map[string]bool, len(names))
	result := make([]string, 0, len(names))
	for _, raw := range names {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		if normalized == "" {
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

// mapItemResult は mapRawItemToIssue の結果である。
type mapItemResult struct {
	Issue Issue
	// Ok が false のとき、Issue は無効である。呼び出し側は Reason をログまたはエラーに使う。
	// state-list 呼び出し（fetch_issues_by_states）はこの結果を「省いてログに残す」
	// （SHOULD）。ID 指定の呼び出し（fetch_issues_by_ids）は「エラーにする」（MUST）
	// （SPEC.md 11.1 / 設計 3-13）。この使い分けは呼び出し側（adapter.go）が行う。
	Ok bool
	// Gone は「item は provider 側に存在するが、continuo から見える範囲にはもう無い」ことを
	// 示す。**Ok が false でも Gone が true のものは、ID 指定の取り直しでもエラーにせず省く。**
	// 「もう見えない」と「壊れている」は意味が違い、前者の省略は SPEC.md 11.1 が
	// 明示的に許している（"IDs no longer visible in the configured scope are omitted"）。
	//
	// これに当たるのは、**ボードとしては正常なのに continuo の候補の集合に入らない**3つである。
	//
	//	archive 済み            … 候補の取得（items）は最初から返さない
	//	Status が未設定          … 候補の取得は Status で絞るので返らない（104件中4件が該当）
	//	Issue でも DraftIssue でもない content … PullRequest 等。候補の取得でも省いている
	//
	// **provider 側の異常（content が空・Issue なのに repository が無い・
	// nameWithOwner の形が壊れている）は Gone にしない。**そちらは SPEC.md 11.1 が
	// エラーを要求する malformed である。
	Gone bool
	// Reason は Ok が false のときの理由（人間可読）である。
	Reason string
	// NotDispatchableReason は Ok が true かつ Issue.Dispatchable が false のときに、
	// なぜ dispatch できないかを人間可読で示す（設計 3-13: 「ログに残す」）。
	NotDispatchableReason string
	// LinkedBranchIgnoredReason は、issue に branch がリンクされているのに、それを
	// 起点として使わなかったときの理由である（設計 3-22d。linkedBranchForBase が決める）。
	//
	// **dispatch は止めない。**既定 branch へ倒して作業は進む。
	// **だが黙って倒すと、リンクした人は手掛かりを1つも持てない。**だから理由を運ぶ。
	// 空文字なら「リンクが無い」か「リンクをそのまま採った」のどちらかである。
	LinkedBranchIgnoredReason string
}

// mapRawItemToIssue は GraphQL から返ってきた1件の project item を、正規化した Issue へ
// 変換する。
//
// raw: 変換対象。nil を渡してはならない（「見つからない ID」の扱いは呼び出し側が
// raw そのものが nil かどうかで判定する。「もう見えない」ことと「正規化できない」ことは
// 別の意味を持つため、この関数の責務にしない）。
// statusFieldName: エラーメッセージ用（tracker.provider.status_field の値）。
// repoTrusted: `<owner>/<repo>` が Claude Code に信頼登録されているかを判定する関数
// （設計 3-13 の「リポジトリが信頼済み」）。nil なら全て信頼済みとして扱う。
// projectNumber: 自分のボードの番号（timeline のイベントを自分のボードへ絞るのに使う。設計 3-54）。
// 戻り値: Ok が true なら Issue が有効。false なら Reason に理由（人間可読）が入る。
// **Gone が true のものは「壊れている」ではなく「候補の集合にもう居ない」である**
// （呼び出し側は ID 指定の取り直しでもエラーにせず省く）。
//   - archive 済みの item（Gone）
//   - Status が未設定（fieldValueByName が nil、または name が空。Gone）
//     （3-13: 「Status 未設定の item」。候補の取得は Status で絞るので最初から返らない）
//   - Issue でも DraftIssue でもない型（PullRequest 等。Gone）
//   - content が無い（provider 側の異常。Gone にしない）
//   - Issue 型なのに repository が無い、または nameWithOwner の形が不正
//     （provider 側の異常。Gone にしない）
func mapRawItemToIssue(
	raw *rawItem, statusFieldName string, repoTrusted RepoTrustFunc, projectNumber int,
) mapItemResult {
	if raw.IsArchived {
		// archive 済みの item はボード上でもう見えない。候補の取得は `items(...)` の既定
		// （archivedStates: [NOT_ARCHIVED]）で最初から返らないが、ID 指定の取り直しには
		// その既定が効かないため、ここで弾く。**「まだ作業中の状態にある」と誤認しないため。**
		return mapItemResult{
			Ok:     false,
			Gone:   true,
			Reason: "item が archive 済みです（カンバン上ではもう見えません）",
		}
	}
	if raw.Content == nil {
		return mapItemResult{Ok: false, Reason: "content が空です（provider 側の異常）"}
	}
	if raw.FieldValueByName == nil || strings.TrimSpace(raw.FieldValueByName.Name) == "" {
		// **Status 未設定は「壊れている」ではなく「候補の集合にもう居ない」である。**
		// 候補の取得（`items(...)`）は Status で絞るので、この item は最初から返らない。
		// 人間がボードの画面で Status を空にするのは異常な操作ではない（本番のボードでも
		// 104件中4件が未設定）。ここをエラーにすると、**1件が未設定なだけで
		// ID 指定の取り直しが丸ごと失敗し、同じ呼び出しに乗った他の run が全部巻き添えになる。**
		return mapItemResult{
			Ok:     false,
			Gone:   true,
			Reason: fmt.Sprintf("Status（%s）が未設定です（候補の一覧にも出てきません）", statusFieldName),
		}
	}

	state := raw.FieldValueByName.Name
	nativeRef := map[string]any{}

	switch raw.Content.Typename {
	case "Issue":
		if raw.Content.Repository == nil || raw.Content.Repository.NameWithOwner == "" {
			return mapItemResult{Ok: false, Reason: "Issue なのに repository が空です（provider 側の異常）"}
		}
		owner, repo, ok := strings.Cut(raw.Content.Repository.NameWithOwner, "/")
		if !ok {
			return mapItemResult{
				Ok:     false,
				Reason: fmt.Sprintf("repository.nameWithOwner の形が不正です: %q", raw.Content.Repository.NameWithOwner),
			}
		}

		identifier := fmt.Sprintf("%s/%s#%d", owner, repo, raw.Content.Number)

		var description *string
		if raw.Content.Body != "" {
			d := raw.Content.Body
			description = &d
		}

		var url *string
		if raw.Content.URL != "" {
			u := raw.Content.URL
			url = &u
		}

		branchName, linkedBranchIgnoredReason := linkedBranchForBase(
			raw.Content.LinkedBranches, raw.Content.Repository.NameWithOwner)

		assigneeID, assignees, assigneeCount := mapAssignees(raw.Content.Assignees, nativeRef)

		var labels []string
		if raw.Content.Labels != nil {
			names := make([]string, len(raw.Content.Labels.Nodes))
			for i, l := range raw.Content.Labels.Nodes {
				names[i] = l.Name
			}
			labels = normalizeLabels(names)
		}

		var blockedBy []BlockerRef
		if raw.Content.BlockedBy != nil {
			for _, b := range raw.Content.BlockedBy.Nodes {
				ref := BlockerRef{ID: b.ID, State: b.State}
				if b.Repository != nil && b.Repository.NameWithOwner != "" {
					ref.Identifier = fmt.Sprintf("%s#%d", b.Repository.NameWithOwner, b.Number)
				}
				blockedBy = append(blockedBy, ref)
			}
		}

		commentCount := 0
		if raw.Content.Comments != nil {
			commentCount = raw.Content.Comments.TotalCount
		}

		nativeRef["issue_node_id"] = raw.Content.ID
		nativeRef["content_type"] = "ISSUE"
		if raw.Content.State != "" {
			nativeRef["github_issue_state"] = raw.Content.State
		}
		if raw.Content.Repository.DefaultBranchRef != nil {
			nativeRef["default_branch"] = raw.Content.Repository.DefaultBranchRef.Name
		}

		// 設計 3-13: dispatchable は「draft issue でない・Status が設定済み・リポジトリが
		// 信頼済み」をすべて集約した1つの真偽値である。ここまで来た時点で前2つは満たしている
		// ので、残る信頼の判定をここで行う。**受け皿をここに置かないと、GitHub 固有の分岐が
		// orchestrator へ積み上がる。**
		dispatchable := true
		notDispatchableReason := ""
		if repoTrusted != nil && !repoTrusted(owner, repo) {
			dispatchable = false
			notDispatchableReason = fmt.Sprintf(
				"リポジトリ %s/%s が Claude Code に信頼登録されていません"+
					"（信頼していないフォルダでは hook が1つも動かず、turn 終了の検知が全滅する。設計 3-6 / 4-3）",
				owner, repo,
			)
		}

		// **いまの Status を書いたのが誰かを、同じ応答から読む**（設計 3-54）。
		// timeline を要求していないクエリ（候補の取得・識別子での照合）ではゼロ値になる。
		author := judgeStatusAuthor(raw.Content.TimelineItems, state, projectNumber)

		issue := Issue{
			ID:                        raw.ID,
			NativeRef:                 nativeRef,
			Identifier:                identifier,
			Title:                     raw.Content.Title,
			Description:               description,
			Priority:                  nil, // 設計 4-2: Priority を読まない。常に nil。
			State:                     state,
			StatusChangedByAutomation: author.Automated,
			StatusChangedBy:           author.Login,
			BranchName:                branchName,
			URL:                       url,
			AssigneeID:                assigneeID,
			Assignees:                 assignees,
			AssigneeCount:             assigneeCount,
			Labels:                    labels,
			BlockedBy:                 blockedBy,
			Dispatchable:              dispatchable,
			CreatedAt:                 raw.Content.CreatedAt,
			UpdatedAt:                 raw.Content.UpdatedAt,
			Owner:                     owner,
			Repo:                      repo,
			RepoIsPrivate:             raw.Content.Repository.IsPrivate,
			Number:                    raw.Content.Number,
			CommentCount:              commentCount,
		}
		return mapItemResult{
			Ok:                        true,
			Issue:                     issue,
			NotDispatchableReason:     notDispatchableReason,
			LinkedBranchIgnoredReason: linkedBranchIgnoredReason,
		}

	case "DraftIssue":
		// 設計 3-13: draft issue は dispatchable=false にして残す。取得の段では落とさない。
		// repository を持たないため、owner/repo/number は空のまま（3-13 が明記する制約）。
		identifier := "draft:" + raw.ID

		var description *string
		if raw.Content.Body != "" {
			d := raw.Content.Body
			description = &d
		}

		assigneeID, assignees, assigneeCount := mapAssignees(raw.Content.Assignees, nativeRef)

		nativeRef["issue_node_id"] = raw.Content.ID
		nativeRef["content_type"] = "DRAFT_ISSUE"

		issue := Issue{
			ID:            raw.ID,
			NativeRef:     nativeRef,
			Identifier:    identifier,
			Title:         raw.Content.Title,
			Description:   description,
			Priority:      nil,
			State:         state,
			BranchName:    nil,
			URL:           nil,
			AssigneeID:    assigneeID,
			Assignees:     assignees,
			AssigneeCount: assigneeCount,
			Labels:        nil,
			BlockedBy:     nil,
			Dispatchable:  false,
			CreatedAt:     raw.Content.CreatedAt,
			UpdatedAt:     raw.Content.UpdatedAt,
			Owner:         "",
			Repo:          "",
			Number:        0,
			CommentCount:  0,
		}
		return mapItemResult{
			Ok:                    true,
			Issue:                 issue,
			NotDispatchableReason: "draft issue はリポジトリを持たないので作業ディレクトリを決められません（設計 3-13）",
		}

	default:
		// **PullRequest 等は「壊れている」ではなく「候補の集合に居ない」である。**
		// 候補の取得でも同じ理由で省いている。ID 指定の取り直しでエラーにすると、
		// 人間が item の content を差し替えただけで、同じ呼び出しに乗った他の run が
		// 全部巻き添えになる。
		return mapItemResult{
			Ok:   false,
			Gone: true,
			Reason: fmt.Sprintf(
				"Issue でも DraftIssue でもない content です（%s）。dispatch できないため除外します",
				raw.Content.Typename),
		}
	}
}
