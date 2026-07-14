// Package view は TUI 描画のための純ロジック（経過時間の整形・鮮度判定・
// 表示行の構築）を提供する。lipgloss などの描画ライブラリには依存せず、
// テスト可能なデータ変換に徹する。骨格情報とのマージは Merge で行う（M4）。
package view

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/herdr"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
)

// Age は updated から now までの経過時間を短い相対表記（例 "12s前"）で返す。
// 未来時刻は 0s前 に丸める。
func Age(now, updated time.Time) string {
	d := max(now.Sub(updated), 0)
	sec := int(d.Seconds())
	switch {
	case sec < 60:
		return strconv.Itoa(sec) + "s前"
	case sec < 3600:
		return strconv.Itoa(sec/60) + "m前"
	case sec < 86400:
		return strconv.Itoa(sec/3600) + "h前"
	default:
		return strconv.Itoa(sec/86400) + "d前"
	}
}

// IsStale は updated が now から threshold より古いかを返す。
func IsStale(now, updated time.Time, threshold time.Duration) bool {
	return now.Sub(updated) > threshold
}

// RefreshAges は行の Age / Stale を now 基準で再計算する（I/O なし）。
// 時刻を持たない行（broken / skeleton）は変更しない。
func RefreshAges(rows []Row, now time.Time, staleThreshold time.Duration) {
	for i := range rows {
		if rows[i].hasTime {
			rows[i].Age = Age(now, rows[i].updated)
			rows[i].Stale = IsStale(now, rows[i].updated, staleThreshold)
		}
	}
}

// Row は 1 space 分の表示行。骨格情報（AgentStatus）は Merge で埋める。
type Row struct {
	Key          string // 突合キー（workspace_id か space）
	Space        string
	WorkspaceID  string
	Status       string // 母艦視点の意味的ステータス
	AgentStatus  string // herdr 検出の稼働状態（骨格・M4 で付与）
	Headline     string
	Next         string
	Blockers     []string
	Done         []string // steps.state==done の label（過去・完了）
	Doing        []string // steps.state==doing の label（現在）
	Todo         []string // steps.state==todo の label（今後の予定）
	Age          string
	updated      time.Time
	hasTime      bool
	Stale        bool
	Broken       bool // state ファイルが壊れている
	FutureSchema bool // 本実装より新しいスキーマ
	InHerdr      bool // herdr の workspace 一覧に存在するか（M4）
	skeleton     bool // 骨格のみ（state ファイルが無い）行
}

// BuildRows は state のロード結果から表示行を構築する。
// 壊れた結果は Broken 行にし、更新時刻の新しい順・壊れた行は末尾に並べる。
func BuildRows(results []state.LoadResult, now time.Time, staleThreshold time.Duration) []Row {
	rows := make([]Row, 0, len(results))
	for _, r := range results {
		if r.Err != nil || r.State == nil {
			rows = append(rows, Row{
				Key:    baseName(r.Path),
				Space:  baseName(r.Path),
				Broken: true,
			})
			continue
		}
		s := r.State
		done, doing, todo := classifySteps(s.Progress)
		row := Row{
			Key:          s.Key(),
			Space:        s.Space,
			WorkspaceID:  s.WorkspaceID,
			Status:       s.Status,
			Headline:     s.Headline,
			Next:         s.Next,
			Blockers:     s.Blockers,
			Done:         done,
			Doing:        doing,
			Todo:         todo,
			FutureSchema: s.IsFutureSchema(),
		}
		if tm, err := s.UpdatedTime(); err == nil {
			row.updated = tm
			row.hasTime = true
			row.Age = Age(now, tm)
			row.Stale = IsStale(now, tm, staleThreshold)
		} else {
			row.Age = "?"
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows
}

// sortRows は「壊れた行は末尾、それ以外は更新時刻の新しい順」に並べる。
// 時刻が同じ/無い場合は space 名で安定化する。
func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Broken != b.Broken {
			return !a.Broken // 壊れていない方が先
		}
		if a.hasTime != b.hasTime {
			return a.hasTime // 時刻あり優先
		}
		if a.hasTime && b.hasTime && !a.updated.Equal(b.updated) {
			return a.updated.After(b.updated) // 新しい順
		}
		return a.Space < b.Space
	})
}

func baseName(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, ".json")
}

// GroupKind は space をセクション分けするグループ種別。
type GroupKind int

const (
	// GroupAttention は母艦（人間）の対応が必要な space（blocked/review/ブロッカーあり）。
	GroupAttention GroupKind = iota
	// GroupActive は各 space で実施中（working/planning）。
	GroupActive
	// GroupWaiting は完了・待機（done / skeleton / broken）。
	GroupWaiting
)

