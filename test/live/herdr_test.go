package live_test

import (
	"context"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: 設定の既定値 herdr.protocol が、いま常駐している herdr の protocol 版と
// 一致することを本物の ping で確かめる（設計 5-2）。
// 与える情報: 常駐している herdr の socket。
// 成功条件: 応答の type が "pong" で、protocol が config.DefaultConfig().Herdr.Protocol と
// 一致すること。
//
// **期待値を直書きしない。**既定値から引く。直書きすると、既定値を上げた瞬間に
// この検査だけが古い値を主張する。
func TestLive_Ping_protocolが設定の既定値と一致する(t *testing.T) {
	client := requireLiveHerdr(t)

	got, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("本物の herdr へ ping を送れませんでした: %v", err)
	}

	if got.Type != "pong" {
		t.Errorf("ping の応答の type が想定と違う: got %q, want %q", got.Type, "pong")
	}
	want := config.DefaultConfig().Herdr.Protocol
	if got.Protocol != want {
		t.Errorf("herdr の protocol 版が設定の既定値と違う: herdr %d（本体 %s）, 既定値 %d。"+
			"internal/config/default.go の Protocol か herdr 本体のどちらかを合わせること",
			got.Protocol, got.Version, want)
	}
}

// 目的: 着手で使う herdr の呼び出しを、本物の socket に対して一続きで通す
// （worktree.open → pane.list → pane.rename → workspace.rename → worktree.remove）。
// **偽 herdr では確かめられない「引数の名前・応答の形」のずれを捕まえるためのものである。**
// 与える情報: t.TempDir() の下に作った使い捨てのリポジトリと、そこから切った worktree 1本。
// 成功条件: 各呼び出しが成功し、応答の type と label と cwd が continuo の想定どおりで
// あること。後始末で workspace が消え、pane が残らないこと。
func TestLive_Herdr_着手で使う呼び出しが本物で一続きに通る(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, _ := addWorktree(t, repo)

	// **herdr を叩く前に後始末を登録する。**途中で t.Fatalf しても消えるようにする。
	janitor := newLiveJanitor(t, client, repo.Root)

	ctx := context.Background()
	focus := false
	openLabel := liveLabelPrefix + "/octocat/hello-world/issues/4"

	opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
		Path:  worktreePath,
		Cwd:   repo.Path,
		Focus: &focus,
		Label: openLabel,
	})
	if err != nil {
		t.Fatalf("本物の herdr で worktree.open が失敗した（path のみ・Cwd あり）: %v", err)
	}
	// **アサーションより先に控える。**
	janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
	janitor.TrackPane(opened.RootPane.PaneID)

	if opened.Type != "worktree_opened" {
		t.Errorf("worktree.open の応答の type が想定と違う: got %q, want %q",
			opened.Type, "worktree_opened")
	}
	if opened.Workspace.WorkspaceID == "" {
		t.Fatalf("worktree.open が workspace_id を返さない。worktree.remove を呼べない: %+v", opened)
	}
	if opened.RootPane.PaneID == "" {
		t.Fatalf("worktree.open が root_pane.pane_id を返さない: %+v", opened)
	}
	if got, want := resolvedPath(opened.Worktree.Path), resolvedPath(worktreePath); got != want {
		t.Errorf("worktree.open が返した worktree のパスが違う: got %q, want %q", got, want)
	}

	// 着手の段8（internal/orchestrator/dispatch.go）は pane.list が
	// **ちょうど1件返すことを前提にしている。**その前提を本物で確かめる。
	list, err := client.PaneList(ctx, herdr.PaneListParams{WorkspaceID: opened.Workspace.WorkspaceID})
	if err != nil {
		t.Fatalf("本物の herdr で pane.list が失敗した: %v", err)
	}
	if list.Type != "pane_list" {
		t.Errorf("pane.list の応答の type が想定と違う: got %q, want %q", list.Type, "pane_list")
	}
	if len(list.Panes) != 1 {
		t.Fatalf("開いたばかりの workspace の pane が1件ではない: got %d 件 %+v", len(list.Panes), list.Panes)
	}
	pane := list.Panes[0]
	if pane.PaneID != opened.RootPane.PaneID {
		t.Errorf("pane.list が返した pane が root_pane と違う: got %q, want %q",
			pane.PaneID, opened.RootPane.PaneID)
	}
	// 復元（internal/orchestrator/restore.go）は pane の cwd だけで worktree と結び付ける。
	// **その結び付けが本物で成立することを確かめる。**
	if got, want := resolvedPath(pane.Cwd), resolvedPath(worktreePath); got != want {
		t.Errorf("pane の cwd が worktree のパスと一致しない: got %q, want %q", got, want)
	}

	// pane.split では label を書けないので、pane.rename が唯一の経路である（設計 3-3）。
	paneLabel := openLabel + "/pane"
	renamed, err := client.PaneRename(ctx, herdr.PaneRenameParams{
		PaneID: pane.PaneID,
		Label:  paneLabel,
	})
	if err != nil {
		t.Fatalf("本物の herdr で pane.rename が失敗した: %v", err)
	}
	if renamed.Type != "pane_info" {
		t.Errorf("pane.rename の応答の type が想定と違う: got %q, want %q", renamed.Type, "pane_info")
	}
	if renamed.Pane.Label != paneLabel {
		t.Errorf("pane.rename で書いた label が応答に載っていない: got %q, want %q",
			renamed.Pane.Label, paneLabel)
	}

	// 開いたあとに workspace の label を書き換える経路はこれだけである。
	workspaceLabel := openLabel + "/workspace"
	wsRenamed, err := client.WorkspaceRename(ctx, herdr.WorkspaceRenameParams{
		WorkspaceID: opened.Workspace.WorkspaceID,
		Label:       workspaceLabel,
	})
	if err != nil {
		t.Fatalf("本物の herdr で workspace.rename が失敗した: %v", err)
	}
	if wsRenamed.Type != "workspace_info" {
		t.Errorf("workspace.rename の応答の type が想定と違う: got %q, want %q",
			wsRenamed.Type, "workspace_info")
	}
	if wsRenamed.Workspace.Label != workspaceLabel {
		t.Errorf("workspace.rename で書いた label が応答に載っていない: got %q, want %q",
			wsRenamed.Workspace.Label, workspaceLabel)
	}

	// 書いた label が読み戻せることも確かめる（応答だけが正しくて実体が変わっていない、を防ぐ）。
	after, err := client.PaneList(ctx, herdr.PaneListParams{WorkspaceID: opened.Workspace.WorkspaceID})
	if err != nil {
		t.Fatalf("本物の herdr で pane.list（書き込み後）が失敗した: %v", err)
	}
	if len(after.Panes) != 1 {
		t.Fatalf("書き込み後の pane が1件ではない: got %d 件", len(after.Panes))
	}
	if after.Panes[0].Label != paneLabel {
		t.Errorf("pane.rename で書いた label が pane.list で読み戻せない: got %q, want %q",
			after.Panes[0].Label, paneLabel)
	}
}

