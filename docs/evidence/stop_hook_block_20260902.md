# 差し戻しの最中に herdr が何を返すかを実機で測った記録（2026-09-02）

**何のために測ったか。**#166（応答が hook に差し戻されたのに、continuo は turn が終わったと
判断して次の指示を送る）の直し方は「空の `Stop` のあと `agent.get` を読み、`working` なら
待ち直す」である。**その土台は「差し戻しの最中、herdr は `working` を返す」だが、
これは誰も測っていなかった。**測らずに実装すると、裏取りが1件も拾わない可能性が残る。

**結論。****差し戻しの最中、herdr は一貫して `working` を返す。**
`Stop` hook が8秒かかる場合も含め、**プロンプトの投入から書き直しの終わりまで、
1度も `idle` を返さなかった。**

---

## 1. 測り方

**herdr 0.8.2 の pane に Claude Code を立て、`Stop` を1回だけ差し戻す hook を張った。**

| 何 | 中身 |
| --- | --- |
| 差し戻す hook | `{"decision":"block","reason":"…書き直してください…"}` を最初の1回だけ返す。2回目以降は何も返さない |
| hook が記録するもの | hook に入った時刻・`block` を返した時刻・`allow` の時刻・`stop_hook_active` の値 |
| 状態の読み取り | `~/.config/herdr/herdr.sock` へ `{"id":"…","method":"agent.get","params":{"target":…}}` を 0.1 秒ごとに送り、`agent_status` と `revision` を記録 |
| turn の投入 | `herdr agent prompt <名前> "…" --wait --until idle --until done --until blocked --timeout 120000`（**continuo が送るのと同じ形**） |

**hook の遅さを変えて2回測った。**Claude Code の版は 2.1.252、herdr は 0.8.2。

---

## 2. 測った結果

**基準（0秒）は、hook が `block` を返した瞬間である。**

| 何 | 速い hook | 遅い hook（8秒眠る） |
| --- | --- | --- |
| hook に入った | -0.00s | **-8.01s** |
| `block` を返した | 0.00s | 0.00s |
| 書き直しが終わった（`allow`） | +2.96s | +3.18s |
| `agent.prompt` の待ちが返った | +3.06s | +3.28s |

**`agent.get` が返した状態。**

| 経過 | 速い hook | 遅い hook |
| --- | --- | --- |
| プロンプト投入の直後 | `working` | `working` |
| **hook が走っている間** | （一瞬） | **`working`（8秒間ずっと）** |
| +0.5s / +1.0s / +1.5s / **+2.0s** | `working` | `working` |
| +2.5s | `working` | `working` |
| 書き直しが終わった直後 | `idle` | `idle` |

**`settle_ms` の既定（2000ms）が閉じる時点で、どちらも `working` である。**

---

## 3. この記録から言えること

| 言えること | 中身 |
| --- | --- |
| **裏取りは効く** | 差し戻しの最中に `agent.get` を読めば `working` が返る。**`idle` へ落ちる瞬間は観測されなかった** |
| **`agent.prompt` の待ちは差し戻しでは返らない** | herdr が `idle` を返さないため。**普通の turn では、continuo が書き直しの最中に窓を開けること自体が起きない**（[docs/plans/impl/issue166_stop_hook_block.md](../plans/impl/issue166_stop_hook_block.md) の 4 節に、窓が開く経路を書いてある） |
| **`stop_hook_active` が真になる実例が取れた** | 2回目の `Stop`（差し戻しのあと）で `true` だった。**1本目は `false` である**ので、この欄だけでは差し戻しを先読みできない |

**`agent.get` の往復は、速い hook の回で n=168、最小 0.31ms・中央 1.13ms・最大 1.77ms だった**
（0.1 秒ごとに接続し直した値。herdr は1接続1要求で閉じる）。
**turn の終わり1回につき1回しか呼ばないので、費用は無視できる。**

---

## 4. 測り直す人向け

**この記録を採ったスクリプトは残していない**（一時ディレクトリで動かした）。
**同じことをするには、次の3つを用意すれば足りる。**

1. `Stop` に `{"decision":"block","reason":"…"}` を1回だけ返す hook を張った、空のディレクトリ
2. そこを `--cwd` にして `herdr pane split` し、`herdr agent start <名前> --kind claude --pane <pane_id>`
3. `~/.config/herdr/herdr.sock` へ `agent.get` を 0.1 秒ごとに投げ続ける小さなスクリプト
   （**`id` は文字列である。**数値を入れると `invalid_request` が返る）

**hook 側で `block` を返した時刻を記録しておくこと。**それが基準になる。
