package hookserver

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// 逃がし先（設計 3-19）のディレクトリ構成である。
//
//	<実行時ディレクトリ>/issues/<issue のスラグ>/pending/<受信時刻>-<hook_event_name>.json
//	<実行時ディレクトリ>/issues/<issue のスラグ>/pending/broken/  … 壊れた JSON の隔離先
//
// 書く側の規則は internal/hookclient にある。ここは読む側である。
const (
	// IssuesDirName は実行時ディレクトリの下の、issue ごとのディレクトリを束ねる名前である。
	IssuesDirName = "issues"
	// PendingDirName は issue ごとのディレクトリの下の、逃がし先の名前である。
	PendingDirName = "pending"
	// BrokenDirName は壊れた JSON を移す先の名前である（消さずに残して人間に見せる）。
	BrokenDirName = "broken"
	// PendingFileExt は読み戻しの対象になる拡張子である。これに一致するものだけを走査する。
	PendingFileExt = ".json"
	// PendingTmpExt は書き込み中のファイルの拡張子である。走査では必ず飛ばす。
	// 書き切ってから os.Rename で PendingFileExt の名前に変わるので、
	// PendingFileExt で見えた時点で中身は必ず完全である（設計 3-19）。
	PendingTmpExt = ".json.tmp"

	// maxBrokenNameAttempts は隔離先の名前がぶつかったときに、連番を足して試す回数の
	// 上限である。書く側（internal/hookclient）が受信時刻をずらして名前の衝突を塞いでいるのに
	// 隔離先だけ上書きすると、設計 3-19 が「消さずに残す」と決めたファイルが消える。
	maxBrokenNameAttempts = 1000

	// staleTmpAge は .json.tmp を「取り残された残骸」と見なすまでの経過時間である。
	//
	// **経過時間を見ずに一括で消してはならない。**逃がし先へ書くのは「socket へ繋がらないとき」
	// ＝まさに continuo の再起動中なので、`continuo hook` が書いている最中
	// （OpenFile → Write → Sync → **Rename の直前**）の .tmp と、起動時の掃除がぶつかりうる。
	// 消すと書く側の os.Rename が ENOENT で失敗し、その hook はどこにも残らずに消える。
	// 消えたのが Stop なら、その run は claude.turn_timeout_ms（既定1時間）まで誰も気づかない。
	//
	// 30秒にする根拠。書く側が1件を書き終えるまでの上限は
	// hookclient.DefaultDialTimeout（2秒）+ hookclient.DefaultWriteTimeout（2秒）+ 書き込みで、
	// その合計より十分に長い。読む側は .json しか読まないので、残しても害は無い
	// （次の起動で消える）。
	staleTmpAge = 30 * time.Second

	// maxReplayEvents は1回の走査で読み戻す hook の最大件数である。
	// 超えた分は読まずに残し、次の起動へ持ち越す（読んだファイルは消えるので必ず前へ進む）。
	maxReplayEvents = 10000

	// maxReplayBytes は1回の走査で読み込む合計バイト数の上限である。
	// 逃がし先は同じ利用者が書ける場所なので、上限が無いと巨大なファイルを1つ置かれるだけで
	// 起動のたびにメモリを使い切って落ちる輪ができる。
	maxReplayBytes int64 = 256 << 20
)

