#!/usr/bin/env python3
"""指示の記録（`.claude/requests/tasks.jsonl`）を読み書きする塊。

**なぜ1つにまとめるか。**`task-store.py` と `check-open-tasks.py` が、
**読み込みも保存も「確かめ方が通ったか」の判定も、同じコードを2つ持っていた。**
片方だけを直せば、同じ記録に対して2つの答えが出る。

**確かめ方の判定を、数字の寄せ集めでやらない。**
`internal/v2/config.go:0` の数字を全部つなげると `20` になり、**0件が「済」になる。**
`counts_from_output` は行ごとに件数を読み、**読めない行が1つでもあれば件数として扱わない。**

**書き込みは、同じディレクトリの一時ファイルへ書き切ってから差し替える**
（CLAUDE.md の「絶対に守る制約」4）。**並列で動くので、書く間はロックを取る。**

**`verify` は、実行する前に形を検査する**（`verify_rejection`）。
`verify` を書くのは LLM であり、記録は `.claude/requests/tasks.jsonl` にあるただのテキストである。
**turn を終えるたびに、そこに書いてあるものが shell で走る。**
通してよい形だけを並べて持ち、**それ以外は実行せずに、断った理由ごと人間と AI に見せる。**
"""
import contextlib
import errno
import json
import os
import re
import shlex
import subprocess
import tempfile
import time

try:
    import fcntl
except ImportError:  # POSIX 以外。ロック無しで動かす（このリポジトリの想定外の環境）
    fcntl = None


def root():
    """リポジトリのルート。hook からは `CLAUDE_PROJECT_DIR` が渡る。"""
    return os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()


def dir_path():
    return os.path.join(root(), ".claude", "requests")


def store_path():
    return os.path.join(dir_path(), "tasks.jsonl")


def lock_path():
    return os.path.join(dir_path(), "tasks.lock")


def load():
    """記録を読む。**壊れた行は捨てる。**1行の壊れで全部を失わないため。"""
    path = store_path()
    if not os.path.exists(path):
        return []
    rows = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    rows.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return rows


def save(rows):
    """**その場で空にしてから書かない。**途中で落ちると全部失う。

    同じディレクトリへ一時ファイルを作り、`flush` と `fsync` まで済ませてから
    `os.replace` で差し替える。**別のファイルシステムへ置くと不可分でなくなる**ので、
    置き場所は `dir_path()` に固定する。
    """
    path = store_path()
    d = dir_path()
    os.makedirs(d, exist_ok=True)
    # **元の権限を引き継ぐ。**無ければ 0600 にする（人間の発言がそのまま入るため）。
    mode = 0o600
    if os.path.exists(path):
        mode = os.stat(path).st_mode & 0o777
    fd, tmp = tempfile.mkstemp(prefix="." + os.path.basename(path) + ".", dir=d)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            for r in rows:
                f.write(json.dumps(r, ensure_ascii=False) + "\n")
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, path)
    except BaseException:
        # 差し替える前に落ちたら、一時ファイルを残さない。
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


@contextlib.contextmanager
def locked(timeout=10.0):
    """記録を書き換える間、ほかのセッションを待たせる。

    **`.claude/rules/parallel-work.md` が並列を既定にしている。**
    同じ `tasks.jsonl` を2つのセッションが読んで書けば、**後から書いたほうが前の追記を消す。**

    **ロックファイルは差し替えない。**差し替えるとロックが切れるので、
    CLAUDE.md の「絶対に守る制約」4 の例外にあたる（同じ制約が挙げている例外そのもの）。
    """
    if fcntl is None:
        yield
        return
    os.makedirs(dir_path(), exist_ok=True)
    f = open(lock_path(), "a+")
    try:
        deadline = time.monotonic() + timeout
        while True:
            try:
                fcntl.flock(f.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except OSError as e:
                if e.errno not in (errno.EACCES, errno.EAGAIN):
                    raise
                if time.monotonic() >= deadline:
                    raise TimeoutError(
                        "%.0f 秒待ってもロックが取れませんでした: %s"
                        % (timeout, lock_path())
                    )
                time.sleep(0.05)
        try:
            yield
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)
    finally:
        f.close()


# `grep -c` は `3` を、`grep -rc` は `path:3` を出す。**末尾の数字だけを件数として読む。**
_COUNT_LINE = re.compile(r"^(?:.*:)?(\d+)$")


