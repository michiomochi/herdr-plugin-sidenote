// Package tui は sidenote watch の TUI 本体（Bubble Tea モデル）と
// state ディレクトリの監視・再描画ループを提供する。
//
// 表示に使う純ロジック（経過時間整形・鮮度判定・行構築・骨格マージ）は
// internal/view に切り出してテスト済みであり、本パッケージは描画と
// イベントループ（手動 verify 対象）に集中する。
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
	runewidth "github.com/mattn/go-runewidth"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/herdr"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/view"
)

// staleThreshold を超えて更新の無い space は薄く表示する。
const staleThreshold = 10 * time.Minute

// Run は指定した state ディレクトリを監視する TUI を起動する。
// interval は herdr 骨格情報のポーリング間隔。
func Run(dir string, interval time.Duration) error {
	events := make(chan struct{}, 1)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify 初期化に失敗: %w", err)
	}
	defer watcher.Close()
	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("ディレクトリ監視の登録に失敗 (%s): %w", dir, err)
	}
	go func() {
		for {
			select {
			case _, ok := <-watcher.Events:
				if !ok {
					return
				}
				// デバウンス: 既にシグナルが溜まっていれば捨てる
				select {
				case events <- struct{}{}:
				default:
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	m := newModel(dir, interval, events)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

type model struct {
	dir      string
	interval time.Duration
	events   <-chan struct{}

	baseRows  []view.Row // state 由来（マージ前）
	rows      []view.Row // 骨格マージ後（表示用）
	loadErr   error
	herdrErr  error
	width     int
	height    int
	lastPoll  time.Time
	workspace []herdr.Workspace
}

func newModel(dir string, interval time.Duration, events <-chan struct{}) model {
	return model{dir: dir, interval: interval, events: events}
}

type reloadMsg struct{}
type fsMsg struct{}
type tickMsg time.Time
type herdrMsg struct {
	workspaces []herdr.Workspace
	err        error
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return reloadMsg{} },
		pollHerdrCmd(),
		waitForFS(m.events),
		tick(),
	)
}

func waitForFS(events <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-events
		return fsMsg{}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// pollHerdrCmd は herdr 骨格取得を非同期に実行する tea.Cmd。
// exec が遅い/ハングしても UI スレッドをブロックしない。
func pollHerdrCmd() tea.Cmd {
	return func() tea.Msg {
		ws, err := herdr.ListWorkspaces()
		return herdrMsg{workspaces: ws, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			m.reloadState()
			m.remerge()
			return m, pollHerdrCmd()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case reloadMsg:
		m.reloadState()
		m.remerge()
		return m, nil
	case fsMsg:
		m.reloadState()
		m.remerge()
		return m, waitForFS(m.events) // 再武装
	case herdrMsg:
		m.herdrErr = msg.err
		if msg.err == nil {
			m.workspace = msg.workspaces
		}
		m.remerge()
		return m, nil
	case tickMsg:
		// 1 秒ごとに経過時間だけ再計算（I/O なし）。
		// interval ごとに herdr 骨格を非同期ポーリングする。
		view.RefreshAges(m.baseRows, time.Now(), staleThreshold)
		m.remerge()
		cmds := []tea.Cmd{tick()}
		if time.Time(msg).Sub(m.lastPoll) >= m.interval {
			m.lastPoll = time.Time(msg)
			cmds = append(cmds, pollHerdrCmd())
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// reloadState は state ファイルを読み直してマージ前の行を再構築する。
func (m *model) reloadState() {
	results, err := state.LoadAll(m.dir)
	m.loadErr = err
	m.baseRows = view.BuildRows(results, time.Now(), staleThreshold)
}

// remerge は現在の骨格情報を state 行にマージして表示行を更新する。
func (m *model) remerge() {
	m.rows = view.Merge(m.baseRows, m.workspace)
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	brokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// TODO 項目の状態別色（stale の有無に関係なく常に適用。鮮度は Age 表記で表す）。
	doneItemStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))             // 完了=緑
	doingItemStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))  // 進行中=太字白
	todoItemStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))            // 予定=グレー
	awaitLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")) // 母艦対応待ち=赤
)

// itemStyleFor は TODO 項目の状態（done/doing/todo）だけで色を決める。
// stale の有無には依存しない（stale でも色を落とさない、が確定仕様）。
func itemStyleFor(itemState string) lipgloss.Style {
	switch itemState {
	case state.StepDone:
		return doneItemStyle
	case state.StepDoing:
		return doingItemStyle
	case state.StepTodo:
		return todoItemStyle
	default:
		return lipgloss.NewStyle()
	}
}

func (m model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	title := headerStyle.Render(fmt.Sprintf("sidenote — %d spaces", len(m.rows)))
	hint := hintStyle.Render("q:quit  r:reload")
	header := title + "  " + hint

	if m.herdrErr != nil {
		header += "\n" + dimStyle.Render("(herdr 骨格取得に失敗: state のみ表示)")
	}
	if m.loadErr != nil {
		header += "\n" + brokenStyle.Render("state 読込エラー: "+m.loadErr.Error())
	}

	if len(m.rows) == 0 {
		return header + "\n\n" + dimStyle.Render("表示できる space がありません。母艦が sidenote set で状況を書き込むと表示されます。")
	}

	// グルーピング見出しは廃止。m.rows は view 側で
	// 「母艦対応待ち → 実施中 → 完了・待機」の順にソート済み。
	// ブロック単位で端末高さに収まるだけ詰め、溢れは末尾に「…他 N 件」。
	lines := []string{header, ""}
	avail := 0
	if m.height > 0 {
		avail = m.height - (strings.Count(header, "\n") + 1) - 1 // ヘッダ＋余白
	}
	shown := 0
	used := 0
	for _, r := range m.rows {
		block := m.renderBlock(r, width)
		// 次のブロック＋（間の空行）＋「…他 N 件」ぶんが入らなければ打ち切る
		if m.height > 0 && shown > 0 && used+1+len(block)+1 > avail {
			break
		}
		if shown > 0 {
			lines = append(lines, "") // ブロック間の空行
			used++
		}
		lines = append(lines, block...)
		used += len(block)
		shown++
	}
	if shown < len(m.rows) {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("…他 %d 件", len(m.rows)-shown)))
	}
	return join(lines)
}

