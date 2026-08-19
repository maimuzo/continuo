package config

import (
	"fmt"
	"strings"
)

// frontMatterDelimiter は front matter の開始・終了を示す行である（設計 5-2 の例に合わせる）。
const frontMatterDelimiter = "---"

// splitFrontMatter は WORKFLOW.md の内容を front matter（YAML 文字列）と本文（プロンプトの
// テンプレート文字列）に分ける。ライブラリを使わず、標準の strings だけで実装する
// （設計「その2」が明示的に要求している。理由は SPEC.md 5.5 のエラー分類を自前の関数境界に
// 対応づけられるようにするため）。
//
// content: WORKFLOW.md をそのまま読み込んだ文字列。
// 戻り値: 1つ目が front matter の YAML 本体（区切り行を含まない）、2つ目が本文。
// 本文が無い（区切り行の直後でファイルが終わる）場合は空文字列を返す。エラーではない。
// エラー: 1行目が区切り行 "---" でない場合、または対応する終端の区切り行が
// 見つからない場合に、判定に使った行番号つきで返す。
//
// **制約。**終端は「開始行の次から現れる最初の、行頭から "---" だけの行」で確定する
// （行末の空白とタブは無視するが、行頭のインデントは無視しない）。したがって
// front matter の途中に行頭から "---" を書くと、そこで front matter が切れ、残りは本文になる。
// **ブロックスカラー（| や >）の中身は必ずインデントされるので、その中の "---" では切れない。**
// YAML 自身も行頭の "---" をドキュメントの区切りとして扱うため、この判定は YAML の解釈と
// 食い違わない。
func splitFrontMatter(content string) (frontMatter string, body string, err error) {
	// CRLF を LF に統一してから行に分割する。以後の行番号はこの正規化後の行を1始まりで数える。
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != frontMatterDelimiter {
		got := ""
		if len(lines) > 0 {
			got = lines[0]
		}
		return "", "", fmt.Errorf(
			"1行目が front matter の開始行 %q ではありません（1行目: %q）",
			frontMatterDelimiter, got,
		)
	}

	endLineIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == frontMatterDelimiter {
			endLineIndex = i
			break
		}
	}
	if endLineIndex == -1 {
		return "", "", fmt.Errorf(
			"front matter の終端行 %q が見つかりません（1行目（%q）に対応する終端が無い）",
			frontMatterDelimiter, frontMatterDelimiter,
		)
	}

	frontMatter = strings.Join(lines[1:endLineIndex], "\n")

	if endLineIndex+1 < len(lines) {
		body = strings.Join(lines[endLineIndex+1:], "\n")
	}

	return frontMatter, body, nil
}
