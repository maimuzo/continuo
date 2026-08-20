package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// maxInputLine は1行の入力として受け付ける長さの上限である。
//
// 受け取るのは番号だけなので、これを超える行は貼り間違いである。上限を置かないと、
// 端末へ流し込まれた巨大な1行をそのまま全部読んでしまう。
const maxInputLine = 4096

// noOptionInput は「この役割に使える選択肢がボードに無い」を表す入力である。
//
// **一覧の番号は1から振るので、0 は既存の選択肢と衝突しない。**
const noOptionInput = 0

// 対話が最後まで進まなかった理由は sentinel error で返す。cmd/continuo はこれを見て
// 終了コードを決める。**理由の説明そのものは Assign が Out へ出し終えている。**
var (
	// ErrTooFewOptions は、ボードの選択肢が RoleCount 個に満たなかったことを表す。
	// **尋ねる前に止める。**足りないまま尋ねると、対話は必ず途中で行き止まる。
	ErrTooFewOptions = errors.New("ボードの Status の選択肢が足りません")
	// ErrNoSuitableOption は、利用者が 0 を入力したことを表す。
	// **その役割へ渡せる選択肢がボードに無い**という表明である。
	ErrNoSuitableOption = errors.New("役割に使える選択肢がボードにありません")
	// ErrInterrupted は、利用者が Ctrl+C で中断したことを表す。
	ErrInterrupted = errors.New("利用者が中断しました")
	// ErrInputClosed は、番号を待っている間に入力が終わったことを表す。
	ErrInputClosed = errors.New("入力が終わりました")
)

// Assignment は5つの役割へ割り当てた Status の選択肢名である。
type Assignment struct {
	// names は役割ごとに割り当てた選択肢名である。添字は Role の値。
	names [RoleCount]string
}

// Name は役割へ割り当てた選択肢名を返す。
//
// r: 役割。
// 戻り値: 割り当てた選択肢名。まだ割り当てていないか、範囲外の値なら空文字。
func (a Assignment) Name(r Role) string {
	if r < 0 || int(r) >= RoleCount {
		return ""
	}
	return a.names[r]
}

// Statuses は割り当てを WORKFLOW.md へ書き込む形へ直す。
//
// 戻り値: scaffold.UpdateStatuses に渡す値。
func (a Assignment) Statuses() scaffold.Statuses {
	return scaffold.Statuses{
		Dispatch: a.names[RoleDispatch],
		Running:  a.names[RoleRunning],
		Review:   a.names[RoleReview],
		Blocked:  a.names[RoleBlocked],
		Done:     a.names[RoleDone],
	}
}

// AssignOptions は Assign の入力である。
type AssignOptions struct {
	// FieldName は選択肢を読んだフィールドの名前である（画面に出すためだけに使う）。
	FieldName string
	// Options はボードから読んだ選択肢名である。**ボードの並び順のまま渡すこと。**
	Options []string
	// In は番号を読む先である（本番では os.Stdin）。
	In io.Reader
	// Out は説明・一覧・結果を出す先である（本番では os.Stdout）。
	Out io.Writer
}

