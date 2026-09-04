package orchestrator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// transcriptMaxLineBytes は transcript の1行として読み切る上限である。
// 1行が API 応答1件ぶんの JSON なので、大きめに取る。
//
// **超えた行は読み捨てて、残りの行の処理を続ける。**大きなファイルを読ませた turn の
// tool 結果が1行に入ると超えうるが、その1行のために turn 全体の表明を落としてはならない。
const transcriptMaxLineBytes = 16 << 20

// transcriptMaxRequestIDs はトークンの重複排除に覚えておく `requestId` の件数の上限である。
//
// **上限を置く理由。**transcript のファイルサイズには上限が無い（Claude Code が書く）。
// 件数ぶんの文字列を無条件に覚えると、長く走ったセッションや細工されたファイルで
// メモリが際限なく増える。**上限に達したら以後は重複排除をやめる**（件数が多少多く
// 出ることはあっても、ダッシュボードに出すだけの値であり判断には使わない。設計 3-15）。
const transcriptMaxRequestIDs = 200000

// transcriptLine は transcript の JSONL の1行である（設計 3-15 / 3-25）。
//
// **未知のキーは無視する。**Claude Code 側で項目が増えても落ちない。
type transcriptLine struct {
	// Type は行の種別である（"user" / "assistant" ほか）。
	Type string `json:"type"`
	// IsSidechain は subagent の会話であることを表す。
	// **表明は `false` に絞って拾う**（subagent の発言を印として拾わないため。設計 3-25）。
	IsSidechain bool `json:"isSidechain"`
	// PromptSource は user 行の出どころである。
	// **`"typed"` が turn の頭である**（設計 3-25）。`prompt_id` では区切らない。
	PromptSource string `json:"promptSource"`
	// PromptID は user 行の prompt_id である。Stop hook の prompt_id との照合に使う。
	PromptID string `json:"promptId"`
	// RequestID は API 応答の識別子である。**トークンの重複排除に使う**（設計 3-15）。
	RequestID string `json:"requestId"`
	// Message は assistant / user のメッセージ本体である。
	Message *transcriptMessage `json:"message"`
}

// transcriptMessage は transcript の行のメッセージ本体である。
type transcriptMessage struct {
	// Content はメッセージのブロックの並びである。
	// **文字列で入ることもある**ので json.RawMessage で受けて後から解く。
	Content json.RawMessage `json:"content"`
	// Usage は API 応答ごとのトークンの計上である（設計 3-15）。
	Usage *transcriptUsage `json:"usage"`
}

// transcriptContentBlock はメッセージのブロック1件である。
type transcriptContentBlock struct {
	// Type はブロックの種別である。表明を拾うのは "text" だけである。
	Type string `json:"type"`
	// Text は本文である。
	Text string `json:"text"`
}

// transcriptUsage は1回の API 応答のトークンの計上である（設計 3-15）。
type transcriptUsage struct {
	// InputTokens は入力のトークン数である。
	InputTokens int `json:"input_tokens"`
	// CacheCreationInputTokens はキャッシュ作成のトークン数である。
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens はキャッシュ読み出しのトークン数である。
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
	// OutputTokens は出力のトークン数である。
	OutputTokens int `json:"output_tokens"`
}

// TokenUsage は transcript から集計したトークンの合計である（設計 3-15）。
//
// **1回の集計が見ているのは transcript 1ファイルである。**セッションをまたいだ累計は
// `runState` が `Add` で足して持つ（transcript のファイル名はセッション UUID なので、
// 再 dispatch で UUID を採り直すと別のファイルになる）。
type TokenUsage struct {
	// APICalls は数えた API 応答の件数（`requestId` で重複排除したあと）である。
	APICalls int
	// Input は入力のトークンの合計である。
	Input int
	// CacheCreation はキャッシュ作成のトークンの合計である。
	CacheCreation int
	// CacheRead はキャッシュ読み出しのトークンの合計である。
	CacheRead int
	// Output は出力のトークンの合計である。
	Output int
}

