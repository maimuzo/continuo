package orchestrator

import (
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/agentplugin"
	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/tracker"
)

// systemPromptFilePerm は system-prompt.md の権限である（issue ごとの設定ファイルと同じ 0600）。
const systemPromptFilePerm = settingsFilePerm

// prepareClaudeLaunch は、Claude Code を起動する前に毎回、plugin とシステムプロンプトを用意する（issue #288）。
//
// **呼ぶのは `claudeStartArgs` を使う3か所（着手と再着手・書かせ直しの復元・direct chat）の起動の直前である。**
// statusline 取得のための起動では呼ばない（その起動には plugin もシステムプロンプトも渡さない）。
//
//  1. plugin を `<実行時ディレクトリ>/plugins/<12桁>/continuo-agent-procedures/` へ書き出す
//     （ファイルごとに期待値と比べ、違えば差し替える。`agentplugin.Install`）
//  2. 古い版と書きかけの残りを片付ける（`agentplugin.Cleanup`。**失敗しても起動は止めない**）
//  3. その issue のシステムプロンプトを `issueDir` の system-prompt.md へ書く（一時ファイルから rename）
//
// **system-prompt.md の置き場所は、身元ファイルの値（エージェントが書き換えられる）からは決めない。**
// continuo が持つ issue ごとのディレクトリ（`issueDir`）から決める。
//
// issue: 起動する issue。
// 戻り値: 用意できなかった理由。**呼び出し側は起動しない**（`--append-system-prompt-file` に無いファイルを渡すと
// Claude Code が起動に失敗する）。
func (o *Orchestrator) prepareClaudeLaunch(issue tracker.Issue) error {
	if !filepath.IsAbs(o.runtimeDir) {
		return i18n.Errorf(i18n.KeyOrchestratorPrepareLaunchRuntimeDirUnknown, o.runtimeDir)
	}
	now := o.now()
	pluginDir, err := agentplugin.Install(o.runtimeDir, now)
	if err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorPrepareLaunchPluginFailed, agentplugin.Hash(), err)
	}
	if err := o.writeSystemPrompt(issue, pluginDir); err != nil {
		return err
	}
	// **片付けはシステムプロンプトを書いたあとに回す。**いま書いた版を「どの system-prompt.md にも載っていない」と
	// 読むことは無い（いまの版は片付けの対象に入らない）が、順を固定しておく。
	if err := agentplugin.Cleanup(o.runtimeDir, now); err != nil {
		o.logger.Warn("古い plugin の片付けに失敗しました（起動は続けます）",
			"identifier", issue.Identifier, "error", err)
	}
	return nil
}

// writeSystemPrompt は、その issue のシステムプロンプトを組み立てて system-prompt.md へ書く（issue #288）。
//
// **展開する変数は最初のプロンプトと同じ `prompt.RenderData` で組み立てる。**`continuo prompt --show --url` も
// 同じ関数を呼ぶので、見せる文面と書く文面がずれない。**試行回数は渡さない**（システムプロンプトは使わない）。
//
// **その場で空にしてから書かない**（CLAUDE.md の「ファイルの書き換えは一時ファイルへ書いてから差し替える」）。
// Claude Code が起動のときに読むファイルなので、書きかけの中身を読ませない。
//
// issue: 起動する issue。
// pluginDir: 書き出した plugin のディレクトリ。
// 戻り値: 組み立てられなかったか書けなかった理由。
func (o *Orchestrator) writeSystemPrompt(issue tracker.Issue, pluginDir string) error {
	data := prompt.RenderData(issue, nil, o.cfg.Tracker.Provider.Handoff.ProgressIntervalMs,
		o.cfg.RateLimit, o.socketPath)
	text, err := o.promptFragments.RenderSystem(data, prompt.Plugin{Dir: pluginDir, Hash: agentplugin.Hash()})
	if err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorPrepareLaunchSystemPromptRenderFailed, err)
	}
	path := o.systemPromptPath(issue.Identifier)
	if err := os.MkdirAll(filepath.Dir(path), settingsDirPerm); err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorPrepareLaunchSystemPromptWriteFailed, filepath.Dir(path), err)
	}
	if err := atomicfile.Write(path, []byte(text), systemPromptFilePerm); err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorPrepareLaunchSystemPromptWriteFailed, path, err)
	}
	return nil
}

// systemPromptPath は、その issue の system-prompt.md のパスを返す。
//
// identifier: issue の識別子。
// 戻り値: `<実行時ディレクトリ>/issues/<issue のスラグ>/system-prompt.md`。
func (o *Orchestrator) systemPromptPath(identifier string) string {
	return filepath.Join(o.issueDir(identifier), agentplugin.SystemPromptFileName)
}
