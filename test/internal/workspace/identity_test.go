package workspace_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/workspace"
)

// prepareWorktree は fixture の Manager で worktree を1つ用意する。
//
// t: 呼び出し元のテスト。
// fx: 使う fixture。
// issue: 用意する issue。
// 戻り値: Prepare の結果。
func prepareWorktree(t *testing.T, fx *managerFixture, issue workspace.IssueRef) *workspace.PrepareResult {
	t.Helper()
	result, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	return result
}

// fullIdentity はすべての項目を埋めた身元ファイルの中身を返す。
//
// created: created_at に入れる時刻。
// 戻り値: 検査に使う Identity。
func fullIdentity(created time.Time) workspace.Identity {
	return workspace.Identity{
		IssueURL:         "https://github.com/maimuzo/koetsumugi/issues/188",
		IssueIdentifier:  "maimuzo/koetsumugi#188",
		ProjectItemID:    "PVTI_lADOAb3c4M4Aq7EzgAR8Xyz",
		Branch:           "continuo/maimuzo/koetsumugi/188",
		HerdrWorkspaceID: "w9",
		SocketPath:       "/tmp/continuo/hooks.sock",
		SettingsPath:     "/tmp/continuo/issues/maimuzo-koetsumugi-188/settings.json",
		AgentName:        "continuo-koetsumugi-188",
		SessionUUID:      "8aebf7af-8b07-4f45-b037-59f457b38feb",
		CreatedAt:        created,
		TakeoverCount:    0,
	}
}

// 目的: 身元ファイルに書いた全項目が、そのまま読み戻せることを確認する（設計 3-18）。
// 与える情報: 全項目を埋めた Identity。
// 成功条件: 書いたあと読み戻した値が、書いた値と一致すること
// （project item の ID と herdr の workspace の ID が復元の主キーになるので、
// 1項目でも落ちると第7段階が実装できない）。
func TestWriteIdentity_書いた全項目を読み戻せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	created := time.Date(2026, 8, 18, 12, 34, 56, 0, time.UTC)
	want := fullIdentity(created)
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, want); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("created_at が一致しない: got %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	got.CreatedAt = want.CreatedAt
	if *got != want {
		t.Fatalf("読み戻した身元ファイルが一致しない:\ngot  %+v\nwant %+v", *got, want)
	}
}

// 目的: 身元ファイルが JSON として設計のサンプルどおりのキー名で書かれることを確認する
// （設計 3-18 の中身のサンプル。キー名が変わると第7段階の復元が読めない）。
// 与える情報: 全項目を埋めた Identity。
// 成功条件: ファイルの中身に issue_url / project_item_id / herdr_workspace_id /
// takeover_count のキーがあり、cleanup_deferred_at はゼロ値なので出ないこと。
func TestWriteIdentity_JSONのキー名が設計どおりである(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	if err := fx.Manager.WriteIdentity(
		context.Background(), prepared.Path, fullIdentity(time.Now()),
	); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	data, err := os.ReadFile(fx.Manager.IdentityPath(prepared.Path))
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("身元ファイルを JSON として読めない: %v", err)
	}
	for _, key := range []string{
		"issue_url", "issue_identifier", "project_item_id", "branch",
		"herdr_workspace_id", "socket_path", "settings_path", "agent_name",
		"session_uuid", "created_at", "takeover_count",
	} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("身元ファイルにキー %q が無い: %v", key, raw)
		}
	}
	if _, ok := raw["cleanup_deferred_at"]; ok {
		t.Fatalf("cleanup_deferred_at はゼロ値なら出ないはずなのに出ている: %v", raw)
	}
}

