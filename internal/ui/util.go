package ui

import (
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/grid"
	"github.com/ayn2op/tview/picker"
	"github.com/code2344/slacker/internal/config"
)

// ConfigureBox configures the provided box according to the provided theme.
func ConfigureBox(box *tview.Box, cfg *config.Theme) {
	padding := cfg.Border.Padding
	BlurBox(box, cfg)
	box.
		SetBorderPadding(padding[0], padding[1], padding[2], padding[3]).
		SetTitleAlignment(cfg.Title.Alignment.Alignment).
		SetFooterAlignment(cfg.Footer.Alignment.Alignment)

	if cfg.Border.Enabled {
		box.SetBorders(tview.BordersAll)
	}
}

func FocusBox(box *tview.Box, cfg *config.Theme) {
	box.SetBorderStyle(cfg.Border.ActiveStyle.Style).
		SetBorderSet(cfg.Border.ActiveSet.BorderSet).
		SetTitleStyle(cfg.Title.ActiveStyle.Style).
		SetFooterStyle(cfg.Footer.ActiveStyle.Style)
}

func BlurBox(box *tview.Box, cfg *config.Theme) {
	box.SetBorderStyle(cfg.Border.NormalStyle.Style).
		SetBorderSet(cfg.Border.NormalSet.BorderSet).
		SetTitleStyle(cfg.Title.NormalStyle.Style).
		SetFooterStyle(cfg.Footer.NormalStyle.Style)
}

func ConfigurePicker(model *picker.Model, cfg *config.Config, title string) {
	model.Box = tview.NewBox()
	ConfigureBox(model.Box, &cfg.Theme)
	FocusBox(model.Box, &cfg.Theme)

	model.SetTitle(title)
	model.SetScrollBarVisibility(cfg.Theme.ScrollBar.Visibility.ScrollBarVisibility)
	model.SetScrollBar(tview.NewScrollBar().
		SetTrackStyle(cfg.Theme.ScrollBar.TrackStyle.Style).
		SetThumbStyle(cfg.Theme.ScrollBar.ThumbStyle.Style).
		SetGlyphSet(cfg.Theme.ScrollBar.GlyphSet.GlyphSet))

	kbs := cfg.Keybinds.Picker
	model.SetKeybinds(picker.Keybinds{
		Cancel: kbs.Cancel.Keybind,
		Select: kbs.Select.Keybind,

		SelectUp:     kbs.SelectUp.Keybind,
		SelectDown:   kbs.SelectDown.Keybind,
		SelectTop:    kbs.SelectTop.Keybind,
		SelectBottom: kbs.SelectBottom.Keybind,
	})
}

// Centered creates a new grid with provided primitive aligned in the center.
func Centered(m tview.Model, width, height int) tview.Model {
	return grid.NewModel().
		SetColumns(0, width, 0).
		SetRows(0, height, 0).
		AddItem(m, 1, 1, 1, 1, 0, 0, true)
}
