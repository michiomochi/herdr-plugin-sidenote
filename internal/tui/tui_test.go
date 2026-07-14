package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
)

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