// 目的: 身元ファイルと**その一時ファイル**が共通ディレクトリの info/exclude に登録され、
// 何度書いても行が積み上がらず、.gitignore は触られないことを確認する（設計 3-18）。
//
// **一時ファイルの行がなぜ要るか。**writeFileAtomic は同じディレクトリに
// `.continuo.json.tmp<乱数>` を作る。通常は消えるが、**常駐プロセスが強制終了すると
// worktree に残る。**残った1つが未追跡ファイルとして数えられると、
// その worktree は二度と片付かない。
//
// 与える情報: 同じ worktree への2回の書き込み。
// 成功条件: `<共通ディレクトリ>/info/exclude` に `/.continuo.json` と
// `/.continuo.json.tmp*` がそれぞれちょうど1行あり、
// worktree にもリポジトリにも .gitignore が作られていないこと。
func TestWriteIdentity_info_excludeに身元ファイルと一時ファイルを1行ずつ登録する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	for i := range 2 {
		if err := fx.Manager.WriteIdentity(
			context.Background(), prepared.Path, fullIdentity(time.Now()),
		); err != nil {
			t.Fatalf("%d 回目の WriteIdentity に失敗した: %v", i+1, err)
		}
	}

	excludePath := filepath.Join(fx.Repo.Dir, ".git", "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("info/exclude を読めない: %v", err)
	}
	for _, want := range []string{"/.continuo.json", "/.continuo.json.tmp*"} {
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == want {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("info/exclude の %q の行数が1でない: got %d\n%s", want, count, data)
		}
	}

	for _, path := range []string{
		filepath.Join(prepared.Path, ".gitignore"),
		filepath.Join(fx.Repo.Dir, ".gitignore"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf(".gitignore を触ってはならないのに %s ができている（err=%v）", path, err)
		}
	}
}

// 目的: 身元ファイルの再利用の規則を確認する（設計 3-18）。
// 与える情報: takeover_count が 2、created_at が過去、cleanup_deferred_at 入りの既存の身元ファイルと、
// socket のパスなどを今回の値で埋めた新しい Identity。
// 成功条件: takeover_count が3になり、created_at が既存の値のまま保たれ、
// cleanup_deferred_at がゼロ値に消され、それ以外は新しい値で書き直されること。
func TestMergeForReuse_引き継ぎ回数を増やし作成時刻を保ち見送りの記録を消す(t *testing.T) {
	oldCreated := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	existing := fullIdentity(oldCreated)
	existing.TakeoverCount = 2
	existing.CleanupDeferredAt = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	existing.SocketPath = "/tmp/old/hooks.sock"

	fresh := fullIdentity(time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	fresh.SocketPath = "/tmp/new/hooks.sock"

	got := workspace.MergeForReuse(fresh, &existing)

	if got.TakeoverCount != 3 {
		t.Fatalf("takeover_count が1つ増えていない: got %d, want 3", got.TakeoverCount)
	}
	if !got.CreatedAt.Equal(oldCreated) {
		t.Fatalf("created_at が保たれていない: got %v, want %v", got.CreatedAt, oldCreated)
	}
	if !got.CleanupDeferredAt.IsZero() {
		t.Fatalf("cleanup_deferred_at が消えていない: got %v", got.CleanupDeferredAt)
	}
	if got.SocketPath != "/tmp/new/hooks.sock" {
		t.Fatalf("socket のパスが書き直されていない: got %q", got.SocketPath)
	}
}

// 目的: 既存の身元ファイルが無い（新規）のときの規則を確認する（設計 3-18）。
// 与える情報: existing が nil の MergeForReuse。
// 成功条件: takeover_count が 0 になり、created_at は今回の値のままであること。
func TestMergeForReuse_新規なら引き継ぎ回数は0になる(t *testing.T) {
	created := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	fresh := fullIdentity(created)
	fresh.TakeoverCount = 7

	got := workspace.MergeForReuse(fresh, nil)

	if got.TakeoverCount != 0 {
		t.Fatalf("新規なのに takeover_count が 0 でない: got %d", got.TakeoverCount)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at が今回の値でない: got %v, want %v", got.CreatedAt, created)
	}
}

// 目的: 身元ファイルの JSON が壊れていたら、それと分かるエラーになることを確認する
// （設計 3-4 の段2。段6 の書き込み途中で落ちた場合。消さずにログへ出す）。
// 与える情報: 途中で切れた JSON を書いた身元ファイル。
// 成功条件: ReadIdentity が ErrIdentityBroken を返し、ファイルは残っていること。
func TestReadIdentity_壊れたJSONはErrIdentityBrokenになる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	path := fx.Manager.IdentityPath(prepared.Path)
	if err := os.WriteFile(path, []byte(`{"issue_url": "https://exa`), 0o600); err != nil {
		t.Fatalf("壊れた身元ファイルを書けない: %v", err)
	}

	_, err := fx.Manager.ReadIdentity(prepared.Path)
	if !errors.Is(err, workspace.ErrIdentityBroken) {
		t.Fatalf("壊れた JSON なのに ErrIdentityBroken にならない: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("壊れた身元ファイルが消されている: %v", statErr)
	}
}

// 目的: 身元ファイルが無いときに、それと分かるエラーになることを確認する
// （設計 3-4 の段2。人間が置いた worktree かもしれないので無視する）。
// 与える情報: 身元ファイルを置いていない worktree のパス。
// 成功条件: ReadIdentity が ErrIdentityNotFound を返すこと。
func TestReadIdentity_無いときはErrIdentityNotFoundになる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	_, err := fx.Manager.ReadIdentity(prepared.Path)
	if !errors.Is(err, workspace.ErrIdentityNotFound) {
		t.Fatalf("身元ファイルが無いのに ErrIdentityNotFound にならない: %v", err)
	}
}

