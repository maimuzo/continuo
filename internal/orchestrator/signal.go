package orchestrator

import (
	"regexp"
	"strings"
)

// targetPattern は表明の行に書かれた対象の issue を切り出す正規表現である（設計 3-26）。
//
//	CONTINUO-STATUS: review              対象なし（いま作業している issue）
//	CONTINUO-STATUS: #45 review          代表の issue と同じリポジトリの #45
//	CONTINUO-STATUS: octocat/hello-world#47 blocked   別リポジトリを明示した形
var targetPattern = regexp.MustCompile(`^(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#(\d+)$`)

// maxSignalsPerTurn は1つの turn で受け付ける表明の件数の上限である。
//
// **上限を置く理由。**表明1件につき `applySignals` がカンバンを全ページ走査する
// GraphQL 呼び出しを1回行い、カンバンに載っていなければコメントも書く。上限が無いと、
// エージェントは1回の応答に印を並べるだけで GitHub API を任意の回数だけ呼ばせられ、
// 枠を使い切ると**他の run のトラッカー操作まで巻き添えで失敗する。**
//
// **10件で足りる根拠。**表明の対象は「まとめて直したグループ」である（設計 3-26）。
// 1つのセッションでまとめて片付ける issue の件数がこれを超えるなら、グループの切り方が
// 大きすぎる。超えた分は捨てて警告を出す。
const maxSignalsPerTurn = 10

// maxSignalFieldBytes は表明の対象と値それぞれの長さの上限（バイト）である。
//
// **識別子は `<owner>/<repo>#<番号>` であり、値は `review` などの短い語である。**
// これを超える語は表明ではない。長い文字列をキーにした写像を持ち回らないために切る。
const maxSignalFieldBytes = 256

// ParseSignals は集めた text から表明を拾う（設計 3-25 の段6・段7、および 3-26）。
//
// **行に割って探すのが要点である。**印が他の文と同じブロックに入ることがある
// （実例: `3つとも完了しました…\n\nCONTINUO-STATUS: review`）。ブロックの一致では取れない。
//
// **印は行頭にあるものだけを拾う**（先頭の空白は許す）。行のどこにあっても拾うと、
// エージェントが issue の本文やコメントを**引用しただけ**で表明が成立する
// （設計 3-29 のとおりエージェントは `gh issue view` で issue を自分で読む）。
// 行き先は Status の変更であり、`terminal_states` に入れば worktree と branch の削除まで
// 進むので、**issue を立てられる人なら誰でも引ける経路になってしまう。**
//
// **1つの turn で受け付けるのは `maxSignalsPerTurn` 件までである。**超えた分は捨てて
// 呼び出し側が警告を出せるように、拾った件数は写像の大きさで分かる形にしてある。
//
// **印が複数あれば、issue ごとに最後に現れたものを採る**（設計 3-25）。
//
// **対象の解決に失敗した行は、対象なしの行として扱う。**先頭の語が
// `#<番号>` にも `<owner>/<repo>#<番号>` にも当てはまらなければ、その語を**表明の値**と
// みなし、対象は `currentIdentifier` にする。**空文字のキーは作らない。**
//
// **解決できた対象は、カンバンに載っているかどうかを見ずにそのまま識別子として返す。**
// 載っているかを引くのは呼び出し側（`applySignals` が `FetchIssueByIdentifier` で引く）
// であり、載っていなければその行を捨ててコメントに残す（設計 3-26）。
//
// texts: assistant の text ブロックの本文の並び（現れた順）。
// prefix: 表明の印（`tracker.status_signal_prefix`。例 `CONTINUO-STATUS:`）。
// currentIdentifier: いま作業している issue の識別子。**対象を書かない行はこれを指す。**
// 戻り値: 対象の識別子から表明の値への対応。
func ParseSignals(texts []string, prefix, currentIdentifier string) map[string]string {
	result := map[string]string{}
	if prefix == "" {
		return result
	}

	owner, repo := splitIdentifier(currentIdentifier)

	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimLeft(line, " \t\u3000")
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			rest := strings.TrimSpace(trimmed[len(prefix):])
			if rest == "" {
				continue
			}
			if len(rest) > maxSignalFieldBytes*2 {
				// 表明の行に収まる長さではない。
				continue
			}
			fields := strings.Fields(rest)

			target := currentIdentifier
			var value string
			switch {
			case len(fields) == 1:
				value = fields[0]
			default:
				resolved, ok := resolveSignalTarget(fields[0], owner, repo)
				if !ok {
					// 対象として解釈できない語が先頭に来た場合は、
					// 「対象なし」の行だと解釈して1語目を値として扱う。
					value = fields[0]
					break
				}
				target = resolved
				value = fields[1]
			}

			value = strings.TrimSpace(strings.Trim(value, ".。"))
			if value == "" {
				continue
			}
			if len(value) > maxSignalFieldBytes || len(target) > maxSignalFieldBytes {
				continue
			}
			if _, known := result[target]; !known && len(result) >= maxSignalsPerTurn {
				// 上限に達している。**新しい対象は増やさない**（既にある対象の
				// 上書きは段7 のとおり続ける）。
				continue
			}
			// 段7: 同じ issue に複数あれば、最後に現れたものが勝つ。
			result[target] = strings.ToLower(value)
		}
	}
	return result
}

// resolveSignalTarget は表明の行に書かれた対象を、`<owner>/<repo>#<番号>` の識別子へ直す
// （設計 3-26）。
//
// **`#<番号>` は代表の issue と同じリポジトリを指す。**別リポジトリを指すときは
// `<owner>/<repo>#<番号>` と書かせる。
//
// raw: 行に書かれた対象の文字列。
// owner: いま作業している issue の所有者名。
// repo: いま作業している issue のリポジトリ名。
// 戻り値の1つ目: 解決した識別子。
// 戻り値の2つ目: 対象として解釈できれば true。
func resolveSignalTarget(raw, owner, repo string) (string, bool) {
	m := targetPattern.FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1] + "#" + m[2], true
	}
	if owner == "" || repo == "" {
		return "", false
	}
	return owner + "/" + repo + "#" + m[2], true
}

// splitIdentifier は `<owner>/<repo>#<番号>` を owner と repo に割る。
//
// identifier: issue の識別子。
// 戻り値の1つ目: 所有者名。割れなければ空文字。
// 戻り値の2つ目: リポジトリ名。割れなければ空文字。
func splitIdentifier(identifier string) (string, string) {
	hash := strings.LastIndex(identifier, "#")
	if hash < 0 {
		return "", ""
	}
	slash := strings.Index(identifier[:hash], "/")
	if slash < 0 {
		return "", ""
	}
	return identifier[:slash], identifier[slash+1 : hash]
}
