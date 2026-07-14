package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
)

const testNow = "2026-07-14T12:00:00+09:00"

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

func TestParseStep(t *testing.T) {
	got, err := ParseStep("ブレスト:done")
	if err != nil {
		t.Fatalf("パース失敗: %v", err)
	}
	if got.Label != "ブレスト" || got.State != state.StepDone {
		t.Fatalf("想定外: %+v", got)
	}
	// label に ':' を含む場合は最後の ':' で分割
	got, err = ParseStep("a:b:doing")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "a:b" || got.State != state.StepDoing {
		t.Fatalf("想定外: %+v", got)
	}
	// 不正な state はエラー
	if _, err := ParseStep("x:bogus"); err == nil {
		t.Fatal("不正 state をエラーにすべき")
	}
	// ':' なしはエラー
	if _, err := ParseStep("nostate"); err == nil {
		t.Fatal("':' なしはエラーにすべき")
	}
}

func TestBuildForSet(t *testing.T) {
	o := Options{
		Space:    strp("my-space"),
		Headline: strp("作業中"),
		Status:   strp(state.StatusWorking),
		Next:     strp("次の一手"),
		Percent:  intp(30),
	}
	s, err := BuildForSet(o, testNow)
	if err != nil {
		t.Fatalf("BuildForSet 失敗: %v", err)
	}
	if s.SchemaVersion != state.CurrentSchemaVersion {
		t.Fatalf("schema_version 未設定: %d", s.SchemaVersion)
	}
	if s.UpdatedAt != testNow {
		t.Fatalf("updated_at が now でない: %s", s.UpdatedAt)
	}
	if s.Space != "my-space" || s.Headline != "作業中" || s.Status != state.StatusWorking {
		t.Fatalf("必須項目が反映されていない: %+v", s)
	}
	if s.Next != "次の一手" {
		t.Fatalf("next 未反映: %+v", s)
	}
	if s.Progress == nil || s.Progress.Percent == nil || *s.Progress.Percent != 30 {
		t.Fatalf("percent 未反映: %+v", s.Progress)
	}
	if err := state.Validate(s); err != nil {
		t.Fatalf("BuildForSet の結果が検証を通らない: %v", err)
	}
}

func fullExisting() *state.State {
	return &state.State{
		SchemaVersion: state.CurrentSchemaVersion,
		Space:         "my-space",
		WorkspaceID:   "w9",
		Headline:      "元の見出し",
		Status:        state.StatusWorking,
		Progress:      &state.Progress{Summary: "元サマリ", Percent: intp(20)},
		Next:          "元の次",
		Blockers:      []string{"元ブロッカー"},
		UpdatedAt:     "2026-07-14T09:00:00+09:00",
	}
}

func TestApplyUpdate_StatusOnly(t *testing.T) {
	existing := fullExisting()
	o := Options{Status: strp(state.StatusReview)}
	got := ApplyUpdate(existing, o, testNow)

	if got.Status != state.StatusReview {
		t.Fatalf("status 未更新: %s", got.Status)
	}
	if got.UpdatedAt != testNow {
		t.Fatalf("updated_at 未更新: %s", got.UpdatedAt)
	}
	// 他は保持
	if got.Headline != "元の見出し" || got.Next != "元の次" {
		t.Fatalf("未指定フィールドが失われた: %+v", got)
	}
	if got.Progress == nil || got.Progress.Summary != "元サマリ" {
		t.Fatalf("Progress が失われた: %+v", got.Progress)
	}
	// 元オブジェクトを破壊していない
	if existing.Status != state.StatusWorking {
		t.Fatalf("元 State が破壊された: %s", existing.Status)
	}
}

func TestApplyUpdate_PercentCreatesProgress(t *testing.T) {
	existing := fullExisting()
	existing.Progress = nil
	o := Options{Percent: intp(75)}
	got := ApplyUpdate(existing, o, testNow)
	if got.Progress == nil || got.Progress.Percent == nil || *got.Progress.Percent != 75 {
		t.Fatalf("percent 更新で Progress が生成されていない: %+v", got.Progress)
	}
}

func TestApplyUpdate_PercentPreservesSummary(t *testing.T) {
	existing := fullExisting()
	o := Options{Percent: intp(90)}
	got := ApplyUpdate(existing, o, testNow)
	if got.Progress.Summary != "元サマリ" {
		t.Fatalf("percent 更新で summary が失われた: %+v", got.Progress)
	}
	if *got.Progress.Percent != 90 {
		t.Fatalf("percent 未更新")
	}
}