// Add は2つの集計を足した新しい値を返す（レシーバは書き換えない）。
//
// **セッションをまたいだ累計を作るためのものである。**同じ transcript を2回足しては
// ならない（`requestId` の重複排除はファイル単位でしか効かない）。
//
// other: 足す集計。
// 戻り値: 項目ごとに足し合わせた集計。
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		APICalls:      u.APICalls + other.APICalls,
		Input:         u.Input + other.Input,
		CacheCreation: u.CacheCreation + other.CacheCreation,
		CacheRead:     u.CacheRead + other.CacheRead,
		Output:        u.Output + other.Output,
	}
}

// TranscriptReadResult は transcript を1回読んだ結果である。
//
// **表明とトークンの両方を、同じ1回の読み取りで取る**（設計 3-15。2回開かない）。
type TranscriptReadResult struct {
	// Signals は拾った表明である。キーは対象の issue の識別子
	// （対象を書かない行は空文字のキーになる）、値は `review` / `blocked` / `working` など。
	// **issue ごとに、最後に現れた1行を採る**（設計 3-25）。
	Signals map[string]string
	// Usage はトークンの合計である。
	Usage TokenUsage
}

// ReadTranscript は transcript の JSONL を読み、表明とトークンの両方を取る
// （設計 3-15 / 3-25）。
//
// 表明の拾い方は設計 3-25 のとおりである。
//
//  1. `type == "user"` かつ `promptId` が一致する行を探す（promptID が空なら飛ばす）
//  2. そこから前へ遡り、`type == "user"` かつ `promptSource == "typed"` の最初の行を見つける
//  3. 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする
//  4. 範囲内の `type == "assistant"` かつ `isSidechain == false` の行から
//     `message.content[]` の `type == "text"` を集める
//  5. 集めた text を**行に割って** prefix の行を拾う（ブロックの一致では取れない）
//
// **`prompt_id` で区切ってはならない**（17件中3件で取り逃した）。1つの人間の指示が
// transcript の中で複数の `prompt_id` に割れる。**`promptSource == "typed"` を起点に
// すれば 17件中17件で取れた。**
//
// トークンは設計 3-15 のとおり、**ファイル全体**の `type == "assistant"` の行を
// `requestId` で重複排除してから足す。
//
// **全行をメモリに載せない。**2回走査する形にしてある。
//
//	1回目  … 行の位置だけを覚えながら turn の範囲（バイト位置）を決め、トークンを足す
//	2回目  … 範囲の中の assistant 行だけを解いて text を集める
//
// 覚えるのは "typed" の user 行のバイト位置（turn ごとに1件）だけなので、**ファイルが
// どれだけ大きくてもメモリはほぼ増えない。**turn の終わりごとに最大6回読み直す経路
// （`readSignals`）があるため、ここでファイル全体を抱えると同時実行数ぶん掛け算になる。
//
// **開くのは通常のファイルだけである。**FIFO を渡されると `os.Open` が書き手の現れるまで
// 永久に返らず、turn ループの goroutine ごと固まる（設計 3-2 の hook の値は外部入力である）。
//
// path: transcript の JSONL のパス。
// promptID: Stop hook が渡した prompt_id。空なら最後の "typed" 起点の範囲を使う。
// prefix: 表明の印（`tracker.status_signal_prefix`）。
// currentIdentifier: いま作業している issue の識別子（対象を書かない行の対象になる）。
// 戻り値の1つ目: 読み取った結果。
// 戻り値の2つ目: ファイルを開けない・通常のファイルでない・読めない場合のエラー。
// **行の JSON が壊れていてもエラーにしない**（読める行だけを使う）。
// **長すぎる行も読み捨てて続ける。**
func ReadTranscript(path, promptID, prefix, currentIdentifier string) (*TranscriptReadResult, error) {
	f, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scan, err := scanTranscript(f, promptID)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyOrchestratorReadTranscriptReadFailed, path, err)
	}

	texts, err := collectTurnTexts(f, scan.start, scan.end)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyOrchestratorReadTranscriptReadFailed, path, err)
	}

	return &TranscriptReadResult{
		Signals: ParseSignals(texts, prefix, currentIdentifier),
		Usage:   scan.usage,
	}, nil
}