def counts_from_output(out):
    """出力を「件数の並び」として読む。**読めない行が1つでもあれば None を返す。**

    None は「これは件数ではない」という意味である。**0 と混ぜてはならない。**
    """
    lines = [ln.strip() for ln in (out or "").splitlines() if ln.strip()]
    if not lines:
        return None
    nums = []
    for ln in lines:
        m = _COUNT_LINE.match(ln)
        if not m:
            return None
        nums.append(int(m.group(1)))
    return nums


def _one_line(text, limit=200):
    """複数行を1行に潰し、長ければ切る。**切ったことが分かるように … を付ける。**"""
    t = " ".join((text or "").split())
    return t if len(t) <= limit else t[:limit] + "…"


def verify_result(returncode, stdout, stderr):
    """確かめ方の実行結果から (通ったか, 見せる出力) を作る。

    **判定に使うのは標準出力だけである。標準エラーは見せるだけで、判定に混ぜない。**
    混ぜると、**警告を1行出しただけのコマンドが「済」になる。**
    実例（2026-09-02）。`grep -rc x dir/` が読めないディレクトリに当たると、
    終了コード 0・標準出力は空・標準エラーに `Permission denied` だけ、という結果になる。
    以前はこれを「出力がある」と数えて **0件を「済」にしていた。**

    **判定の順番。**

    1. 終了コードが 0 でなければ「未完了」。`grep -c` は0件のとき 1 を返す
    2. **標準出力**が空なら「未完了」。grep が何も出さないのは「無い」ということである
    3. 標準出力が件数として読めるなら、**合計が 0 より大きいときだけ「済」**
    4. 件数として読めないなら、標準出力があること自体を「済」とみなす
    """
    out = (stdout or "").strip()
    err = (stderr or "").strip()
    if returncode != 0:
        shown = out or err or "（終了コード %d）" % returncode
        return False, shown
    if not out:
        if err:
            return False, "（標準出力が空。標準エラー: %s）" % _one_line(err)
        return False, "（出力が空）"
    shown = out if not err else "%s ／ 標準エラー: %s" % (out, _one_line(err))
    nums = counts_from_output(out)
    if nums is not None:
        return sum(nums) > 0, shown
    return True, shown


# --------------------------------------------------------------- verify の検査
#
# **`verify` は LLM が書いた文字列であり、turn を終えるたびに shell で走る。**
# 確認も承認も挟まらないので、**通してよい形をここに並べて持ち、それ以外は実行しない。**
#
# 通す範囲は、いま実際に使っている3つに合わせてある
# （`grep -c` / `test -f` / `gh` の読み取り。`.claude/requests/tasks.jsonl` の実物で確認）。
# **書き込むもの・任意のコードを走らせるものは通さない。**
# 通信は `gh` の読み取りだけを通す（`gh release list` で release の有無を見るため）。

# 引数を見なくてよいもの。**読むだけで、書き込みも通信もしない。**
_PLAIN_COMMANDS = frozenset({
    "grep", "egrep", "fgrep", "rg",
    "test", "[",
    "echo", "printf", "true", "false",
    "cat", "head", "tail", "wc", "ls", "sort", "uniq", "cut", "tr",
    "basename", "dirname", "jq",
    # **待つだけのもの。**何も読まず何も書かない。
    # 打ち切りの仕組み（check-open-tasks.py の TOTAL_BUDGET）を試すのに要る。
    "sleep",
})

# 第1引数（サブコマンド）まで見て決めるもの。
_SUBCOMMANDS = {
    # 読むだけの git。**`add` / `commit` / `checkout` などは入れない。**
    "git": frozenset({
        "log", "show", "status", "diff", "grep", "blame",
        "ls-files", "ls-tree", "rev-parse", "cat-file", "describe", "shortlog",
    }),
    # このリポジトリが作るコマンド。**`version` だけ。**
    "continuo": frozenset({"version"}),
}

