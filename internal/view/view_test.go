package view

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/herdr"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestAge(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	cases := []struct {
		delta time.Duration
		want  string
	}{
		{0, "0s前"},
		{5 * time.Second, "5s前"},
		{59 * time.Second, "59s前"},
		{60 * time.Second, "1m前"},
		{90 * time.Second, "1m前"},
		{59 * time.Minute, "59m前"},
		{60 * time.Minute, "1h前"},
		{23 * time.Hour, "23h前"},
		{24 * time.Hour, "1d前"},
		{50 * time.Hour, "2d前"},
	}
	for _, c := range cases {
		updated := now.Add(-c.delta)
		if got := Age(now, updated); got != c.want {
			t.Errorf("Age(delta=%v)=%q want %q", c.delta, got, c.want)
		}
	}
}

func TestAge_FutureClampedToZero(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	updated := now.Add(10 * time.Second) // 未来
	if got := Age(now, updated); got != "0s前" {
		t.Fatalf("未来時刻は 0s前 に丸める想定: %q", got)
	}
}

func TestIsStale(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	threshold := 10 * time.Minute
	if IsStale(now, now.Add(-5*time.Minute), threshold) {
		t.Fatal("閾値内は stale でない")
	}
	if !IsStale(now, now.Add(-20*time.Minute), threshold) {
		t.Fatal("閾値超過は stale")
	}
}

func TestBuildRows_Basic(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "space-a", WorkspaceID: "w1",
			Headline: "作業中", Status: state.StatusWorking, Next: "次",
			UpdatedAt: "2026-07-14T11:59:30+09:00", // 30s 前
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if len(rows) != 1 {
		t.Fatalf("行数: %d", len(rows))
	}
	r := rows[0]
	if r.Space != "space-a" || r.Headline != "作業中" || r.Status != state.StatusWorking {
		t.Fatalf("行内容が不正: %+v", r)
	}
	if r.Next != "次" {
		t.Fatalf("next 未反映: %+v", r)
	}
	if r.Age != "30s前" {
		t.Fatalf("age が不正: %q", r.Age)
	}
	if r.Stale || r.Broken || r.FutureSchema {
		t.Fatalf("フラグが想定外: %+v", r)
	}
}

