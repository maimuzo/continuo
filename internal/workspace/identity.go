package workspace

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// identityFilePerm は身元ファイルのパーミッションである。
// 中に issue の URL と各種パスが入るので、他人に読ませない 0600 にする。
const identityFilePerm os.FileMode = 0o600

// excludeFilePerm は `.git/info/exclude` を新しく作るときのパーミッションである。
const excludeFilePerm os.FileMode = 0o644

// ErrIdentityNotFound は worktree に身元ファイルが無いことを表す。
// **復元の走査（3-4 の段2）では「人間が置いた worktree かもしれない」ので無視する。**
var ErrIdentityNotFound = errors.New("身元ファイルがありません")

// ErrIdentityBroken は身元ファイルの JSON が壊れていることを表す。
// **段6 の書き込み途中で落ちた場合に起こる。消してはならない**（3-4 の段2）。
// worktree の再利用のときは新規として扱う（3-18）。
var ErrIdentityBroken = errors.New("身元ファイルの JSON が壊れています")

// Identity は worktree の直下に置く .continuo.json の中身である（3-18）。
//
// **これが復元の主キーになる。**worktree のディレクトリ名は識別子から使えない文字を
// 潰して作るため一方向の変換であり、**ディレクトリ名から issue へは戻れない。**
type Identity struct {
	// IssueURL は issue の URL である。逆引きの主キーであり、人間が見ても分かる。
	IssueURL string `json:"issue_url"`
	// IssueIdentifier は `<owner>/<repo>#<番号>` の形の人間可読な名前である。
	IssueIdentifier string `json:"issue_identifier"`
	// ProjectItemID は project item の ID である。ボードを ID 指定でまとめて取り直すのに使う。
	ProjectItemID string `json:"project_item_id"`
	// Branch は worktree が指す branch 名である。片付けで消す対象を確定するのに使う。
	Branch string `json:"branch"`
	// HerdrWorkspaceID は herdr の workspace の ID である。
	// **worktree.remove がこの ID を要求する**（path でも branch でもない。3-9）。
	// 再起動後に取り直す経路が他に無いので、必ずここに書く。
	HerdrWorkspaceID string `json:"herdr_workspace_id"`
	// SocketPath は hook を受ける socket の絶対パスである。
	// 探索順が環境に依存するので、再起動で別のパスに落ちうる。一致の検査に使う。
	SocketPath string `json:"socket_path"`
	// SettingsPath は Claude Code の設定ファイルのパスである（worktree の外に置く。3-12）。
	// 片付けのときに一緒に消す。
	SettingsPath string `json:"settings_path"`
	// AgentName は herdr の agent 名である。agent.prompt / agent.wait の宛先になる。
	// **着手の段6 の時点では確定していない**（重複したら連番が付く。3-3）。
	// 段9 で agent.start が通ったあとに SetAgentName で追記する。
	AgentName string `json:"agent_name"`
	// SessionUUID は Claude Code のセッション UUID である。hook の対応づけの復元に使う。
	SessionUUID string `json:"session_uuid"`
	// CreatedAt は worktree を作った時刻である。古い残骸の判別に使う。
	// **再利用のときは既存の値を保つ**（3-18）。
	CreatedAt time.Time `json:"created_at"`
	// TakeoverCount は再起動をまたいで引き継いだ回数である。
	// **これが無いと、落ちるたびに turn 数が 1 に戻るので打ち切りが永久に発火しない**（3-4）。
	TakeoverCount int `json:"takeover_count"`
	// CleanupDeferredAt は、未コミットや未 push で片付けを見送った時刻である。
	// ゼロ値でなければ、issue へのコメントは既に書いてある（2回目以降はログにのみ残す。3-9）。
	//
	// **タグは omitzero である**（05_workspace.md の型は omitempty と書いているが、
	// **omitempty は構造体である time.Time には効かない**ため、ゼロ値でも
	// `"0001-01-01T00:00:00Z"` が出てしまう）。「書いていないなら出さない」という
	// 意図をそのまま実現できる omitzero（Go 1.24 以降）を使う。
	CleanupDeferredAt time.Time `json:"cleanup_deferred_at,omitzero"`
}

