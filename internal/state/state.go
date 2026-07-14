// Package state は sidenote の「意味情報」（母艦が書く各 space の状況）を
// 表す State スキーマと、その永続化（atomic write）・読み込み・検証を担う。
//
// この層は言語非依存の設計（docs/design.md §5・§6）を Go で表現したものであり、
// TUI 描画や herdr CLI 連携には依存しない純ロジックとして保つ。
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CurrentSchemaVersion は本実装が書き出すスキーマのバージョン。
const CurrentSchemaVersion = 1

// MaxDoneLog は done_log の保存上限（append 時に古い順で丸める）。
const MaxDoneLog = 100

// status（母艦視点の意味的ステータス）の取り得る値。
const (
	StatusPlanning = "planning"
	StatusWorking  = "working"
	StatusBlocked  = "blocked"
	StatusReview   = "review"
	StatusDone     = "done"
)

// progress.steps[].state の取り得る値。
const (
	StepTodo  = "todo"
	StepDoing = "doing"
	StepDone  = "done"
)

var validStatuses = map[string]bool{
	StatusPlanning: true, StatusWorking: true, StatusBlocked: true,
	StatusReview: true, StatusDone: true,
}

var validStepStates = map[string]bool{
	StepTodo: true, StepDoing: true, StepDone: true,
}

// State は 1 つの space の状況（意味情報）。docs/design.md §6 のスキーマに対応する。
type State struct {
	SchemaVersion int         `json:"schema_version"`
	Space         string      `json:"space"`
	WorkspaceID   string      `json:"workspace_id,omitempty"`
	Headline      string      `json:"headline"`
	Status        string      `json:"status"`
	Progress      *Progress   `json:"progress,omitempty"`
	Next          string      `json:"next,omitempty"`
	Blockers      []string    `json:"blockers,omitempty"`
	Notes         []string    `json:"notes,omitempty"`
	DoneLog       []DoneEntry `json:"done_log,omitempty"`
	UpdatedAt     string      `json:"updated_at"`
}

// DoneEntry は完了履歴（done_log）の 1 件。append 専用で積み上げる。
type DoneEntry struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// AppendDoneEntry は done_log に 1 件追加し、保存上限 MaxDoneLog を超えたら
// 古い順に切り捨てる。末尾が最新。
func AppendDoneEntry(log []DoneEntry, text, now string) []DoneEntry {
	log = append(log, DoneEntry{At: now, Text: text})
	if len(log) > MaxDoneLog {
		log = append([]DoneEntry(nil), log[len(log)-MaxDoneLog:]...)
	}
	return log
}

// Progress は進捗の内訳。すべて任意項目。
type Progress struct {
	Summary string `json:"summary,omitempty"`
	Steps   []Step `json:"steps,omitempty"`
	Percent *int   `json:"percent,omitempty"`
}

// Step は progress.steps の 1 要素。
type Step struct {
	Label string `json:"label"`
	State string `json:"state"`
}

// IsFutureSchema は state が本実装より新しいスキーマで書かれているかを返す。
// TUI はこれを見て「要更新」を薄く表示し、クラッシュせず読み続ける。
func (s *State) IsFutureSchema() bool {
	return s.SchemaVersion > CurrentSchemaVersion
}

// UpdatedTime は updated_at を time.Time としてパースする。
func (s *State) UpdatedTime() (time.Time, error) {
	return time.Parse(time.RFC3339, s.UpdatedAt)
}

// Key は state の突合キー（workspace_id 優先、無ければ space）を返す。
func (s *State) Key() string {
	if s.WorkspaceID != "" {
		return s.WorkspaceID
	}
	return s.Space
}