// 目的: worktree.open の path と branch が**排他である**ことを本物で確かめる
// （実測: 2026-08-20。この組み合わせを間違えて実機の着手が全件落ちた）。
// 与える情報: 使い捨てのリポジトリから切った worktree 1本と、その branch 名。
// 成功条件: path だけ・branch だけは成功し、両方渡す・どちらも渡さないは
// invalid_request で弾かれること。
//
// **test/e2e/fakeherdr_test.go の worktree.open の台本と同じ4通りを並べてある。**
// 偽 herdr と本物がずれたら、どちらかがここで落ちる。
func TestLive_WorktreeOpen_pathとbranchは片方だけ受け付ける(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, branch := addWorktree(t, repo)

	janitor := newLiveJanitor(t, client, repo.Root)
	ctx := context.Background()
	focus := false

	t.Run("path だけなら開ける", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:  worktreePath,
			Cwd:   repo.Path,
			Focus: &focus,
		})
		if err != nil {
			t.Fatalf("path だけの worktree.open が失敗した: %v", err)
		}
		janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
		janitor.TrackPane(opened.RootPane.PaneID)
	})

	t.Run("branch だけでも開ける", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Branch: branch,
			Cwd:    repo.Path,
			Focus:  &focus,
		})
		if err != nil {
			t.Fatalf("branch だけの worktree.open が失敗した: %v", err)
		}
		janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
		janitor.TrackPane(opened.RootPane.PaneID)
	})

	t.Run("両方渡すと invalid_request で弾かれる", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:   worktreePath,
			Branch: branch,
			Cwd:    repo.Path,
			Focus:  &focus,
		})
		if err == nil {
			// 通ってしまった場合も、作られたものは必ず片付ける。
			janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
			janitor.TrackPane(opened.RootPane.PaneID)
			t.Fatalf("path と branch を両方渡した worktree.open が通ってしまった: %+v", opened)
		}
		if !herdr.IsCode(err, "invalid_request") {
			t.Errorf("path と branch を両方渡したときのエラーコードが想定と違う: %v", err)
		}
	})

	t.Run("どちらも渡さないと invalid_request で弾かれる", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Cwd:   repo.Path,
			Focus: &focus,
		})
		if err == nil {
			janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
			janitor.TrackPane(opened.RootPane.PaneID)
			t.Fatalf("path も branch も渡さない worktree.open が通ってしまった: %+v", opened)
		}
		if !herdr.IsCode(err, "invalid_request") {
			t.Errorf("path も branch も渡さないときのエラーコードが想定と違う: %v", err)
		}
	})
}