// ValidateIdentityFileName は workspace.identity_file が「ファイルの名前」かを確かめる（3-18）。
//
// 3-18 はこの値を **worktree の直下に置くファイルの名前**と定めている。
// パスの区切りや `..` を含む値（`../secret.json` など）を許すと、
// **身元ファイルが worktree の外側へ書かれ、`info/exclude` へ書く行も
// `/../secret.json` になる。**normalize.Normalize はドットもスラッシュも通すので
// （branch 名に必要なため）、ここで別に弾く。
//
// name: 設定の workspace.identity_file。
// 戻り値: 空文字・パスの区切りを含む・`.` や `..` そのもの・先頭や末尾に空白がある
// 場合のエラー。問題なければ nil。
func ValidateIdentityFileName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace.identity_file が空です（身元ファイルの名前を決められません）")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("workspace.identity_file %q の前後に空白があります（ファイルの名前にすること）", name)
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf(
			"workspace.identity_file %q にパスの区切りが入っています"+
				"（worktree の直下に置くファイルの名前だけを書くこと。3-18）", name)
	}
	if name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf(
			"workspace.identity_file %q はファイルの名前ではありません"+
				"（worktree の外側を指しうる値は使えない。3-18）", name)
	}
	return nil
}

// IdentityPath は worktree の身元ファイルの絶対パスを返す。
// ファイル名は設定の workspace.identity_file で変えられる（3-18）。
// **その値がファイルの名前であることは New が ValidateIdentityFileName で確かめている。**
//
// worktreePath: worktree の絶対パス。
// 戻り値: `<worktree>/<workspace.identity_file>`。
func (m *Manager) IdentityPath(worktreePath string) string {
	return filepath.Join(worktreePath, m.cfg.Workspace.IdentityFile)
}

// ReadIdentity は worktree の身元ファイルを読む（3-18）。
//
// worktreePath: worktree の絶対パス。
// 戻り値の1つ目: 読めた身元ファイルの中身。
// 戻り値の2つ目: ファイルが無ければ ErrIdentityNotFound、JSON が壊れていれば
// ErrIdentityBroken を包んだエラー（errors.Is で判別できる）。
// **どちらの場合も呼び出し側は worktree を消してはならない**（3-4 の段2）。
func (m *Manager) ReadIdentity(worktreePath string) (*Identity, error) {
	path := m.IdentityPath(worktreePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrIdentityNotFound, path)
		}
		return nil, fmt.Errorf("身元ファイル %s を読めません: %w", path, err)
	}
	var identity Identity
	if err := json.Unmarshal(data, &identity); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrIdentityBroken, path, err)
	}
	return &identity, nil
}

// WriteIdentity は worktree の身元ファイルを書き、共通ディレクトリの `info/exclude` へ
// 登録する（3-18。着手の段6 で呼ぶ）。
//
// **書き込みは一時ファイル + rename で行う。**途中で落ちたときに壊れた JSON が
// 残るのを避けるためである（壊れていると復元の走査が読めない）。
//
// **`.gitignore` は触らない。**利用者のリポジトリを汚さないため、commit 対象ではない
// `info/exclude` を使う。登録先は共通ディレクトリ側の1本である
// （`git rev-parse --git-common-dir`。worktree ごとには無い）。
//
// ctx: git を実行するときに適用するコンテキスト。
// worktreePath: worktree の絶対パス。
// identity: 書き込む中身。
// 戻り値: 書き込みに失敗した場合のエラー。**exclude への登録に失敗しても
// エラーにはせず警告としてログに出す**（身元ファイルそのものは書けているので、
// 復元も片付けも成立する）。
func (m *Manager) WriteIdentity(ctx context.Context, worktreePath string, identity Identity) error {
	path := m.IdentityPath(worktreePath)
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("身元ファイルの内容を JSON 化できません: %w", err)
	}
	data = append(data, '\n')

	if err := writeFileAtomic(path, data, identityFilePerm); err != nil {
		return err
	}

	if err := m.registerExclude(ctx, worktreePath); err != nil {
		m.logger.Warn("身元ファイルを info/exclude へ登録できませんでした",
			"worktree", worktreePath, "error", err)
	}
	return nil
}

// writeFileAtomic は同じディレクトリに一時ファイルを作ってから rename で置き換える。
//
// path: 書き込み先の絶対パス。
// data: 書き込む中身。
// perm: 作るファイルのパーミッション。
// 戻り値: 一時ファイルの作成・書き込み・rename に失敗した場合のエラー。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("%s の一時ファイルを作成できません: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s へ書き込めません: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s のパーミッションを設定できません: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s を閉じられません: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%s を %s へ置き換えられません: %w", tmpName, path, err)
	}
	return nil
}

