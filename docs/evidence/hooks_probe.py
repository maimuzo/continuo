#!/usr/bin/env python3
"""Claude Code の hook から呼ばれ、stdin の JSON をそのまま JSONL に追記する観測用スクリプト。

引数1 = 呼び出し元のラベル（"Stop@wt-87" のような形）。
記録先は固定の絶対パス（hook 実行時の環境変数に依存しないため）。
"""
import datetime
import json
import os
import sys

PROBE_DIR = os.path.dirname(os.path.abspath(__file__))
LOG = os.path.join(PROBE_DIR, "probe.jsonl")

label = sys.argv[1] if len(sys.argv) > 1 else "(no-arg)"
raw = sys.stdin.read()

rec = {
    "label": label,
    "at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "hook_cwd": os.getcwd(),
    "hook_pid": os.getpid(),
    "hook_ppid": os.getppid(),
    "env_claude": {k: v for k, v in os.environ.items() if "CLAUDE" in k.upper()},
    "raw_len": len(raw),
}
try:
    rec["payload"] = json.loads(raw)
except Exception as exc:  # noqa: BLE001
    rec["parse_error"] = str(exc)
    rec["raw"] = raw

with open(LOG, "a", encoding="utf-8") as fh:
    fh.write(json.dumps(rec, ensure_ascii=False) + "\n")

# hook は何も返さない（exit 0 で通過させる）
sys.exit(0)
