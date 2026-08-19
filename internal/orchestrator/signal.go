package orchestrator

import (
	"regexp"
	"strings"
)

// targetPattern は表明の行に書かれた対象の issue を切り出す正規表現である（設計 3-26）。
//
//	CONTINUO-STATUS: review              対象なし（いま作業している issue）
//	CONTINUO-STATUS: #45 review          代表の issue と同じリポジトリの #45
//	CONTINUO-STATUS: maimuzo/repo#47 blocked   別リポジトリを明示した形
var targetPattern = regexp.MustCompile(`^(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#(\d+)$`)

// ParseSignals は集めた text から表明を拾う（設計 3-25 の段6・段7、および 3-26）。
//
// **行に割って探すのが要点である。**印が他の文と同じブロックに入ることがある
// （実例: `3つとも完了しました…\n\nCONTINUO-STATUS: review`）。ブロックの一致では取れない。
//
// **印が複数あれば、issue ごとに最後に現れたものを採る**（設計 3-25）。
//
// **対象の解決に失敗した行は、対象なしの行として扱う。**先頭の語が
// `#<番号>` にも `<owner>/<repo>#<番号>` にも当てはまらなければ、その語を**表明の値**と
// みなし、対象は `currentIdentifier` にする。**空文字のキーは作らない。**
//
// **解決できた対象は、ボードに載っているかどうかを見ずにそのまま識別子として返す。**
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
			idx := strings.Index(line, prefix)
			if idx < 0 {
				continue
			}
			rest := strings.TrimSpace(line[idx+len(prefix):])
			if rest == "" {
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
