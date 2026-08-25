package trust

import (
	"fmt"
	"io"
	"strings"
)

// WriteRequirements は、信頼を登録すると何が効くようになるかを人間に見せる（3-33）。
//
// **これが信頼のダイアログの代わりである。**Claude Code のダイアログは
// 「このリポジトリの設定が何を要求しているか」を1リポジトリずつ人間に見せる仕組みであり、
// `continuo trust` はそれを一括で外す。**代わりに、外す前にここで同じものを見せる。**
//
// w: 出力先。
// r: Plan が返した結果。
// 戻り値: 書き出せなかった場合のエラー。
func WriteRequirements(w io.Writer, r *Report) error {
	var b strings.Builder

	b.WriteString("信頼を登録すると、次の設定が Claude Code に効くようになります。\n")
	b.WriteString("**そのリポジトリで動くエージェントが、ここに書かれた操作を確認なしで実行できます。**\n")
	b.WriteString(fmt.Sprintf("書き込む先: %s\n", r.ClaudeConfigPath))
	b.WriteString("\n")

	if len(r.Entries) == 0 {
		b.WriteString("WORKFLOW.md の trust.repositories が空です。登録する対象がありません。\n")
		b.WriteString("`continuo init` がボードから拾って並べます。要らない行を消してから、もう一度実行してください。\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	for _, e := range r.Entries {
		writeEntry(&b, e)
		b.WriteString("\n")
	}

	pending := r.Pending()
	problems := r.Problems()
	switch {
	case len(pending) == 0 && len(problems) == 0:
		b.WriteString("すべて信頼済みです。書き込むものはありません。\n")
	case len(pending) == 0:
		b.WriteString(fmt.Sprintf("書き込むものはありません（%d件は調べられませんでした）。\n", len(problems)))
	default:
		names := make([]string, 0, len(pending))
		for _, e := range pending {
			names = append(names, e.Repository)
		}
		b.WriteString(fmt.Sprintf("信頼を登録する対象は %d 件です: %s\n", len(pending), strings.Join(names, ", ")))
		if len(problems) > 0 {
			b.WriteString(fmt.Sprintf("ほかに %d 件は調べられませんでした（登録の対象から外します）。\n", len(problems)))
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// writeEntry は1つのリポジトリぶんの要求内容を書く。
//
// b: 書き足す先。
// e: 1つのリポジトリの調査結果。
func writeEntry(b *strings.Builder, e Entry) {
	switch {
	case e.Problem != "":
		b.WriteString(fmt.Sprintf("✗ %s\n", e.Repository))
		b.WriteString(fmt.Sprintf("    %s\n", e.Problem))
		return
	case e.Unconfirmed != "":
		// **要求内容の一覧はそのまま出す。**何を確かめられなかったのかは、そこにしか出ない。
		b.WriteString(fmt.Sprintf("✗ %s（要求内容を確かめられません。登録の対象から外します）\n", e.Repository))
	case e.Trusted:
		b.WriteString(fmt.Sprintf("✓ %s（既に信頼済み。触りません）\n", e.Repository))
	default:
		b.WriteString(fmt.Sprintf("! %s（未信頼。登録の対象）\n", e.Repository))
	}
	b.WriteString(fmt.Sprintf("    登録するパス: %s\n", e.TrustKey))

	req := e.Requirements
	switch {
	case req.SettingsUnreadable:
		// **「ありません」と書かない。**実在するファイルについて無いと書くと、
		// 読む人は「このリポジトリは何も要求していない」と読む。
		b.WriteString(fmt.Sprintf("    %s: あるが読めませんでした（中身を確かめていません）\n", settingsRelPath))
	case !req.SettingsFound:
		b.WriteString(fmt.Sprintf("    %s: ありません\n", settingsRelPath))
	default:
		b.WriteString(fmt.Sprintf("    %s:\n", settingsRelPath))
		writeList(b, "permissions.allow", req.Allow)
		writeList(b, "permissions.additionalDirectories", req.AdditionalDirectories)
		writeHooks(b, req.Hooks)
	}
	if req.MCPUnreadable {
		b.WriteString(fmt.Sprintf("    %s: あるが読めませんでした（中身を確かめていません）\n", mcpRelPath))
	} else if !req.MCPFound {
		b.WriteString(fmt.Sprintf("    %s: ありません\n", mcpRelPath))
	} else if len(req.MCPServers) == 0 {
		b.WriteString(fmt.Sprintf("    %s: MCP サーバーの記述がありません\n", mcpRelPath))
	} else {
		b.WriteString(fmt.Sprintf("    %s の MCP サーバー:\n", mcpRelPath))
		for _, s := range req.MCPServers {
			b.WriteString(fmt.Sprintf("      - %s  （%s）\n", s.Name, s.Summary))
		}
	}
	for _, n := range req.Notes {
		b.WriteString(fmt.Sprintf("    ! %s\n", n))
	}
	if e.Unconfirmed != "" {
		b.WriteString(fmt.Sprintf("    ✗ %s\n", e.Unconfirmed))
	}
}

// writeHooks は hooks に書かれたコマンドを、契機ごとに並べる。
//
// **`permissions` と同じ場所に、同じ形で並べる。**hooks は `permissions` を1つも持たない
// リポジトリでも任意のコマンドを走らせられるので、**見せないと「何も要求していません」に見える。**
//
// b: 書き足す先。
// hooks: 実行される文字列。
func writeHooks(b *strings.Builder, hooks []HookCommand) {
	if len(hooks) == 0 {
		b.WriteString("      hooks: なし\n")
		return
	}
	b.WriteString("      hooks（確認なしで実行されるコマンド）:\n")
	for _, h := range hooks {
		b.WriteString(fmt.Sprintf("        - %s: %s\n", h.Event, h.Command))
	}
}

// writeList は項目の並びを、空でも意味が分かる形で書く。
//
// b: 書き足す先。
// label: 見出し。
// items: 並べる項目。
func writeList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		b.WriteString(fmt.Sprintf("      %s: なし\n", label))
		return
	}
	b.WriteString(fmt.Sprintf("      %s:\n", label))
	for _, it := range items {
		b.WriteString(fmt.Sprintf("        - %s\n", it))
	}
}

// WriteApplyResult は Apply が何をしたかを人間に見せる。
//
// w: 出力先。
// res: Apply が返した結果。
// 戻り値: 書き出せなかった場合のエラー。
func WriteApplyResult(w io.Writer, res *ApplyResult) error {
	var b strings.Builder

	if len(res.Changed) == 0 {
		b.WriteString(fmt.Sprintf("%s は書き換えていません（変えるものがありませんでした）。\n", res.ClaudeConfigPath))
		if len(res.Skipped) > 0 {
			b.WriteString(fmt.Sprintf("既に信頼済み: %s\n", strings.Join(repoNames(res.Skipped), ", ")))
		}
		_, err := io.WriteString(w, b.String())
		return err
	}

	b.WriteString(fmt.Sprintf("バックアップを取りました: %s\n", res.BackupPath))
	b.WriteString("**このバックアップは消しません。要らなくなったら人間が消してください。**\n")
	b.WriteString(fmt.Sprintf("%s に信頼を登録しました。\n", res.ClaudeConfigPath))
	for _, c := range res.Changed {
		b.WriteString(fmt.Sprintf("  ✓ %s → %s\n", c.Repository, c.TrustKey))
	}
	if len(res.Skipped) > 0 {
		b.WriteString(fmt.Sprintf("既に信頼済みだったので触っていません: %s\n", strings.Join(repoNames(res.Skipped), ", ")))
	}
	if len(res.VerifyProblems) > 0 {
		b.WriteString("\n書き込んだあとの確認で問題が出ました。\n")
		for _, p := range res.VerifyProblems {
			b.WriteString(fmt.Sprintf("  ✗ %s\n", p))
		}
		b.WriteString(fmt.Sprintf("元に戻すなら %s を書き戻してください。\n", res.BackupPath))
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// repoNames は Change の並びからリポジトリ名だけを取り出す。
//
// changes: 対象。
// 戻り値: "owner/repo" の並び。
func repoNames(changes []Change) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Repository)
	}
	return out
}