// openRegularFile は通常のファイルだけを開く。
//
// **`O_NONBLOCK` を付けて開き、開いたあとに種別を確かめる。**先に `os.Lstat` で見るだけ
// では、見てから開くまでの間に差し替えられる。FIFO は `O_NONBLOCK` があれば即座に返り、
// そのあとの検査で弾ける。**通常のファイルには `O_NONBLOCK` は影響しない。**
//
// path: 開くパス。
// 戻り値の1つ目: 開いたファイル。
// 戻り値の2つ目: 開けない・通常のファイルでない場合のエラー。
func openRegularFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyOrchestratorOpenRegularFileOpenFailed, path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, i18n.Errorf(i18n.KeyOrchestratorOpenRegularFileStatFailed, path, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, i18n.Errorf(i18n.KeyOrchestratorOpenRegularFileNotRegularFile, path, info.Mode())
	}
	return f, nil
}

// 会話の記録がどう判定されたかの理由である。**ログに出して、運用者が原因を見分けられるようにする。**
//
// **1つの文面で済ませてはならない。**身元ファイルの改竄（設計 3-2 / 3-23）と、
// 利用者が `~/.claude/projects` を消しただけの場合が、同じ1行に見えることになる。
const (
	// transcriptFound は記録が見つかったことを表す。
	transcriptFound = "記録がある"
	// transcriptMissing は、その UUID の記録が根の下に1件も無かったことを表す
	// （大きさが0のものしか無かった場合を含む）。
	transcriptMissing = "記録が無い"
	// transcriptUUIDUnsafe は、身元ファイルの UUID がパスの部品として使えない形だったことを表す。
	transcriptUUIDUnsafe = "身元ファイルの session_uuid がパスに使えない形である"
	// transcriptUndecidable は、記録があるかを決められなかったことを表す（根が決まっていない・読めない）。
	transcriptUndecidable = "記録の置き場所を読めないので判定できない"
)