// 目的: **worktree.open に cwd を渡すと workspace が2つ開き、worktree.remove では
// 片方しか閉じない**ことを本物で固定する（実測: 2026-08-24）。
// 与える情報: 使い捨てのリポジトリと、そこから切った worktree 1本。
// 成功条件: worktree.open のあとに「worktree を指す workspace」と
// 「cwd のリポジトリを指す workspace」の2つが現れ、worktree.remove の後も後者が残ること。
// 残った後者が workspace.close で閉じられること。
//
// **これは偽 herdr では見つからなかった。**issue #19 の元になった観測である。
// いまは [internal/workspace/cleanup.go](internal/workspace/cleanup.go) の段3b が
// リポジトリの親 workspace を閉じる（条件は
// [internal/workspace/repoworkspace.go](internal/workspace/repoworkspace.go) にある）。
// **この検査は「herdr がそう振る舞う」という前提のほうを固定する。**
// 前提が変わったらここが落ち、片付け側の条件を見直す合図になる。
func TestLive_WorktreeOpen_cwdを渡すとリポジトリ側のworkspaceも開く(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, _ := addWorktree(t, repo)

	janitor := newLiveJanitor(t, client, repo.Root)
	ctx := context.Background()
	focus := false

	opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
		Path:  worktreePath,
		Cwd:   repo.Path,
		Focus: &focus,
		Label: liveLabelPrefix + "/octocat/hello-world/issues/4",
	})
	if err != nil {
		t.Fatalf("本物の herdr で worktree.open が失敗した: %v", err)
	}
	janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
	janitor.TrackPane(opened.RootPane.PaneID)

	mine := myWorkspaces(t, client, repo.Root)
	if len(mine) != 2 {
		t.Fatalf("worktree.open のあとに開いている workspace の数が想定と違う: got %d 件 %v, want 2 件"+
			"（worktree のぶんと cwd のリポジトリのぶん）", len(mine), mine)
	}

	if _, err := client.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: opened.Workspace.WorkspaceID,
		Force:       true,
	}); err != nil {
		t.Fatalf("本物の herdr で worktree.remove が失敗した: %v", err)
	}
	janitor.Forget(opened.Workspace.WorkspaceID)

	left := myWorkspaces(t, client, repo.Root)
	if len(left) != 1 {
		t.Fatalf("worktree.remove のあとに残った workspace の数が想定と違う: got %d 件 %v, want 1 件"+
			"（cwd のリポジトリのぶんは worktree.remove では閉じない）", len(left), left)
	}
	// **残ったぶんは workspace.close でしか閉じられない。**後始末係が段2でこれを行う。
	if _, err := client.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: left[0]}); err != nil {
		t.Fatalf("残った workspace を workspace.close で閉じられない: %v", err)
	}
	if remaining := myWorkspaces(t, client, repo.Root); len(remaining) != 0 {
		t.Errorf("workspace.close のあとも workspace が残っている: %v", remaining)
	}
}

