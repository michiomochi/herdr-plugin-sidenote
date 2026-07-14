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

	// stale ブロック用。ブロック全体を一律に濃くグレーアウトすると本文が
	// 読めなくなるため、ヘッダ（鮮度の手掛かり）はやや弱いグレー、本文は
	// しっかり読める明るいグレーに留める（faint 属性は使わない）。
	// 表示レビューで「まだ暗い」との指摘を受け、さらに明るく調整（244→247 / 250→253）。
	staleHeadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	staleBodyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("253"))
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

	groups := view.GroupRows(m.rows)
	lines := []string{header, ""}

	// 端末高さに収まるよう、要母艦対応グループから順に「ブロック単位」で詰める。
	// 見出しも 1 行として数え、入り切らなくなったら打ち切り末尾に「…他 N 件」。
	avail := 0
	if m.height > 0 {
		avail = m.height - (strings.Count(header, "\n") + 1) - 1 // ヘッダ＋余白
	}
	total := len(m.rows)
	shown := 0
	used := 0
	clipped := false

	limited := m.height > 0
	overflow := func(n int) bool {
		// shown>0 のとき、n 行＋「…他 N 件」ぶんを入れると溢れるか
		return limited && shown > 0 && used+n+1 > avail
	}

groupLoop:
	for gi, g := range groups {
		headLine := groupHeaderStyle(g.Kind).Render(
			fmt.Sprintf("%s %s (%d)", groupSymbol(g.Kind), g.Title, len(g.Rows)))

		// 見出しは「見出し＋最低 1 ブロック」が入るときだけ出す（見出しの空振り防止）。
		firstBlock := m.renderBlock(g.Rows[0], width)
		need := len(firstBlock) + 1 // 見出し
		if gi > 0 {
			need++ // グループ間の空行
		}
		if overflow(need) {
			clipped = true
			break groupLoop
		}
		if gi > 0 {
			lines = append(lines, "")
			used++
		}
		lines = append(lines, headLine)
		used++

		for bi, r := range g.Rows {
			block := firstBlock
			if bi > 0 {
				block = m.renderBlock(r, width)
			}
			sep := 0
			if bi > 0 {
				sep = 1 // グループ内ブロック間の空行
			}
			if overflow(sep + len(block)) {
				clipped = true
				break groupLoop
			}
			if bi > 0 {
				lines = append(lines, "")
				used++
			}
			lines = append(lines, block...)
			used += len(block)
			shown++
		}
	}

	if clipped || shown < total {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("…他 %d 件", total-shown)))
	}
	return join(lines)
}

// groupSymbol はセクション見出しの記号を返す。
func groupSymbol(k view.GroupKind) string {
	switch k {
	case view.GroupAttention:
		return "●"
	case view.GroupActive:
		return "▸"
	default:
		return "✓"
	}
}

// groupHeaderStyle はセクション見出しの色。要対応は目を引く色、実施中は通常、
// 完了・待機は弱め（dim 2階調と整合）。
func groupHeaderStyle(k view.GroupKind) lipgloss.Style {
	switch k {
	case view.GroupAttention:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true) // 赤系で注意喚起
	case view.GroupActive:
		return lipgloss.NewStyle().Bold(true)
	default:
		return staleHeadStyle // 完了・待機は弱いグレー
	}
}

// renderBlock は 1 space を複数行ブロック（ヘッダ＋headline＋済/いま/予定/障害）
// として描画する。空の区分行は省略し、headline は常に出す。
// stale な space はブロック全体をグレーアウトする。
func (m model) renderBlock(r view.Row, width int) []string {
	trunc := func(s string) string { return runewidth.Truncate(s, width, "…") }
	// stale の本文は「読める明るいグレー」に留める（濃い dim にしない）。
	body := func(s string) string {
		if r.Stale {
			return staleBodyStyle.Render(s)
		}
		return s
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
	head := trunc(fmt.Sprintf("▍ %s   herdr:%s  母艦:%s   %s", name, agent, orDash(r.Status), r.Age))

	var lines []string
	if r.Stale {
		// ヘッダはやや弱いグレーにして「古い」手掛かりを残す。
		lines = append(lines, staleHeadStyle.Render(head))
	} else {
		lines = append(lines, statusStyle(r.Status).Bold(true).Render(head))
	}

	headline := r.Headline
	if headline == "" {
		headline = "-"
	}
	lines = append(lines, body(trunc("    "+headline)))

	// 1 列の TODO リスト: 済み(✓)→いま(→)→予定(□) の順、1 項目 1 行。
	for _, t := range r.DoneItems {
		lines = append(lines, body(trunc("    ✓ "+t)))
	}
	if r.DoneOverflow > 0 {
		lines = append(lines, body(trunc(fmt.Sprintf("    ✓ …他 %d 件", r.DoneOverflow))))
	}
	for _, t := range r.Doing {
		lines = append(lines, body(trunc("    → "+t)))
	}
	for _, t := range r.Todo {
		lines = append(lines, body(trunc("    □ "+t)))
	}
	if r.Next != "" {
		lines = append(lines, body(trunc("    □ "+r.Next)))
	}
	if len(r.Blockers) > 0 {
		bl := trunc("    ⚠ 障害 " + strings.Join(r.Blockers, " ・ "))
		if r.Stale {
			bl = staleBodyStyle.Render(bl) // 古くても本文は読める明るさに
		} else {
			bl = brokenStyle.Render(bl)
		}
		lines = append(lines, bl)
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func join(lines []string) string {
	return strings.Join(lines, "\n")
}