// hasTranscriptFor は、そのセッション UUID の会話の記録が残っているかを返す（設計 3-3b）。
//
// **記録の無い UUID へ `--resume` を投げると、`herdr.startup_timeout_ms` をまるごと捨てる。**
// `claude --resume <無い UUID>` は `No conversation found with session ID:` を出して落ち、
// herdr 経由では `agent.start` が timeout を返すので、`confirmStartupWithRestart` が
// 期限まで `agent.start` をやり直し続ける。**着手が段9 の途中で落ちると、身元ファイルには
// 会話が1度も作られていない UUID が残る**ので、そのまま復帰しにいく道がある。
//
// **置き場所のディレクトリ名は当てない。**Claude Code が cwd を1つのディレクトリ名へ畳むときの
// 綴り直しの規則は確かめきれていない（[internal/redact/redact.go](../redact/redact.go) の
// `homeDashChars` が「`_` が置き換わることは確かめられていない」と書いている）。
// **セッション UUID は一意なので、根の直下を1階層だけ広げれば足りる。**
//
// **そのぶん、残る穴が1つある。**`claude --resume` は cwd のプロジェクトのディレクトリで
// 会話を解決するので、**worktree を別のパスへ作り直すと、古いパスの記録に当たって
// `--resume` を渡してしまう。**そこでは元と同じ空回りが起きる（設計 3-3b）。
//
// **エントリの種別で絞り込まない。**`os.ReadDir` が返す種別は lstat なので、
// **根の下に symlink で置かれたディレクトリを丸ごと飛ばすことになる。**
// 中のファイルを見に行けば、通っていれば当たり、通っていなければ `os.Lstat` が失敗する。
//
// **ファイルの側は `os.Lstat` で見る。**同じファイルの他の3箇所と同じ規則である
// （`SubagentTranscriptsFor` / `ListSubagentTranscripts` / `subagentDirOf`）。
// **`session_uuid` はエージェントが書き換えられる**ので、symlink を通常のファイルとして数えない。
//
// **大きさが0のファイルは「記録が無い」とみなす。**Claude Code は記録を非同期に書くので、
// 起動直後は1バイトも書かれていないことがある（`acceptTranscriptPath` の説明）。
// **会話が1バイトも書かれていないセッションへ復帰しても、引き継げる内容が無い。**
//
// sessionUUID: 復帰しようとしているセッションの UUID。
// 戻り値の1つ目: 記録があるか、または判定できなければ true。
// 戻り値の2つ目: そう決めた理由（`transcriptFound` などのいずれか。ログに出す）。
func (o *Orchestrator) hasTranscriptFor(sessionUUID string) (bool, string) {
	// **身元ファイルは worktree の中にあり、エージェントが書き換えられる**（設計 3-2 / 3-23）。
	// **`agent_id` と同じ規則で足りる**（英数字と `-` と `_` だけ。セッション UUID はこの形に収まる）。
	// `/` も `\` も `.` も通さないので、`..` で根の外へ出る組み立て方が成立しない。
	// **空文字もここで落ちる。**
	if !safeAgentID(sessionUUID) {
		return false, transcriptUUIDUnsafe
	}
	if o.transcriptRoot == "" {
		return true, transcriptUndecidable
	}
	entries, err := os.ReadDir(o.transcriptRoot)
	if err != nil {
		return true, transcriptUndecidable
	}
	name := sessionUUID + transcriptExt
	for _, e := range entries {
		info, statErr := os.Lstat(filepath.Join(o.transcriptRoot, e.Name(), name))
		if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		return true, transcriptFound
	}
	return false, transcriptMissing
}

// subagentDirName は subagent の記録を置くディレクトリの名前である。
//
// **Claude Code は親の記録の隣に掘る。**`<親の記録から `.jsonl` を落としたパス>/subagents/`
// である。docs/evidence/hooks_probe_20260817.jsonl の `SubagentStop` の1行に、
// `"transcript_path": "…/00000000-0000-4000-8000-000000000007.jsonl"` と
// `"agent_transcript_path": "…/00000000-0000-4000-8000-000000000007/subagents/agent-a1f9f743842d397e1.jsonl"`
// が同時に入っている。
const subagentDirName = "subagents"

// subagentTranscriptGlob は subagent の記録のファイル名の型である。
//
// **名前を決め打ちしない。**Glob で拾えば、ファイル名の付け方が変わっても壊れない。
const subagentTranscriptGlob = subagentTranscriptPrefix + "*" + transcriptExt

// subagentTranscriptPrefix は subagent の記録のファイル名の前置きである。
//
// **`agent_id` を挟むと `agent_transcript_path` になる**（実測記録1件で確認。
// `SubagentTranscriptsFor` の説明を見ること）。
const subagentTranscriptPrefix = "agent-"

// transcriptExt は記録のファイル名の拡張子である。**セッションの記録も subagent の記録も同じである。**
//
//	<記録の根>/<cwd を綴り直したもの>/<セッション UUID>.jsonl
//	<記録の根>/<cwd を綴り直したもの>/<セッション UUID>/subagents/agent-<agent_id>.jsonl
//
// （hookinput.go の `defaultTranscriptDirName` と同じ実測。設計 3-15）
const transcriptExt = ".jsonl"

// subagentMaxCandidates は Glob の結果を見る件数の上限である。
//
// **上限を置く理由は `transcriptMaxRequestIDs` と同じである。**ディレクトリに何件並ぶかには
// 上限が無く（Claude Code が書くもので、細工もできる）、全件を `os.Lstat` して並べ替えると、
// 引き渡しのコメントを1件作るだけで際限なく時間とメモリを使う。
const subagentMaxCandidates = 1000

