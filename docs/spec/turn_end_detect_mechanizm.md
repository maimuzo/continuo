# turn の終わりと「止まっている」の検知

**この文書は、continuo が「Claude Code の pane は止まっているか」を判断するために使える信号を、
1つずつ測って並べたものである。**

---

## 0. この文書の使い方

**言いたいこと。**この判定は7周のレビューで7回 Critical を出した。**毎回、同じ境目である。**
**条件が確定していなかったので、指摘のたびに場当たりで均衡を動かしていた。**
**この文書は、その条件を確定させるための土台である。**

| 何 | どうするか |
| --- | --- |
| **誰が読むか** | **この issue に限らない。**「止まっているか」を判断する必要が出た全ての作業で読む |
| **何が書いてあるか** | **使える信号の全部。**それぞれ「何を答えるか」「どう測ったか」「結果」「限界」の4つ |
| **なぜ手法まで書くか** | **同じ調査を何度もやり直しているためである。**この文書だけで再検証できる形にする |
| **間違いを見つけたら** | **その場で直す。**測り直した日付と、叩いたコマンドを添える |
| **実装へのリンクを直したら** | **中身で検算する。**行が空でないことを見るだけでは足りない（2026-09-08 に17本が隣の門へ着地していた。下のコマンド） |
| **測っていないことは** | **「測っていない」と書く。**空欄にしない |

**実装へのリンクを検算するコマンド。**
**この文書は `internal/` の行番号を70本以上で根拠にしている。**
**行を足すと、その下を指すものが全部ずれる。**

```sh
python3 - <<'EOF'
import re, pathlib
doc = pathlib.Path("docs/spec/turn_end_detect_mechanizm.md").read_text(encoding="utf-8")
for m in re.finditer(r'\.\./\.\./(internal/[a-z/]*\.go)#L(\d+)', doc):
    f, a = m.group(1), int(m.group(2))
    lines = pathlib.Path(f).read_text(encoding="utf-8").split("\n")
    print(f"{f}#L{a} | {lines[a-1].strip()[:70]}")
EOF
```

**出た行を目で読むこと。**「空行でない」で通してはならない。
**ずれていたら、目印の文字列**（関数名・`if` の条件・コメントの1行）**で測り直して貼り直す。**
**機械的に何行ずらす、は誤りである。**ずれ幅は場所ごとに違う。

**実測の環境。**特記が無ければ、次のとおりである。

| 何 | 値 |
| --- | --- |
| 測った日 | 2026-09-08 |
| herdr | **0.8.2**（`herdr --version`） |
| Claude Code | **2.1.263**（`claude --version`） |
| herdr のソース | **0.9.0 を clone した**（`v0.8.2` の tag は公開されていない）。**判定に使う点は実機でも裏を取ってある** |

**herdr のソースの取り方。**この文書は herdr のソースのファイル名と行番号を15箇所で根拠にしている
（検索パターン `src/[a-z_/]*\.rs:[0-9]*` で、重複を除いた `file:line` が15）。
**同じものを手元に出すコマンドを、ここに置く。**

```sh
git clone --depth 1 --branch v0.9.0 https://github.com/herdrdev/herdr.git
```

**行番号は upstream で動く。**版が違うときは、行番号ではなく**関数名と文字列**で探すこと
（例: `osc_title_working` / `full_lifecycle_hook_authority` / `state_change_seq`）。
**ずれていたら、この文書の側を測り直して直す。**

**Claude Code の公式ドキュメントの取り方。**この文書は `hooks.md` / `interactive-mode.md` /
`sub-agents.md` / `hooks-guide.md` の行番号を8箇所で根拠にしている。
**この checkout には1本も入っていない**（検索パターン `**/interactive*.md`、対象パスはリポジトリ全体で0件）。
**測ったのは docs.claude.com の版である**（2026-09-08 時点。Claude Code 2.1.263）。

| ファイル | どこから取るか |
| --- | --- |
| `hooks.md` | https://docs.claude.com/en/docs/claude-code/hooks.md |
| `hooks-guide.md` | https://docs.claude.com/en/docs/claude-code/hooks-guide.md |
| `interactive-mode.md` | https://docs.claude.com/en/docs/claude-code/interactive-mode.md |
| `sub-agents.md` | https://docs.claude.com/en/docs/claude-code/sub-agents.md |

**こちらも行番号は版で動く。**`Usage limit reached` や `stop_hook_active` のような
**原文の文字列で探すこと。**

---

## 1. 用語

**一般的でない語を、先に定義する。**

