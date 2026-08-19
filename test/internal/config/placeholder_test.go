package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: tracker.provider.owner が雛形のプレースホルダのままなら、キーを名指しして起動を止めることを確認する。
// 与える情報: owner に __FILL_ME__ を書いた front matter（他のキーは検証を通る値）。
// 成功条件: config.Load がエラーを返し、その文言に tracker.provider.owner と
// プレースホルダの値 __FILL_ME__ が含まれること。
func TestLoad_ownerがプレースホルダのままなら落ちる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: " + config.Placeholder + "\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n"
	path := writeWorkflow(t, front, "本文\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("owner がプレースホルダのままなのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tracker.provider.owner") {
		t.Errorf("エラーの文言に tracker.provider.owner が名指しされていない: %s", msg)
	}
	if !strings.Contains(msg, config.Placeholder) {
		t.Errorf("エラーの文言にプレースホルダの値 %s が含まれていない: %s", config.Placeholder, msg)
	}
	if !strings.Contains(msg, "プレースホルダ") {
		t.Errorf("エラーの文言が「プレースホルダのまま」であることを伝えていない: %s", msg)
	}
}

// 目的: project_number が 0 のとき、値域の説明ではなく「プレースホルダのまま」と報告することを確認する。
// 雛形が 0 を置いているので、0 は「まだ埋めていない」という意味である（設計 3-32）。
// 与える情報: project_number に 0 を書いた front matter（owner は埋めてある）。
// 成功条件: エラーの文言が tracker.provider.project_number を名指しし、
// 既存の値域の検証の文言（「0より大きい整数にすること」）が出ていないこと。
func TestLoad_project_numberが0ならプレースホルダとして報告する(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 0\n" +
		"    status_field: Status\n"
	path := writeWorkflow(t, front, "本文\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("project_number が 0 なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tracker.provider.project_number") {
		t.Errorf("エラーの文言に tracker.provider.project_number が名指しされていない: %s", msg)
	}
	if !strings.Contains(msg, "プレースホルダ") {
		t.Errorf("エラーの文言が「プレースホルダのまま」であることを伝えていない: %s", msg)
	}
	if strings.Contains(msg, "0より大きい整数にすること") {
		t.Errorf("プレースホルダの検出が値の検証より後になっている（値域の説明が出ている）: %s", msg)
	}
}

// 目的: project_number のキーそのものが書かれていない場合も、0 のときと同じ文言で落ちることを確認する。
// 読み込むと 0 になるため、書かれていないのか 0 が書かれているのかは区別できない（設計 3-32）。
// 与える情報: project_number のキーを省いた front matter。
// 成功条件: エラーの文言が tracker.provider.project_number を名指しし、「プレースホルダ」を含むこと。
func TestLoad_project_numberのキーが無くても同じ文言で落ちる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    status_field: Status\n"
	path := writeWorkflow(t, front, "本文\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("project_number が未設定なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tracker.provider.project_number") {
		t.Errorf("エラーの文言に tracker.provider.project_number が名指しされていない: %s", msg)
	}
	if !strings.Contains(msg, "プレースホルダ") {
		t.Errorf("エラーの文言が「プレースホルダのまま」であることを伝えていない: %s", msg)
	}
}

// 目的: プレースホルダが2つとも残っていたら、1つのエラーに2件とも並べて出すことを確認する。
// 1件ずつ直させると、埋め忘れの数だけ起動をやり直すことになる。
// 与える情報: owner と project_number の両方がプレースホルダのままの front matter。
// 成功条件: 返ったエラーが1つで、その文言に両方のキー名が含まれ、件数（2 件）が示されること。
func TestLoad_プレースホルダが2件残っていたら1つのエラーに並べる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: " + config.Placeholder + "\n" +
		"    project_number: 0\n" +
		"    status_field: Status\n"
	path := writeWorkflow(t, front, "本文\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("プレースホルダが2件残っているのにエラーが返らなかった")
	}
	msg := err.Error()
	for _, key := range []string{"tracker.provider.owner", "tracker.provider.project_number"} {
		if !strings.Contains(msg, key) {
			t.Errorf("1つのエラーに %s が並んでいない: %s", key, msg)
		}
	}
	if !strings.Contains(msg, "2 件") {
		t.Errorf("エラーの文言に残っている件数が示されていない: %s", msg)
	}
}

// 目的: プレースホルダを埋めた設定は、プレースホルダの検査で止まらないことを確認する。
// 与える情報: owner と project_number に実際の値を書いた front matter。
// 成功条件: config.Load がエラーを返さず、埋めた値が反映されていること。
func TestLoad_プレースホルダを埋めた設定は通る(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 7\n" +
		"    status_field: Status\n"
	path := writeWorkflow(t, front, "本文\n")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("プレースホルダを埋めた設定が読み込めなかった: %v", err)
	}
	if loaded.Config.Tracker.Provider.Owner != "acme" {
		t.Errorf("tracker.provider.owner が反映されていない: got %q, want %q", loaded.Config.Tracker.Provider.Owner, "acme")
	}
	if loaded.Config.Tracker.Provider.ProjectNumber != 7 {
		t.Errorf("tracker.provider.project_number が反映されていない: got %d, want %d", loaded.Config.Tracker.Provider.ProjectNumber, 7)
	}
}