// ReplayPending は逃がし先に溜まった hook を読み戻す（復元の段5e の1回目の走査。
// 設計 3-4 / 3-19）。
//
// 走査するのは <実行時ディレクトリ>/issues/*/pending/ の全部である。
// 手順は次のとおりで、**必ず Start（段5d）のあと、StartDelivery（段6b）の前に呼ぶ。**
//
//  1. 取り残された .json.tmp を消し、消したことをログに残す（書いている途中で落ちた残骸である）
//  2. *.json を受信時刻（ファイル名）の昇順に読み、読んだファイルを消す。
//     解釈できなかったものは消さずに pending/broken/ へ移す
//
// **読み戻したものは、この時点ではキューへ積まない。**積むのは StartDelivery である。
// 段5e の2回目の走査を配送の直前に行い、「1回目の走査を始めてから配送を始めるまでの間に
// continuo hook が書いたもの」まで拾うためである（設計 3-4）。
//
// 戻り値: 次の場合にエラーを返す。
//   - 既に Close されている
//   - 既に StartDelivery を呼んでいる（キューの先頭へ割り込むと、socket で先に届いた
//     新しい hook より逃がし先の古い hook が後になる。順序が壊れる）
//   - issues ディレクトリを読めなかった（ディレクトリがまだ無い場合はエラーにしない。
//     着手前の起動では普通に起きる）
//
// ファイル1件ごとの失敗はログに残して読み進める（1件の障害で残り全部を落とさないため）。
func (s *Server) ReplayPending() error {
	s.mu.Lock()
	closed := s.closed
	delivering := s.delivering
	s.mu.Unlock()
	if closed {
		return errors.New("既に Close された hookserver は逃がし先を読み戻せません")
	}
	if delivering {
		return errors.New(
			"配送を始めたあとに ReplayPending を呼ぶことはできません" +
				"（逃がし先の古い hook が、socket で先に届いた新しい hook より後に配送されます。" +
				"復元の段5e は段6b より前です）",
		)
	}

	dirs, err := s.pendingDirs()
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		s.removeStaleTmpFiles(dir)
	}

	// **ここから先でエラーを返さない。**読んだ時点でファイルはディスクから消えているので、
	// 読めた分を手放したまま return すると、その hook は永久に失われる（設計 3-19）。
	events := s.scanPendingDirs(dirs)
	if len(events) > 0 {
		s.logger.Info("逃がし先に溜まっていた hook を読み戻しました", "count", len(events))
	}

	s.mu.Lock()
	s.replayed = append(s.replayed, events...)
	s.mu.Unlock()
	return nil
}

// scanLatePending は配送を始める直前に逃がし先をもう一度走査する
// （復元の段5e の2回目の走査。StartDelivery から呼ぶ）。
//
// 拾うのは「1回目の走査を始めてから配送を始めるまでの間に continuo hook が書いたもの」である
// （設計 3-4）。listen は段5d で始まっているが、繋がりに行くのは Claude Code 側の都合なので、
// この窓の間も socket へ繋げなかった continuo hook は逃がし先へ書き続ける。
//
// 1回目の間に新しく作られた issue のディレクトリも拾えるよう、ディレクトリの一覧から取り直す。
// **この2回目では .tmp を消さない。**いま書かれている最中のものを壊さないためである。
//
// 戻り値: 読み出せた HookEvent を受信時刻の昇順に並べたもの。走査に失敗した場合は
// 警告をログに出して空を返す（配送そのものは始める。次の起動で読み戻せる）。
func (s *Server) scanLatePending() []HookEvent {
	dirs, err := s.pendingDirs()
	if err != nil {
		s.logger.Warn("配送を始める直前の逃がし先の走査に失敗しました（この分は次の起動で読み戻します）",
			"error", err)
		return nil
	}
	return s.scanPendingDirs(dirs)
}

// pendingDirs は走査対象の逃がし先ディレクトリを列挙する。
//
// 戻り値: <実行時ディレクトリ>/issues/*/pending のうち、実在するディレクトリの絶対パス。
// issues ディレクトリがまだ無い場合は空スライスと nil を返す。
// issues ディレクトリを読めなかった場合はエラーを返す。
func (s *Server) pendingDirs() ([]string, error) {
	issuesDir := filepath.Join(s.runtimeDir, IssuesDirName)
	entries, err := os.ReadDir(issuesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Errorf(i18n.KeyHookserverPendingDirsIssuesDirUnreadable, issuesDir, err)
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(issuesDir, e.Name(), PendingDirName)
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		dirs = append(dirs, p)
	}
	return dirs, nil
}