// registerExclude は身元ファイルの名前を共通ディレクトリの `info/exclude` へ登録する（3-18）。
//
// 書く行は **`/<身元ファイル名>`**（先頭にスラッシュを付ける）。付けないと配下の
// 全階層で無視される。**冪等にする。**共通ディレクトリの1本を issue ごとに触るので、
// 同じ行が既にあれば書かない（そのままだと積み上がる）。
//
// ctx: git を実行するときに適用するコンテキスト。
// worktreePath: worktree の絶対パス。
// 戻り値: 共通ディレクトリを引けない場合・ファイルを書けない場合のエラー。
func (m *Manager) registerExclude(ctx context.Context, worktreePath string) error {
	commonDir, err := gitCommonDir(ctx, worktreePath)
	if err != nil {
		return err
	}
	infoDir := filepath.Join(commonDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("%s を作成できません: %w", infoDir, err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	line := "/" + m.cfg.Workspace.IdentityFile

	existing, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s を読めません: %w", excludePath, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == line {
			return nil
		}
	}

	var buf strings.Builder
	buf.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString(line + "\n")

	if err := os.WriteFile(excludePath, []byte(buf.String()), excludeFilePerm); err != nil {
		return fmt.Errorf("%s へ書き込めません: %w", excludePath, err)
	}
	return nil
}

// MergeForReuse は worktree を再利用するときに書く Identity を組み立てる（3-18）。
//
// | 項目 | 再利用のとき |
// | takeover_count | 既存の値を1つ増やす。新規なら 0 |
// | created_at | 既存の値を保つ。新規のときだけ fresh の値を使う |
// | cleanup_deferred_at | 消す（ゼロ値にする） |
// | それ以外 | 全部書き直す |
//
// **cleanup_deferred_at を消す理由。**再利用するということは、その issue が再び
// dispatch されたということであり、そこから先は別の run である。前の run の記録を
// 持ち越すと、新しい run で見送ったときに人間がそれを知れない（3-9 の手順2c）。
//
// fresh: 今回の値で埋めた Identity（CreatedAt には現在時刻を入れておく）。
// existing: 既存の身元ファイル。無い・壊れている場合は nil を渡す（新規として扱う）。
// 戻り値: 書き込むべき Identity。
func MergeForReuse(fresh Identity, existing *Identity) Identity {
	fresh.CleanupDeferredAt = time.Time{}
	if existing == nil {
		fresh.TakeoverCount = 0
		return fresh
	}
	fresh.TakeoverCount = existing.TakeoverCount + 1
	if !existing.CreatedAt.IsZero() {
		fresh.CreatedAt = existing.CreatedAt
	}
	return fresh
}

// SetAgentName は身元ファイルの agent_name だけを書き換える（3-18。着手の段9 のあと）。
//
// **段6 の時点では agent 名が確定していない**（重複したら連番が付く。3-3）ので、
// agent.start が通ってから追記する経路が要る。
//
// ctx: git を実行するときに適用するコンテキスト（exclude の登録に使う）。
// worktreePath: worktree の絶対パス。
// agentName: herdr が実際に付けた agent 名。
// 戻り値: 身元ファイルを読めない・書けない場合のエラー。
func (m *Manager) SetAgentName(ctx context.Context, worktreePath, agentName string) error {
	identity, err := m.ReadIdentity(worktreePath)
	if err != nil {
		return err
	}
	identity.AgentName = agentName
	return m.WriteIdentity(ctx, worktreePath, *identity)
}

// IncrementTakeover は身元ファイルの takeover_count を1つ増やして書き戻す（3-4 の段5b）。
//
// **引き継いだときと再 dispatch したときの両方で増やす**（3-18）。
//
// ctx: git を実行するときに適用するコンテキスト（exclude の登録に使う）。
// worktreePath: worktree の絶対パス。
// 戻り値の1つ目: 増やしたあとの Identity。
// 戻り値の2つ目: 身元ファイルを読めない・書けない場合のエラー。
func (m *Manager) IncrementTakeover(ctx context.Context, worktreePath string) (*Identity, error) {
	identity, err := m.ReadIdentity(worktreePath)
	if err != nil {
		return nil, err
	}
	identity.TakeoverCount++
	if err := m.WriteIdentity(ctx, worktreePath, *identity); err != nil {
		return nil, err
	}
	return identity, nil
}

// MarkCleanupDeferred は身元ファイルの cleanup_deferred_at に時刻を書く（3-9 の手順2c）。
//
// **orchestrator が「issue へのコメントの投稿に成功したあと」に呼ぶ。**
// 投稿の前に書くと、投稿が失敗したときにコメントが永久に出なくなる。
//
// worktreePath: worktree の絶対パス。
// at: 見送った時刻。
// 戻り値: 身元ファイルを読めない・書けない場合のエラー。
//
// **この関数は git を呼ばない**（exclude は既に登録済みなので、書き込みだけで足りる）。
func (m *Manager) MarkCleanupDeferred(worktreePath string, at time.Time) error {
	identity, err := m.ReadIdentity(worktreePath)
	if err != nil {
		return err
	}
	identity.CleanupDeferredAt = at

	data, err := json.MarshalIndent(*identity, "", "  ")
	if err != nil {
		return fmt.Errorf("身元ファイルの内容を JSON 化できません: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(m.IdentityPath(worktreePath), data, identityFilePerm)
}
