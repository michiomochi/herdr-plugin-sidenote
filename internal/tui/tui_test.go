package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/view"
)

const longEpicURL = "https://example.com/very/long/epic/url/that/exceeds/width"

func TestRenderBlock_EpicRawURL(t *testing.T) {
	m := model{}
	r := view.Row{Space: "s", Status: state.StatusWorking, Epic: longEpicURL, Age: "1s前"}
	// width を URL より小さくしても URL は trunc されない
	lines := m.renderBlock(r, 20)

	var urlLine string
	for _, l := range lines {
		if strings.Contains(l, "🔗") {
			urlLine = l
		}
	}
	if urlLine == "" {
		t.Fatalf("生URLサブ行(🔗)が無い: %#v", lines)
	}
	if !strings.Contains(urlLine, longEpicURL) {
		t.Fatalf("URL が trunc され不完全: %q", urlLine)
	}
	// ヘッダ直下（lines[1]）に URL 行があること
	if !strings.Contains(lines[1], "🔗") {
		t.Fatalf("URL 行がヘッダ直下でない: %#v", lines)
	}
	// タイトルに ↗ / OSC8 が残っていないこと
	for _, l := range lines {
		if strings.Contains(l, "↗") {
			t.Fatalf("↗ が残っている: %q", l)
		}
		if strings.Contains(l, "\x1b]8;;") {
			t.Fatalf("OSC8 が残っている: %q", l)
		}
	}
}

func TestRenderBlock_NoEpicNoURLLine(t *testing.T) {
	m := model{}
	r := view.Row{Space: "s", Status: state.StatusWorking, Age: "1s前"}
	lines := m.renderBlock(r, 80)
	for _, l := range lines {
		if strings.Contains(l, "🔗") {
			t.Fatalf("epic 無しで URL 行が出た: %q", l)
		}
	}
}

// itemStyleFor は stale の有無に依存せず、項目の状態だけで色を決める
// （stale でも色を落とさない、が確定仕様）。
func TestItemStyleFor(t *testing.T) {
	if got := itemStyleFor(state.StepDone).GetForeground(); got != lipgloss.Color("42") {
		t.Errorf("完了は緑42のはず: %v", got)
	}
	if got := itemStyleFor(state.StepDoing).GetForeground(); got != lipgloss.Color("15") {
		t.Errorf("進行中は白15のはず: %v", got)
	}
	if !itemStyleFor(state.StepDoing).GetBold() {
		t.Error("進行中は太字のはず")
	}
	if got := itemStyleFor(state.StepTodo).GetForeground(); got != lipgloss.Color("245") {
		t.Errorf("予定は灰245のはず: %v", got)
	}
}

// 見出しは status・stale に関係なく常に白（太字・前景無指定）。
func TestHeaderStyleIsBold(t *testing.T) {
	if !headerStyle.GetBold() {
		t.Error("見出しは太字のはず")
	}
	if headerStyle.GetForeground() != (lipgloss.NoColor{}) {
		t.Errorf("見出しは前景無指定(白)のはず: %v", headerStyle.GetForeground())
	}
}
