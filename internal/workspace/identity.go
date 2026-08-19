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
	// Base は worktree を作ったときの base である（PrepareResult.Base をそのまま書く）。
	// **片付けの手順2b が、upstream が無い branch を判定するのに要る**（3-9）。
	// **ここに書いておかないと、再起動をまたいだ片付け（巡回の手順7）が
	// 「base が分からないので判定できない」で永久に見送る。**
	// 呼び出し側が空で書いた場合は、その見送りの理由がそのまま人間に見える。
	Base string `json:"base"`
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

// identityLockKey は同じ worktree の身元ファイルの更新を直列化する鍵を作る。
//
// **シンボリックリンクを解決してから鍵にする**（呼び出し側が解決前のパスを渡しても
// 同じ鍵に寄せる。after_run の印と同じ考え方）。
//
// worktreePath: worktree のパス。
// 戻り値: 鍵に使う文字列。
func identityLockKey(worktreePath string) string {
	return "identity:" + resolveOrClean(worktreePath)
}

// excludeLockKey は同じリポジトリの `info/exclude` の更新を直列化する鍵を作る。
//
// **共通ディレクトリの1本のファイルを worktree ごとに触る**ので、
// 読んで足す処理が並行に走ると行が重複する。
//
// excludePath: `info/exclude` のパス。
// 戻り値: 鍵に使う文字列。
func excludeLockKey(excludePath string) string {
	return "exclude:" + filepath.Clean(excludePath)
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
// 戻り値: 書き込みに失敗した場合のエラー。**exclude への登録に失敗してもエラーにはせず
// 警告としてログに出す。**登録は「利用者の `git status` を汚さないための親切」であって、
// **片付けの正しさはこの成否に依存しない**（片付けの手順2 は身元ファイルとその一時ファイルを
// pathspec で数から外す。cleanup.go の leftoverReasons と git.go の gitStatusPorcelain）。
//
// **同じ worktree に対する身元ファイルの更新は直列化する**（読んで書き戻すため）。
func (m *Manager) WriteIdentity(ctx context.Context, worktreePath string, identity Identity) error {
	unlock := m.identityMu.lock(identityLockKey(worktreePath))
	defer unlock()
	return m.writeIdentityLocked(ctx, worktreePath, identity)
}

// writeIdentityLocked は identityMu を取った状態で身元ファイルを書く。
//
// **既に鍵を取っている経路（SetAgentName / IncrementTakeover）から呼ぶ。**
// 取り直すと同じ鍵で自分を待つ。
//
// ctx: git を実行するときに適用するコンテキスト。
// worktreePath: worktree の絶対パス。
// identity: 書き込む中身。
// 戻り値: 書き込みに失敗した場合のエラー。
func (m *Manager) writeIdentityLocked(ctx context.Context, worktreePath string, identity Identity) error {
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

// excludeLines は `info/exclude` へ登録する行を返す（3-18）。
//
// **2行いる。**身元ファイル本体と、writeFileAtomic が同じディレクトリに作る一時ファイル
// （`<身元ファイル名>.tmp<乱数>`）である。一時ファイルは通常なら消えるが、
// **常駐プロセスが強制終了すると worktree に残る。**残った1つが未追跡ファイルとして
// 数えられると、その worktree は二度と片付かない。
//
// 先頭のスラッシュは必須である（付けないと配下の全階層で無視される）。
//
// 戻り値: 登録する行（`/.continuo.json` と `/.continuo.json.tmp*`）。
func (m *Manager) excludeLines() []string {
	base := "/" + m.cfg.Workspace.IdentityFile
	return []string{base, base + ".tmp*"}
}

// registerExclude は身元ファイルの名前を共通ディレクトリの `info/exclude` へ登録する（3-18）。
//
// **冪等にする。**共通ディレクトリの1本を issue ごとに触るので、同じ行が既にあれば
// 書かない（そのままだと積み上がる）。**同じリポジトリに対する更新は直列化する。**
//
// **書き足すだけで、書き直さない。**`info/exclude` は利用者のファイルであり、
// 読み取り → 連結 → 全置換（os.WriteFile は O_TRUNC）で更新すると、**途中で落ちたときに
// 利用者が自分で書いた除外規則が消える。**追記なら既存の内容にも権限にも触らない。
//
// **書き込む先のリポジトリは検算する。**worktree の `.git` はエージェントが書き換えられる
// ファイルなので、検算しないと任意のリポジトリの `info/exclude` に行を足せる
// （repo.go の verifiedRepo を見よ）。
//
// ctx: git を実行するときに適用するコンテキスト。
// worktreePath: worktree の絶対パス。
// 戻り値: 共通ディレクトリを引けない場合・検算に落ちた場合・ファイルを書けない場合のエラー。
func (m *Manager) registerExclude(ctx context.Context, worktreePath string) error {
	commonDir, _, err := m.verifiedRepo(ctx, worktreePath)
	if err != nil {
		return err
	}
	infoDir := filepath.Join(commonDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("%s を作成できません: %w", infoDir, err)
	}
	excludePath := filepath.Join(infoDir, "exclude")

	unlock := m.identityMu.lock(excludeLockKey(excludePath))
	defer unlock()

	existing, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s を読めません: %w", excludePath, err)
	}
	registered := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		registered[strings.TrimSpace(scanner.Text())] = true
	}

	var add strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		add.WriteString("\n")
	}
	for _, line := range m.excludeLines() {
		if registered[line] {
			continue
		}
		add.WriteString(line + "\n")
	}
	if add.Len() == 0 {
		return nil
	}

	file, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, excludeFilePerm)
	if err != nil {
		return fmt.Errorf("%s を開けません: %w", excludePath, err)
	}
	if _, err := file.WriteString(add.String()); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s へ書き込めません: %w", excludePath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%s を閉じられません: %w", excludePath, err)
	}
	return nil
}

// MergeForReuse は worktree を再利用するときに書く Identity を組み立てる（3-18）。
//
// | 項目 | 再利用のとき |
// | takeover_count | 既存の値を1つ増やす。新規なら 0 |
// | created_at | 既存の値を保つ。新規のときだけ fresh の値を使う |
// | cleanup_deferred_at | 消す（ゼロ値にする） |
// | base | fresh が空なら既存の値を保つ（再利用のとき Prepare は base を決めない） |
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
	if fresh.Base == "" {
		// **再利用のときは base を作り直せない**（worktree は既にあるので、
		// Prepare は base を決めずに戻る）。前の run が書いた値を落とすと、
		// 片付けが「base が分からない」で永久に見送る（3-9 の手順2b）。
		fresh.Base = existing.Base
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
	// **読んで書き戻すので、その間ほかの更新を入れない**（入れると片方が消える）。
	unlock := m.identityMu.lock(identityLockKey(worktreePath))
	defer unlock()

	identity, err := m.ReadIdentity(worktreePath)
	if err != nil {
		return err
	}
	identity.AgentName = agentName
	return m.writeIdentityLocked(ctx, worktreePath, *identity)
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
	// **読んで書き戻すので、その間ほかの更新を入れない**（入れると数え落とす）。
	unlock := m.identityMu.lock(identityLockKey(worktreePath))
	defer unlock()

	identity, err := m.ReadIdentity(worktreePath)
	if err != nil {
		return nil, err
	}
	identity.TakeoverCount++
	if err := m.writeIdentityLocked(ctx, worktreePath, *identity); err != nil {
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
	// **読んで書き戻すので、その間ほかの更新を入れない。**
	unlock := m.identityMu.lock(identityLockKey(worktreePath))
	defer unlock()

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
