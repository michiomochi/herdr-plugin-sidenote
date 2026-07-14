package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intp(n int) *int { return &n }

// validState は検証を通る最小＋αの State を返す。
func validState() *State {
	return &State{
		SchemaVersion: CurrentSchemaVersion,
		Space:         "herdr-plugin-sidenote",
		WorkspaceID:   "w2F",
		Headline:      "設計ブレスト中",
		Status:        StatusWorking,
		UpdatedAt:     "2026-07-14T10:50:00+09:00",
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := Validate(validState()); err != nil {
		t.Fatalf("正常な State が検証エラー: %v", err)
	}
}

func TestValidate_FullValid(t *testing.T) {
	s := validState()
	s.Progress = &Progress{
		Summary: "4軸を整理中",
		Steps: []Step{
			{Label: "ブレスト", State: StepDone},
			{Label: "設計Doc", State: StepDoing},
		},
		Percent: intp(40),
	}
	s.Next = "設計Docを書く"
	s.Blockers = []string{}
	s.Notes = []string{"メモ"}
	if err := Validate(s); err != nil {
		t.Fatalf("フル項目の State が検証エラー: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]func(*State){
		"space空":          func(s *State) { s.Space = "" },
		"headline空":       func(s *State) { s.Headline = "" },
		"status不正":        func(s *State) { s.Status = "bogus" },
		"updated_at空":     func(s *State) { s.UpdatedAt = "" },
		"updated_at不正":    func(s *State) { s.UpdatedAt = "2026/07/14 10:00" },
		"schema_version0": func(s *State) { s.SchemaVersion = 0 },
		"percent負":        func(s *State) { s.Progress = &Progress{Percent: intp(-1)} },
		"percent超過":       func(s *State) { s.Progress = &Progress{Percent: intp(101)} },
		"step_state不正":    func(s *State) { s.Progress = &Progress{Steps: []Step{{Label: "x", State: "bogus"}}} },
		"step_label空":     func(s *State) { s.Progress = &Progress{Steps: []Step{{Label: "", State: StepTodo}}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := validState()
			mutate(s)
			if err := Validate(s); err == nil {
				t.Fatalf("検証エラーを期待したが nil: %s", name)
			}
		})
	}
}

func TestValidate_AllStatuses(t *testing.T) {
	for _, st := range []string{StatusPlanning, StatusWorking, StatusBlocked, StatusReview, StatusDone} {
		s := validState()
		s.Status = st
		if err := Validate(s); err != nil {
			t.Fatalf("status=%q が検証エラー: %v", st, err)
		}
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	s := validState()
	s.Progress = &Progress{Summary: "x", Percent: intp(40)}
	if err := Save(dir, s); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	got, err := Load(filepath.Join(dir, FileName(s)))
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if got.Space != s.Space || got.Headline != s.Headline || got.Status != s.Status {
		t.Fatalf("往復で内容が変化: got=%+v want=%+v", got, s)
	}
	if got.Progress == nil || got.Progress.Percent == nil || *got.Progress.Percent != 40 {
		t.Fatalf("Progress が往復で失われた: %+v", got.Progress)
	}
}

func TestSave_Atomic_NoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := validState()
	if err := Save(dir, s); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("一時ファイルが残っている可能性: %v", names)
	}
	if entries[0].Name() != FileName(s) {
		t.Fatalf("想定外のファイル名: %s", entries[0].Name())
	}
}

func TestSave_InvalidRejected(t *testing.T) {
	dir := t.TempDir()
	s := validState()
	s.Status = "bogus"
	if err := Save(dir, s); err == nil {
		t.Fatal("不正な State の Save がエラーにならなかった")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("不正な Save でファイルが書かれた: %d 件", len(entries))
	}
}

func TestFileName(t *testing.T) {
	// workspace_id があればそれを使う
	s := validState()
	if got := FileName(s); got != "w2F.json" {
		t.Fatalf("workspace_id ベースのファイル名が不正: %s", got)
	}
	// workspace_id が空なら space を使う
	s.WorkspaceID = ""
	s.Space = "my-space"
	if got := FileName(s); got != "my-space.json" {
		t.Fatalf("space ベースのファイル名が不正: %s", got)
	}
	// パス区切りは無害化される
	s.WorkspaceID = "a/b"
	if got := FileName(s); strings.ContainsRune(got, '/') {
		t.Fatalf("パス区切りが残っている: %s", got)
	}
}

func TestFileNameForKey(t *testing.T) {
	if got := FileNameForKey("w1"); got != "w1.json" {
		t.Fatalf("通常キー: %s", got)
	}
	if got := FileNameForKey("a/b"); strings.ContainsRune(got, '/') {
		t.Fatalf("パス区切りが無害化されていない: %s", got)
	}
	if got := FileNameForKey(""); got != "state.json" {
		t.Fatalf("空キーのフォールバック: %s", got)
	}
	// FileName(State) と FileNameForKey(Key()) は一致する
	s := validState()
	if FileName(s) != FileNameForKey(s.Key()) {
		t.Fatal("FileName と FileNameForKey が非対称")
	}
}

func TestResolveDir_EnvOverride(t *testing.T) {
	t.Setenv("SIDENOTE_STATE_DIR", "/tmp/sidenote-test-xyz")
	got, err := ResolveDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/sidenote-test-xyz" {
		t.Fatalf("環境変数が優先されていない: %s", got)
	}
}

func TestResolveDir_Default(t *testing.T) {
	t.Setenv("SIDENOTE_STATE_DIR", "")
	got, err := ResolveDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join(".herdr", "sidenote", "state")) {
		t.Fatalf("既定パスが想定と異なる: %s", got)
	}
}

func TestLoadAll_MultipleAndBroken(t *testing.T) {
	dir := t.TempDir()
	// 正常 2 件
	a := validState()
	a.WorkspaceID = "w1"
	b := validState()
	b.WorkspaceID = "w2"
	if err := Save(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, b); err != nil {
		t.Fatal(err)
	}
	// 壊れた JSON
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// json 以外は無視される
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll 自体はエラーにしない想定: %v", err)
	}
	var ok, bad int
	for _, r := range results {
		if r.Err != nil {
			bad++
		} else {
			ok++
		}
	}
	if ok != 2 {
		t.Fatalf("正常ロード件数が想定外: %d", ok)
	}
	if bad != 1 {
		t.Fatalf("壊れた JSON がエラー結果として1件出る想定: %d", bad)
	}
}

func TestLoadAll_MissingDirIsEmpty(t *testing.T) {
	results, err := LoadAll(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("存在しないディレクトリはエラーにしない想定: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("存在しないディレクトリは 0 件の想定: %d", len(results))
	}
}

func TestLoad_UnknownSchemaVersionStillLoads(t *testing.T) {
	dir := t.TempDir()
	future := `{"schema_version":999,"space":"x","headline":"h","status":"working","updated_at":"2026-07-14T10:50:00+09:00"}`
	p := filepath.Join(dir, "future.json")
	if err := os.WriteFile(p, []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("未知バージョンでも読めるべき: %v", err)
	}
	if !got.IsFutureSchema() {
		t.Fatal("未来バージョンは IsFutureSchema() が真になるべき")
	}
}