// pendingFile は逃がし先から読み出した hook 1件である。
type pendingFile struct {
	// path はファイルの絶対パスである。
	path string
	// name はファイル名（<受信時刻>-<hook_event_name>.json）である。並べ替えの鍵になる。
	name string
	// event は解釈した hook のイベントである。
	event HookEvent
}

// scanPendingDirs は複数の逃がし先を走査し、受信時刻の昇順に並べたイベントを返す。
//
// dirs: 走査する逃がし先ディレクトリの一覧。
// 戻り値: 読み出せた HookEvent を、ファイル名（受信時刻）の昇順に並べたもの。
// 読めたファイルは消し、解釈できなかったファイルは pending/broken/ へ移す。
func (s *Server) scanPendingDirs(dirs []string) []HookEvent {
	budget := &scanBudget{remainingFiles: maxReplayEvents, remainingBytes: maxReplayBytes}
	var files []pendingFile
	for _, dir := range dirs {
		files = append(files, s.scanPendingDir(dir, budget)...)
	}
	// ファイル名の先頭はマイクロ秒の受信時刻なので、名前の昇順が受信順になる。
	// 別の issue のファイルが同名になった場合に順序が揺れないよう、パスで決着させる。
	sort.Slice(files, func(i, j int) bool {
		if files[i].name != files[j].name {
			return files[i].name < files[j].name
		}
		return files[i].path < files[j].path
	})

	events := make([]HookEvent, 0, len(files))
	for _, f := range files {
		events = append(events, f.event)
	}
	return events
}

// scanBudget は1回の走査で読み込んでよい残りの量である。
//
// 逃がし先は同じ利用者が書ける場所なので、置かれたものを無条件に全部読むと
// メモリを使い切って落ちる。落ちてもファイルは残るので、そのままだと起動のたびに
// 落ちる輪になる。読む量を区切り、残りは次の起動へ持ち越す。
type scanBudget struct {
	// remainingFiles はこの走査で読んでよい残りの件数である。
	remainingFiles int
	// remainingBytes はこの走査で読んでよい残りのバイト数である。
	remainingBytes int64
	// exhausted は上限に達したことを既にログへ出したかどうかである（同じ警告を繰り返さない）。
	exhausted bool
}

// scanPendingDir は1つの逃がし先を走査する。
//
// 走査するのは *.json にだけ一致する**通常ファイル**である。.json.tmp は必ず飛ばす
// （書き込み中である。設計 3-19）。broken/ などのディレクトリも飛ばす。
//
// **通常ファイルでないもの（名前付きパイプ・シンボリックリンクなど）は読まずに隔離する。**
// 名前付きパイプを1つ置かれるだけで、読む側は書き手が現れるまで開いたまま止まり、
// ReplayPending が無期限に返らなくなる（Close でも中断できない）。
//
// **大きさの上限は maxMessageBytes である**（socket の1行の上限と同じ）。
// 書く側（internal/hookclient）が書けるのは高々 DefaultMaxInputBytes なので、
// これを超えるものは continuo hook が書いたものではない。読まずに隔離する。
//
// dir: 走査する逃がし先ディレクトリ。
// budget: 1回の走査で読んでよい残りの量。読んだ分だけ減る。
// 戻り値: 読み出せた hook の一覧（並べ替えは呼び出し側が行う）。
func (s *Server) scanPendingDir(dir string, budget *scanBudget) []pendingFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.logger.Warn("hook の逃がし先を読めませんでした", "dir", dir, "error", err)
		return nil
	}

	var files []pendingFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// .json.tmp は「.json で終わらない」ので、この判定だけで飛ばせる。
		if !strings.HasSuffix(name, PendingFileExt) {
			continue
		}
		path := filepath.Join(dir, name)

		if !e.Type().IsRegular() {
			s.moveToBroken(dir, path, i18n.Errorf(i18n.KeyHookserverPendingNotRegularFile, e.Type()))
			continue
		}
		if budget.remainingFiles <= 0 || budget.remainingBytes <= 0 {
			if !budget.exhausted {
				budget.exhausted = true
				s.logger.Warn("1回の走査で読み戻す量の上限に達しました（残りは次の起動で読み戻します）",
					"dir", dir, "max_replay_events", maxReplayEvents, "max_replay_bytes", maxReplayBytes)
			}
			continue
		}

		data, err := s.readPendingFile(path)
		if err != nil {
			// 読めない理由がファイル自身にある（大きすぎる・通常ファイルでない）ものは
			// 隔離済みである。ここに来るのは入出力の失敗なので、消さずに残す。
			if !errors.Is(err, errPendingQuarantined) {
				s.logger.Warn("hook の逃がし先のファイルを読めませんでした（消さずに残します）",
					"path", path, "error", err)
			}
			continue
		}
		budget.remainingFiles--
		budget.remainingBytes -= int64(len(data))

		ev, err := decodeEvent(data)
		if err != nil {
			s.moveToBroken(dir, path, err)
			continue
		}

		if err := os.Remove(path); err != nil {
			// 消せないまま配送すると、次の起動で同じ hook を再生してしまう。
			// 二重に配送するより取りこぼしのほうが安全なので、この1件は配送しない。
			s.logger.Warn("読み戻した hook のファイルを消せませんでした（再生を避けるため、この1件は配送しません）",
				"path", path, "error", err)
			continue
		}

		files = append(files, pendingFile{path: path, name: name, event: ev})
	}
	return files
}

