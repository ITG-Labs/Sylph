package wizard

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestEscAndCtrlCCancelForms(t *testing.T) {
	km := keyMap()
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyEscape}, km.Quit) {
		t.Fatal("esc must cancel a form")
	}
	if !key.Matches(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, km.Quit) {
		t.Fatal("ctrl+c must cancel a form")
	}
	if km.Select.Filter.Enabled() {
		t.Fatal("select filter would fight esc")
	}
}
