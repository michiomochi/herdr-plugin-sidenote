package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/view"
)

const longEpicURL = "https://example.com/very/long/epic/url/that/exceeds/width"

func TestRenderBlock_EpicURLInHeader(t *testing.T) {
	m := model{}
	r := view.Row{Space: "myspace", Status: state.StatusWorking, Epic: longEpicURL, Age: "1s前"}
	lines := m.renderBlock(r, 200)

	// ヘッダ行（lines[0]）に URL が完全に含まれ、タイトル直後（"myspace "＋URL）
	if !strings.Contains(lines[0], longEpicURL) {
		t.Fatalf("ヘッダに完全 URL が無い: %q", lines[0])
	}
	if !strings.Contains(lines[0], "myspace "+longEpicURL) {
		t.Fatalf("タイトル直後に URL が来ていない: %q", lines[0])
	}
	// 🔗 サブ行・↗・OSC8 は残っていないこと
	for _, l := range lines {
		if strings.Contains(l, "🔗") {
			t.Fatalf("🔗 サブ行が残っている: %q", l)
		}
		if strings.Contains(l, "↗") {
			t.Fatalf("↗ が残っている: %q", l)
		}
		if strings.Contains(l, "\x1b]8;;") {
			t.Fatalf("OSC8 が残っている: %q", l)
		}
	}
}

func TestRenderBlock_EpicURLPreservedWhenNarrow(t *testing.T) {
	m := model{}
	r := view.Row{Space: "s", Status: state.StatusWorking, Epic: longEpicURL, Age: "1s前"}
	// URL(約55) より狭い幅でも URL は trunc されない（status 側を犠牲に URL 保全）
	lines := m.renderBlock(r, 30)
	if !strings.Contains(lines[0], longEpicURL) {
		t.Fatalf("狭幅で URL が trunc された: %q", lines[0])
	}
	// URL に … が入っていないこと
	if strings.Contains(lines[0], longEpicURL[:20]+"…") {
		t.Fatalf("URL に … が入っている: %q", lines[0])
	}
}

func TestRenderBlock_NoEpicNoURL(t *testing.T) {
	m := model{}
	r := view.Row{Space: "s", Status: state.StatusWorking, Age: "1s前"}
	lines := m.renderBlock(r, 80)
	if strings.Contains(lines[0], "http") {
		t.Fatalf("epic 無しでヘッダに URL が出た: %q", lines[0])
	}
	for _, l := range lines {
		if strings.Contains(l, "🔗") {
			t.Fatalf("🔗 が残っている: %q", l)
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
