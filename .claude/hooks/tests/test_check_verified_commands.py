#!/usr/bin/env python3
"""check-verified-commands.py が、狙った場合に止まり、狙わない場合に通ることを確かめる。

**いちばん上のケースが本題である。**「今回の事故そのもの」が止まらなければ、この hook に意味は無い。
前に作ったものは「その turn で Bash を呼んだか」しか見ておらず、**事故を素通りさせた。**

    python3 .claude/hooks/tests/test_check_verified_commands.py

**リポジトリのルートから実行すること。**
"""

import json, subprocess, tempfile, os, sys
LONG = "あ" * 200
HOOK = ".claude/hooks/check-verified-commands.py"

def run(rows):
    with tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False, encoding="utf-8") as f:
        f.write("\n".join(json.dumps(r, ensure_ascii=False) for r in rows))
        path = f.name
    out = subprocess.run(["python3", HOOK], input=json.dumps({"transcript_path": path}),
                         capture_output=True, text=True).stdout.strip()
    os.unlink(path)
    return "止まった" if out else "通った"

def user(t): return {"type":"user","message":{"role":"user","content":t}}
def bash(c): return {"type":"assistant","message":{"role":"assistant","content":[
    {"type":"tool_use","name":"Bash","input":{"command":c}}]}}
def say(t): return {"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":t+LONG}]}}

cases = [
 ("【本番】今回の事故そのもの（別のコマンドは実行した）", "止まった", [
   user("コマンドを教えて"),
   bash("which continuo && continuo version"),
   say("```bash\ncontinuo abandon https://github.com/x/y/issues/1 --dry-run\n```\n")]),
 ("書いたコマンドを実際に実行した", "通った", [
   user("コマンドを教えて"),
   bash("/tmp/co abandon --dry-run https://github.com/x/y/issues/1 /path"),
   say("```bash\ncontinuo abandon --dry-run <URL>\n```\n")]),
 ("実行していないが「未実行」と断った", "通った", [
   user("コマンドを教えて"),
   say("本番へ書き込むので**未実行**です。\n\n```bash\ncontinuo abandon --to Done <URL>\n```\n")]),
 ("フラグが足りない（--force を書いたが実行は --dry-run）", "止まった", [
   user("コマンドを教えて"),
   bash("continuo abandon --dry-run <URL>"),
   say("```bash\ncontinuo abandon --force <URL>\n```\n")]),
 ("実行時に余分なフラグを足していた（書いた分は含まれる）", "通った", [
   user("コマンドを教えて"),
   bash("continuo abandon --dry-run --force <URL> /path"),
   say("```bash\ncontinuo abandon --dry-run <URL>\n```\n")]),
 ("コマンドブロックが無い", "通った", [
   user("説明して"), say("これはこういう仕組みです。")]),
 ("text ブロック（出力の引用）は数えない", "通った", [
   user("出力を見せて"),
   say("```text\ncontinuo abandon --dry-run <URL>\n```\n")]),
 ("cd や echo だけの行は数えない", "通った", [
   user("手順は?"), say("```bash\ncd ~/work\necho hello\n```\n")]),
]

cases += [
 ("【既知の抜け道】別プログラムの同名サブコマンド", "通った", [
   user("手順は?"), bash("herdr version"), say("```bash\ncontinuo version\n```\n")]),
]

ng = 0
for name, want, rows in cases:
    got = run(rows)
    mark = "OK " if got == want else "NG "
    if got != want: ng += 1
    print(f"{mark} {name}\n     期待={want} / 実際={got}")
print(f"\n合わなかったもの: {ng} 件")
sys.exit(1 if ng else 0)