// 目的: agent 名だけをあとから追記できることを確認する
// （設計 3-18。段6 の時点では重複による連番が付くかどうかが分からない）。
// 与える情報: agent_name を空にして書いた身元ファイルと、段9 のあとに確定した agent 名。
// 成功条件: agent_name だけが書き変わり、他の項目は元のままであること。
func TestSetAgentName_agent名だけを追記する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	base := fullIdentity(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	base.AgentName = ""
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, base); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	if err := fx.Manager.SetAgentName(context.Background(), prepared.Path, "continuo-koetsumugi-188-2"); err != nil {
		t.Fatalf("SetAgentName に失敗した: %v", err)
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if got.AgentName != "continuo-koetsumugi-188-2" {
		t.Fatalf("agent_name が追記されていない: got %q", got.AgentName)
	}
	if got.ProjectItemID != base.ProjectItemID || got.HerdrWorkspaceID != base.HerdrWorkspaceID {
		t.Fatalf("agent_name 以外が書き変わっている: %+v", *got)
	}
}

// 目的: 引き継ぐたびに takeover_count を増やして書き戻せることを確認する（設計 3-4 の段5b）。
// 与える情報: takeover_count が 0 の身元ファイルに対する2回の IncrementTakeover。
// 成功条件: ファイルの値が 2 になること（メモリ上だけで増えていないこと）。
func TestIncrementTakeover_引き継ぐたびに増やして書き戻せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	if err := fx.Manager.WriteIdentity(
		context.Background(), prepared.Path, fullIdentity(time.Now()),
	); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	for i := range 2 {
		if _, err := fx.Manager.IncrementTakeover(context.Background(), prepared.Path); err != nil {
			t.Fatalf("%d 回目の IncrementTakeover に失敗した: %v", i+1, err)
		}
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if got.TakeoverCount != 2 {
		t.Fatalf("takeover_count がファイルに書き戻されていない: got %d, want 2", got.TakeoverCount)
	}
}

// 目的: 片付けを見送った時刻を身元ファイルへ書けることを確認する（設計 3-9 の手順2c）。
// 与える情報: 見送った時刻。
// 成功条件: cleanup_deferred_at にその時刻が入り、他の項目が壊れないこと
// （orchestrator は issue へのコメントの投稿に成功したあとにこれを呼ぶ）。
func TestMarkCleanupDeferred_見送った時刻を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	if err := fx.Manager.WriteIdentity(
		context.Background(), prepared.Path, fullIdentity(time.Now()),
	); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	at := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	if err := fx.Manager.MarkCleanupDeferred(prepared.Path, at); err != nil {
		t.Fatalf("MarkCleanupDeferred に失敗した: %v", err)
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if !got.CleanupDeferredAt.Equal(at) {
		t.Fatalf("cleanup_deferred_at が書かれていない: got %v, want %v", got.CleanupDeferredAt, at)
	}
	if got.ProjectItemID == "" {
		t.Fatalf("他の項目が壊れている: %+v", *got)
	}
}

