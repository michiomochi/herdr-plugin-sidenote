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
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case state.StatusWorking:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // green
	case state.StatusBlocked:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // red
	case state.StatusReview:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	case state.StatusDone:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // blue
	case state.StatusPlanning:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244")) // gray
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

	// 列幅: space(18) herdr(8) 母艦(9) age(7) 残りを headline/next に。
	const wSpace, wHerdr, wStatus, wAge = 20, 8, 9, 7
	wMain := max(width-wSpace-wHerdr-wStatus-wAge-4, 10)

	colHead := row(
		pad("SPACE", wSpace),
		pad("HERDR", wHerdr),
		pad("母艦", wStatus),
		pad("状況 / 次アクション", wMain),
		pad("更新", wAge),
	)
	lines := []string{header, "", dimStyle.Render(colHead)}

	// 端末高さに収まるようデータ行数をクリップし、溢れた件数を末尾に示す。
	// 行は更新の新しい順なので、末尾（古い側）を切り落とす。
	dataRows := m.rows
	overflow := 0
	if m.height > 0 {
		headerLines := strings.Count(header, "\n") + 1
		maxData := m.height - headerLines - 2 - 1 // 空行 + 列見出し + 溢れ表示の余白
		maxData = max(maxData, 1)
		if len(dataRows) > maxData {
			overflow = len(dataRows) - maxData
			dataRows = dataRows[:maxData]
		}
	}

	for _, r := range dataRows {
		lines = append(lines, m.renderRow(r, wSpace, wHerdr, wStatus, wMain, wAge))
	}
	if overflow > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("…他 %d 件", overflow)))
	}
	return join(lines)
}

func (m model) renderRow(r view.Row, wSpace, wHerdr, wStatus, wMain, wAge int) string {
	if r.Broken {
		return brokenStyle.Render(row(
			pad(r.Space, wSpace),
			pad("-", wHerdr),
			pad("壊れ", wStatus),
			pad("state ファイルが壊れています", wMain),
			pad("-", wAge),
		))
	}

	main := r.Headline
	if r.Next != "" {
		main += "  → " + r.Next
	}
	if r.FutureSchema {
		main = "[要更新] " + main
	}
	if len(r.Blockers) > 0 {
		main = "⚠ " + main
	}

	agent := r.AgentStatus
	if agent == "" {
		if !r.InHerdr {
			agent = "-"
		} else {
			agent = "?"
		}
	}

	line := row(
		pad(r.Space, wSpace),
		pad(agent, wHerdr),
		statusStyle(r.Status).Render(pad(orDash(r.Status), wStatus)),
		pad(main, wMain),
		pad(r.Age, wAge),
	)
	if r.Stale {
		return dimStyle.Render(line)
	}
	return line
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// pad は文字列を表示幅 w に切り詰め or 右パディングする（CJK 幅対応）。
func pad(s string, w int) string {
	s = runewidth.Truncate(s, w, "…")
	return runewidth.FillRight(s, w)
}

func row(cols ...string) string {
	return strings.Join(cols, " ")
}

func join(lines []string) string {
	return strings.Join(lines, "\n")
}