| 語 | 意味 |
| --- | --- |
| **pane** | herdr が管理する画面の1枚。continuo はここで Claude Code を起動する |
| **turn** | Claude Code へ1回指示を送ってから、その応答が終わるまで |
| **枠** | Claude の定額プランの上限。**3種類ある**（5時間ごと／1週間の全体／1週間のモデル別） |
| **余裕値** | `100 − 使用率 − マージン`。0以下なら「余裕が無い」 |
| **手放す** | その issue の担当者から自分を外し、他の機械が入札し直せる状態へ戻すこと |
| **打ち切る** | 固まったと判断して worker を止め、リトライを積むこと |
| **枠待ちの印** | その run が枠の回復を待っている、という continuo 側の記録。**打ち切りの時計を止める** |
| **無音** | **「hook が来ていない」ではない。**`LastSeenAt`（打ち切りの時計）から経った時間である。この時計は hook のほか、turn を送った時点・枠待ちを外した時点・`agent_status` が `working` だったのを確かめた時点にも進む（3-7） |
| **閾値** | `claude.turn_timeout_ms`（既定 3600000 ミリ秒＝1時間）。**0以下にすると打ち切りを行わない**（4-6） |
| **巡回** | continuo が全部の run を上から順に見て回ること。**間隔は `polling.interval_ms`（既定 30000 ミリ秒＝30秒。[internal/config/default.go:143](../../internal/config/default.go#L143)）。**この文書の「1巡回ぶん」「2巡回（既定60秒）」は、全部この値である |
| **面倒を見ている** | 手放しの側が「この run はこちらで始末する」と名乗ること。**名乗った run は、同じ巡回の打ち切りが飛ばす**（4-4） |

---

## 2. 判定が要る場面は3つある。混ぜてはならない

**4節が条件を確定させるのは、この3つである。**

| 問い | 誰が使うか | 誤るとどうなるか |
| --- | --- | --- |
| **A. この run は進んでいるか** | 打ち切り（`checkStalls`）。**毎巡回・全 run** | 進んでいる run を殺す／固まった run を放置する |
| **B. この pane は完全に止まっているか** | 枠が尽きた run の手放し（`paneStopped`）。**まれ** | 動いている pane を閉じて書きかけを失う／永久に手放せない |
| **C. この run は枠で止まっているのか** | 枠待ちの印（`isQuotaWaiting`） | 打ち切りの時計を止めるべきでない run で止める／止めるべき run で止めない |

**この3つを1つの関数で答えようとして、過去に失敗している**（5節。9行10件）。

### 2-1. `agent_status` を読んで進退を決める場所は、この3つだけではない

**4節の条件は、上の3つにしか当てはまらない。**
**下の7箇所は、同じ信号を読んでいるが、別の問いに答えている。**
**4節をそのまま持ち込んではならない。**

**実測（2026-09-08。2026-09-08 に数え直した）。**検索パターン
`AgentStatusWorking\|AgentStatusIdle\|AgentStatusDone\|AgentStatusBlocked`、
対象パス `internal/orchestrator/`（`_test.go` を除く）。**17行・9関数。**
うち2関数が問A（`checkStalls`）と問B（`paneStopped`）なので、**下の表は残り7つである。**
**問C は1行も当たらない。**`isQuotaWaitingWith` は `agent_status` を読まず、
使用率と `runIdleForTurnTimeout` だけで決める
（[internal/orchestrator/turn.go:785-792](../../internal/orchestrator/turn.go#L785-L792)）。

| どこ | 何を決めるか | どこへ倒すか | 読めなかったら |
| --- | --- | --- | --- |
| **`confirmStartup`**（[internal/orchestrator/dispatch.go:1513-1541](../../internal/orchestrator/dispatch.go#L1513-L1541)） | 起動できたと見なすか、`agent.start` をやり直すか | **`idle`/`done` かつ `interactive_ready` が成功。**`working` は `herdr.startup_timeout_ms` まで待ち、**超えたら起動失敗**。`blocked` は `esc` を送って失敗 | **3通りに割れる。**(一) `agent_not_found` で、作業中にしか出ない hook が届いていれば `ErrStartupBusy`（やり直さない。[internal/orchestrator/dispatch.go:1496-1503](../../internal/orchestrator/dispatch.go#L1496-L1503)）。(二) `agent_not_found` で届いていなければ `ErrStartupRetryable`（やり直す）。**(三) それ以外の読み取り失敗は包まないので、やり直さずに失敗する**（[internal/orchestrator/dispatch.go:1507-1510](../../internal/orchestrator/dispatch.go#L1507-L1510)） |
| **`sendTurn`**（[internal/orchestrator/turn.go:515-523](../../internal/orchestrator/turn.go#L515-L523)） | 待ち受けが返った直後、引き渡すか turn の終わりを確かめるか | `blocked` なら引き渡し。**それ以外は全部 `confirmTurnEnd` へ**（`working` と `unknown` も。「想定外なので `Stop` を確かめてから判断する」） | — |
| **`afterWaitTimeout` の待ち直し**（[internal/orchestrator/turn.go:601-611](../../internal/orchestrator/turn.go#L601-L611)） | 枠待ちの最中に待ちを終えるか | `blocked` なら引き渡し。`idle` かつ `Stop` を受けていれば終わり | — |
| **`confirmTurnEnd`**（[internal/orchestrator/turn.go:976-991](../../internal/orchestrator/turn.go#L976-L991)） | 差し戻して書き直させている最中を、終わったと読むか | **順に3つ見る。**(一) 枠待ちなら待ちへ。(二) `blocked` なら引き渡し。**(三) 書き直しを待っている窓でだけ、`working` でなければ turn の終わりとして進む** | **進む側**（(三) の条件に `stErr != nil` が入っている） |
| **`stillWorkingAfterStop`**（[internal/orchestrator/turn.go:1236](../../internal/orchestrator/turn.go#L1236)） | 空の `Stop` のあと turn を終えるか、待ち直すか | `working` なら**待ち直す** | 終える側 |
| **`afterQuotaReset`**（[internal/orchestrator/turn.go:703-717](../../internal/orchestrator/turn.go#L703-L717)） | 枠明けに継続の指示を送るか | **`idle`/`done` が「送ってよい」。`working` は「送らない」** | **送る側** |
| **復元の引き継ぎ**（[internal/orchestrator/restore.go:733-764](../../internal/orchestrator/restore.go#L733-L764)） | 再起動後、その pane をどう引き継ぐか | `working` なら turn の終わりを待つ。**`blocked` は `failure_state` へ落としてから pane を閉じる**（[internal/orchestrator/restore.go:743-759](../../internal/orchestrator/restore.go#L743-L759)。turn を送ると保留中の権限要求が承認されるため） | **pane を閉じる**（[internal/orchestrator/restore.go:760-764](../../internal/orchestrator/restore.go#L760-L764) の `default:`。`unknown` も同じ枝。**`failure_state` へは落とさない**） |

**`blocked` は3通りに扱われている。**問A は打ち切り、問B は「止まっているに含めない」、
**復元は `failure_state` へ落とす。**揃えようとするときは、3つとも見ること。

**倒す向きは3通りある。**同じ信号の同じ失敗が、問いごとに違う解決をされている。

| 向き | どこ |
| --- | --- |
| **`working` が「進んでいる」** | 問A・`stillWorkingAfterStop`・`confirmTurnEnd` |
| **`idle`/`done` が「止まっている」** | 問A・問B |
| **`idle`/`done` が「介入してよい」** | **`afterQuotaReset`。**読めなかったときも「送る側」へ倒す |

**これは誤りではない。**問いが違うので、間違えたときの損も違う。
**`confirmTurnEnd` の `working` でなければ進む**を問A・問B に合わせて書き換えてはならない。
**差し戻して書き直させている最中を「終わった」と読むことになる**——5-1 の 2026-09-02 の事故である。

**5節の9行10件のうち、問A・問B・問C に属するのは4件である。**
**残りは、この表の場所と、turn の終わりの判定と、subagent の追跡で起きている。**
**条件を強くしても、その6件は塞がらない。**

---

## 3. 使える信号

### 3-1. herdr の `agent_status`

**何を答えるか。**herdr が画面と端末タイトルを正規表現で照合した結果。

| 値 | 意味（herdr のソースの doc コメント） |
| --- | --- |
| `idle` | エージェントが終わり、プロンプトが見えていて、何も起きていない。**かつ tab が人間に見られた** |
| `done` | **`idle` と同じ状態のうち、tab がまだ人間に見られていないもの** |
| `working` | エージェントが実際に働いている／処理している |
| `blocked` | 人間の入力を必要としていて、返答待ちで止まっている |
| `unknown` | **ただのシェル、または認識できないプログラム。**agent を特定できていない pane だけ |

**どう測ったか。**herdr のソース `src/detect/mod.rs:11-20`（`AgentState`）と
`src/app/api_helpers.rs:96-107`（`pane_agent_status`）を読んだ。
**`Done` は `AgentState` に無く、API へ出すときに `(Idle, seen=false)` から作られる。**

```rust
// src/app/api_helpers.rs:96-107
match (state, seen) {
    (AgentState::Idle, false) => AgentStatus::Done,
    (AgentState::Idle, true)  => AgentStatus::Idle,
    (AgentState::Working, _)  => AgentStatus::Working,
    (AgentState::Blocked, _)  => AgentStatus::Blocked,
    (AgentState::Unknown, _)  => AgentStatus::Unknown,
}
```

**限界が2つある。**

**(一) 時間のしきい値が1つも無い。**判定は画面と端末タイトルの照合だけである。
`src/detect/` の本番コードに時間の定数は無い（`git grep -c "Duration\|Instant" -- src/detect/` が返すのは
`src/detect/mod.rs` の3行だけで、それは `#[cfg(test)]` の中）。
**つまり「何秒黙っていたら idle」ではない。**

**(二) どの規則にも当たらなかったときの既定値が `idle` である。**
`src/detect/manifest.rs:565-573` の `fallback_explain` が、Claude と分かっている pane では `AgentState::Idle` を返す。
**「本当にプロンプトが見えている `idle`」と「herdr が読めなかった `idle`」が、`agent.get` の戻り値では区別できない。**

**長い1回のツール呼び出しで黙っている間は `working` のままである。**
**実測（2026-09-08。herdr 0.8.2）：**自分の pane を2秒おきに60回読みながら `go test` を1回叩き、
**60サンプル全部が `working` だった。**

**測り直すときはこう叩く。**

```sh
# 別の pane で、時間のかかるツール呼び出しを1つ走らせておく（例）
go test ./... > /dev/null 2>&1 &

# その pane の agent 名を控えてから、2秒おきに60回読む
for i in $(seq 60); do
  herdr agent get "<agent 名>" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["agent"]["agent_status"])'
  sleep 2
done | sort | uniq -c
```

**この形は、結果から組み直したものである。**2026-09-08 に叩いた1行そのものは記録していない。
**次に測る人は、上の形で測って、結果と一緒にコマンドも残すこと。**

**6節が残している宿題**（`Task`（subagent）・`WebFetch`・MCP の呼び出し中も `working` か）**は、
`go test` の代わりにその道具を走らせれば、同じ形で測れる。**

### 3-2. herdr の `revision`（画面の版）— **continuo の使い方では動かない**

**何を答えるか。****画面ではない。**端末タイトルから装飾を落とした本文が変わった回数である。

**どう測ったか。**

```
$ herdr agent list      # 3秒おきに40回、2分間（2026-09-08）
```

| agent | 2分間の観測 | `revision` | `state_change_seq` |
| --- | --- | --- | --- |
| `continuo-continuo-246` | 40回とも `working` | **ずっと 1** | ずっと 1378 |
| `continuo-continuo-245` | 40回とも `working` | **ずっと 1** | ずっと 1382 |
| `continuo-continuo-173` | 12回 `working` → 28回 `done` | **ずっと 1** | **1384 → 1385** |

**なぜ動かないか。**herdr のソース上、`revision` を増やす経路は3つしかない。

| 場所 | 何が起きたとき |
| --- | --- |
| `src/terminal/state.rs:227-241` | **端末タイトルの、装飾を落とした本文が変わったとき** |
| `src/app/api/panes.rs:1732-1740` | `pane.report_metadata` で揮発性の付加情報が書き換わったとき |
| `src/app/actions.rs:425-432` | その付加情報が TTL で消えたとき |

**continuo は `pane.report_metadata` を呼ばない**（`git grep -n "report_metadata" -- internal/` は型のコメントだけ）。
**だから continuo にとって `revision` は端末タイトルの本文の版そのものである。**

落とす装飾は、点字1文字か `·✢✳✶✻✽◐◓◑◒` の10文字（`src/terminal/title.rs:1-23`）。
**`agent_status` が `working` を出す根拠にしている、まさにその文字である。**

**continuo は端末タイトルを設定しない**（`git grep -n 'terminal_title\|SetTitle' -- internal/` は読み取り用の型だけ）。
**書いているのは Claude Code 自身で、continuo の pane では `<owner>/<repo>#<番号>` を最後まで変えない。**

**「恒真」とは書かない。**同じ機械の対話用の pane は `revision: 2` を返した
（タイトルの本文が会話の要約で、途中で1度変わったため）。
**正しくは「continuo の使い方では動かない」である。**
**これは仕組みの保証ではなく、いまの Claude Code の振る舞いの観測である。**

### 3-3. herdr の `state_change_seq`（状態が変わった連番）

**何を答えるか。**その agent の状態が最後に変わった時点の通し番号。

**どう測ったか。**上の40サンプルに加えて、ソースで裏を取った。

```rust
// src/app/actions.rs:1874-1879
if change.previous_state != change.state {
    self.next_agent_state_change_seq += 1;
    if let Some(terminal) = self.terminals.get_mut(&terminal_id) {
        terminal.last_agent_state_change_seq = Some(self.next_agent_state_change_seq);
    }
}
```

**通し番号そのものは全体で1本である**（実測で 1378 / 1382 / 1384 と別々なのはそのため）。
**だが刻み直すのは、状態が実際に変わった当の terminal だけである。**
**他の agent が動いても、この agent の値は動かない。**

**`idle` と `done` の行き来では動かない。**あの2つの違いは `pane.seen`（人間がその tab を見たか）で、
**内部の状態は同じ `Idle` である**（3-1 の表）。**人間が herdr の画面を覗いても、この連番は動かない。**

**`agent.get` でも返る。**実測（2026-09-08）。

```
$ herdr agent get continuo-continuo-173
  返ってきた欄: agent, agent_session, agent_status, cwd, focused, foreground_cwd,
                interactive_ready, name, pane_id, revision, state_change_seq, tab_id, ...
  state_change_seq = 1401
  revision = 1
  agent_status = working
```

**`pane.list` では返らない。**実測で、`pane` の欄は
`agent_status` / `cwd` / `focused` / `foreground_cwd` / `pane_id` / `revision` / `scroll` / `tab_id` / `terminal_id` / `workspace_id` の10個だけだった。
**復元の経路は pane から読むので、そこでは連番を取れない。**

**限界が2つある。**

**(一) `working` が続いている間は動かない。**上の40サンプルで、働き続けた2つは2分間まったく動かなかった。
**「動いているか」ではなく「状態が変わっていないか」を答える信号である。**

**(二) `omitempty` である**（`internal/herdr/types.go` の `json:"state_change_seq,omitempty"`）。
**欄を返さない herdr の版では、全 agent が 0 として読まれる。**
**0 を「変わっていない」と読むと、`revision` と同じ恒真へ戻る。**

**herdr を再起動すると 0 から振り直される**（`src/app/mod.rs:487` と `src/app/state.rs:1059` の
`next_agent_state_change_seq: 0`）。**continuo は再起動を検知できない。**

### 3-4. herdr の `agent.explain`

**何を答えるか。**判定の内訳。**「本当に `idle`」と「規則に当たらなかった `idle`」を見分けられる唯一の口である。**

**どう測ったか。**

```
$ herdr agent explain continuo-continuo-173 --json
```

| 欄 | 何を答えるか | 実測値 |
| --- | --- | --- |
| **`fallback_reason`** | **どの規則にも当たらなかったか。**当たっていれば `null` | `null` |
| **`matched_rule`** | 当たった規則（`id` / `priority` / `region` / `state`） | `{"id":"osc_title_working","priority":1100,…}` |
| **`screen_detection_skipped`** | 画面からの検知を飛ばしたか | `false` |
| `visible_idle` / `visible_working` / `visible_blocker` | 画面から直接そう見えたか | `false` / `true` / `false` |

**`fallback_reason` が `null` でないのは2通りだけである**（`src/detect/manifest.rs`）。
`"unknown_agent"`（agent のラベルを解釈できない）と
`DEFAULT_KNOWN_AGENT_IDLE_FALLBACK`（**どの規則にも当たらず、既知の agent なので `Idle` を返した**）。

**限界が2つある。**

**(一) `screen_detection_skipped` は Claude では常に偽である。**
`src/app/agents.rs:387` が `terminal.full_lifecycle_hook_authority_active()` を渡し、
`src/detect/mod.rs:316-326` の `full_lifecycle_hook_authority` は
pi / omp / mastracode / opencode / kilo / kimi の6つしか真にしない。**`claude` は入っていない。**
**判定の条件に足しても、1度も真偽を分けない。**

**(二) agent を引けないときは `agent_not_found` を返す**（`src/app/api/agents.rs:246-262`）。
**`agent.get` と同じ誤りで返るので、「読めない」の見分けには使えない。**

### 3-5. `agent.read` で読んだ画面の本文

**何を答えるか。****唯一の本物の内容の信号である。**

**どう測ったか。**`--source visible` で読んだ本文の sha256 を、4秒おきに3回取った。

```sh
for i in 1 2 3; do
  herdr agent read "<agent 名>" --source visible | shasum -a 256 | cut -c1-16
  sleep 4
done
```

**`w4:p7` は、そのとき人間が対話に使っていた pane の識別子である。**環境ごとに違う。
**`herdr pane list` で自分の環境の識別子を探すこと。**

| 対象 | `agent_status` | 3回のハッシュ（先頭16文字） |
| --- | --- | --- |
| `continuo-continuo-245` | **working** | `74f60d7d21e12e52` / `fabf70fc55b98976` / `f17e9fd3f735f6f8`（**3回とも違う**） |
| `w4:p7`（人間が対話中の pane） | **idle** | `b54a50efd070f64a` / 同じ / 同じ（**3回とも同じ**） |

**働いている pane では4秒で本文が変わり、止まっている pane では変わらない。**

**限界が3つある。**

**(一) 戻り値の `revision` は使えない。**herdr が 0 を固定で入れている
（`src/app/api/agents.rs:237` と `src/app/api/panes.rs:1522`）。**自分でハッシュを取る必要がある。**

**(二) 「止まっているのに変わる」を測っていない。**
画面の下段には `⏵⏵ don't ask on … · ← 3 agents · ↓ to manage` のような行があり、
**背景の agent が増減すると、メインの turn が1バイトも進んでいなくても変わる。**
**真を返し続けると、打ち切りが永久に効かなくなる。**

**(三) `idle` の観測は、人間が対話に使っている pane で取った。**
**continuo が起動した pane が `idle` のときは測れていない**（測った時点で3つとも `working` だった）。

### 3-6. Claude Code の hook

**continuo が張っているのは8種類である**（`internal/orchestrator/settings.go` の `hookEventNames`）。
`Stop` / `UserPromptSubmit` / `SubagentStop` / `SubagentStart` / `Notification` /
`SessionStart` / `PreToolUse`（matcher `*`）/ `PostToolUse`（matcher `*`）。

**hook の無音では、原理的に判定できない。**2026-08-18 の実測（設計 1-3）。

- **道具の実行中は hook が1つも飛ばない。**45秒の道具で 45.107〜45.116 秒、90秒の道具で 90.115 秒
- **turn が終わったあとの無音は 60.040〜60.058 秒で `Notification`（`idle_prompt`）に破られる**（12回中12回）
- **turn 内の無音（90秒）が turn 外の無音（60秒）を追い越すので、どこに線を引いても分離できない**

**枠を待つ間は、その60秒の心拍も止まる。**公式 `hooks.md` 2228行。

> Expect `idle_prompt` about 60 seconds after Claude finishes responding, and only if you haven't typed since.
> **Claude Code doesn't send `idle_prompt` while it waits for a claude.ai usage limit to reset.**
> When the wait ends on its own, one of the `quota_auto_resume_*` types fires instead.
>
> **訳。**`idle_prompt` は Claude が応答を終えて約60秒後に来る。それ以降タイプしていない場合に限る。
> **claude.ai の利用上限のリセットを待っている間、Claude Code は `idle_prompt` を送らない。**
> **待ちが自然に終わったときは、代わりに `quota_auto_resume_*` のいずれかが飛ぶ。**

**枠で turn が終わっても `Stop` は飛ばない。**公式 `hooks.md` 2464-2466行。

> Runs when the main Claude Code agent has finished responding. Does not run if
> the stoppage occurred due to a user interrupt. **API errors fire StopFailure instead.**

**`StopFailure` の `error` に `rate_limit` が入る**（同 322 / 2574 / 2584行）。
**continuo は `StopFailure` を張っていない**（`git grep -c StopFailure -- internal/` が0）。
**`-- internal/` を落としてはならない。**この文書自身が当たって非0を返す。

**ただし、対話モードでは turn が終わらない。**公式 `interactive-mode.md` 606行。

> When a claude.ai usage limit stops Claude mid-task, **Claude Code waits in the open session**
> and continues the task on its own after the limit resets.
> **Automatic continue is on by default in interactive sessions signed in with a claude.ai subscription.**
> Requires Claude Code v2.1.234 or later.

**待ちが明けたことは `Notification` で届く**（同 313 / 2212-2214行）。

| 型 | 意味 |
| --- | --- |
| `quota_auto_resume_fired` | 上限で止めた作業を続ける |
| **`quota_auto_resume_stale`** | **リセットが、機械が約30分より長く眠っている間に起きた。Claude Code は続けずに `Enter` を待つ** |
| `quota_auto_resume_disabled` | 待ちを終えるが、作業を続けない |

**continuo は `Notification` を matcher 無しで張っているので、この3つは既に届いている。**
**`git grep -c quota_auto_resume -- internal/` が0件。読まずに捨てている。**
**ここでも `-- internal/` を落とさない。**この文書自身が当たる。

### 3-8. continuo が持っている、走行中の subagent の一覧

**何を答えるか。****人間が与えた「止まっている」の定義に、いちばん直接に答える信号である。**

> 今paneの内容が動いていたらそれが止まるまで待って**(つまりそのセッションのサブエージェントを含め完全停止するまで待って)**、止まったらすぐに担当を変更して…

**実体。**[internal/orchestrator/runstate.go:994](../../internal/orchestrator/runstate.go#L994) の `runningSubagentList`。
`SubagentStart` の hook で名前を足し、`SubagentStop` で外す。

**問B（手放し）には使えない。**一度は3つ目の条件にして、取り下げた（5-2 の1件目）。
**一覧を空にする経路が `SubagentStop` の hook と次の turn の始まりの2つしか無く、
枠待ちの最中はどちらも起きない**（[internal/orchestrator/reconcile.go:416](../../internal/orchestrator/reconcile.go#L416) が
「`runningSubagentList()` は使えない」と書いている）。
**枠が尽きた瞬間に subagent が走っていた run は、一覧が永久に空にならない。**

**それでも、いまも使っている場所が1つある。**
[internal/orchestrator/turn.go:192](../../internal/orchestrator/turn.go#L192) が、
`blocked` で人間へ引き渡す直前に `waitForRunningSubagents`
（[internal/orchestrator/turn.go:314-345](../../internal/orchestrator/turn.go#L314-L345)）を呼ぶ。
**5-2 の「取り下げた」は、問B の条件から外したという意味であって、コードから消したという意味ではない。**

**限界。**上と同じ理由で、**枠待ちの最中に subagent を抱えた run が `blocked` へ落ちると、
`waitForRunningSubagents` は猶予（`claude.poll_wait_ms`。既定30秒）を丸ごと使い切ってから
「走行中のまま `esc` を送ります」を出す**（[internal/orchestrator/turn.go:336-340](../../internal/orchestrator/turn.go#L336-L340)）。
**これは 5-2 の記録から導ける帰結であって、実際に観測したものではない。**

**2-1 の表には出てこない。**あの表は `agent_status` の定数を検索して作ったので、
**`agent_status` を読まない判定は構造上1つも入らない**（問C も同じ理由で入っていない）。

### 3-7. continuo が自分で持っている時計

| 何 | 何を答えるか | 進む条件 |
| --- | --- | --- |
| **`LastSeenAt`** | 打ち切りの時計 | **run を作った時点**（[internal/orchestrator/runstate.go:474](../../internal/orchestrator/runstate.go#L474)）／hook を受けた／turn を送った／枠待ちを外した／**`agent_status` が `working` だったのを確かめた**（`noteWorking`）。**枠待ちの間は進めない** |
| **`LastHookAt`** | 最後に hook を受けた時刻 | **どの hook でも進む**（`SessionStart` と `Notification` を含む） |
| **`LastBusyHookAt`** | **turn を処理している間にしか出ない hook** を最後に受けた時刻 | `UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `SubagentStart` / `SubagentStop` / `Stop` の6つだけ。**turn をまたいで持ち越す** |
| **`hookSeenThisTurn`** | この turn で hook を1件でも受けたか | **`beginTurn` が毎 turn 偽へ戻す** |

**`hookSeenThisTurn` の落とし穴。**`runIdleForTurnTimeout` はこれを先に見て、
**偽なら経過を測らずに「進んでいない」と答える。**
**`beginTurn` が毎 turn 偽へ戻すので、指示を送った直後は必ずそう読まれる。**

---

## 4. 確定した条件

**言いたいこと。**3つの問いは、答える信号が別々である。**1つの信号で3つに答えようとしたのが、10件の失敗の原因である。**
**`agent_status` が、いちばん大事な区別（働いている／入力を待っている）を単独で付けられる。**
**しかも、その値は既に手元にある。**

### 4-0. 信号と問いの対応

| 信号 | 問A（進んでいるか） | 問B（完全に止まっているか） | 問C（枠で止まっているか） |
| --- | --- | --- | --- |
| **`agent_status`** | **使える。**`working` なら進んでいる | **使える。**`idle` / `done` が必要条件 | 使えない |
| **`state_change_seq`** | **使えない**（`working` が続く間は動かない） | **使える。**「その間に状態が変わっていない」を証明する | 使えない |
| **`revision`** | **使えない**（continuo の pane では動かない） | **使えない**（同上） | 使えない |
| **hook の無音** | **単独では使えない**（turn 内90秒 > turn 外60秒） | 補助として使える | **使えない**（枠待ち中は心拍も止まる） |
| **`agent.read` の本文** | **使えるはずだが、危険側を測っていない** | **判定したい状態そのもので測っていない**（`idle` の観測は人間が使う pane で取った。3-5 の限界(三)） | 使えない |
| **使用量 API** | 使えない | 使えない | **使える**（いま使っている） |
| **`quota_auto_resume_*`** | 使えない | 使えない | **使えるが、読んでいない** |

### 4-1. 問A「この run は進んでいるか」（打ち切り）

**確定した条件。**

```
agent_status が working                         →  進んでいる。段2 へ行かずに次の run へ
agent.get が誤りを返した                        →  進んでいない側へ倒し、段2 へ
それ以外（idle / done / blocked / unknown）     →  進んでいない。段2 へ

段2: 枠待ちか（4-3）                            →  枠待ちなら印を立てて次の run へ。打ち切らない
段3: 枠待ちでもない                             →  打ち切る
```

**この箱は `paneStopped` の契約ではなく、`checkStalls` の段1 である。**
**4-2 と同じく、その前に門がある。**

#### 上の箱は段1 だけである。打ち切りまでには、あと5つの門と段2 がある

**`checkStalls` は、`agent.get` を呼ぶ前に5つの `continue` を通す**
（[internal/orchestrator/reconcile.go:616-640](../../internal/orchestrator/reconcile.go#L616-L640)）。
**箱だけを実装すると、この5つと段2 が落ちる。**

| 門 | どこ | 無いとどうなるか |
| --- | --- | --- |
| **枠待ちの印が立っていない** | [internal/orchestrator/reconcile.go:618-622](../../internal/orchestrator/reconcile.go#L618-L622) | 枠明けを待っている run を打ち切る |
| **手放しが面倒を見ていない**（`releasing`） | [internal/orchestrator/reconcile.go:623-631](../../internal/orchestrator/reconcile.go#L623-L631) | **90〜99%の帯で、打ち切りが毎回先に殺し、手放しが1回も成立しない。**枠が足りないだけの issue が `failure_state` へ落ちる（5-3 の事故） |
| **バックオフ中でない** | [internal/orchestrator/reconcile.go:632-634](../../internal/orchestrator/reconcile.go#L632-L634) | リトライ待ちの run を毎巡回また打ち切る |
| **agent 名を持ち、`LastSeenAt` がゼロでない** | [internal/orchestrator/reconcile.go:635-637](../../internal/orchestrator/reconcile.go#L635-L637) | まだ起動していない run を打ち切る |
| **無音が閾値を超えている** | [internal/orchestrator/reconcile.go:638-640](../../internal/orchestrator/reconcile.go#L638-L640) | **turn を送った直後の run を打ち切る** |

**そして段1 のあとに段2 がある**（[internal/orchestrator/reconcile.go:684-697](../../internal/orchestrator/reconcile.go#L684-L697)）。
**枠待ちなら印を立てて次の run へ進み、打ち切らない。**
**実際に打ち切るのは、段2 を通り抜けた先の
[internal/orchestrator/reconcile.go:702](../../internal/orchestrator/reconcile.go#L702) の `abandonRunAsync` である。**

**2つ目の門を落とすと、この issue が直そうとしている症状そのものが戻る。**

**根拠。**`agent_status` は、長いツール呼び出しの最中でも `working` を返す。
**実測：**`go test` を走らせながら2秒おきに60回読み、**60サンプル全部が `working` だった。**
**これが「1つの指示に何時間かかっても打ち切らない」という約束を果たす唯一の信号である。**

**実装した**（2026-09-08。issue #173）。
[internal/orchestrator/reconcile.go:670-677](../../internal/orchestrator/reconcile.go#L670-L677) が
`agentInfo` の応答の `agent.AgentStatus` を見て、`working` なら
[internal/orchestrator/runstate.go:1215](../../internal/orchestrator/runstate.go#L1215) の
`noteWorking` で `LastSeenAt` を進め、その巡回を飛ばす。
**`agent.get` を追加で叩いてはいない。**同じ応答を、ログに載せる代わりに判定へ回しただけである。

**それまでは、この値をログの1行に載せるだけで、判定には `agent.Revision` を使っていた。**
**あれは continuo の pane では動かない**（3-2）。
**つまり、正しい値を手に持ったまま、動かない値で判定していた。**

#### 読めなかったときは、打ち切る側へ倒す。問B と逆である

**4-2 は「読めなかった」と「止まっている」を必ず分ける。問A は分けない。**
**わざとである。**間違えたときに失うものが逆だからである。

| 問い | 読めなかったら | 間違えたときに失うもの |
| --- | --- | --- |
| **問A（打ち切り）** | **打ち切る側へ倒す**（[internal/orchestrator/reconcile.go:678-681](../../internal/orchestrator/reconcile.go#L678-L681) が `Warn` を出して段2 へ落とす。**打ち切るのは段2 を通り抜けた先である**） | worker が止まり、リトライが1つ積まれる。**担当も worktree もこの機械に残る** |
| **問B（手放し）** | **手放さない側**（4-2） | `git push`・担当者・pane・会話の文脈。**取り返しがつかない** |

**そのうえで、読み取りの失敗だけで打ち切ることはない。**
**入口に「無音が閾値を超えた」の門があり**（[internal/orchestrator/reconcile.go:638-640](../../internal/orchestrator/reconcile.go#L638-L640)）、
**hook が1件でも届いていれば `LastSeenAt` が進むので、そこまで落ちてこない。**
**そのあとにも段2（枠待ちか）がある。**

**ただし、その救済が効かない場面が1つある。**
**5-1 の2026-09-05 は「動いている本人の hook を捨てていた」事故である。**
**hook が届かない事故と `agent.get` の失敗が重なると、動いている run が打ち切られる。**
**重なった実例は観測していない。**

#### 限界(一): `working` の見た目のまま固まった run は、誰も止めない

`agent_status` は画面への正規表現の照合だけで決まり、**時間の閾値を1つも持たない**（3-1）。
Claude Code の process が1回のツール呼び出しの途中で固まっても、
画面には `esc to interrupt` の行が残るので、herdr は `working` を返し続ける。

**打ち切りが飛ばされるだけではない。**`working` を確かめるたびに `noteWorking` が
`LastSeenAt` を進めるので、**手放しの側も時間の門で止まる**
（[internal/orchestrator/reconcile.go:372-374](../../internal/orchestrator/reconcile.go#L372-L374)）。
`agent_status` を見る前に落ちるので、**4-2 の条件を緩めても手放せない。**
**その run は pane とスロットを握ったまま、continuo を再起動するまで残る。**

**この形を承知のうえで採っている。**逆にすると、**長い1回のツール呼び出しを毎回殺す**——
それが 5-1 の6件の症状そのものだからである。
**時間の閾値で救うなら、`working` が続いた時間の上限を別に持つことになる。**
**その値は測っていない**（6 の「測っていないこと」）。

#### 限界(二): `working` の根拠は、端末タイトルのスピナー1文字である

**この文書の3つの記述を並べると出てくる。**

| どこ | 何と書いてあるか |
| --- | --- |
| 3-2 | 落とす装飾は、点字1文字か `·✢✳✶✻✽◐◓◑◒` の10文字（`src/terminal/title.rs:1-23`） |
| 3-4 の実測 | `matched_rule` が `{"id":"osc_title_working","priority":1100,…}` |
| 3-1 の限界(二) | **どの規則にも当たらなかったときの既定値が `idle` である** |
| 3-1 の限界(一) | **時間のしきい値が1つも無い** |

**つまり、Claude Code が端末タイトルへ書くスピナーの文字が変わるか消えると、
`osc_title_working` が当たらなくなり、herdr は既定値の `idle` を返し続ける。**
**その run は「それ以外」に入り、閾値のあとで打ち切られる。**

**どこまで悪いか。****この変更の前とまったく同じ振る舞いに戻るだけである。**
`revision` は continuo の pane では永久に動かなかったので（3-2）、
**「長いツール呼び出しの run を、閾値のあとで必ず打ち切る」は、2026-09-08 まで実際にそうなっていた。**
**新しく下回るわけではない。**それでも、**いま唯一の防波堤がその1文字に乗っている**ことは変わらない。

**`agent.explain` の `fallback_reason` は、この場面を見分けられる唯一の口である**（3-4）。
**採らない。**4-2 が同じ道具を退けたのと同じ理由で、
**「規則が当たらなくなる頻度」を測っていないので、判定を1つ増やす価値を測れない**（6節に載せた）。

#### 限界(三): `blocked`（人間の入力待ち）も打ち切る

**4-2 は `blocked` を「止まっている」に含めない。**閉じると確認の画面ごと消えるためである。
**問A は含める。**「それ以外」に入るので、閾値のあとで pane を閉じる。

**振る舞いを変えない。**`blocked` を打ち切りからも外すと、
**その run を止める者が1人もいなくなる**（手放しも `blocked` を通さない）。
**turn ループが生きていれば、そちらが先に拾って引き渡しへ回す**
（[internal/orchestrator/turn.go:980-982](../../internal/orchestrator/turn.go#L980-L982)）。
**`checkStalls` まで落ちてくるのは、turn ループが死んでいる run だけである**（4-5 の #1）。
**打ち切りは、そこでの最後の安全網である。**

**`state_change_seq` を問A に使ってはならない。**
`working` が続く間は動かないので、**長いツール呼び出しでは `revision` と同じく恒真である。**
**そのうえ、状態が往復する run**（巡回のたびに `idle` → `working` → `idle`）**では毎回動くので、
永久に打ち切れなくなる。**

### 4-2. 問B「この pane は完全に止まっているか」（手放し）

**確定した条件。**

```
agent_status が idle か done                 かつ
state_change_seq が2回続けて同じ             かつ
その連番が 0 でない                          →  止まっている
```

**根拠。**

| 条件 | なぜ要るか |
| --- | --- |
| **`agent_status` が `idle` か `done`** | **`working` / `blocked` / `unknown` は「止まっている」ではない。**`blocked` を閉じると確認の画面ごと消える。`unknown` は「読めなかった」である |
| **連番が2回続けて同じ** | **`agent_status` だけでは、30秒あけた2回の読み取りの間に `working` の山が入っていても気づけない。**連番なら、間に1度でも状態が変われば値が動く |
| **連番が 0 でない** | **`omitempty` なので、欄を返さない herdr の版では全 agent が 0 になる。**0 を「同じ」と読むと恒真へ戻る |

**「読めなかった」と「止まっている」を、必ず分ける。**
`agent.get` が誤りを返したとき、`agent_status` が `working` / `blocked` / `unknown` のとき、
連番が 0 のとき——**この3つは「止まっている」ではない。**
**そして「面倒を見ている」とも名乗らない。**名乗ると打ち切りからも守ることになり、止める者が1人もいなくなる。

**`agent.explain` は要らない。**「読めなかった `idle`」を見分けられるが、
**`screen_detection_skipped` は Claude では常に偽で、`fallback_reason` は `agent_not_found` の場面では返らない。**
**判定を1つ増やすだけの価値が測れていない。**

#### 上の3条件は `paneStopped` の契約であって、手放しの条件ではない

**手放しは、`paneStopped` を呼ぶ前に5つの門を通す**
（[internal/orchestrator/reconcile.go:325-398](../../internal/orchestrator/reconcile.go#L325-L398)）。
**この5つを落として上の3条件だけを実装すると、健全な run を手放す。**

| 門 | どこ | 無いとどうなるか |
| --- | --- | --- |
| **agent 名を持っている** | [internal/orchestrator/reconcile.go:336-338](../../internal/orchestrator/reconcile.go#L336-L338) | まだ起動していない run を手放す |
| **バックオフ中でない** | [internal/orchestrator/reconcile.go:339-341](../../internal/orchestrator/reconcile.go#L339-L341) | **打ち切られて pane を閉じた run へ `agent.get` を投げ続ける。**毎巡回1行ずつログが積まれる（4-5 の #5 が減らそうとしているものである） |
| **1週間の枠の余裕が無く、待つ上限を超えている** | [internal/orchestrator/reconcile.go:346-348](../../internal/orchestrator/reconcile.go#L346-L348) | **枠と無関係に手放す。**手放しは枠のための仕組みである |
| **`LastSeenAt` がゼロでない** | [internal/orchestrator/reconcile.go:369-371](../../internal/orchestrator/reconcile.go#L369-L371) | 時計を持たない run で、経過を 1970 年から測る |
| **無音が閾値に達している／`runIdleForTurnTimeout` が真** | [internal/orchestrator/reconcile.go:372-378](../../internal/orchestrator/reconcile.go#L372-L378) | **指示を送った直後の run が「進んでいない」と読まれ、`idle` が2回続いた時点で手放される。**turn の開始から2巡回（既定60秒）である |

**4つ目がいちばん効く。**別の機械が入札し直し、**同じ worktree に2本目の Claude Code が立つ。**

### 4-3. 問C「この run は枠で止まっているのか」（枠待ちの印）

**確定した条件（いまのまま）。**

```
使用率100の枠がある  かつ  runIdleForTurnTimeout が真  →  枠待ち
```

**`runIdleForTurnTimeout` は「閾値のあいだ hook が来ていない」ではない。**
[internal/orchestrator/turn.go:812-824](../../internal/orchestrator/turn.go#L812-L824) は
**`hookSeenThisTurn` が偽なら、経過を測らずに真を返す。**
`beginTurn` が毎 turn 偽へ戻すので、**指示を送った直後は必ず真である**（3-7 の落とし穴）。

**それでも枠待ちと誤判定しないのは、外側に門があるからである。**
`checkStalls` は [internal/orchestrator/reconcile.go:638](../../internal/orchestrator/reconcile.go#L638) で
無音が閾値を超えたことを確かめてから、この判定へ入る。
**この述語を別の場所から呼ぶときは、同じ門を自分で置くこと。**
**置かないと、指示を送った直後の run が枠待ちと名乗り、打ち切りの時計が止まったまま戻らない。**

#### 印を立てる場所は2つある。述語を呼ぶ3箇所のうち2つには無音の門が無い

**印（`setWaitingQuota`）を立てるのは、巡回と turn ループの2箇所である。**
**述語（`isQuotaWaiting`）を呼ぶのは3箇所で、うち2つは無音の門を持たない。**

**実測（2026-09-08）。**検索パターン `setWaitingQuota|isQuotaWaiting`、
対象パス `internal/orchestrator/`（`_test.go` を除く）。

| どこ | 無音の門 | 何をするか |
| --- | --- | --- |
| **巡回**（[internal/orchestrator/reconcile.go:684-693](../../internal/orchestrator/reconcile.go#L684-L693)） | **ある**（reconcile.go:635） | 印を立てて、打ち切りの時計を止める |
| **待ち受けが時間切れになったとき**（[internal/orchestrator/turn.go:562-580](../../internal/orchestrator/turn.go#L562-L580)） | **無い** | 述語が真なら印を立て、枠明けまで待つ |
| **turn の終わりを確かめる窓**（[internal/orchestrator/turn.go:976](../../internal/orchestrator/turn.go#L976)） | **無い** | 述語が真なら枠待ちの待ちへ移る |

**下の2つに門が無い理由は、同じではない。**

| どこ | 何が守っているか |
| --- | --- |
| [internal/orchestrator/turn.go:562](../../internal/orchestrator/turn.go#L562) | **窓そのものが `claude.turn_timeout_ms` である。**閾値ぶん待ち切ったあとに呼んでいる |
| [internal/orchestrator/turn.go:976](../../internal/orchestrator/turn.go#L976) | **窓は `settle_ms`（既定2秒）か `poll_wait_ms`（既定30秒）で、閾値ではない。**守っているのは「そこへ着くのは `Stop` を1件でも受けた turn だけ」という性質であり、**そのとき `hookSeenThisTurn` は真なので、`runIdleForTurnTimeout` は経過を実際に測る** |

**「窓が閉じたから安全」ではない。**2秒の窓でも免除が成り立つ、と読んではならない。
**免除が成り立つ条件は次の2つで、どちらかを満たすことである。**

```
その経路に着く時点で hookSeenThisTurn が真になっている      （経過を実際に測る）
または、既に claude.turn_timeout_ms を待ち切っている        （門と同じことをした）
```

**新しく `isQuotaWaiting` を呼ぶ場所を足すときは、この2つのどちらかを確かめること。**
**どちらも満たさない場所から呼ぶと、`hookSeenThisTurn` が偽の run に枠待ちの印が立つ。**
**そのとき打ち切りの時計は止まったまま戻らない。**

**塞げていない経路が2つある。**
**`confirmTurnEnd` を `strictFirstWait = false` で呼ぶのは3箇所で、免除が成り立つのは1つだけである**
（検索パターン `confirmTurnEnd\(`、対象パス `internal/orchestrator/`。`_test.go` を除く）。

| 呼び出し | 免除は成り立つか |
| --- | --- |
| [internal/orchestrator/turn.go:498](../../internal/orchestrator/turn.go#L498) | **成り立つ。**直前の `agent.prompt` を `claude.turn_timeout_ms` で待ち切っている（[internal/orchestrator/turn.go:481](../../internal/orchestrator/turn.go#L481)） |
| [internal/orchestrator/turn.go:712](../../internal/orchestrator/turn.go#L712)（`afterQuotaReset`） | **成り立たない。**枠待ちの間は hook が来ないので `hookSeenThisTurn` が偽のまま |
| [internal/orchestrator/turn.go:130](../../internal/orchestrator/turn.go#L130)（`awaitTurnEnd` の枝） | **成り立たない。**turn を1度も送らないので `beginTurn` が呼ばれず、`hookSeenThisTurn` が偽のまま |

**3つ目へ着く原因は3つある。**再起動で `working` の pane を引き継いだとき
（[internal/orchestrator/restore.go:742](../../internal/orchestrator/restore.go#L742)）、
`turnTransient` のあと（[internal/orchestrator/turn.go:273](../../internal/orchestrator/turn.go#L273)）、
起動の確認が `ErrStartupBusy` へ倒れたとき（[internal/orchestrator/dispatch.go:1333](../../internal/orchestrator/dispatch.go#L1333)）。
**3つ目だけは、その経路自体が「作業中の hook が届いている」ことを条件にしているので、免除が成り立つ。**

**つまり、再起動で引き継いだ、実際に動いている run に、30秒で枠待ちの印が立ちうる。**
**実例は観測していない**（6節に載せた）。**印が立っても打ち切りの時計が止まるだけで、
枠が明ければ `clearQuotaWaitWhenBack` が外す。**

**この表の作り方に注意すること。**上の 4-3 の表は `afterWaitTimeout` を
「窓そのものが `claude.turn_timeout_ms` である」と説明しているが、
**`afterWaitTimeout` の呼び出しは2つあり**（[internal/orchestrator/turn.go:562](../../internal/orchestrator/turn.go#L562) と
[internal/orchestrator/turn.go:977](../../internal/orchestrator/turn.go#L977)）**、
後者の窓は `settle_ms` か `poll_wait_ms` で、閾値ではない。**
**呼び出し元が複数ある述語を「1つだけ見て安全と書く」のが、この節で2度起きた誤りである。**

**この節が数えている述語は2つある。**表は `setWaitingQuota` と `isQuotaWaiting` を数えたが、
**門を要求している述語は `runIdleForTurnTimeout` である。**
それを直接呼ぶのは [internal/orchestrator/turn.go:791](../../internal/orchestrator/turn.go#L791) と
[internal/orchestrator/reconcile.go:376](../../internal/orchestrator/reconcile.go#L376) の2箇所で、
**後者は上の門を自分で置いている**（[internal/orchestrator/reconcile.go:369-375](../../internal/orchestrator/reconcile.go#L369-L375)）。

#### `checkStalls` では、`working` が問C を短絡する

**打ち切りの段1 が `working` を見つけると `continue` するので、段2 の問C へ落ちてこない**
（[internal/orchestrator/reconcile.go:670-676](../../internal/orchestrator/reconcile.go#L670-L676)）。
**そのうえ `noteWorking` が `LastSeenAt` を進めるので、次の巡回では入口の門も超えられない。**

**つまり、枠待ちの pane が herdr に `working` と見えている場合、
巡回の側では枠待ちの印が永久に立たない。**
**その pane がどう見えるかは測っていない**（6節の1行目）。
**turn ループ側の2箇所は `agent_status` を見ないので、そちらでは立つ。**
**印が立てば打ち切りの時計は止まるので、実害は「巡回のログに枠待ちが出ない」ことである。**
**線を余裕値へ下げてはならない。**使用率90%では Claude Code は普通に応答する。
**そこで打ち切りの時計を止めると、本当に固まった run が5時間の枠が90%を割るまで殺されない。**
**既定では最大6時間、スロットと pane を握り続ける。**

**将来、`quota_auto_resume_*` を読めば、この推測は要らなくなる。**
**既に届いているのに読み捨てている**（3-6）。**とくに `quota_auto_resume_stale` は
「リセットは済んだが `Enter` を待って止まっている」を意味し、使用量の API からは
「回復した」としか見えない状態である。**

### 4-4. 3つの判定が同じ run を取り合わないための決まり

**問A と問B は、同じ巡回で同じ run を触る。**過去の Critical はここに集中している。

| 決まり | なぜ |
| --- | --- |
| **「面倒を見ている」と名乗れるのは、問B が `idle`/`done` と連番を読めたときだけ** | それ以外は問B の経路で二度と進まない。**打ち切りに任せないと、止める者がいなくなる** |
| **守るのは、1回目の観測の直後の1巡回だけ** | 2回目以降も守ると、状態が往復する run が永久に守られる |
| **手放しを撃ったあとは守らない** | 撃って失敗し続ける run を守ると、打ち切りもリトライも `failure_state` も来ない |
| **枠の写しは、1回の巡回で1回だけ読む** | 2回読むと、判定した写しとログに出す数字が別の読み取りから作られる |
| **段1（`working` か）を段2（枠待ちか）より前に置く** | 枠待ちの2条件は「枠を待っている」と「長い1つの仕事をしている」を区別できない。**後ろに置くと、正常に走っている run が枠待ちと名乗り、打ち切りの時計が止まったまま戻らない**（[internal/orchestrator/reconcile.go:560-562](../../internal/orchestrator/reconcile.go#L560-L562)） |

#### 上の決まりは、同時発火を防いでいない。防いでいるのは `beginTerminal` である

**3行目の「手放しを撃ったあとは守らない」は、撃った run を打ち切りの本体まで落とす。**
その run は `WaitingQuota`（90〜99%の帯では偽）・
`releasing`（**下の `handling` と同じ `map` である。**[internal/orchestrator/reconcile.go:319](../../internal/orchestrator/reconcile.go#L319) で `handling` として作り、
[internal/orchestrator/reconcile.go:583](../../internal/orchestrator/reconcile.go#L583) が `releasing` として受ける。撃った run は入っていない）・
無音の閾値（既に超えている）・`agent_status`（`idle`/`done`。撃つ条件そのもの）・
`isQuotaWaitingWith`（100%未満なので偽）**を全部通り、`abandonRunAsync` に到達する。**

**二重に走らないのは、手放しも打ち切りも `beginTerminal()` を同期で取るからである。**
[internal/orchestrator/runstate.go:1979-1989](../../internal/orchestrator/runstate.go#L1979-L1989) が
`terminating || Finished` を見て `terminalTaken` を返し、**2人目は何もせずに戻る。**

**呼び出しは6箇所ある**（検索パターン `beginTerminal\(\)`、対象パス `internal/`。`_test.go` を除く。
定義を除いて6行）。

| どこ | 何のために取るか |
| --- | --- |
| [internal/orchestrator/handoff.go:684](../../internal/orchestrator/handoff.go#L684) | 枠が尽きた run の手放し |
| [internal/orchestrator/lifecycle.go:697](../../internal/orchestrator/lifecycle.go#L697) | **巡回の側の**打ち切り |
| [internal/orchestrator/runstate.go:2018](../../internal/orchestrator/runstate.go#L2018) | `claimTerminal`。**turn ループの側の打ち切り**（[internal/orchestrator/lifecycle.go:680](../../internal/orchestrator/lifecycle.go#L680) の `abandonRun` が使う）。**打ち切りの入口は2つある** |
| [internal/orchestrator/lifecycle.go:573](../../internal/orchestrator/lifecycle.go#L573) | 正常な終了 |
| [internal/orchestrator/lifecycle.go:761](../../internal/orchestrator/lifecycle.go#L761) | 人間への引き渡し |
| [internal/orchestrator/unknownstate.go:552](../../internal/orchestrator/unknownstate.go#L552) | 状態を読めなくなった run の始末 |

**戻り値は3つある。**[internal/orchestrator/runstate.go:1985-1987](../../internal/orchestrator/runstate.go#L1985-L1987) が
`rewriting` のとき `terminalRewriting` を返す。
**そのときの振る舞いは、呼び出し側で2つに割れる。**

| 呼び出し側 | `terminalRewriting` を受けたら |
| --- | --- |
| **巡回の5箇所**（`handoff.go:684` / `lifecycle.go:573`・`697`・`761` / `unknownstate.go:552`） | **`!= terminalClaimed` で黙って戻る。**その巡回では走らない |
| **`claimTerminal`**（[internal/orchestrator/runstate.go:2017-2034](../../internal/orchestrator/runstate.go#L2017-L2034)） | **書き戻しの終わりを待ってから取り直す。**`switch` に `terminalRewriting` の枝が無いので、下の `select` へ落ちる |

**つまり「書き戻しの間はどの経路も止まる」ではない。**
**turn ループ側の打ち切り**（`abandonRun`）**は、書き戻しが終わった直後に走る。**
**待たずに戻ると、turn の上限に達した run が Status も動かさず、
引き渡しのコメントも出さず、印も外れないまま残る**
（[internal/orchestrator/runstate.go:1992-1997](../../internal/orchestrator/runstate.go#L1992-L1997) がそう書いている）。

**この節を読んで実装する人は、その集合だけを作ってはならない。**
**それだけだと、5-3 が記録した事故——打ち切りと手放しが競走し、
枠が足りないだけの issue が `failure_state` へ落ちる——がそのまま戻る。**

### 4-5. 10件の検証の結果（2026-09-08）

**follow-up として切り出そうとしたものを、この issue の中で検証した。**
**2件は「起きない」だった。**

| # | 何を疑ったか | 判定 | 中身 | いまどうなっているか |
| --- | --- | --- | --- | --- |
| **1** | turn ループが戻ったまま起き直せない | **条件付きで起きる** | turn ループは死ぬ。既定では打ち切りが1時間後に拾う。**`claude.turn_timeout_ms` が0以下の機械では誰も拾わない** | **残っている。**4-6 に書いた |
| **2** | 枠が短いと、信頼していないリポジトリの案内が出ない | **起きない** | **未信頼は `Dispatchable == false` になり、枠の門より前の枝で `preflight` を通る**（`internal/orchestrator/dispatch.go:520-531`）。**隙間は信頼のキャッシュの30秒だけ** | **直すものが無い** |
| **3** | `released` を書けなくても成功を返す | **起きる**（人間が情報を失う） | **`Reason` は機械が読まない**（読み手は `From` だけ）。**失うのは「push 済みか」を人間が grep する1行である** | **直した。**commit `232150a`。その場の1行に帰結を書いた |
| **4** | カンバンから消えた issue で毎巡回 WARN | **その原因では起きない** | `reconcileRunning` が先に印を取り、終わらせる印を同期で押さえる。**毎巡回の WARN は「GitHub が読めない」ときに出る** | **直すものが無い** |
| **5** | 同じ run に `agent.get` を2回叩く | **起きる** | **90〜99%の帯で2回。**しかも同じ run について `Info` と `Warn` が並び、2つの障害に見える | **残っている。**`handling` に入るのは「止まっていない、かつ初回」の run だけなので、`working`/`blocked`/`unknown`/読み取り失敗の run と、手放しを撃った run は、いまも2回叩かれる |
| **6** | ユースケース記述が消した関数を指す | **起きる** | **3箇所。**うち「入札の線をどう引くか」の判断は仕様の中身も食い違っている（「最大を採る」と書いてあるが、実装は「余裕の無い枠が1つでもあるか」の選言） | **直した。**[docs/spec/usecases/particular_case/](../../docs/spec/usecases/particular_case/) の下の4本——`issue の担当を入札で決める` / `issue を1件処理する` / `レートリミットで待って再開する` / `本家のリポジトリへ PR を出す`——の `*.rucm.md`（ユースケース記述）と `*.judge_log.md`（その判断の記録）を書き直し、`*.cfg.json`（そこから機械が作る中間形式）を再生成した |
| **7** | 枠で turn が終わったことを知る手段が無い | **条件付きで起きる** | **API キー・クラウドプロバイダ・従量課金では確実に `StopFailure` が飛ぶ**（`interactive-mode.md:649` が「待つべきリセットが無い」と明記）。**中間の4条件は公式ドキュメントに書かれておらず、確定できなかった** | **残っている。**6節に「実測していない」として載せた |
| **8** | 枠の待ちが明けたことを hook から知らない | **起きる** | **`Notification` を matcher 無しで張っているので届いている。**`quota_auto_resume` は continuo のコードに0件 | **残っている。**4-3 が「将来これを読めば推測は要らなくなる」と書いている |
| **9** | 長いツール呼び出しで打ち切られる | **起きる** | **防ぐ信号が2つ在る。**`agent_status`（**同じ応答に既に入っている**）と `agent.read`（**クライアントは実装済みで本番の呼び出しが0件**） | **直した。**commit `4df2108`。4-1 が `agent_status` を使う |
| **10** | 打ち切りを切っている機械で健全な run を手放す | **起きる** | **時間の物差しが1つも残らない**（4-6）**。**turn と turn のあいだが30〜60秒に伸びる経路が3つある（下に列挙した） | **残っている。**4-6 に書いた |

**この10件の検証で、4節の条件が決まった。**とくに9番目である。
**`agent_status` を判定に使えば、長いツール呼び出しを守れる。**その値は既に手元にある。

**10件のうち4件が残っている**（#1 / #5 / #7 / #10）**。**
**どれも issue を立てていない。**人間が「全部この issue の中でやれ」と決めたので、
**この表が唯一の記録である。**

### 4-6. 打ち切りを切っている機械（`claude.turn_timeout_ms` が0以下）

**`SPEC.md` 8.4 の流儀に合わせて、0以下なら打ち切りを行わない。**
**その機械では、上の3つの問いのうち問A が丸ごと消え、問B の門が2つ外れる。**

| 何が | どうなるか |
| --- | --- |
| **問A** | [internal/orchestrator/reconcile.go:611-614](../../internal/orchestrator/reconcile.go#L611-L614) の `if silence <= 0 { return }` で、巡回ごと飛ぶ |
| **問B の時間の門** | [internal/orchestrator/reconcile.go:372-378](../../internal/orchestrator/reconcile.go#L372-L378) の `silence > 0` と `!stallDetectionOff()` が両方偽になり、**2つとも外れる** |
| **残る条件** | **4-2 の5つの門のうち4つは残る**（agent 名を持っている／バックオフ中でない／**1週間の枠の余裕が無い**／`LastSeenAt` がゼロでない）。**外れるのは5つ目の時間の門だけである。**そのうえで、`agent_status` が `idle`/`done`・連番が2回続けて同じ |

**時間の物差しが1つも残らない**（4-5 の #10）。**それを承知で、手放しだけは効かせている。**
**効かせないと、`weekly_wait_limit_minutes` がその設定の機械で一度も効かない。**

**turn と turn のあいだが30〜60秒に伸びる経路は3つある**（4-5 の #10）。
**打ち切りが効いていれば、そのどれでも `claude.turn_timeout_ms` が受け止める。切っていると受け止める者がいない。**

| 経路 | どこ | 何秒空くか |
| --- | --- | --- |
| **turn の終わりを確かめる待ち受けが空振りする** | [internal/orchestrator/turn.go:942-947](../../internal/orchestrator/turn.go#L942-L947) の `patience` | `poll_wait_ms`（既定30秒） |
| **枠明けに継続の指示を送らず、hook を待つ** | [internal/orchestrator/turn.go:705-712](../../internal/orchestrator/turn.go#L705-L712) の `working` の枝 | 同上 |
| **`blocked` の引き渡しの前に subagent を待つ** | [internal/orchestrator/turn.go:314-345](../../internal/orchestrator/turn.go#L314-L345)（3-8） | 同上。**2つ重なれば60秒** |

**塞げていない組み合わせが1つある。**
**`state_change_seq` を返さない herdr の版**（連番が 0）**と、この設定が重なると、
`paneStopped` は「判定できない。打ち切りに任せる」と答えるが、任せる先が存在しない。**
**その run は手放されもせず打ち切られもせず、pane とスロットを握ったまま残る。**
**実例は観測していない**（6節に載せた）。

---

## 5. 過去に判定を誤った実例

**9行10件である。**動いている run を殺したのが5行6件（うち1行が2件）、
止まっている run を動いていると読んだのが3行3件、線の引き方の誤りが1行1件である。
**以下「9行10件」と数える。**

**調べ方。**`git log --oneline --all -- internal/orchestrator/reconcile.go internal/orchestrator/turn.go`（55件）と
`git log --all --grep="stall\|画面の版\|打ち切" -i`（25件）から12件の commit 本文を読み、
`gh issue list --state all --limit 300` を「打ち切/stall/手放/pane/hook/turn/固ま/止ま/殺/枠/誤」で絞った18件の
うち11件の本文とコメントを読んだ。あわせて `~/.claude/projects/` の continuo 関連24ディレクトリを
`agent_not_found` で絞り、当たった17ファイルから2026-09-05 の生ログを取り出した。

### 5-1. 動いている run を殺した（5行6件）

| いつ | 何が起きたか | そのとき何がどうだったか | どう直したか |
| --- | --- | --- | --- |
| **2026-09-05**（2件） | 復帰した Claude Code が起動1.06秒後から `git merge` のコンフリクト解消をしていたのに、**「起動していない」と判断して pane を閉じた。**`MERGE_HEAD` と未解決の4ファイルが残った | **`agent_status` は取れていない**（`agent.get` が `agent_not_found`）。**画面は動いていた。****hook も来ていた**（`PreToolUse` / `PostToolUse` が3件）。ただし直前に hook の宛先を新しいセッション識別子へ張り替えており、**動いている本人の hook を捨てていた** | **直った。**issue #235 に対する PR #241（v0.1.15） |
| **2026-08-27** | Claude Code が `background_tasks` を載せた `Stop`（＝「まだ動いています」という本人の申告）を送ったのに、**空配列でないという理由で読み捨て、2秒後に pane を閉じた** | **hook は来ていた。**`agent_status` は見ていない。**バックグラウンド処理が走っていた。****issue に残った文面は「Stop hook から continuo へ通知が届きませんでした」で、事実と逆だった** | **直った。**issue #77 に対する PR #97。**4日後に同じ穴をもう1つ塞いでいる**（`settle_ms` の窓に届いた走行中の `Stop` も捨てていた） |
| **2026-09-02** | `Stop` hook が差し戻して書き直させている最中を「turn が終わった」と読み、**Status を動かし、次の指示を送っていた** | **`agent_status` は `working`**（0.1秒刻みで n=168 を取り、`idle` へ落ちる瞬間は1度も観測されなかった）。**画面の版は動いていた。**`stop_hook_active` の欄は受け取っていたが、読んでいる箇所が1行も無かった | **直った。**commit `d75235e`。**settle の窓が閉じた瞬間に herdr へ1回聞き、`working` なら待ち直す** |
| **2026-08-27** | サブエージェント2つが走っている最中に esc を送って人間へ引き渡した。**4件中3件まで編集を終えていて、4件目で止まった** | **`agent_status` は `blocked`。**親の transcript はサブエージェント起動で止まっていた。**`SubagentStart` は届いていたが、読む行が1つも無かった** | **直った。**issue #65 に対する PR #71 |
| **2026-08-21** | **`turn_timeout_ms` を「turn の総時間」として実装していた。**1回の指示で数時間かかることは普通にあるので、**動いていても打ち切っていた** | 沈黙かどうかを見る信号を1つも持っていなかった | **直った。**commit `e90e682`。`SPEC.md` 10.6 は「turn の流れが動いている間の**最大の沈黙の間隔**。**総実行時間の上限ではない**」と定めている。**そのとき採った信号は画面の版だったが、2026-09-08 に `agent_status` へ替えた**（4-1） |

### 5-2. 止まっている run を動いていると読んだ（3件）

| いつ | 何がそう見せたか | どう直したか |
| --- | --- | --- |
| **2026-08-27** | **`SubagentStop` 自身が、いま終わったその subagent を `status: running` のまま `background_tasks` に載せて届く。**印を下ろす経路が1つ足りず、残り続けた | **直った。**commit `fe4c9a9` |
| **2026-09-07** | 手放しの条件に「走っているサブエージェントが無い」を入れていた。**あの一覧を空にする経路は「次の turn を始める」と「`SubagentStop` を受ける」の2つだけで、枠待ちの最中はどちらも起きない。**永久に「走っている」と見えた | **直った。**commit `d858c0e`。条件を取り下げ、`agent_status` に受け持たせた |
| **2026-09-07** | 画面の版を `LastRevision` と比べていた。**あれを書くのは着手のときと巡回の stall 検知だけで、枠待ちの run では stall 検知が走らない。**凍りついた値と比べるので永久に不一致だった | **直った。**commit `2a2e60d`。**手放しの判定が自分で読んだ値だけを覚える形にした** |

### 5-3. 線の引き方の誤り（1件）

**2026-09-06〜09-07。**「枠待ちの印を立てる線」と「手放しの線」が別々にあり、
**使用率90〜99の帯では、run が枠待ちにならないまま手放しの条件だけを満たした。**
そこで打ち切り（retry を積む）と手放し（担当を外す）が競走し、
**どちらが勝つかで、枠が足りないだけの issue が `failure_state` へ落ちた。**

**1度は線を1本へ揃えようとしたが、取り下げた。**
**使用率90%で打ち切りの時計を止めると、本当に固まった run が5時間の枠が90%を割るまで殺されない。**
**既定では最大6時間、スロットと pane を握り続ける。**

### 5-4. 9行10件に共通する形

**3つある。**

| 形 | 何件で起きたか |
| --- | --- |
| **1つの信号を、それが答えていない問いの答えとして読んだ** | **10件とも** |
| **印を立てる経路より、下ろす経路のほうが少ない** | 5-2 の3件 |
| **読めなかったことを「止まっている」と読んだ** | 5-1 の1件目 |

**とくに1つ目である。**`agent_not_found` は「herdr が登録していない」であって「起動していない」ではない。
hook の無音は「ツールが長い」と区別できない。`background_tasks` が空の `Stop` は
「止まってよいか hook に尋ねた」であって「終わった」ではない。`agent_status` が `idle` なのは
「枠を待っている」でも同じになる。

**だから「止まっている」と言ってよい条件は、単一の信号では書けない。**

---

## 6. 測っていないこと

**空欄にしない。次に調べる人が、ここから始められるようにする。**

| 何 | なぜ測れていないか |
| --- | --- |
| **枠待ちの pane が、herdr にどの状態で見えるか** | 枠が尽きた状態を意図的に作れない。**画面には `Usage limit reached · continuing automatically at 3:45pm · esc to cancel` が出る**（公式 `interactive-mode.md` 608-612行）が、**herdr の `live_blocked_form` は `contains` と `any` の AND なので当たらない**（`any` が `enter to confirm` か `enter to select` を求める） |
| **`agent.read` の本文が「止まっているのに変わる」場面があるか** | 3-5 の限界(二) |
| **continuo の pane が `idle` のときの、画面の本文の安定性** | 3-5 の限界(三) |
| **`StopFailure` が実際に飛ぶ条件** | 対話モード＋claude.ai のサブスクリプション＋v2.1.234 以降では飛ばないと読めるが、**API キーや古い版では飛びうる。**実測していない |
| **サブエージェントがキャンセルされたとき `SubagentStop` が飛ぶか** | 公式ドキュメントに記述が無い（`hooks.md` / `sub-agents.md` / `hooks-guide.md` の3ファイル6204行で0件） |
| **`working` の見た目のまま固まった Claude Code が、実際にどれくらいの頻度で起きるか** | **意図的に作れない。**process を `SIGSTOP` で止めれば画面は固まるが、それは実運用の壊れ方と同じとは言えない。**頻度が分からないので、`working` が続いた時間の上限（4-1 の限界(一)）を何分に置くべきかも決められない** |
| **herdr の `osc_title_working` が当たらなくなる頻度** | **Claude Code の端末タイトルの書き方が変わったときにだけ起きるので、こちらから作れない。**頻度が分からないので、`agent.explain` の `fallback_reason` を毎回読む価値を測れない（4-1 の限界(二)） |
| **`agent_not_found` が返る条件と頻度** | **4-1 が「読めない＝打ち切る」を採っているので、判定の安全性を直接左右する。**3-4 の限界(二)が存在に触れているだけで、**いつ返るかは測っていない。**5-1 の2026-09-05 の直接の原因である |
| **subagent が走っている最中も、Claude Code が端末タイトルへスピナーを書き続けるか** | **`working` の決め方そのものは測ってある**（3-4 の `matched_rule` が `osc_title_working`。4-1 の限界(二)）。**残っているのはこの1点だけである。**3-1 の実測は Bash の1コマンド（`go test`）で取ったもので、`Task`（subagent）・`WebFetch`・MCP の呼び出し中は測っていない。**外れると、枠が尽きた run の subagent を書きかけごと閉じる**（[internal/orchestrator/reconcile.go:423-429](../../internal/orchestrator/reconcile.go#L423-L429) が、この前提の上に立っている） |
| **枠明けの `confirmTurnEnd` から、`hookSeenThisTurn` が偽のまま枠待ちの印が立つ経路が実際に起きるか** | **枠が明けた直後に、枠がまだ満杯に見える状態を作れない。**4-3 の末尾に書いた経路である。**起きても打ち切りの時計が止まるだけで、枠が明ければ印は外れる** |
| **herdr を再起動したあとの `state_change_seq`** | **3-3 が「0 から振り直される。continuo は再起動を検知できない」と書いている。**4-2 の「2回続けて同じ連番」は、2回が同じ番号空間にあることを前提にしている。**その前提が崩れる場面で何が返るかを測っていない** |
| **`quota_auto_resume_*` が実際に届くか** | **3-6 は「既に届いている」と書いているが、根拠は matcher の設定だけである。**受信した記録は取っていない。**4-3 は、この断定の上に将来の計画を立てている** |
| **`state_change_seq` を返さない herdr の版と、打ち切りを切っている機械の組み合わせ** | **両方をそろえた環境を作っていない。**その組み合わせでは run を止める者が1人もいなくなる（4-6） |

---

## 7. この文書の履歴

**レビューの周回数と指摘の件数は書かない**（[.claude/rules/reporting.md](../../.claude/rules/reporting.md) の
「数字は、合否の線とセットでしか出さない」。労力の量は品質の根拠にならない）。
**書くのは「何が変わったか」だけである。**周ごとの件数と判断は、対になる issue のコメントに残してある。

| いつ | 何を書いたか |
| --- | --- |
| **2026-09-08** | 初版。信号7つを測って並べ（3-8 はあとで足した）、過去に誤った9行10件と、疑った10件の検証を書いた。**4節で条件を確定させた** |
| **2026-09-08** | 4-1 を確定した条件どおりに実装し、**この文書を実装後の記述へ直した。**打ち切りが見るものが `revision` から `agent_status` へ替わった。**`working` のまま固まる run を誰も止めないという限界を、4-1 と6 へ書いた** |
| **2026-09-08** | **敵対的レビューを受けて直した。**2-1（`agent_status` を読む他の場所）・4-6（打ち切りを切っている機械）を新設し、4-1 に読み取り失敗と `blocked` と限界(二)を、4-2 に手放しの門を、4-4 に `beginTerminal` を足した。**4-5 に「いまどうなっているか」の列を足し、10件のうち4件が残っていることを明記した** |
| **2026-09-08** | **敵対的レビューを受けて直した。**2-1 の表を数え直し（`sendTurn` / `afterWaitTimeout` の待ち直し / `confirmTurnEnd` が落ちていた）、**`confirmStartup` と復元の「倒す向き」が実装と逆だったのを直した。**4-3 に「印を立てる場所は2つある」と「`working` が問C を短絡する」を、4-2 にバックオフの門を、4-4 に `claimTerminal` と `terminalRewriting` を足した。**4-5 を 4-4 と 4-6 のあいだへ移し、見出しを `###` へ揃えた。**打ち切りの文面から「一度も」を落とした（読むのは1サンプルである） |
| **2026-09-08** | **敵対的レビューを受けて直した。****4-1 に門の表を足した**——箱に書いてあるのは段1 だけで、打ち切りまでには5つの門と段2（枠待ちか）がある。**`claimTerminal` は `terminalRewriting` で黙って戻らず、書き戻しを待って取り直す**ことを 4-4 へ書いた。4-3 の免除の条件を「窓が閉じた」から「`hookSeenThisTurn` が真か、閾値を待ち切ったか」へ書き直し、**塞げていない経路を1つ 6節へ載せた。**2-1 の表の4行が分岐を落としていたのを直し、`blocked` が3通りに扱われていることを書いた。**0節に Claude Code の公式ドキュメントの取り方を足した**（`interactive-doc.md` は `interactive-mode.md` の書き誤りだった） |
| **2026-09-08** | **敵対的レビューを受けて直した。**3-8（走行中の subagent の一覧）を新設した——**人間が与えた「止まっている」の定義がこの信号を名指ししており、いまも `blocked` の引き渡しの前で使っている。**4-3 の免除の抜け道を1つから2つへ数え直し（`confirmTurnEnd` を `false` で呼ぶのは3箇所である）、**`afterWaitTimeout` も呼び出し元が2つあることを書いた。**3-1 と 3-5 の実測に、測り直すコマンドを足した。1節に `polling.interval_ms` を、4-6 に「30〜60秒に伸びる経路」の3つを足した。**`checkStalls` を指すリンク17本がずれていたので、目印の文字列から測り直して貼り直した** |