// 目的: **worktree.open の cwd はリポジトリ本体でなければならない**ことを本物で固定する
// （issue #19 の「cwd を渡さない」案を落とした根拠。実測: 2026-08-25）。
// 与える情報: 使い捨てのリポジトリと、そこから切った worktree 1本。
// 成功条件: cwd を省くと worktree_not_found、cwd に worktree のパスを渡すと
// linked_worktree_source で断られること。**どちらの場合も workspace は1つも開かないこと。**
//
// **なぜこの検査が要るか。**issue #19 の直し方の候補には「cwd を渡さない」があった。
// 渡さなければ workspace は1つしか開かず、閉じ残しも起きない。**だが herdr が断る。**
// リポジトリの親 workspace は herdr の必須の親であり、外せない。
func TestLive_WorktreeOpen_cwdはリポジトリ本体しか受け付けない(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, _ := addWorktree(t, repo)

	janitor := newLiveJanitor(t, client, repo.Root)
	ctx := context.Background()
	focus := false

	t.Run("cwd を省くと worktree_not_found で断られる", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:  worktreePath,
			Focus: &focus,
		})
		if err == nil {
			// 通ってしまった場合も、作られたものは必ず片付ける。
			janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
			janitor.TrackPane(opened.RootPane.PaneID)
			t.Fatalf("cwd を省いた worktree.open が通ってしまった: %+v", opened)
		}
		if !herdr.IsCode(err, "worktree_not_found") {
			t.Errorf("cwd を省いたときのエラーコードが想定と違う: %v", err)
		}
	})

	t.Run("cwd に worktree を渡すと linked_worktree_source で断られる", func(t *testing.T) {
		opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
			Path:  worktreePath,
			Cwd:   worktreePath,
			Focus: &focus,
		})
		if err == nil {
			janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
			janitor.TrackPane(opened.RootPane.PaneID)
			t.Fatalf("cwd に worktree を渡した worktree.open が通ってしまった: %+v", opened)
		}
		if !herdr.IsCode(err, "linked_worktree_source") {
			t.Errorf("cwd に worktree を渡したときのエラーコードが想定と違う: %v", err)
		}
	})

	if mine := myWorkspaces(t, client, repo.Root); len(mine) != 0 {
		t.Errorf("断られたはずなのに workspace が開いている: %v", mine)
	}
}

// 目的: **リポジトリの親 workspace を閉じると、その下の worktree の workspace と pane も
// 一緒に消える**ことを本物で固定する（実測: 2026-08-25）。
// 与える情報: 使い捨てのリポジトリと、そこから切った worktree 1本。
// 成功条件: 親を workspace.close で閉じたあと、worktree 側の workspace も pane も
// 一覧から消えていること。
//
// **これが片付けの条件そのものである。**だから
// [internal/workspace/repoworkspace.go](internal/workspace/repoworkspace.go) の
// closeRepoWorkspace は、**同じリポジトリの worktree の workspace が1つも残っていない
// ことを確かめてからしか親を閉じない。**確かめずに閉じると、別の issue が使っている
// Claude Code の pane ごと消える。
func TestLive_WorkspaceClose_親を閉じると配下のworktreeも消える(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, _ := addWorktree(t, repo)

	janitor := newLiveJanitor(t, client, repo.Root)
	ctx := context.Background()
	focus := false

	opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
		Path:  worktreePath,
		Cwd:   repo.Path,
		Focus: &focus,
		Label: liveLabelPrefix + "/octocat/hello-world/issues/19",
	})
	if err != nil {
		t.Fatalf("本物の herdr で worktree.open が失敗した: %v", err)
	}
	janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
	janitor.TrackPane(opened.RootPane.PaneID)

	// 親は「worktree 側ではないほう」である。
	parent := ""
	for _, id := range myWorkspaces(t, client, repo.Root) {
		if id != opened.Workspace.WorkspaceID {
			parent = id
		}
	}
	if parent == "" {
		t.Fatalf("リポジトリの親 workspace が見つからない: %v", myWorkspaces(t, client, repo.Root))
	}

	if _, err := client.WorkspaceClose(ctx, herdr.WorkspaceCloseParams{WorkspaceID: parent}); err != nil {
		t.Fatalf("リポジトリの親 workspace を閉じられない: %v", err)
	}
	// 親を閉じた時点で worktree 側も消えているので、後始末の対象から外す。
	janitor.Forget(opened.Workspace.WorkspaceID)

	if remaining := myWorkspaces(t, client, repo.Root); len(remaining) != 0 {
		t.Errorf("親を閉じたのに workspace が残っている: %v", remaining)
	}
	list, err := client.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		t.Fatalf("pane.list が失敗した: %v", err)
	}
	for _, p := range list.Panes {
		if p.PaneID == opened.RootPane.PaneID {
			t.Errorf("親を閉じたのに worktree 側の pane %q が残っている", p.PaneID)
		}
	}
}

