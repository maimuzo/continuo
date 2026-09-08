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
| **測っていないことは** | **「測っていない」と書く。**空欄にしない |

**実測の環境。**特記が無ければ、次のとおりである。

| 何 | 値 |
| --- | --- |
| 測った日 | 2026-09-08 |
| herdr | **0.8.2**（`herdr --version`） |
| Claude Code | **2.1.263**（`claude --version`） |
| herdr のソース | **0.9.0 を clone した**（`v0.8.2` の tag は公開されていない）。**判定に使う点は実機でも裏を取ってある** |

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

---

## 2. 判定が要る場面は3つある。混ぜてはならない

| 問い | 誰が使うか | 誤るとどうなるか |
| --- | --- | --- |
| **A. この run は進んでいるか** | 打ち切り（`checkStalls`）。**毎巡回・全 run** | 進んでいる run を殺す／固まった run を放置する |
| **B. この pane は完全に止まっているか** | 枠が尽きた run の手放し（`paneStopped`）。**まれ** | 動いている pane を閉じて書きかけを失う／永久に手放せない |
| **C. この run は枠で止まっているのか** | 枠待ちの印（`isQuotaWaiting`） | 打ち切りの時計を止めるべきでない run で止める／止めるべき run で止めない |

**この3つを1つの関数で答えようとして、過去に9回失敗している**（5節）。

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
**実測：**自分の pane を2秒おきに60回読みながら `go test` を1回叩き、**60サンプル全部が `working` だった。**

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
**continuo は `StopFailure` を張っていない**（`git grep -c StopFailure` がリポジトリ全体で0）。

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
**`quota_auto_resume` という文字列はリポジトリ全体で0件。読まずに捨てている。**

### 3-7. continuo が自分で持っている時計

| 何 | 何を答えるか | 進む条件 |
| --- | --- | --- |
| **`LastSeenAt`** | 打ち切りの時計 | hook を受けた／turn を送った／枠待ちを外した／版が増えたのを確かめた。**枠待ちの間は進めない** |
| **`LastHookAt`** | 最後に hook を受けた時刻 | **どの hook でも進む**（`SessionStart` と `Notification` を含む） |
| **`LastBusyHookAt`** | **turn を処理している間にしか出ない hook** を最後に受けた時刻 | `UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `SubagentStart` / `SubagentStop` / `Stop` の6つだけ。**turn をまたいで持ち越す** |
| **`hookSeenThisTurn`** | この turn で hook を1件でも受けたか | **`beginTurn` が毎 turn 偽へ戻す** |

**`hookSeenThisTurn` の落とし穴。**`runIdleForTurnTimeout` はこれを先に見て、
**偽なら経過を測らずに「進んでいない」と答える。**
**`beginTurn` が毎 turn 偽へ戻すので、指示を送った直後は必ずそう読まれる。**

---

## 4. 確定した条件

**（この節は、10件の検証が返ってから書く。）**

---

## 5. 過去に判定を誤った実例

**（この節は次の版で書く。9件を調べてある。）**

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

---

## 7. この文書の履歴

| いつ | 何を書いたか |
| --- | --- |
| **2026-09-08** | 初版。信号7つを測って並べた。**4節と5節は未了** |