// errPendingQuarantined は readPendingFile が「読まずに隔離した」ことを表す印である。
// 呼び出し側は、この印のときだけ追加のログを出さない（隔離のときに既に出しているため）。
var errPendingQuarantined = errors.New("逃がし先のファイルを読まずに隔離しました")

// readPendingFile は逃がし先のファイル1件を、大きさの上限つきで読む。
//
// **os.ReadFile を使わない。**開いてから中身を確かめるまでの間に、通常ファイルでないものへ
// すり替えられる余地を消すためである。O_NONBLOCK を付けて開くので、名前付きパイプへ
// すり替えられていても書き手を待って止まることが無い。開いたあとに fstat で
// 通常ファイルかどうかと大きさを見る。
//
// path: 読むファイルの絶対パス。
// 戻り値: 読めた中身と、失敗した場合のエラー。読まずに隔離した場合は
// errPendingQuarantined を包んだエラーを返す。
func (s *Server) readPendingFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if !st.Mode().IsRegular() {
		s.moveToBroken(dir, path, i18n.Errorf(i18n.KeyHookserverPendingNotRegularFile, st.Mode()))
		return nil, fmt.Errorf("%w: %s", errPendingQuarantined, path)
	}
	limit := int64(s.maxMessageBytes)
	if st.Size() > limit {
		s.moveToBroken(dir, path, i18n.Errorf(
			i18n.KeyHookserverReadPendingFileTooLarge, limit, st.Size()))
		return nil, fmt.Errorf("%w: %s", errPendingQuarantined, path)
	}

	// fstat のあとに書き足された場合に備えて、読む量も上限で止める。
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// moveToBroken は解釈できなかった逃がし先のファイルを pending/broken/ へ移す。
//
// 消さないのは、原因（ディスクの障害か、人間が手で置いたファイルか）を人間が
// 確かめられるようにするためである（設計 3-19）。
//
// **同じ名前のファイルが隔離先に既にあっても上書きしない。**os.Rename は黙って上書きするので、
// 先に隔離してあったファイルが消えてしまう。名前がぶつかったら `-1` `-2` … と連番を足す。
// 名前を取るのに os.Link を使うのは、「存在したら必ず失敗する」ことがファイルシステムの
// 側で保証されるためである（存在確認と rename の2手に分けると、その隙間で取り違えうる）。
//
// dir: そのファイルが入っている逃がし先ディレクトリ。
// path: 移す対象のファイルの絶対パス。
// cause: 解釈できなかった理由（ログに出す）。
func (s *Server) moveToBroken(dir, path string, cause error) {
	brokenDir := filepath.Join(dir, BrokenDirName)
	if err := os.MkdirAll(brokenDir, 0o700); err != nil {
		s.logger.Warn("壊れた hook の隔離先を作れませんでした（ファイルはそのまま残します）",
			"dir", brokenDir, "path", path, "error", err)
		return
	}

	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 0; i < maxBrokenNameAttempts; i++ {
		dest := filepath.Join(brokenDir, base)
		if i > 0 {
			dest = filepath.Join(brokenDir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		}

		err := os.Link(path, dest)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			// hard link を張れないファイルシステム向けの逃げ道である。
			// 名前が空いていることを確かめてから os.Rename で移す。
			if _, statErr := os.Lstat(dest); statErr == nil {
				continue
			}
			if renameErr := os.Rename(path, dest); renameErr != nil {
				s.logger.Warn("壊れた hook を隔離先へ移せませんでした（ファイルはそのまま残します）",
					"path", path, "dest", dest, "link_error", err, "rename_error", renameErr)
				return
			}
			s.logger.Warn("hook の逃がし先に壊れた JSON があったので隔離しました",
				"path", path, "dest", dest, "error", cause)
			return
		}

		if err := os.Remove(path); err != nil {
			s.logger.Warn("壊れた hook を隔離先へ複製しましたが、元のファイルを消せませんでした"+
				"（次の起動でもう一度隔離を試みます）",
				"path", path, "dest", dest, "error", err)
			return
		}
		s.logger.Warn("hook の逃がし先に壊れた JSON があったので隔離しました",
			"path", path, "dest", dest, "error", cause)
		return
	}

	s.logger.Warn("壊れた hook の隔離先の名前が続けてぶつかりました（ファイルはそのまま残します）",
		"path", path, "dir", brokenDir, "attempts", maxBrokenNameAttempts)
}