// Assign は5つの役割それぞれに、ボードの Status の選択肢を1つずつ割り当てる。
//
// **役割の名前より先に「continuo がその Status で何をするか」を出してから番号を待つ。**
// 初見の利用者は、どの Status がどの役割かを知らないためである。
//
// **同じ選択肢を2つの役割へ割り当てさせない。**役割が重なると continuo が壊れる
// （着手待ちと完了が同じなら、取った直後の issue の worktree を片付ける。着手待ちと
// 作業中が同じなら、書き込んだ Status がそのまま次の候補になり同じ issue を取り続ける）。
// **重なったときは打ち切らず、同じ役割を尋ね直す。**打ち間違いはその場で直せるので、
// それまでの回答をすべて入れ直させる理由が無い。
//
// **ボードへは1文字も書かない。**選択肢が足りないとき・役割に渡せる選択肢が無いときは、
// GitHub の画面から足すよう案内して打ち切る。
//
// ctx: 中断を受け取るコンテキスト。**Ctrl+C はこれを取り消して伝える**
// （呼び出し側が signal.NotifyContext で作る）。
// opts: 選択肢と入出力。
// 戻り値の1つ目: 5つの役割すべてが埋まった割り当て。エラーのときはゼロ値。
// 戻り値の2つ目: ErrTooFewOptions / ErrNoSuitableOption / ErrInterrupted / ErrInputClosed、
// または入力を読めなかった理由。**なぜ止まったかの説明は Out へ出し終えている。**
func Assign(ctx context.Context, opts AssignOptions) (Assignment, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	fieldName := opts.FieldName
	if fieldName == "" {
		fieldName = DefaultStatusFieldName
	}

	// **尋ねる前に選択肢の数を確かめる**（RUCM の基本フロー5）。足りないまま尋ねると、
	// 何回か答えさせたあとで必ず行き止まる。利用者に無駄な入力をさせない。
	if len(opts.Options) < RoleCount {
		fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortTooFew, fieldName, len(opts.Options), RoleCount, RoleCount))
		writeAddOptionRemedy(out)
		return Assignment{}, ErrTooFewOptions
	}

	writeOptionList(out, fieldName, opts.Options)

	reader := newLineReader(opts.In)
	defer reader.close()

	var a Assignment
	// **どの選択肢がどの役割に埋まっているか**を引くための逆引き。添字は Options の添字。
	takenBy := make([]int, len(opts.Options))
	for i := range takenBy {
		takenBy[i] = -1
	}

	for turn, role := range roleOrder {
		for {
			fmt.Fprintln(out)
			fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptAsk, turn+1, RoleCount, role.ConfigKey(), role.Description()))
			fmt.Fprint(out, i18n.T(i18n.KeySetupPromptInput))

			line, err := reader.read(ctx)
			if err != nil {
				fmt.Fprintln(out)
				switch {
				case errors.Is(err, ErrInterrupted):
					fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortInterrupted))
				case errors.Is(err, ErrInputClosed):
					fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortInputClosed))
				}
				return Assignment{}, err
			}

			n, ok := parseNumber(line)
			if !ok {
				fmt.Fprintln(out, i18n.T(i18n.KeySetupErrNotANumber, strings.TrimSpace(line), len(opts.Options)))
				continue
			}
			// **範囲の検査を先に、0 の検査をあとにする**（RUCM の基本フロー10 と 11）。
			// 0 は「使える選択肢が無い」という意味を持つ入力なので、範囲外として弾かない。
			if n < noOptionInput || n > len(opts.Options) {
				fmt.Fprintln(out, i18n.T(i18n.KeySetupErrOutOfRange, len(opts.Options)))
				continue
			}
			if n == noOptionInput {
				fmt.Fprintln(out)
				fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortNoOption, role.ConfigKey(), RoleCount))
				writeAddOptionRemedy(out)
				return Assignment{}, ErrNoSuitableOption
			}

			idx := n - 1
			if other := takenBy[idx]; other >= 0 {
				fmt.Fprintln(out, i18n.T(i18n.KeySetupErrDuplicate, opts.Options[idx], Role(other).ConfigKey()))
				continue
			}

			a.names[role] = opts.Options[idx]
			takenBy[idx] = int(role)
			fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptAssigned, role.ConfigKey(), opts.Options[idx]))
			break
		}
	}

	writeSummary(out, a)
	return a, nil
}

// writeOptionList は選択肢を番号付きで並べ、答え方を案内する。
//
// out: 出力先。
// fieldName: 選択肢を読んだフィールドの名前。
// options: 選択肢の名前（ボードの並び順）。
func writeOptionList(out io.Writer, fieldName string, options []string) {
	fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptOptionsHeader, fieldName))
	for i, name := range options {
		fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptOptionLine, i+1, name))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptIntroCount, RoleCount))
	fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptIntroZero))
	fmt.Fprintln(out, i18n.T(i18n.KeySetupPromptIntroInterrupt))
}