# `gh` は2階層まで見る。**読み取りだけを並べる。**
# `pr merge` / `pr ready` / `issue close` / `project item-edit` などは入っていない。
_GH_SUBCOMMANDS = frozenset({
    ("pr", "view"), ("pr", "list"), ("pr", "diff"), ("pr", "checks"), ("pr", "status"),
    ("issue", "view"), ("issue", "list"),
    ("release", "view"), ("release", "list"),
    ("run", "view"), ("run", "list"),
    ("workflow", "view"), ("workflow", "list"),
    ("repo", "view"),
    ("label", "list"),
    ("cache", "list"),
    ("auth", "status"),
    ("project", "view"), ("project", "list"),
    ("project", "item-list"), ("project", "field-list"),
    ("search", "issues"), ("search", "prs"), ("search", "code"), ("search", "repos"),
})

# `gh api` は GET だけ通す。**通す flag のほうを並べる。**
# **通さないほうを並べると、値を続けて書いた形（`-XPOST` / `-ffoo=bar`）が漏れる。**
# `--method` / `-X` / `-f` / `-F` / `--field` / `--raw-field` / `--input` は、
# ここに無いので通らない。
_GH_API_READ_FLAGS = frozenset({
    "--jq", "-q", "--template", "-t", "--paginate", "--slurp",
    "--cache", "--header", "-H", "--hostname", "--include", "-i",
    "--verbose", "--preview", "-p",
})

# コマンドを繋いでよい記号。**これ以外の記号は通さない。**
_SEPARATORS = frozenset({"&&", "||", "|", ";"})

# `shlex` が記号として切り出す文字（`punctuation_chars=True` の既定）。
# **この文字だけでできたトークンは、shell の演算子である。**
# **`_SEPARATORS` に無いものは全部断る。**書き込み（`>`）・別のプロセス（`&`）・
# 入れ子（`(`）はここで落ちる。**並べて持たずに「それ以外」で落とすのは、
# `|&` や `;;` のような、こちらが思いつかなかった綴りを取り逃がさないためである。**
_PUNCT_CHARS = "();<>|&"

# 単引用符で囲まれた部分。**中身は shell が展開しないので、危ない記号を探す前に外す。**
_SINGLE_QUOTED = re.compile(r"'[^']*'")


def _unquote(token):
    """トークンの前後の引用符を外す。**中身は見ない。**"""
    if len(token) >= 2 and token[0] == token[-1] and token[0] in ("'", '"'):
        return token[1:-1]
    return token


def _expansion_in(token):
    """トークン1つに展開の記号があれば、断る理由を返す。無ければ None。

    **単引用符の中は shell が展開しないので、先に外す。**
    `grep -v '^0$'` の `$` や `grep -c '\\`' f` の backtick で落とさないため。
    **外に出ている `$` と backtick だけを見る。**
    """
    bare = _SINGLE_QUOTED.sub("''", token)
    if "`" in bare:
        return "backtick は使えません（別のコマンドが走ります）: %s" % token
    if "$" in bare:
        return ("引用符の外に `$` があります。`$(…)` も `${…}` も別のコマンドや値に化けるので、"
                "**そのまま書いてください**: %s" % token)
    return None


def _check_gh_api(argv):
    """`gh api …` の argv を見て、断る理由を返す。

    **並べるのは通す flag のほうである。**通さないほうを並べると、
    `-XPOST` や `-ffoo=bar` のように**値を続けて書いた形**が漏れる
    （実測: 2026-09-02。`-X` と `--method` だけを見ていたので `-XPOST` が通った）。
    """
    for t in argv[1:]:
        u = _unquote(t)
        if not u.startswith("-"):
            continue          # サブコマンドや flag の値である
        if u == "--":
            continue
        head = u.split("=", 1)[0]
        if head in _GH_API_READ_FLAGS:
            continue
        # 短い flag は値を続けて書ける（`-qxxx`）。**先頭2文字で見る。**
        if len(head) > 2 and not head.startswith("--") and head[:2] in _GH_API_READ_FLAGS:
            continue
        return ("`gh api` は GET だけ通します。`%s` は通す flag に入っていません"
                "（通るのは %s）" % (head, " / ".join(sorted(_GH_API_READ_FLAGS))))
    return None


def _check_gh(argv):
    """`gh …` の argv を見て、断る理由を返す。通してよければ None。"""
    words = [_unquote(t) for t in argv[1:] if not _unquote(t).startswith("-")]
    if not words:
        return "`gh` にサブコマンドがありません"
    if words[0] == "api":
        return _check_gh_api(argv)
    pair = tuple(words[:2])
    if pair in _GH_SUBCOMMANDS:
        return None
    return ("`gh %s` は読み取りとして登録されていません。"
            "読み取りなら task_common.py の _GH_SUBCOMMANDS に足してください"
            % " ".join(words[:2]))