// removeStaleTmpFiles は取り残された .json.tmp を消す（設計 3-19）。
//
// continuo hook が書いている途中で落ちた残骸であり、中身が不完全なので復元できない。
// 消したことは必ずログに残す（hook を1件失っていることに人間が気づけるようにする）。
//
// **更新時刻が staleTmpAge より新しいものは消さない。**それは「いま continuo hook が
// 書いている最中のもの」でありうる。消すと書く側の os.Rename が ENOENT で失敗し、
// その hook はどこにも残らずに消える（消えたのが Stop なら30分誰も気づかない）。
// 読む側は .json しか読まないので、残しても害は無い（次の起動で消える）。
//
// 起動時（ReplayPending の1回目の走査）にだけ呼ぶ。2回目の走査で呼ばないのは、
// そのときの .tmp は「いま continuo hook が書いている最中のもの」でありうるためである。
//
// dir: 走査する逃がし先ディレクトリ。
func (s *Server) removeStaleTmpFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.logger.Warn("hook の逃がし先を読めませんでした", "dir", dir, "error", err)
		return
	}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), PendingTmpExt) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			s.logger.Warn("書きかけの hook ファイルの更新時刻を調べられませんでした（消さずに残します）",
				"path", path, "error", err)
			continue
		}
		if age := now.Sub(info.ModTime()); age < staleTmpAge {
			s.logger.Info("書きかけの hook ファイルがまだ新しいので消しませんでした"+
				"（continuo hook が書いている最中の可能性があります。次の起動で消えます）",
				"path", path, "age", age, "stale_tmp_age", staleTmpAge)
			continue
		}
		if err := os.Remove(path); err != nil {
			s.logger.Warn("取り残された書きかけの hook ファイルを消せませんでした", "path", path, "error", err)
			continue
		}
		s.logger.Warn("取り残された書きかけの hook ファイルを消しました（中身が不完全なので復元できません。この hook は1件失われています）",
			"path", path)
	}
}