func TestBuildRows_BrokenAndFuture(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/broken.json", Err: filepath.ErrBadPattern}, // 壊れた JSON を模す
		{Path: "/x/future.json", State: &state.State{
			SchemaVersion: 999, Space: "fut", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if len(rows) != 2 {
		t.Fatalf("行数: %d", len(rows))
	}

	var broken, future *Row
	for i := range rows {
		if rows[i].Broken {
			broken = &rows[i]
		}
		if rows[i].FutureSchema {
			future = &rows[i]
		}
	}
	if broken == nil {
		t.Fatal("壊れた行が Broken=true になっていない")
	}
	if broken.Space != "broken" {
		t.Fatalf("壊れた行の Space はファイル名ベースの想定: %q", broken.Space)
	}
	if future == nil {
		t.Fatal("未来スキーマ行が FutureSchema=true になっていない")
	}
}

func TestBuildRows_SortNewestFirstBrokenLast(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/old.json", State: &state.State{
			SchemaVersion: 1, Space: "old", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T10:00:00+09:00",
		}},
		{Path: "/x/broken.json", Err: filepath.ErrBadPattern},
		{Path: "/x/new.json", State: &state.State{
			SchemaVersion: 1, Space: "new", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if rows[0].Space != "new" {
		t.Fatalf("最新が先頭でない: %+v", rows)
	}
	if rows[1].Space != "old" {
		t.Fatalf("古い方が2番目でない: %+v", rows)
	}
	if !rows[2].Broken {
		t.Fatalf("壊れた行が末尾でない: %+v", rows)
	}
}

func TestBuildRows_ClassifiesSteps(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			Progress: &state.Progress{Steps: []state.Step{
				{Label: "A", State: state.StepDone},
				{Label: "B", State: state.StepDoing},
				{Label: "C", State: state.StepTodo},
				{Label: "D", State: state.StepDone},
			}},
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	r := BuildRows(results, now, 10*time.Minute)[0]
	// done_log 無し → steps.done が DoneItems に合成される（後方互換）
	if len(r.DoneItems) != 2 || r.DoneItems[0] != "A" || r.DoneItems[1] != "D" {
		t.Fatalf("DoneItems 分類が不正: %+v", r.DoneItems)
	}
	if len(r.Doing) != 1 || r.Doing[0].Text != "B" {
		t.Fatalf("Doing 分類が不正: %+v", r.Doing)
	}
	if len(r.Todo) != 1 || r.Todo[0].Text != "C" {
		t.Fatalf("Todo 分類が不正: %+v", r.Todo)
	}
}

func TestBuildRows_StepAwait(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			Progress: &state.Progress{Steps: []state.Step{
				{Label: "X", State: state.StepTodo, Await: true},
				{Label: "Y", State: state.StepDoing},
			}},
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	r := BuildRows(results, now, 10*time.Minute)[0]
	if len(r.Todo) != 1 || !r.Todo[0].Await {
		t.Fatalf("todo の await 未反映: %+v", r.Todo)
	}
	if len(r.Doing) != 1 || r.Doing[0].Await {
		t.Fatalf("doing は await=false のはず: %+v", r.Doing)
	}
	if !r.HasAwait() {
		t.Fatal("HasAwait は true のはず")
	}
}

func TestBuildRows_SortByRank(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	mk := func(space, status string, await bool) state.LoadResult {
		var steps []state.Step
		if await {
			steps = []state.Step{{Label: "x", State: state.StepTodo, Await: true}}
		}
		return state.LoadResult{Path: "/x/" + space + ".json", State: &state.State{
			SchemaVersion: 1, Space: space, Headline: "h", Status: status,
			Progress: &state.Progress{Steps: steps}, UpdatedAt: "2026-07-14T11:00:00+09:00",
		}}
	}
	results := []state.LoadResult{
		mk("done1", state.StatusDone, false),    // 完了・待機 rank2
		mk("work1", state.StatusWorking, false), // 実施中 rank1
		mk("await1", state.StatusWorking, true), // 母艦対応待ち rank0（await 昇格）
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if rows[0].Space != "await1" || rows[1].Space != "work1" || rows[2].Space != "done1" {
		t.Fatalf("ランク順が不正: %s, %s, %s", rows[0].Space, rows[1].Space, rows[2].Space)
	}
}

func TestBuildRows_NoStepsEmptyClassification(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	r := BuildRows(results, now, 10*time.Minute)[0]
	if len(r.DoneItems) != 0 || len(r.Doing) != 0 || len(r.Todo) != 0 {
		t.Fatalf("steps 無しは空分類のはず: %+v", r)
	}
}

func TestBuildRows_DoneLogNewestFirst(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			DoneLog: []state.DoneEntry{ // 末尾が最新
				{At: "t1", Text: "古い完了"},
				{At: "t2", Text: "新しい完了"},
			},
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	r := BuildRows(results, now, 10*time.Minute)[0]
	if len(r.DoneItems) != 2 || r.DoneItems[0] != "新しい完了" || r.DoneItems[1] != "古い完了" {
		t.Fatalf("done_log は新しい順であるべき: %+v", r.DoneItems)
	}
}

func TestBuildDoneItems_MergeAndCap(t *testing.T) {
	// done_log は末尾が最新。新しい順で返り、steps.done を後ろに合成、上限で丸め。
	var log []state.DoneEntry
	for i := range 7 {
		log = append(log, state.DoneEntry{Text: fmt.Sprintf("L%d", i)}) // L0..L6, L6 が最新
	}
	items, overflow := buildDoneItems(log, []string{"S1"}, 5)
	if len(items) != 5 {
		t.Fatalf("上限 5 件に丸めるべき: %+v", items)
	}
	if items[0] != "L6" {
		t.Fatalf("先頭は最新であるべき: %q", items[0])
	}
	// done_log 8件相当(7+steps1=8) → overflow 3
	if overflow != 3 {
		t.Fatalf("overflow=3 のはず: %d", overflow)
	}
}

func TestBuildDoneItems_BackwardCompatStepsOnly(t *testing.T) {
	items, overflow := buildDoneItems(nil, []string{"S1", "S2"}, 5)
	if len(items) != 2 || items[0] != "S1" || overflow != 0 {
		t.Fatalf("done_log 無しは steps.done を表示: %+v ov=%d", items, overflow)
	}
}

func TestBuildRows_Blockers(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusBlocked,
			Blockers:  []string{"API 待ち"},
			UpdatedAt: "2026-07-14T11:59:00+09:00",
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if len(rows[0].Blockers) != 1 || rows[0].Blockers[0] != "API 待ち" {
		t.Fatalf("blockers 未反映: %+v", rows[0].Blockers)
	}
}

func TestBuildRows_BrokenSortsLast(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/broken.json", Err: filepath.ErrBadPattern},
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "done1", Headline: "h", Status: state.StatusDone,
			UpdatedAt: "2026-07-14T11:00:00+09:00",
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if rows[len(rows)-1].Space != "broken" || !rows[len(rows)-1].Broken {
		t.Fatalf("broken は末尾のはず: %+v", rows)
	}
}

func TestRefreshAges(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T11:59:30+09:00",
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if rows[0].Age != "30s前" || rows[0].Stale {
		t.Fatalf("初期状態が想定外: %+v", rows[0])
	}
	// now を 1 時間進めて Age/Stale を再計算（I/O なし）
	RefreshAges(rows, now.Add(time.Hour), 10*time.Minute)
	if rows[0].Age != "1h前" {
		t.Fatalf("Age が再計算されていない: %q", rows[0].Age)
	}
	if !rows[0].Stale {
		t.Fatal("Stale が再計算されていない")
	}
}

func TestRefreshAges_SkipsRowsWithoutTime(t *testing.T) {
	// broken / skeleton 行（時刻なし）は Age を触らない
	rows := []Row{{Space: "broken", Broken: true, Age: "-"}}
	RefreshAges(rows, mustTime(t, "2026-07-14T12:00:00+09:00"), 10*time.Minute)
	if rows[0].Age != "-" {
		t.Fatalf("時刻なし行の Age が変更された: %q", rows[0].Age)
	}
}

func TestMerge_AttachesAgentStatusByID(t *testing.T) {
	rows := []Row{
		{Key: "w2F", Space: "herdr-plugin-sidenote", WorkspaceID: "w2F", Status: state.StatusWorking},
	}
	ws := []herdr.Workspace{
		{WorkspaceID: "w2F", Label: "herdr-plugin-sidenote", AgentStatus: "working"},
	}
	got := Merge(rows, ws)
	if len(got) != 1 {
		t.Fatalf("行数: %d", len(got))
	}
	if got[0].AgentStatus != "working" || !got[0].InHerdr {
		t.Fatalf("骨格が突合されていない: %+v", got[0])
	}
}

func TestMerge_MatchByLabelWhenNoID(t *testing.T) {
	rows := []Row{
		{Key: "HQ", Space: "HQ", WorkspaceID: "", Status: state.StatusPlanning},
	}
	ws := []herdr.Workspace{
		{WorkspaceID: "w27", Label: "HQ", AgentStatus: "idle"},
	}
	got := Merge(rows, ws)
	if got[0].AgentStatus != "idle" || !got[0].InHerdr {
		t.Fatalf("label 突合が効いていない: %+v", got[0])
	}
}

func TestMerge_AddsSkeletonOnlyRows(t *testing.T) {
	rows := []Row{} // state 情報なし
	ws := []herdr.Workspace{
		{WorkspaceID: "w27", Label: "HQ", AgentStatus: "idle"},
	}
	got := Merge(rows, ws)
	if len(got) != 1 {
		t.Fatalf("骨格のみ行が追加されていない: %d", len(got))
	}
	if got[0].Space != "HQ" || got[0].WorkspaceID != "w27" || got[0].AgentStatus != "idle" {
		t.Fatalf("骨格のみ行の内容が不正: %+v", got[0])
	}
	if !got[0].InHerdr {
		t.Fatal("骨格のみ行は InHerdr=true のはず")
	}
	if got[0].Status != "" || got[0].Headline != "" {
		t.Fatalf("骨格のみ行に意味情報が付いている: %+v", got[0])
	}
}

func TestMerge_StateNotInHerdrKeptWithFlag(t *testing.T) {
	rows := []Row{
		{Key: "gone", Space: "gone", WorkspaceID: "wX", Status: state.StatusWorking},
	}
	ws := []herdr.Workspace{
		{WorkspaceID: "w27", Label: "HQ", AgentStatus: "idle"},
	}
	got := Merge(rows, ws)
	// state 行 + 骨格のみ行(HQ) = 2
	if len(got) != 2 {
		t.Fatalf("行数: %d rows=%+v", len(got), got)
	}
	var gone *Row
	for i := range got {
		if got[i].WorkspaceID == "wX" {
			gone = &got[i]
		}
	}
	if gone == nil {
		t.Fatal("herdr から消えた state 行が失われた")
	}
	if gone.InHerdr {
		t.Fatal("herdr に無い行は InHerdr=false のはず")
	}
}

func TestMerge_NilWorkspacesKeepsRows(t *testing.T) {
	rows := []Row{{Key: "w1", Space: "s", WorkspaceID: "w1", Status: state.StatusWorking}}
	got := Merge(rows, nil)
	if len(got) != 1 || got[0].InHerdr {
		t.Fatalf("骨格なしでは state 行をそのまま(InHerdr=false)返す想定: %+v", got)
	}
}

func TestBuildRows_StaleFlag(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00+09:00")
	results := []state.LoadResult{
		{Path: "/x/w1.json", State: &state.State{
			SchemaVersion: 1, Space: "s", Headline: "h", Status: state.StatusWorking,
			UpdatedAt: "2026-07-14T11:00:00+09:00", // 1h 前
		}},
	}
	rows := BuildRows(results, now, 10*time.Minute)
	if !rows[0].Stale {
		t.Fatal("1h 前で閾値 10m なら stale のはず")
	}
}