func TestApplyUpdate_StepsReplace(t *testing.T) {
	existing := fullExisting()
	steps := []state.Step{{Label: "s1", State: state.StepDone}}
	o := Options{Steps: &steps}
	got := ApplyUpdate(existing, o, testNow)
	if len(got.Progress.Steps) != 1 || got.Progress.Steps[0].Label != "s1" {
		t.Fatalf("steps 置換が反映されていない: %+v", got.Progress.Steps)
	}
}

func TestApplyUpdate_BlockersReplace(t *testing.T) {
	existing := fullExisting()
	bl := []string{}
	o := Options{Blockers: &bl}
	got := ApplyUpdate(existing, o, testNow)
	if len(got.Blockers) != 0 {
		t.Fatalf("blockers を空に更新できていない: %+v", got.Blockers)
	}
}

func TestSet_WritesValidFile(t *testing.T) {
	dir := t.TempDir()
	o := Options{
		Space:       strp("s"),
		WorkspaceID: strp("w1"),
		Headline:    strp("h"),
		Status:      strp(state.StatusPlanning),
	}
	if err := Set(dir, o, testNow); err != nil {
		t.Fatalf("Set 失敗: %v", err)
	}
	loaded, err := state.Load(filepath.Join(dir, "w1.json"))
	if err != nil {
		t.Fatalf("保存ファイルが読めない: %v", err)
	}
	if loaded.Headline != "h" {
		t.Fatalf("内容不一致: %+v", loaded)
	}
}

func TestSet_MissingRequiredFails(t *testing.T) {
	dir := t.TempDir()
	o := Options{Space: strp("s")} // headline / status 欠如
	if err := Set(dir, o, testNow); err == nil {
		t.Fatal("必須欠如の Set がエラーにならなかった")
	}
}

func TestUpdate_MergesExistingFile(t *testing.T) {
	dir := t.TempDir()
	// 事前に set
	if err := Set(dir, Options{
		Space: strp("s"), WorkspaceID: strp("w1"),
		Headline: strp("h"), Status: strp(state.StatusWorking),
	}, "2026-07-14T09:00:00+09:00"); err != nil {
		t.Fatal(err)
	}
	// status だけ update
	if err := Update(dir, "w1", Options{Status: strp(state.StatusDone)}, testNow); err != nil {
		t.Fatalf("Update 失敗: %v", err)
	}
	loaded, _ := state.Load(filepath.Join(dir, "w1.json"))
	if loaded.Status != state.StatusDone {
		t.Fatalf("status 未更新: %s", loaded.Status)
	}
	if loaded.Headline != "h" {
		t.Fatalf("headline が失われた: %s", loaded.Headline)
	}
	if loaded.UpdatedAt != testNow {
		t.Fatalf("updated_at 未更新: %s", loaded.UpdatedAt)
	}
}

func TestUpdate_MissingTargetFails(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, "nope", Options{Status: strp(state.StatusDone)}, testNow); err == nil {
		t.Fatal("存在しない対象の Update はエラーにすべき")
	}
}

func TestClear_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	if err := Set(dir, Options{
		Space: strp("s"), WorkspaceID: strp("w1"),
		Headline: strp("h"), Status: strp(state.StatusWorking),
	}, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir, "w1"); err != nil {
		t.Fatalf("Clear 失敗: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "w1.json")); !os.IsNotExist(err) {
		t.Fatal("ファイルが削除されていない")
	}
}

func TestSetUpdateClear_KeyWithSlashSymmetry(t *testing.T) {
	dir := t.TempDir()
	// space に '/' を含み workspace_id 無し → 保存ファイル名は sanitize される
	if err := Set(dir, Options{
		Space: strp("a/b"), Headline: strp("h"), Status: strp(state.StatusWorking),
	}, testNow); err != nil {
		t.Fatal(err)
	}
	// 同じ key で update/clear が対象を見つけられること（sanitize 対称性）
	if err := Update(dir, "a/b", Options{Status: strp(state.StatusDone)}, testNow); err != nil {
		t.Fatalf("sanitize 非対称で更新に失敗: %v", err)
	}
	if err := Clear(dir, "a/b"); err != nil {
		t.Fatalf("sanitize 非対称で削除に失敗: %v", err)
	}
}

func TestClear_MissingIsError(t *testing.T) {
	dir := t.TempDir()
	if err := Clear(dir, "nope"); err == nil {
		t.Fatal("存在しない対象の Clear はエラーにすべき")
	}
}

func TestList_Output(t *testing.T) {
	dir := t.TempDir()
	_ = Set(dir, Options{
		Space: strp("space-a"), WorkspaceID: strp("w1"),
		Headline: strp("見出しA"), Status: strp(state.StatusWorking),
	}, testNow)
	var buf bytes.Buffer
	if err := List(dir, &buf); err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "space-a") || !strings.Contains(out, "見出しA") {
		t.Fatalf("List 出力に内容が含まれない: %q", out)
	}
}