def _check_segment(argv):
    """1つのコマンド（パイプの1区切り）を見て、断る理由を返す。"""
    if not argv:
        return "空のコマンドがあります"
    name = os.path.basename(_unquote(argv[0]))
    if name in _PLAIN_COMMANDS:
        return None
    if name == "gh":
        return _check_gh(argv)
    if name in _SUBCOMMANDS:
        if len(argv) < 2:
            return "`%s` にサブコマンドがありません" % name
        sub = _unquote(argv[1])
        if sub in _SUBCOMMANDS[name]:
            return None
        return ("`%s %s` は読み取りとして登録されていません（`%s` で通るのは %s）"
                % (name, sub, name, " / ".join(sorted(_SUBCOMMANDS[name]))))
    return ("`%s` は確かめ方に使えません。通るのは %s と、`git` / `gh` / `continuo` の"
            "読み取りだけです" % (name, " / ".join(sorted(_PLAIN_COMMANDS))))


def verify_rejection(cmd):
    """確かめ方のコマンドを検査し、**断る理由**を返す。通してよければ None。

    **通すもの。**`grep -c …` / `test -f … && echo 1` /
    `gh pr view … | grep -c MERGED` のような、読むだけのコマンドを
    `&&` `||` `|` `;` で繋いだもの。

    **通さないもの。**書き込むコマンド、`>` などのリダイレクト、`$(…)` と backtick、
    `sh` / `python3` / `curl` のような何でも走らせられるコマンド。
    """
    text = (cmd or "").strip()
    if not text:
        return "確かめ方が空です"

    # **改行を先に断る。**`shlex` は改行を「ただの空白」として扱うので、
    # `echo 1\ntouch x` が1つのコマンドに見える。**shell は2つのコマンドとして走らせる。**
    # 実測（2026-09-02）。`run_verify("echo 1\ntouch <パス>")` が (True, '1') を返し、
    # **ファイルができた。**改行1つで、この検査は丸ごと素通しになっていた。
    bad = [ch for ch in text if ord(ch) < 32 and ch != "\t"]
    if bad:
        return ("改行や制御文字は使えません（U+%04X）。"
                "**shell はそこで別のコマンドを始めます。**1行で書いてください" % ord(bad[0]))

    lexer = shlex.shlex(text, posix=False, punctuation_chars=True)
    lexer.whitespace_split = True
    lexer.commenters = ""  # `#` を注釈として落とさない。grep の柄に入りうる
    try:
        tokens = list(lexer)
    except ValueError as e:
        return "コマンドとして読めません（%s）" % e

    segments, current = [], []
    for t in tokens:
        if t and all(ch in _PUNCT_CHARS for ch in t):
            # 記号だけのトークンは shell の演算子である。**繋ぐもの以外は断る。**
            if t in _SEPARATORS:
                segments.append(current)
                current = []
                continue
            return "`%s` は使えません（書き込みや別のプロセスにつながります）" % t
        # 展開の記号は、**トークンごとに**見る。
        # **コマンド全体で見てはならない。**単引用符を消す正規表現が語をまたいで対になり、
        # 間の backtick を隠す（実測: 2026-09-02。`grep -c "don't" f `id` "isn't"` が通った）。
        # `shlex` は引用符を尊重して切るので、トークンに割ったあとなら対をまたがない。
        why = _expansion_in(t)
        if why:
            return why
        current.append(t)
    segments.append(current)

    for argv in segments:
        why = _check_segment(argv)
        if why:
            return why
    return None


def run_verify(cmd, timeout=30):
    """確かめ方のコマンドを実行し、(通ったか, 出力) を返す。

    **通してよい形かを先に見る。**通らないものは**実行せず**、断った理由を出力として返す。
    """
    why = verify_rejection(cmd)
    if why:
        return False, "（通してよい形でないので実行していません: %s）" % why
    try:
        p = subprocess.run(cmd, shell=True, cwd=root(), capture_output=True,
                           text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "（%g 秒で打ち切りました）" % timeout
    return verify_result(p.returncode, p.stdout, p.stderr)
