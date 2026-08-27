package tracker

import (
	"context"
	"fmt"
	"strings"
)

// allItemsQueryTemplate は FetchIssueByIdentifier が使うクエリである。
//
// **Status で絞らない**（設計 3-25）。グループの他の issue は `Ice Box` に置かれるので、
// `active_states` で絞ると表明が1件も反映されない。
// `items(query:)` の検索構文で識別子だけを絞る書き方が確認できていないため、
// ボードを丸ごと読んで照合する。
//
// **費用は「2リクエスト・計 8 point」である**（104件のボード。設計 3-31 の式
// `cost = (1 + 親の件数 × ネストした connection の本数) ÷ 100`）。
// このクエリは itemFieldsFragment を埋め込んでおり、その下に labels / assignees /
// blockedBy / linkedBranches の**4本**のネストした connection を持つので
// 1リクエスト 4 point、104件は `items(first: 100)` で切れて2ページ目が要る。
var allItemsQueryTemplate = `
query($login: String!, $number: Int!, $statusField: String!, $after: String) {
  repositoryOwner(login: $login) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        items(first: 100, after: $after, orderBy: { field: POSITION, direction: ASC }) {
          pageInfo { hasNextPage endCursor }
          nodes {` + itemFieldsFragment + `
          }
        }
      }
    }
  }
}
`

// FetchIssueByIdentifier は `<owner>/<repo>#<番号>` の形の識別子で issue を1件引く
// （設計 3-25 の「対象を書いた行は、識別子から project item の ID を引く」）。
//
// **エージェントの表明に対象付きの行（`CONTINUO-STATUS: #45 review`）があったときだけ
// 呼ぶ。**巡回では呼ばない。その issue は `Ice Box` に置かれているので、巡回で読んだ
// 候補（`active_states` で絞ってある）には入っていないためである。
//
// **「見つからない」をエラーで表さない。**エージェントが存在しない issue 番号を書くことは
// ありうるので、それをエラーにしない。エラーは通信の失敗と権限の不足だけに使う。
//
// ctx: 呼び出しに適用するコンテキスト。
// identifier: `<owner>/<repo>#<番号>` の形の識別子。大文字小文字は無視して照合する。
// 戻り値の1つ目: 見つかった Issue。見つからなければゼロ値。
// 戻り値の2つ目: ボードに載っていれば true。載っていなければ false。
// 戻り値の3つ目: GraphQL 呼び出しが失敗した場合、または project が見つからない場合のエラー。
func (a *Adapter) FetchIssueByIdentifier(ctx context.Context, identifier string) (Issue, bool, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return Issue{}, false, nil
	}

	after := ""
	for page := 1; ; page++ {
		if page > maxItemPages {
			return Issue{}, false, &Error{
				Category: CategoryPagination,
				Message: fmt.Sprintf(
					"ボードのページ数が上限 %d を超えました（1ページ100件。ボードが想定外に育っています。"+
						"1件の表明ごとにこれだけ読むのは GitHub の API 枠に見合いません）",
					maxItemPages,
				),
			}
		}
		var resp candidateQueryResponse
		vars := map[string]any{
			"login":       a.owner,
			"number":      a.projectNumber,
			"statusField": a.statusField,
		}
		if after != "" {
			vars["after"] = after
		} else {
			vars["after"] = nil
		}

		if err := a.gql.do(ctx, allItemsQueryTemplate, vars, &resp); err != nil {
			return Issue{}, false, err
		}
		if resp.RepositoryOwner == nil || resp.RepositoryOwner.ProjectV2 == nil {
			return Issue{}, false, &Error{
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
			// **信頼の判定関数をここでは渡さない。**判定は ghq と git を毎回起動して
			// `~/.claude.json` を読み直すので（約56ミリ秒／件）、識別子が一致するか見る前に
			// 全件へ掛けると、表明1行あたりボード104件ぶん・外部プロセス208回になる。
			// 一致した1件にだけ掛け直す（下）。
			mapped := mapRawItemToIssue(raw, a.statusField, nil, a.projectNumber)
			if !mapped.Ok {
				continue
			}
			if strings.EqualFold(mapped.Issue.Identifier, target) {
				// 一致した1件だけ、信頼の判定を入れて作り直す（Dispatchable を正しく埋める）。
				resolved := mapRawItemToIssue(raw, a.statusField, a.repoTrusted, a.projectNumber)
				if !resolved.Ok {
					return mapped.Issue, true, nil
				}
				return resolved.Issue, true, nil
			}
		}

		if !conn.PageInfo.HasNextPage {
			break
		}
		if conn.PageInfo.EndCursor == "" {
			return Issue{}, false, &Error{
				Category: CategoryPagination,
				Message:  "hasNextPage が真なのに endCursor が空です（provider 側の異常）",
			}
		}
		after = conn.PageInfo.EndCursor
	}

	a.logger.Info("識別子で引いた issue はボードに載っていません", "identifier", target)
	return Issue{}, false, nil
}