// subagentDirOf は親の記録から subagent の記録の置き場所を組み立て、検査して返す。
//
// **検査は `acceptTranscriptPath` と同じ順である。**まず解決し、実在とディレクトリで
// あることを見て、**そのあと許可された根の内側かを比べる**（解決は実在するときにしか通らない）。
//
// parentPath: 親のセッションの記録の絶対パス。
// root: 受け入れてよい置き場所の根。空なら根の検査だけを飛ばす。
// 戻り値の1つ目: 解決した置き場所の絶対パス。
// 戻り値の2つ目: 使ってよければ true。
func subagentDirOf(parentPath, root string) (string, bool) {
	if !strings.HasSuffix(parentPath, transcriptExt) {
		return "", false
	}
	dir := filepath.Clean(filepath.Join(strings.TrimSuffix(parentPath, transcriptExt), subagentDirName))
	// **実在するなら解決してから比べる。**実在しなければ字句のままにしておく。
	if resolved, ok := resolvePath(dir); ok {
		dir = resolved
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", false
	}
	if root != "" && !isUnder(root, dir) {
		return "", false
	}
	if !info.IsDir() {
		return "", false
	}
	return dir, true
}

// safeAgentID は `agent_id` をパスの部品として使ってよいかを判定する。
//
// **セッション UUID にも使う**（`hasTranscriptFor`）。どちらも外部が書き換えられる値を
// ファイル名の部品にするので、通してよい文字は同じでよい。**UUID は英数字と `-` だけなので
// この規則に収まる。**
//
// **`agent_id` は hook から来る外部入力である**（設計 3-2 / 3-23）。
// **英数字とハイフンとアンダースコアだけを通す。**`/` も `\` も `.` も通さないので、
// **`..` で置き場所の外へ出る組み立て方が成立しない。**
//
// 実測で出ている値は `a1f9f743842d397e1` である
// （[docs/evidence/hooks_probe_20260817.jsonl](../../docs/evidence/hooks_probe_20260817.jsonl)）。
//
// id: 判定する `agent_id`。
// 戻り値: パスの部品に使ってよければ true。
func safeAgentID(id string) bool {
	if id == "" || len(id) > maxAgentIDBytes {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// maxAgentIDBytes は `agent_id` をパスの部品に使うときの長さの上限（バイト）である。
//
// **上限を置く理由は `maxTrackedSubagents` と同じである。**外部入力をそのまま
// ファイル名にすると、置き場所の走査に際限なく時間を使う組み立て方ができる。
// 実測で出ている値は17バイトである。
const maxAgentIDBytes = 128

// SubagentTranscriptsFor は、走っている subagent の `agent_id` から記録のパスを組み立てる
// （設計 3-11）。
//
// **推測しない。**`SubagentStart` が `agent_id` を持っているので、置き場所の規則から
// 一意に決まる。**glob で「たぶんこれだろう」と選ぶ必要が無い。**
//
//	<親の記録から `.jsonl` を落としたパス>/subagents/agent-<agent_id>.jsonl
//
// **この規則は実測記録1件から言えることである**（docs/evidence/hooks_probe_20260817.jsonl の
// `SubagentStop` 1件。`agent_id` = `a1f9f743842d397e1` に対して
// `agent_transcript_path` が `…/subagents/agent-a1f9f743842d397e1.jsonl` だった）。
// **同じ記録の `SubagentStart` には `agent_transcript_path` が入っていなかった**ので、
// 開始の側からは組み立てるしかない。
//
// **ファイルは1バイトも開かない。**`os.Lstat` で種別を見るだけである。
// **まだ書かれていないものは落とす。**Claude Code は記録を非同期に書くので（設計 3-25）、
// 走り始めた直後の subagent はファイルを持たないことがある。**そのときは呼び出し側が
// `ListSubagentTranscripts` に落ちる。**
//
// parentPath: 親のセッションの記録の絶対パス。
// root: 受け入れてよい置き場所の根。空なら根の検査だけを飛ばす。
// agentIDs: 走っている subagent の `agent_id`。
// limit: 返す件数の上限。0以下なら何も返さない。
// 戻り値の1つ目: subagent の記録の置き場所（解決した絶対パス）。無ければ空文字列。
// 戻り値の2つ目: 実在した記録のパス。渡された順のまま返す。無ければ nil。
func SubagentTranscriptsFor(parentPath, root string, agentIDs []string, limit int) (string, []string) {
	if limit <= 0 || len(agentIDs) == 0 {
		return "", nil
	}
	dir, ok := subagentDirOf(parentPath, root)
	if !ok {
		return "", nil
	}
	out := make([]string, 0, len(agentIDs))
	for _, id := range agentIDs {
		if !safeAgentID(id) {
			continue
		}
		path := filepath.Join(dir, subagentTranscriptPrefix+id+transcriptExt)
		// **組み立てたパスも、置き場所の検査を通す**（`acceptTranscriptPath` と同じ順。
		// まず解決し、次に実在と種別を見て、そのあと内側かを比べる）。
		// **`safeAgentID` が既に区切り文字を弾いているので、ここは二重の備えである。**
		// 弾き方を1つに頼ると、名前の付け方が変わったときに黙って穴が開く。
		if resolved, ok := resolvePath(path); ok {
			path = resolved
		}
		// **`os.Lstat` である。**シンボリックリンクは通常のファイルとして数えない
		// （`ListSubagentTranscripts` と同じ判断）。
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if !isUnder(dir, path) {
			continue
		}
		out = append(out, path)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return dir, nil
	}
	return dir, out
}

// ListSubagentTranscripts は親の記録の隣にある subagent の記録を、新しい順に列挙する。
//
// **ファイルは1バイトも開かない。**`os.Lstat` で種別と更新時刻を見るだけである。
// 引き渡しの通知に「どこを見ればよいか」を書くためのものであり、中身は読まない。
//
// 手順。
//
//  1. 親のパスが `.jsonl` で終わらなければ何も返さない
//  2. `.jsonl` を落としたパスに `subagents` を継いだディレクトリを組み立てる
//  3. 実在してディレクトリであること・許可された根の内側であることを確かめる
//     （`acceptTranscriptPath` と同じ順である。**まず解決してから根と比べる**）
//  4. `agent-*.jsonl` を Glob し、通常のファイルだけ残す
//  5. 更新時刻の新しい順に並べ、limit 件まで返す
//
// **ディレクトリが無いのは正常な並びである**（subagent を1つも使わなかった turn では
// 作られない）。**エラーにも警告にもしない。**
//
// parentPath: 親のセッションの記録の絶対パス。
// root: 受け入れてよい置き場所の根（`Options.TranscriptRoot` から解決したもの）。
// 空なら根の検査だけを飛ばす。
// limit: 返す件数の上限。0以下なら何も返さない。
// 戻り値の1つ目: subagent の記録の置き場所（解決した絶対パス）。無ければ空文字列。
// 戻り値の2つ目: 記録のパスを新しい順に並べたもの。無ければ nil。
func ListSubagentTranscripts(parentPath, root string, limit int) (string, []string) {
	if limit <= 0 {
		return "", nil
	}
	dir, ok := subagentDirOf(parentPath, root)
	if !ok {
		return "", nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, subagentTranscriptGlob))
	if err != nil {
		// パターンが壊れているときだけ返る。置き場所は分かっているので、そこだけ返す。
		return dir, nil
	}
	if len(matches) > subagentMaxCandidates {
		matches = matches[:subagentMaxCandidates]
	}

	type subagentEntry struct {
		path    string
		modTime time.Time
	}
	found := make([]subagentEntry, 0, len(matches))
	for _, m := range matches {
		// **`os.Lstat` である。**シンボリックリンクは通常のファイルとして数えない
		// （リンク先が FIFO でも、ここでは開かないので固まりはしないが、
		// 人間に「開けばよい」と案内するのは実体のあるファイルだけにする）。
		fi, err := os.Lstat(m)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		found = append(found, subagentEntry{path: m, modTime: fi.ModTime()})
	}
	slices.SortFunc(found, func(a, b subagentEntry) int {
		if c := b.modTime.Compare(a.modTime); c != 0 {
			return c
		}
		// **更新時刻が同じなら名前で決める。**同じ入力で並びが変わってはならない。
		return strings.Compare(a.path, b.path)
	})
	if len(found) > limit {
		found = found[:limit]
	}
	out := make([]string, 0, len(found))
	for _, e := range found {
		out = append(out, e.path)
	}
	return dir, out
}

// transcriptScan は1回目の走査の結果である。
type transcriptScan struct {
	// start は turn の範囲の開始位置（バイト。含む）である。
	start int64
	// end は turn の範囲の終了位置（バイト。含まない）である。
	end int64
	// usage はファイル全体のトークンの集計である。
	usage TokenUsage
}

// scanTranscript は1回目の走査を行い、turn の範囲とトークンの集計を求める。
//
// **覚えるのは "typed" の user 行のバイト位置だけである**（turn ごとに1件）。
//
// f: 読むファイル（先頭から読み直す）。
// promptID: Stop hook が渡した prompt_id。空なら最後の行を起点にする。
// 戻り値の1つ目: 範囲とトークン。
// 戻り値の2つ目: 読めない場合のエラー。
func scanTranscript(f *os.File, promptID string) (transcriptScan, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return transcriptScan{}, err
	}
	r := bufio.NewReaderSize(f, 64*1024)

	var (
		out          transcriptScan
		typedOffsets []int64
		anchor       int64 = -1
		lastOffset   int64 = -1
		offset       int64
		seen         = map[string]bool{}
	)

	for {
		raw, consumed, truncated, err := readTranscriptLine(r)
		lineStart := offset
		offset += consumed
		if len(raw) > 0 && !truncated {
			var line transcriptLine
			// 壊れた行は飛ばす。1行の障害で残り全部を落とさない。
			if json.Unmarshal(raw, &line) == nil {
				lastOffset = lineStart
				if isTypedUser(line) {
					typedOffsets = append(typedOffsets, lineStart)
				}
				if promptID != "" && line.Type == "user" && line.PromptID == promptID {
					anchor = lineStart
				}
				addUsage(&out.usage, line, seen)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return transcriptScan{}, err
		}
	}

	out.end = offset
	if anchor < 0 {
		anchor = lastOffset
	}
	if anchor < 0 {
		// 読める行が1つも無い。
		return transcriptScan{start: 0, end: 0, usage: out.usage}, nil
	}

	// 段3: anchor から前へ遡り、最初の "typed" の user 行を turn の頭とする。
	out.start = 0
	for i := len(typedOffsets) - 1; i >= 0; i-- {
		if typedOffsets[i] <= anchor {
			out.start = typedOffsets[i]
			break
		}
	}
	// 段4: 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする。
	for _, off := range typedOffsets {
		if off > out.start {
			out.end = off
			break
		}
	}
	return out, nil
}

// addUsage は1行ぶんのトークンを集計へ足す（設計 3-15）。
//
// **`requestId` で必ず重複排除する。**assistant の行が API 呼び出しと1対1である保証は
// 取れていない。重複排除しておけば、どちらでも正しい値になる。
//
// usage: 足し込む先。
// line: 1行。
// seen: これまでに数えた `requestId`（`transcriptMaxRequestIDs` を上限に覚える）。
func addUsage(usage *TokenUsage, line transcriptLine, seen map[string]bool) {
	if line.Type != "assistant" || line.Message == nil || line.Message.Usage == nil {
		return
	}
	if line.RequestID != "" {
		if seen[line.RequestID] {
			return
		}
		if len(seen) < transcriptMaxRequestIDs {
			seen[line.RequestID] = true
		}
	}
	u := line.Message.Usage
	usage.APICalls++
	usage.Input += u.InputTokens
	usage.CacheCreation += u.CacheCreationInputTokens
	usage.CacheRead += u.CacheReadInputTokens
	usage.Output += u.OutputTokens
}

// collectTurnTexts は2回目の走査で、turn の範囲の assistant の text を集める
// （設計 3-25 の段5）。
//
// **`isSidechain == false` に絞る。**subagent の発言を印として拾わないためである。
//
// f: 読むファイル。
// start: 範囲の開始位置（バイト。含む）。
// end: 範囲の終了位置（バイト。含まない）。
// 戻り値の1つ目: 集めた text の並び。
// 戻り値の2つ目: 読めない場合のエラー。
func collectTurnTexts(f *os.File, start, end int64) ([]string, error) {
	if end <= start {
		return nil, nil
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(f, 64*1024)

	var texts []string
	offset := start
	for offset < end {
		raw, consumed, truncated, err := readTranscriptLine(r)
		offset += consumed
		if len(raw) > 0 && !truncated {
			var line transcriptLine
			if json.Unmarshal(raw, &line) == nil &&
				line.Type == "assistant" && !line.IsSidechain && line.Message != nil {
				texts = append(texts, contentTexts(line.Message.Content)...)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	return texts, nil
}

// readTranscriptLine は JSONL の1行を読む。
//
// **`bufio.Scanner` を使わない。**上限を超えた行で `bufio.ErrTooLong` を返して以降を
// 読めなくなるため、**長すぎる行はその1行だけを読み捨てて続ける。**
//
// r: 読み元。
// 戻り値の1つ目: 行の中身（改行を含まない）。読み捨てた場合は nil。
// 戻り値の2つ目: 消費したバイト数（改行を含む）。**呼び出し側の位置の計算に使う。**
// 戻り値の3つ目: 上限を超えて読み捨てたなら true。
// 戻り値の4つ目: ファイルの終わりなら io.EOF。それ以外は読み取りの失敗。
func readTranscriptLine(r *bufio.Reader) ([]byte, int64, bool, error) {
	var (
		line      []byte
		consumed  int64
		truncated bool
	)
	for {
		chunk, err := r.ReadSlice('\n')
		consumed += int64(len(chunk))
		if !truncated {
			if len(line)+len(chunk) > transcriptMaxLineBytes {
				truncated = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return trimLineEnd(line), consumed, truncated, err
		}
		return trimLineEnd(line), consumed, truncated, nil
	}
}

// trimLineEnd は行末の改行を落とす。
//
// line: 1行。
// 戻り値: 前後の空白と改行を落とした行。空白だけの行は nil を返す。
func trimLineEnd(line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

// isTypedUser は「turn の頭」の行かを判定する（設計 3-25）。
//
// **`promptSource` は `"typed"` に一致するかだけを見る。**観測できた値は
// `typed` / `system` / 項目なし の3通りだが、他の値が出ないことは確認できていない。
// **一致しない値はすべて「turn の頭ではない」として扱う。**
//
// line: 判定する行。
// 戻り値: turn の頭なら true。
func isTypedUser(line transcriptLine) bool {
	return line.Type == "user" && line.PromptSource == "typed"
}

// contentTexts は message.content から text ブロックの本文を取り出す。
//
// content は配列のことも文字列のこともあるので、両方を受ける。
//
// content: message.content の生の JSON。
// 戻り値: 取り出した本文の並び。
func contentTexts(content json.RawMessage) []string {
	if len(content) == 0 {
		return nil
	}
	var blocks []transcriptContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var out []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				out = append(out, b.Text)
			}
		}
		return out
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}
