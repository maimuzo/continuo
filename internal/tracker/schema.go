package tracker

import (
	"context"
)

// schemaSupportQuery は、接続先が continuo の問い合わせに要る要素を持っているかを訊く
// 問い合わせである（設計 3-86）。**スキーマを読むだけで、カンバンにもデータにも触らない。**
//
// **訊くのは4要素だけである。**continuo が送る問い合わせ13本（query.go の12本と
// by_identifier.go の1本）を GitHub Enterprise Server の公開スキーマ
// （`https://docs.github.com/public/ghes-<版>/schema.docs-enterprise.graphql`）に対して
// GraphQL の検証器で検証したところ、3.20・3.21・3.22 は13本とも誤りが0件で、
// 3.19 以下で足りない要素はこの4つだけだった（2026-10-03 に実測。3.16 以前は
// 公開スキーマが配られていないので確かめていない）。
//
// **問い合わせを変えたら、同じ検証をやり直すこと。**新しい要素を使うと、この一覧から漏れる。
// 漏れると、`continuo doctor` は「対応している」と言うのに、巡回が GraphQL の誤りで落ちる。
//
// **`fields` と `enumValues` に `includeDeprecated: true` を付ける。**付けないと、
// 要素が将来非推奨になったときに「無い」と読む。**`args` には付けない。**
// `args(includeDeprecated:)` は新しい仕様で、GitHub Enterprise Server が受け付けるかを
// 確かめられていない。付けて照会そのものが落ちると、動く接続先を「対応していない」と読む。
// だから `items` の `query` 引数が非推奨になった場合だけは、「無い」と読む。
const schemaSupportQuery = `
query {
  projectV2: __type(name: "ProjectV2") { fields(includeDeprecated: true) { name args { name } } }
  event: __type(name: "ProjectV2ItemStatusChangedEvent") { name }
  itemTypes: __type(name: "IssueTimelineItemsItemType") { enumValues(includeDeprecated: true) { name } }
  issue: __type(name: "Issue") { fields(includeDeprecated: true) { name } }
}
`

// スキーマの照会で「足りない」と報告する要素の名前である。**人間が読む文面へそのまま出す。**
const (
	// SchemaElementItemsQuery は `ProjectV2.items` の `query` 引数である。
	// 候補の取得と Bootstrap が、Status で絞るのに使う。
	SchemaElementItemsQuery = "ProjectV2.items(query:)"
	// SchemaElementStatusChangedEvent は型 `ProjectV2ItemStatusChangedEvent` である。
	// 「誰が Status を書いたか」の判定に使う。
	SchemaElementStatusChangedEvent = "ProjectV2ItemStatusChangedEvent"
	// SchemaElementStatusChangedEnum は enum `IssueTimelineItemsItemType` の値である。
	SchemaElementStatusChangedEnum = "IssueTimelineItemsItemType.PROJECT_V2_ITEM_STATUS_CHANGED_EVENT"
	// SchemaElementBlockedBy は `Issue.blockedBy` である。
	SchemaElementBlockedBy = "Issue.blockedBy"
)

// schemaNamed は照会の応答のうち、名前だけを持つ要素である。
type schemaNamed struct {
	Name string `json:"name"`
}

// schemaField は照会の応答の、フィールド1つである。
type schemaField struct {
	Name string        `json:"name"`
	Args []schemaNamed `json:"args"`
}

// schemaSupportResponse は schemaSupportQuery の応答である。
// **存在しない型は null で返る**（誤りにはならない。2026-10-03 に github.com で実測）ので、
// どれもポインタで受ける。
type schemaSupportResponse struct {
	ProjectV2 *struct {
		Fields []schemaField `json:"fields"`
	} `json:"projectV2"`
	Event     *schemaNamed `json:"event"`
	ItemTypes *struct {
		EnumValues []schemaNamed `json:"enumValues"`
	} `json:"itemTypes"`
	Issue *struct {
		Fields []schemaNamed `json:"fields"`
	} `json:"issue"`
}

// MissingSchemaElements は、接続先が continuo の問い合わせに要る要素を持っているかを訊き、
// 足りない要素の名前を返す（設計 3-86）。
//
// **`continuo doctor` のカンバンの検査が、接続先が github.com でないときだけ呼ぶ。**
// 常駐プロセスの起動時には呼ばない（人間の決定。issue #86）。
//
// **分かるのは「要る要素の名前がスキーマに在るか」までである。**在っても、
// その接続先で continuo が動くことまでは言えない（GitHub Enterprise の実機では確かめていない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: 足りない要素の名前（SchemaElement… の定数。決まった順）。全部在れば空。
// 戻り値の2つ目: 照会そのものが失敗した場合のエラー（通信の失敗・認証の失敗・レートリミット）。
func (a *Adapter) MissingSchemaElements(ctx context.Context) ([]string, error) {
	var resp schemaSupportResponse
	if err := a.gql.do(ctx, schemaSupportQuery, nil, &resp); err != nil {
		return nil, err
	}
	var missing []string
	if !hasItemsQueryArg(resp) {
		missing = append(missing, SchemaElementItemsQuery)
	}
	if resp.Event == nil {
		missing = append(missing, SchemaElementStatusChangedEvent)
	}
	if !hasStatusChangedEnum(resp) {
		missing = append(missing, SchemaElementStatusChangedEnum)
	}
	if !hasBlockedBy(resp) {
		missing = append(missing, SchemaElementBlockedBy)
	}
	return missing, nil
}

// hasItemsQueryArg は `ProjectV2.items` が `query` 引数を持つかを返す。
func hasItemsQueryArg(resp schemaSupportResponse) bool {
	if resp.ProjectV2 == nil {
		return false
	}
	for _, f := range resp.ProjectV2.Fields {
		if f.Name != "items" {
			continue
		}
		for _, arg := range f.Args {
			if arg.Name == "query" {
				return true
			}
		}
	}
	return false
}

// hasStatusChangedEnum は enum が `PROJECT_V2_ITEM_STATUS_CHANGED_EVENT` を持つかを返す。
func hasStatusChangedEnum(resp schemaSupportResponse) bool {
	if resp.ItemTypes == nil {
		return false
	}
	for _, v := range resp.ItemTypes.EnumValues {
		if v.Name == "PROJECT_V2_ITEM_STATUS_CHANGED_EVENT" {
			return true
		}
	}
	return false
}

// hasBlockedBy は `Issue` が `blockedBy` を持つかを返す。
func hasBlockedBy(resp schemaSupportResponse) bool {
	if resp.Issue == nil {
		return false
	}
	for _, f := range resp.Issue.Fields {
		if f.Name == "blockedBy" {
			return true
		}
	}
	return false
}