// writeSummary は決まった割り当てを役割ごとに並べる。
//
// out: 出力先。
// a: 5つの役割が全部埋まった割り当て。
func writeSummary(out io.Writer, a Assignment) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, i18n.T(i18n.KeySetupSummaryHeader, RoleCount))
	for _, role := range roleOrder {
		fmt.Fprintln(out, i18n.T(i18n.KeySetupSummaryLine, role.ConfigKey(), a.names[role]))
	}
}

// writeAddOptionRemedy は選択肢の足し方を案内する。
//
// **「API で足すな」を必ず添える。**この警告が無いと、利用者は `gh project field-create` や
// `updateProjectV2Field` で足そうとする。選択肢の指定は全件の置き換えとして扱われるので、
// **設定済みの Status が全部消える。**
//
// out: 出力先。
func writeAddOptionRemedy(out io.Writer) {
	fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortRemedyUI))
	fmt.Fprintln(out, i18n.T(i18n.KeySetupAbortRemedyNoAPI))
}

// parseNumber は入力の1行を番号として読む。
//
// **前後の空白は落とす。**端末から貼り付けたときに空白が混ざるためである。
//
// line: 入力の1行。
// 戻り値の1つ目: 読み取った番号。
// 戻り値の2つ目: 10進の整数として読めたなら真。
func parseNumber(line string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0, false
	}
	return n, true
}

// lineReader は入力を1行ずつ読み、**コンテキストの取り消しで待ちを打ち切れるようにする。**
//
// io.Reader の Read は取り消せないので、読む側を goroutine に分けて、
// 「読めた行」と「コンテキストの取り消し」を select で待つ。
type lineReader struct {
	// lines は読めた行が流れてくる。
	lines chan string
	// errs は読み終わり（io.EOF）または読めなかった理由が1度だけ流れてくる。
	errs chan error
	// done は goroutine を止める合図である。close で伝える。
	done chan struct{}
}

// newLineReader は r を1行ずつ読む goroutine を起こす。
//
// **goroutine は r の Read で止まったままになりうる。**取り消したあとに
// 端末が1行を返すまで残るが、close で合図を受けたら送らずに戻るので、何も掴まない。
//
// r: 読む先。nil なら何も読めない（すぐ ErrInputClosed になる）。
// 戻り値: 読み手。使い終わったら close を呼ぶこと。
func newLineReader(r io.Reader) *lineReader {
	lr := &lineReader{
		lines: make(chan string),
		errs:  make(chan error, 1),
		done:  make(chan struct{}),
	}
	if r == nil {
		lr.errs <- io.EOF
		return lr
	}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 1024), maxInputLine)
		for sc.Scan() {
			select {
			case lr.lines <- sc.Text():
			case <-lr.done:
				return
			}
		}
		err := sc.Err()
		if err == nil {
			err = io.EOF
		}
		// errs は容量1なので、受け手が居なくてもここで止まらない。
		lr.errs <- err
	}()
	return lr
}

// read は1行を読む。
//
// ctx: 取り消しを受け取るコンテキスト。
// 戻り値の1つ目: 読めた1行（改行は含まない）。
// 戻り値の2つ目: 取り消されたら ErrInterrupted、入力が終わったら ErrInputClosed、
// それ以外に読めなかったらその理由。
func (l *lineReader) read(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ErrInterrupted
	case line := <-l.lines:
		return line, nil
	case err := <-l.errs:
		if errors.Is(err, io.EOF) {
			return "", ErrInputClosed
		}
		return "", err
	}
}

// close は読む goroutine に「もう要らない」と伝える。**2回呼んではならない。**
func (l *lineReader) close() {
	close(l.done)
}