// 目的: `info/exclude` は利用者のファイルなので、**書き足すだけで書き直さない**ことを
// 確認する（設計 3-18）。読み取り → 連結 → 全置換（os.WriteFile は O_TRUNC）で更新すると、
// 途中で落ちたときに利用者が自分で書いた除外規則が消える。
// 与える情報: 利用者が先に書いた除外規則を持つ `info/exclude`。
// 成功条件: 利用者の行がそのまま残り、continuo の行がそのあとに足されていること。
func TestWriteIdentity_利用者のinfo_excludeを消さずに書き足す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	excludePath := filepath.Join(fx.Repo.Dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		t.Fatalf("info ディレクトリを作れない: %v", err)
	}
	existing := "# 利用者が書いた除外規則\n/私の下書き.md\n"
	if err := os.WriteFile(excludePath, []byte(existing), 0o644); err != nil {
		t.Fatalf("利用者の info/exclude を書けない: %v", err)
	}

	if err := fx.Manager.WriteIdentity(
		context.Background(), prepared.Path, fullIdentity(time.Now()),
	); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("info/exclude を読めない: %v", err)
	}
	if !strings.HasPrefix(string(data), existing) {
		t.Fatalf("利用者が書いた除外規則を消している:\n%s", data)
	}
	if !strings.Contains(string(data), "/.continuo.json\n") {
		t.Fatalf("身元ファイルの行が足されていない:\n%s", data)
	}
}

// 目的: 同じ worktree の身元ファイルを複数の goroutine から同時に更新しても、
// 更新を取りこぼさないことを確認する（Manager は複数の run から共有される）。
//
// **読んで書き戻す処理を直列化していないと、あとの書き込みが前の書き込みを消す。**
//
// 与える情報: 同時に走らせる8回の IncrementTakeover。
// 成功条件: ファイルに書かれた takeover_count が 8 になること。
func TestIncrementTakeover_同時に呼んでも数え落とさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	base := fullIdentity(time.Now())
	base.TakeoverCount = 0
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, base); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	const times = 8
	var wg sync.WaitGroup
	errs := make(chan error, times)
	for range times {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fx.Manager.IncrementTakeover(context.Background(), prepared.Path); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("IncrementTakeover に失敗した: %v", err)
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if got.TakeoverCount != times {
		t.Fatalf("同時に更新すると数え落とす: got %d, want %d", got.TakeoverCount, times)
	}
}

// 目的: 身元ファイルの base を書いて読み戻せることを確認する（設計 3-18 / 3-9 の手順2b）。
// **これが無いと、再起動をまたいだ片付けが base を復元できない。**
// 与える情報: base を埋めた Identity。
// 成功条件: JSON に base のキーがあり、読み戻せること。
func TestWriteIdentity_baseを書いて読み戻せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	want := fullIdentity(time.Now())
	want.Base = "main"
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, want); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	data, err := os.ReadFile(fx.Manager.IdentityPath(prepared.Path))
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("身元ファイルを JSON として読めない: %v", err)
	}
	if raw["base"] != "main" {
		t.Fatalf("身元ファイルに base が書かれていない: %v", raw)
	}

	got, err := fx.Manager.ReadIdentity(prepared.Path)
	if err != nil {
		t.Fatalf("ReadIdentity に失敗した: %v", err)
	}
	if got.Base != "main" {
		t.Fatalf("base を読み戻せない: got %q", got.Base)
	}
}

// 目的: 再利用のときに base を落とさないことを確認する（設計 3-18）。
// **再利用の経路では Prepare が base を決めないので、既存の値を保たないと
// 片付けが「base が分からない」で永久に見送る。**
// 与える情報: base を持つ既存の身元ファイルと、base が空の新しい Identity。
// 成功条件: 既存の base が保たれること。
func TestMergeForReuse_baseは既存の値を保つ(t *testing.T) {
	existing := fullIdentity(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	existing.Base = "main"

	fresh := fullIdentity(time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	fresh.Base = ""

	got := workspace.MergeForReuse(fresh, &existing)
	if got.Base != "main" {
		t.Fatalf("再利用で base を落としている: got %q", got.Base)
	}
}
