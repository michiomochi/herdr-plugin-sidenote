// Package cli は母艦が状況を書き込むための操作（set/update/clear/list）の
// コアロジックを提供する。フラグ解釈・stdout などの副作用は最小化し、
// マージ・構築などの純ロジックをテスト可能な関数として切り出している。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
)

// Options は set/update に共通の入力。
// 各ポインタが nil のフィールドは「指定なし」を表す。
// update ではこの指定分だけを既存 State に上書きする。
type Options struct {
	Space       *string
	WorkspaceID *string
	Headline    *string
	Status      *string
	Next        *string
	Summary     *string
	Percent     *int
	Steps       *[]state.Step
	Blockers    *[]string
	Notes       *[]string
}

// ParseStep は "label:state" または "label:state:await" 形式を state.Step に
// パースする。末尾が正確に ":await" のときだけ先に剥がして Await=true とし、
// 残りを label:state（最後の ':' で分割）として解釈するため、label に ':' を
// 含むケースとも両立する。
func ParseStep(s string) (state.Step, error) {
	await := false
	if strings.HasSuffix(s, ":await") {
		await = true
		s = strings.TrimSuffix(s, ":await")
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return state.Step{}, fmt.Errorf("step は \"label:state[:await]\" 形式: %q", s)
	}
	label := strings.TrimSpace(s[:i])
	st := strings.TrimSpace(s[i+1:])
	if label == "" {
		return state.Step{}, fmt.Errorf("step の label が空: %q", s)
	}
	switch st {
	case state.StepTodo, state.StepDoing, state.StepDone:
	default:
		return state.Step{}, fmt.Errorf("step の state が不正 (todo/doing/done): %q", st)
	}
	return state.Step{Label: label, State: st, Await: await}, nil
}

// BuildForSet は set 用に新しい State を構築する（未指定項目はゼロ値）。
// schema_version と updated_at は自動付与し、最後に検証する。
func BuildForSet(o Options, now string) (*state.State, error) {
	s := &state.State{
		SchemaVersion: state.CurrentSchemaVersion,
		UpdatedAt:     now,
	}
	applyOptions(s, o)
	if err := state.Validate(s); err != nil {
		return nil, err
	}
	return s, nil
}

// ApplyUpdate は既存 State に o の指定分だけを上書きした新しい State を返す
// （元 State は破壊しない）。updated_at と schema_version は最新化する。
func ApplyUpdate(existing *state.State, o Options, now string) *state.State {
	s := cloneState(existing)
	s.SchemaVersion = state.CurrentSchemaVersion
	s.UpdatedAt = now
	applyOptions(s, o)
	return s
}

// applyOptions は o の非 nil フィールドを s に反映する。
func applyOptions(s *state.State, o Options) {
	if o.Space != nil {
		s.Space = *o.Space
	}
	if o.WorkspaceID != nil {
		s.WorkspaceID = *o.WorkspaceID
	}
	if o.Headline != nil {
		s.Headline = *o.Headline
	}
	if o.Status != nil {
		s.Status = *o.Status
	}
	if o.Next != nil {
		s.Next = *o.Next
	}
	if o.Blockers != nil {
		s.Blockers = append([]string(nil), (*o.Blockers)...)
	}
	if o.Notes != nil {
		s.Notes = append([]string(nil), (*o.Notes)...)
	}
	// Progress 系（summary / percent / steps）は指定があれば Progress を保証する。
	if o.Summary != nil || o.Percent != nil || o.Steps != nil {
		if s.Progress == nil {
			s.Progress = &state.Progress{}
		}
		if o.Summary != nil {
			s.Progress.Summary = *o.Summary
		}
		if o.Percent != nil {
			p := *o.Percent
			s.Progress.Percent = &p
		}
		if o.Steps != nil {
			s.Progress.Steps = append([]state.Step(nil), (*o.Steps)...)
		}
	}
}

// cloneState は State をディープコピーする（Progress / スライスも複製）。
func cloneState(in *state.State) *state.State {
	out := *in
	if in.Progress != nil {
		p := *in.Progress
		if in.Progress.Percent != nil {
			v := *in.Progress.Percent
			p.Percent = &v
		}
		if in.Progress.Steps != nil {
			p.Steps = append([]state.Step(nil), in.Progress.Steps...)
		}
		out.Progress = &p
	}
	if in.Blockers != nil {
		out.Blockers = append([]string(nil), in.Blockers...)
	}
	if in.Notes != nil {
		out.Notes = append([]string(nil), in.Notes...)
	}
	if in.DoneLog != nil {
		out.DoneLog = append([]state.DoneEntry(nil), in.DoneLog...)
	}
	return &out
}

// Set は set コマンド本体。State を構築して dir に保存する。
// done_log は set の上書き対象外とし、既存ファイルがあれば引き継ぐ（非破壊）。
func Set(dir string, o Options, now string) error {
	s, err := BuildForSet(o, now)
	if err != nil {
		return err
	}
	if existing, lerr := state.Load(filepath.Join(dir, state.FileNameForKey(s.Key()))); lerr == nil && existing != nil {
		s.DoneLog = existing.DoneLog
	}
	return state.Save(dir, s)
}

// Done は done コマンド本体。key に対応する既存 State の done_log に
// {at: now, text} を append する（上限は state 側で丸め）。
func Done(dir, key, text, now string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("--text は必須です")
	}
	path := filepath.Join(dir, state.FileNameForKey(key))
	existing, err := state.Load(path)
	if err != nil {
		return fmt.Errorf("done の対象が読み込めない (%s): %w", key, err)
	}
	existing.DoneLog = state.AppendDoneEntry(existing.DoneLog, text, now)
	existing.UpdatedAt = now // 完了追記も更新とみなし鮮度を進める
	if err := state.Validate(existing); err != nil {
		return err
	}
	return state.Save(dir, existing)
}

// Update は update コマンド本体。dir 内の key に対応する既存 State を
// 読み込み、o の指定分をマージして保存する。
func Update(dir, key string, o Options, now string) error {
	path := filepath.Join(dir, state.FileNameForKey(key))
	existing, err := state.Load(path)
	if err != nil {
		return fmt.Errorf("更新対象が読み込めない (%s): %w", key, err)
	}
	updated := ApplyUpdate(existing, o, now)
	if err := state.Validate(updated); err != nil {
		return err
	}
	return state.Save(dir, updated)
}

// Clear は clear コマンド本体。key に対応する state ファイルを削除する。
func Clear(dir, key string) error {
	path := filepath.Join(dir, state.FileNameForKey(key))
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("削除対象が存在しない (%s): %w", key, err)
	}
	return os.Remove(path)
}

// List は list コマンド本体。dir 内の全 state を人間可読な JSON 配列で w に書く。
func List(dir string, w io.Writer) error {
	results, err := state.LoadAll(dir)
	if err != nil {
		return err
	}
	type entry struct {
		Path  string       `json:"path"`
		State *state.State `json:"state,omitempty"`
		Error string       `json:"error,omitempty"`
	}
	var out []entry
	for _, r := range results {
		e := entry{Path: r.Path, State: r.State}
		if r.Err != nil {
			e.Error = r.Err.Error()
		}
		out = append(out, e)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
