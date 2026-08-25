# 設定と診断と信頼の9件の直し

**言いたいこと。**`continuo setup` が WORKFLOW.md を壊す・書き落とす経路、`continuo doctor` が
起動できない状態を ✓ と言う経路、`continuo trust` が中身を見せずに信頼を渡す経路を塞いだ。
**足したテストは全部、守りを1つ潰した版で FAIL することを確かめてある。**

## 直した内容

| 指摘 | どう直したか |
| --- | --- |
| setup が cleanup.on_states を置き去りにする | 書き換えるキーを8つにし、`cleanup.on_states` を割り当ての完了 Status で書く |
| block 形式のリストを setup が壊して成功と報告する | 値が下の行にぶら下がるキーは書き換えず止める。書き込む前に組み立てた全文を自分で読み直す |
| CRLF の WORKFLOW.md を setup が「キーが無い」と誤報する | front matter の判定で行末の CR を落とし、書き換えた行の CR を戻す |
| setup が WORKFLOW.md に書かれた owner とボード番号を捨てる | CheckUpdatable が拾った値を検出の既定値にし、使うボードを画面に出す |
| 長い1行で setup が無言で終わる | bufio.Scanner をやめ、長すぎる行は改行まで捨てて同じ役割を尋ね直す。読めない理由は必ず出す |
| hook の置き場所が「作れます」と嘘をつく | EADDRINUSE のあと繋いでみて、繋がらなければ ✗ と残骸の消し方を出す |
| doctor のテストが実機の socket を触り、検査本体を1度も走らせていない | 置き場所を一時ディレクトリへ閉じ、実機を触っていないことを毎回確かめ、失敗の3本を足す |
| hooks を見せずに信頼を渡す | `.claude/settings.json` の hooks を集めて一覧に出し、Empty の判定にも入れる |
| 読めなかった設定を「ありません」と言って信頼する | 「無い」と「読めなかった」を分け、読めなかったものは登録の対象から外す |

## 直したファイル

| 何を | どこ |
| --- | --- |
| 書き換えるキーと、書き換えられない形の判定 | internal/scaffold/fill.go |
| 書き込む前の読み直し・新しい sentinel error | internal/scaffold/update.go |
| front matter の構文だけを見る入口 | internal/config/frontmatter.go（`CheckFrontMatterSyntax`） |
| 長すぎる行の読み捨てと、読めない理由の表示 | internal/setup/assign.go |
| hook の置き場所の判定 | internal/doctor/checks.go |
| hooks の収集と、読めなかったかどうかの区別 | internal/trust/requirements.go |
| 確かめられなかったものを登録の対象から外す | internal/trust/trust.go |
| 一覧の出し分け | internal/trust/report.go |
| どのボードを読むかの決め方 | internal/cli/cli.go（runSetup） |

## 設計と RUCM

- 設計に新しい節を足した（既存の節は書き換えていない）: 3-32d / 3-32e / 3-32f / 3-32g / 3-33b
- RUCM「既存のボードの Status を割り当てる」の文言を直し、CFG を再生成した
  （sha256 `0522685f2ac9ef7313389909ac9deb636619176f727600d752a7f1a0ada70b8a`。パスの数は10のまま）
- mermaid の2ブロックは `mermaid-validate validate-md` で検証済み（Valid: 2）

## 変異で確かめた記録

### cleanup.on_states を書く

変異: `internal/scaffold/fill.go` の `statusKeys` から `cleanup.on_states` の項目を消した。

```
--- FAIL: TestStatusKeyNames_雛形に8つのキーが全部ある (0.00s)
    statuses_test.go:386: 書き換えるキーが 7 件（期待 8 件）: [tracker.status_signal_map.review … tracker.failure_state]
--- FAIL: TestUpdateStatuses_片付けを始めるStatusも書き換える (0.01s)
    update_form_test.go:46: cleanup.on_states に割り当てた完了の Status が入っていない:
```

### block 形式は書き換えない

変異: `internal/scaffold/fill.go` の `hasNestedValue` の分岐を消し、そのまま書き換えるようにした。

```
--- FAIL: TestUpdateStatuses_値が下の行にぶら下がっていたら書かずに止める (0.00s)
    update_form_test.go:73: ErrKeysNotRewritable が返らなかった: 書き換えると WORKFLOW.md を読めなくなります: …/WORKFLOW.md: [23:5] value is not allowed in this context
          22 |   active_states: ["着手待ち", "作業中"]
        > 23 |     - "Ready"
                   ^
--- FAIL: TestCheckUpdatable_値が下の行にぶら下がっていたら尋ねる前に止める (0.00s)
```

### 書き込む前に読み直す