// myWorkspaces は root の下を指す workspace の ID を集める。
//
// t: 呼び出し元のテスト。workspace.list に失敗したらテストを止める。
// client: herdr の呼び出し口。
// root: テストが作ったディレクトリ。
// 戻り値: 該当する workspace の ID。
func myWorkspaces(t *testing.T, client *herdr.Client, root string) []string {
	t.Helper()

	probe := &liveJanitor{t: t, client: client, roots: []string{root}}
	ids, err := probe.strayWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("本物の herdr で workspace.list が失敗した: %v", err)
	}
	return ids
}

// 目的: worktree.remove が **workspace_id を必須で取る**ことを本物で確かめる（設計 3-9）。
// path や branch では消せない。
// 与える情報: 使い捨ての worktree を開いて得た workspace の ID と、存在しない ID。
// 成功条件: 存在しない workspace_id では失敗し、正しい workspace_id では成功して、
// 応答の type が "worktree_removed" になること。
func TestLive_WorktreeRemove_workspace_idで消える(t *testing.T) {
	client := requireLiveHerdr(t)
	repo := newLiveRepo(t)
	worktreePath, _ := addWorktree(t, repo)

	janitor := newLiveJanitor(t, client, repo.Root)
	ctx := context.Background()
	focus := false

	opened, err := client.WorktreeOpen(ctx, herdr.WorktreeOpenParams{
		Path:  worktreePath,
		Cwd:   repo.Path,
		Focus: &focus,
	})
	if err != nil {
		t.Fatalf("本物の herdr で worktree.open が失敗した: %v", err)
	}
	janitor.TrackWorkspace(opened.Workspace.WorkspaceID)
	janitor.TrackPane(opened.RootPane.PaneID)

	// **存在しない ID では消えない。**「消したつもり」を成功と誤認しないことの担保である。
	if _, err := client.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: "w-continuo-live-test-nonexistent",
		Force:       true,
	}); err == nil {
		t.Errorf("存在しない workspace_id の worktree.remove が成功してしまった")
	}

	removed, err := client.WorktreeRemove(ctx, herdr.WorktreeRemoveParams{
		WorkspaceID: opened.Workspace.WorkspaceID,
		Force:       true,
	})
	if err != nil {
		t.Fatalf("本物の herdr で worktree.remove が失敗した: %v", err)
	}
	if removed.Type != "worktree_removed" {
		t.Errorf("worktree.remove の応答の type が想定と違う: got %q, want %q",
			removed.Type, "worktree_removed")
	}
	if removed.WorkspaceID != opened.Workspace.WorkspaceID {
		t.Errorf("worktree.remove の応答の workspace_id が違う: got %q, want %q",
			removed.WorkspaceID, opened.Workspace.WorkspaceID)
	}

	// ここで消し終えているので、後始末係の対象から外す。
	// **控えたままにすると、2度目の worktree.remove が失敗して偽の後始末エラーになる。**
	janitor.Forget(opened.Workspace.WorkspaceID)
}