// Validate は State が必須項目・enum・範囲を満たすか検証する。
func Validate(s *State) error {
	if s == nil {
		return fmt.Errorf("state が nil")
	}
	if s.SchemaVersion < 1 {
		return fmt.Errorf("schema_version は 1 以上が必要 (got %d)", s.SchemaVersion)
	}
	if strings.TrimSpace(s.Space) == "" {
		return fmt.Errorf("space は必須")
	}
	if strings.TrimSpace(s.Headline) == "" {
		return fmt.Errorf("headline は必須")
	}
	if !validStatuses[s.Status] {
		return fmt.Errorf("status が不正: %q", s.Status)
	}
	if strings.TrimSpace(s.UpdatedAt) == "" {
		return fmt.Errorf("updated_at は必須")
	}
	if _, err := time.Parse(time.RFC3339, s.UpdatedAt); err != nil {
		return fmt.Errorf("updated_at が RFC3339 でない: %q", s.UpdatedAt)
	}
	for i, e := range s.DoneLog {
		if strings.TrimSpace(e.Text) == "" {
			return fmt.Errorf("done_log[%d].text は必須", i)
		}
	}
	if s.Progress != nil {
		if p := s.Progress.Percent; p != nil && (*p < 0 || *p > 100) {
			return fmt.Errorf("percent は 0〜100 (got %d)", *p)
		}
		for i, st := range s.Progress.Steps {
			if strings.TrimSpace(st.Label) == "" {
				return fmt.Errorf("steps[%d].label は必須", i)
			}
			if !validStepStates[st.State] {
				return fmt.Errorf("steps[%d].state が不正: %q", i, st.State)
			}
		}
	}
	return nil
}

// FileName は state を保存するファイル名（<key>.json）を返す。
func FileName(s *State) string {
	return FileNameForKey(s.Key())
}

// FileNameForKey は突合キーから state ファイル名を返す。
// パス区切り等を無害化するため、書き込み(Save)・読み込み(Update/Clear)の
// 双方で必ずこの関数を通してファイル名を解決すること。
func FileNameForKey(key string) string {
	k := sanitize(key)
	if k == "" {
		k = "state"
	}
	return k + ".json"
}

// sanitize はファイル名として危険な文字を "-" に置換する。
func sanitize(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', 0, ':':
			return '-'
		}
		return r
	}
	out := strings.Map(repl, s)
	// 先頭ドットは隠しファイル化を避けるため置換
	out = strings.TrimPrefix(out, ".")
	return strings.TrimSpace(out)
}

// ResolveDir は state ディレクトリのパスを返す。
// SIDENOTE_STATE_DIR があればそれを、無ければ ~/.herdr/sidenote/state を返す。
func ResolveDir() (string, error) {
	if v := os.Getenv("SIDENOTE_STATE_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリ解決に失敗: %w", err)
	}
	return filepath.Join(home, ".herdr", "sidenote", "state"), nil
}

// EnsureDir は state ディレクトリが無ければ作成する。
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Save は State を検証したうえで dir 配下に atomic write する。
// 同一ディレクトリ内の一時ファイルに書いてから rename することで、
// TUI が書き込み途中の壊れた JSON を読む事故を防ぐ（docs/design.md §5）。
func Save(dir string, s *State) error {
	if err := Validate(s); err != nil {
		return fmt.Errorf("検証エラー: %w", err)
	}
	if err := EnsureDir(dir); err != nil {
		return fmt.Errorf("ディレクトリ作成に失敗: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 変換に失敗: %w", err)
	}
	data = append(data, '\n')

	target := filepath.Join(dir, FileName(s))
	tmp, err := os.CreateTemp(dir, ".sidenote-*.tmp")
	if err != nil {
		return fmt.Errorf("一時ファイル作成に失敗: %w", err)
	}
	tmpName := tmp.Name()
	// 失敗時に一時ファイルを残さない
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("一時ファイル書き込みに失敗: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync に失敗: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("一時ファイルクローズに失敗: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename に失敗: %w", err)
	}
	return nil
}

// Load は 1 つの state ファイルを読み込む。壊れた JSON はエラーを返す。
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("JSON パースに失敗 (%s): %w", filepath.Base(path), err)
	}
	return &s, nil
}

// LoadResult は LoadAll の 1 ファイル分の結果。State か Err のどちらかが入る。
type LoadResult struct {
	Path  string
	State *State
	Err   error
}

// LoadAll は dir 配下の *.json をすべて読み込む。
// 壊れたファイルは Err 付きの結果として返し、全体としてはエラーにしない
// （1 ファイルの破損で TUI 全体を落とさないため）。
// ディレクトリが存在しない場合は空の結果を返す。
func LoadAll(dir string) ([]LoadResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []LoadResult
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		s, lerr := Load(p)
		results = append(results, LoadResult{Path: p, State: s, Err: lerr})
	}
	return results, nil
}