// Group はセクション（見出し＋所属行）。Title はプレーンな見出し語で、
// 記号・件数・色付けは描画側（tui）の責務とする。
type Group struct {
	Kind  GroupKind
	Title string
	Rows  []Row
}

var groupTitles = map[GroupKind]string{
	GroupAttention: "要母艦対応",
	GroupActive:    "実施中",
	GroupWaiting:   "完了・待機",
}

// groupOf は行の所属グループを母艦 status ベースで判定する（上から評価）。
// herdr の agent_status は分類に使わない（ヘッダ併記のみ）。
func groupOf(r Row) GroupKind {
	switch {
	case r.Broken:
		return GroupWaiting
	case len(r.Blockers) > 0:
		return GroupAttention // status=working でも取りこぼし防止で昇格
	case r.Status == state.StatusBlocked || r.Status == state.StatusReview:
		return GroupAttention
	case r.Status == state.StatusWorking || r.Status == state.StatusPlanning:
		return GroupActive
	default:
		// done / skeleton(status=="") など
		return GroupWaiting
	}
}

// GroupRows はソート済みの rows を固定グループ順
// （要母艦対応 → 実施中 → 完了・待機）に安定分配する。
// 空グループは省略し、グループ内の順序は入力順（＝既存ソート）を保つ。
func GroupRows(rows []Row) []Group {
	buckets := map[GroupKind][]Row{}
	for _, r := range rows {
		k := groupOf(r)
		buckets[k] = append(buckets[k], r)
	}
	order := []GroupKind{GroupAttention, GroupActive, GroupWaiting}
	out := make([]Group, 0, len(order))
	for _, k := range order {
		if len(buckets[k]) == 0 {
			continue // 空グループは見出しごと省略
		}
		out = append(out, Group{Kind: k, Title: groupTitles[k], Rows: buckets[k]})
	}
	return out
}

// classifySteps は progress.steps を done/doing/todo の 3 群（label 配列）に
// 分類する。過去（Done）・現在（Doing）・未来（Todo）の複数行表示に使う。
func classifySteps(p *state.Progress) (done, doing, todo []string) {
	if p == nil {
		return nil, nil, nil
	}
	for _, s := range p.Steps {
		switch s.State {
		case state.StepDone:
			done = append(done, s.Label)
		case state.StepDoing:
			doing = append(doing, s.Label)
		case state.StepTodo:
			todo = append(todo, s.Label)
		}
	}
	return done, doing, todo
}

// Merge は state 由来の行に herdr の骨格情報（agent_status）を突合して付与し、
// state ファイルが無い workspace は骨格のみ行として追加する。
// 突合は workspace_id 一致を優先し、無ければ space==label で照合する。
// workspaces が空/nil の場合は rows をそのまま返す（骨格取得失敗時のフォールバック）。
func Merge(rows []Row, workspaces []herdr.Workspace) []Row {
	if len(workspaces) == 0 {
		return rows
	}

	// 突合用インデックス
	byID := map[string]herdr.Workspace{}
	byLabel := map[string]herdr.Workspace{}
	for _, w := range workspaces {
		if w.WorkspaceID != "" {
			byID[w.WorkspaceID] = w
		}
		if w.Label != "" {
			byLabel[w.Label] = w
		}
	}

	matched := map[string]bool{} // 突合できた workspace_id を記録
	out := make([]Row, 0, len(rows)+len(workspaces))

	for _, r := range rows {
		if w, ok := matchWorkspace(r, byID, byLabel); ok {
			r.AgentStatus = w.AgentStatus
			r.InHerdr = true
			matched[w.WorkspaceID] = true
		} else {
			r.InHerdr = false
		}
		out = append(out, r)
	}

	// state ファイルが無い workspace を骨格のみ行として追加
	for _, w := range workspaces {
		if matched[w.WorkspaceID] {
			continue
		}
		out = append(out, Row{
			Key:         w.WorkspaceID,
			Space:       w.Label,
			WorkspaceID: w.WorkspaceID,
			AgentStatus: w.AgentStatus,
			InHerdr:     true,
			skeleton:    true,
		})
	}

	sortRows(out)
	return out
}

func matchWorkspace(r Row, byID, byLabel map[string]herdr.Workspace) (herdr.Workspace, bool) {
	if r.WorkspaceID != "" {
		if w, ok := byID[r.WorkspaceID]; ok {
			return w, true
		}
	}
	if r.Space != "" {
		if w, ok := byLabel[r.Space]; ok {
			return w, true
		}
	}
	return herdr.Workspace{}, false
}
