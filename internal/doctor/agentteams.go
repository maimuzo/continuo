package doctor

import (
	"strconv"

	"github.com/maimuzo/continuo/internal/i18n"
)

// EnvAgentTeams は Claude Code の agent teams を切り替える環境変数の名前である
// （設計 3-70。issue #137）。
//
// **公式が意味を決めているのは `1` と `0` の2つだけである。**
//
//	1 … agent teams が有効になる（名前つきの Agent が teammate として起動する）
//	0 … 無効になる（名前つきの Agent はサブエージェントとして起動する）
//
// 出典: Orchestrate teams of Claude Code sessions（2026-09-01 取得。設計 3-70 が引用している）。
const EnvAgentTeams = "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"

// agentTeamsOn / agentTeamsOff は EnvAgentTeams の、公式が意味を決めている2つの値である。
const (
	agentTeamsOn  = "1"
	agentTeamsOff = "0"
)

// checkAgentTeams は Claude Code の agent teams が有効にならないかを見る
// （見出し語 `agent teams`。設計 3-70。issue #137）。
//
// **何が起きるのか。**agent teams が有効な環境では、名前つきの `Agent` ツールの呼び出しが
// **teammate として起動する。**teammate が許可を求めると、確認の画面はリードの pane に出る。
// **continuo はそれを `blocked` と読み、esc を送って pane を閉じ、issue を
// `tracker.failure_state` へ落とす。**
//
// **continuo は agent teams に対応しない**（設計 3-70）。**切る仕組みも持たない。**
// **いま文書に「有効だと正しく動きません」と書いてあるだけなので、読まなかった人は
// 落ちてから初めて気づく。**この見出し語が、起動する前に気づく場所である。
//
// **読める出どころは2つだけである。**
//
//	`claude.env`                 … continuo が `--settings` で渡す設定ファイルの `env`（設計 3-12）。
//	                               **continuo が Claude Code へ環境変数を渡す経路は、ここ1本しかない**
//	doctor を叩いたシェルの環境変数 … `Options.LookupEnv`
//
// **読まないものが5つある。**組織の managed settings（OS ごとに場所が違う）、
// 対象リポジトリの `.claude/settings.json` と `.claude/settings.local.json`
// （doctor が見るのは clone で、Claude Code が走るのは worktree。別の branch のことがある。
// 後者はそもそも gitignore される）、利用者の `~/.claude/settings.json`
// （**設計 3-12 が「利用者の `~/.claude/settings.json` は読み書きしない」と決めている**）、
// herdr の pane の環境（continuo は `claude` を直接起動しない）。
//
// **読んでいない出どころは、`✓` のときも必ず内訳に出す。**
// **読めた範囲だけを見て「有効になっていません」と断言してはならない。**
//
// **書かれていないことを警告しない。**設計 3-70 が
// 「**continuo がこれを既定で書き込むことはしない**（2026-08-28、人間の判断）。
// **無効なものを無効にする設定は、読む人を惑わせるだけである。**」と決めている。
// **`1` を見つけたときだけ知らせる。**
//
// **`0` と `1` 以外の値は、どちらの出どころでも `!` にする。**
// 公式が意味を決めていない値なので、有効になるかどうかを判定できない。
// **片方だけ厳しくすると、同じ設計の中に判定基準が2つできる。**
//
// **記号は `✗` ではなく `!` にする。**agent teams を自分の対話用に有効にしている人が
// 版を上げただけで continuo が起動しなくなる、ということを起こさない
// （見出し語 `片付けの状態` と `未記入の項目` と同じ理由である）。
//
// opts: 検査の入力（`LookupEnv` だけを使う）。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkAgentTeams(opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK || !cfg.OK {
		return Result{
			Label:  LabelAgentTeams,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorAgentTeamsConfigUnreadable),
			Notes:  []string{i18n.T(i18n.KeyDoctorAgentTeamsNoteUnread)},
		}
	}

	// **`claude.env` を先に見る。**`--settings` で渡すものは、利用者の設定にも
	// シェルの export にも勝つ（設計 3-70 が引く公式の2文）。
	if value, ok := cfg.Config.Claude.Env[EnvAgentTeams]; ok && value != "" {
		switch value {
		case agentTeamsOff:
			return Result{
				Label:  LabelAgentTeams,
				Symbol: SymbolOK,
				Detail: i18n.T(i18n.KeyDoctorAgentTeamsOff, EnvAgentTeams),
				Notes:  []string{i18n.T(i18n.KeyDoctorAgentTeamsNoteUnread)},
			}
		case agentTeamsOn:
			return agentTeamsProblem(i18n.T(i18n.KeyDoctorAgentTeamsOnSettings, EnvAgentTeams))
		default:
			return agentTeamsProblem(
				i18n.T(i18n.KeyDoctorAgentTeamsUnknownValue, EnvAgentTeams, strconv.Quote(value)))
		}
	}

	// **`claude.env` に書かれていない（または空文字である）。**
	// **空文字は「書かれていない」と同じに扱う。**`1` ではないので agent teams は
	// 有効にならず、シェル側も空文字を素通りさせている。
	// **片方だけ厳しくすると、同じ検査の中に判定基準が2つできる。**
	// このときだけ、doctor を叩いたシェルを見る。
	// **herdr の pane と同じ環境とは限らない**ので、文言でそう断る。
	//
	// **`0` と空文字以外は、値をそのまま出して `!` にする。**`claude.env` の側と
	// 同じものさしである（公式が意味を決めているのは `0` と `1` だけなので、
	// `true` のような値では有効になるかどうかを判定できない）。
	if value, ok := opts.LookupEnv(EnvAgentTeams); ok && value != "" && value != agentTeamsOff {
		return agentTeamsProblem(
			i18n.T(i18n.KeyDoctorAgentTeamsOnShell, EnvAgentTeams, strconv.Quote(value)))
	}

	return Result{
		Label:  LabelAgentTeams,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorAgentTeamsNotFound),
		Notes:  []string{i18n.T(i18n.KeyDoctorAgentTeamsNoteUnread)},
	}
}

// agentTeamsProblem は「有効になる（かもしれない）」ときの結果を組み立てる。
//
// **直し方と、切らないと何が起きるかを必ず添える。**見出し語だけでは、
// 読んだ人がなぜ困るのかを判断できない。
//
// detail: 記号の右に出す1行。
// 戻り値: 検査結果。
func agentTeamsProblem(detail string) Result {
	return Result{
		Label:  LabelAgentTeams,
		Symbol: SymbolUnknown,
		Detail: detail,
		Notes:  []string{i18n.T(i18n.KeyDoctorAgentTeamsNoteUnread)},
		Remedies: []string{
			i18n.T(i18n.KeyDoctorAgentTeamsRemedyOff, EnvAgentTeams),
			i18n.T(i18n.KeyDoctorAgentTeamsRemedyWhy),
		},
	}
}
