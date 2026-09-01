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
"""
import contextlib
import errno
import json
import os
import re
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


def verify_result(returncode, stdout, stderr):
    """確かめ方の実行結果から (通ったか, 見せる出力) を作る。

    **判定の順番。**

    1. 終了コードが 0 でなければ「未完了」。`grep -c` は0件のとき 1 を返す
    2. 出力が空なら「未完了」。grep が何も出さないのは「無い」ということである
    3. 出力が件数として読めるなら、**合計が 0 より大きいときだけ「済」**
    4. 件数として読めないなら、出力があること自体を「済」とみなす
    """
    out = (stdout or "").strip() or (stderr or "").strip()
    if returncode != 0:
        return False, out or "（終了コード %d）" % returncode
    if not out:
        return False, "（出力が空）"
    nums = counts_from_output(out)
    if nums is not None:
        return sum(nums) > 0, out
    return True, out


def run_verify(cmd, timeout=30):
    """確かめ方のコマンドを実行し、(通ったか, 出力) を返す。"""
    try:
        p = subprocess.run(cmd, shell=True, cwd=root(), capture_output=True,
                           text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "（%g 秒で打ち切りました）" % timeout
    return verify_result(p.returncode, p.stdout, p.stderr)