// renderBlock は 1 space を複数行ブロック（ヘッダ＋済/いま/予定/障害）として描画する。
// 見出しは status・stale に関係なく常に白（headerStyle）。TODO 項目は stale の有無に
// 関係なく常に状態別色（鮮度は Age 表記で表す）。await は赤ラベルを右に付す。
func (m model) renderBlock(r view.Row, width int) []string {
	trunc := func(s string) string { return runewidth.Truncate(s, width, "…") }
	awaitLabel := "〈母艦対応待ち〉"
	renderItem := func(marker, text string, itemStyle lipgloss.Style, await bool) string {
		w := width
		if await {
			w = max(width-runewidth.StringWidth(awaitLabel)-1, 4)
		}
		content := itemStyle.Render(runewidth.Truncate("    "+marker+" "+text, w, "…"))
		if await {
			content += " " + awaitLabelStyle.Render(awaitLabel)
		}
		return content
	}

	if r.Broken {
		return []string{brokenStyle.Render(trunc("▍ " + r.Space + "  — state ファイルが壊れています"))}
	}

	agent := r.AgentStatus
	if agent == "" {
		if r.InHerdr {
			agent = "?"
		} else {
			agent = "-"
		}
	}
	name := r.Space
	if r.FutureSchema {
		name = "[要更新] " + name
	}
	// 見出し: 「▍ <space名[+↗]>   herdr:x  母艦:y  age」。
	// epic があれば名前＋↗ を OSC8 リンク化（ゼロ幅シーケンス）。↗ の可視幅は
	// budget に算入し整列がズレないようにする。見出しは常に白・太字。
	prefix := "▍ "
	suffix := fmt.Sprintf("   herdr:%s  母艦:%s   %s", agent, orDash(r.Status), r.Age)
	arrow := ""
	if r.Epic != "" {
		arrow = " ↗"
	}
	budget := max(width-runewidth.StringWidth(prefix)-runewidth.StringWidth(suffix)-runewidth.StringWidth(arrow), 4)
	nameTrunc := runewidth.Truncate(name, budget, "…")
	head := prefix + renderHeadName(nameTrunc, r.Epic) + suffix

	lines := []string{headerStyle.Render(head)}

	// 1 列 TODO: 済み(✓緑)→いま(→太字白)→予定(□グレー) の順、1 項目 1 行。
	for _, t := range r.DoneItems {
		lines = append(lines, renderItem("✓", t, itemStyleFor(state.StepDone), false))
	}
	if r.DoneOverflow > 0 {
		lines = append(lines, renderItem("✓", fmt.Sprintf("…他 %d 件", r.DoneOverflow), itemStyleFor(state.StepDone), false))
	}
	for _, it := range r.Doing {
		lines = append(lines, renderItem("→", it.Text, itemStyleFor(state.StepDoing), it.Await))
	}
	for _, it := range r.Todo {
		lines = append(lines, renderItem("□", it.Text, itemStyleFor(state.StepTodo), it.Await))
	}
	if r.Next != "" {
		lines = append(lines, renderItem("□", r.Next, itemStyleFor(state.StepTodo), false))
	}
	if len(r.Blockers) > 0 {
		lines = append(lines, renderItem("⚠", "障害 "+strings.Join(r.Blockers, " ・ "), brokenStyle, false))
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// renderHeadName は space 名を返す。epic が非空なら「名前＋↗」を OSC8
// ハイパーリンク（ESC]8;;URL BEL … ESC]8;;BEL、ゼロ幅シーケンス）で包む。
// epic が空なら名前をそのまま返す（↗・リンクなし）。
func renderHeadName(name, epic string) string {
	if epic == "" {
		return name
	}
	return "\x1b]8;;" + epic + "\x07" + name + " ↗" + "\x1b]8;;\x07"
}

func join(lines []string) string {
	return strings.Join(lines, "\n")
}