変異: `internal/scaffold/update.go` の `config.CheckFrontMatterSyntax` の関門を消した。

```
--- FAIL: TestUpdateStatuses_書き換えると読めなくなるなら書かない (0.01s)
    update_form_test.go:131: ErrWouldBreakConfig が返らなかった: <nil>
```

### CRLF

変異1: `frontMatterRange` の `TrimRight(lines[0], " \t\r")` から `\r` を外した。

```
--- FAIL: TestUpdateStatuses_CRLFのファイルでもキーを見つけて書き換える (0.00s)
    update_form_test.go:159: CRLF のファイルを書き換えられなかった: WORKFLOW.md に書き換える対象のキーがありません: …: tracker.status_signal_map.review / … / cleanup.on_states
```

変異2: `rewriteValue` で行末の CR を戻さないようにした。

```
--- FAIL: TestUpdateStatuses_CRLFのファイルでもキーを見つけて書き換える (0.01s)
    update_form_test.go:172: 割り当てた選択肢が CRLF の行として入っていない:
```

### どのボードを読むか

変異: `internal/cli/cli.go` の `check.Owner` / `check.ProjectNumber` を使う4行を消した。

```
--- FAIL: TestRunSetup_WORKFLOWmdに書かれたボードを使う (0.00s)
    setup_board_test.go:80: WORKFLOW.md に書かれたボードを使っていない: {Owner: ProjectNumber:0 RunGH:<nil> Timeout:0s}
```

### 長すぎる1行

変異: `internal/setup/assign.go` の `ErrLineTooLong` の分岐と、読めない理由を出す `default` を消し、
長すぎる行を打ち切りに戻した。

```
--- FAIL: TestAssign_長すぎる1行は捨てて同じ役割を尋ね直す (0.00s)
    long_line_test.go:31: 長い1行で打ち切られた: 1行が長すぎます（画面: …）
--- FAIL: TestAssign_長すぎる1行が続いても答え終えられる (0.00s)
--- FAIL: TestAssign_読めなかった理由は必ず画面に出す (0.00s)
```

### hook の置き場所

変異: `internal/doctor/checks.go` の `runtimeDirInUse(sock, notes)` の呼び出しを、
元の「EADDRINUSE ならそのまま ✓」へ戻した。

```
--- FAIL: TestDoctorRuntimeDir_既にcontinuoが待ち受けていれば通る (0.20s)
    runtime_dir_test.go:69: 既に使われていることが出ていない: "…/run/hooks.sock に socket を作れます"
--- FAIL: TestDoctorRuntimeDir_残骸があれば足りないと出す (0.20s)
    runtime_dir_test.go:98: hook の置き場所 の記号が ✗ ではなく ✓ だった（説明: …/run/hooks.sock に socket を作れます / 内訳: []）
--- FAIL: TestDoctorRuntimeDir_置き場所がディレクトリなら足りないと出す (0.21s)
    runtime_dir_test.go:122: hook の置き場所 の記号が ✗ ではなく ✓ だった（説明: …/run/hooks.sock に socket を作れます / 内訳: []）
```

### doctor のテストが実機を触らない

変異: `test/internal/doctor/helpers_test.go` の `t.Setenv(envRuntimeDir, fx.RunDir)` を外した。

```
--- FAIL: TestDoctorRuntimeDir_何も無ければ作って消す (0.23s)
    runtime_dir_test.go:36: テストの一時ディレクトリの外の socket を見ています（実機を触っています）: /var/folders/…/T/continuo/hooks.sock に socket を作れます
          一時ディレクトリ: /private/var/folders/…/T/cdoc2072060563
```

### hooks を見せる

変異: `internal/trust/requirements.go` の `req.Hooks = collectHooks(parsed.Hooks)` を消した。

```
--- FAIL: TestPlan_hooksだけを持つリポジトリも要求内容として見せる (0.03s)
    requirements_test.go:39: hooks を拾えていない: []
```

### 読めなかった設定

変異1: `internal/trust/trust.go` の `Requirements.Unconfirmed()` の分岐を消した。

```
--- FAIL: TestPlan_読めなかった設定はありませんと言わず登録もしない (0.05s)
    requirements_test.go:88: 確かめられなかったことが出ていない:
        ! octocat/unreadable（未信頼。登録の対象）
```

変異2: `internal/trust/report.go` の `req.SettingsUnreadable` の分岐を消した。

```
--- FAIL: TestPlan_読めなかった設定はありませんと言わず登録もしない (0.05s)
    requirements_test.go:85: 実在するファイルを「ありません」と報告している:
        ✗ octocat/unreadable（要求内容を確かめられません。登録の対象から外します）
            .claude/settings.json: ありません
```
