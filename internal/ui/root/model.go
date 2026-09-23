package root

import (
	"fmt"
	"sort"

	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/flex"
	"github.com/ayn2op/tview/layers"
	"github.com/ayn2op/tview/tree"
	"github.com/code2344/slacker/internal/config"
	"github.com/code2344/slacker/internal/slack"
	"github.com/code2344/slacker/internal/ui"
	"github.com/gdamore/tcell/v3"
)

// Model is the first Slack-backed shell of the three-pane interface.
// Message loading and the composer are wired in subsequent backend slices.
type Model struct {
	*layers.Layers
	cfg      *config.Config
	sidebar  *tree.Model
	messages *tview.TextArea
	composer *tview.TextArea
	main     *flex.Model
}

func NewModel(cfg *config.Config, workspaceName string, conversations []slack.Conversation) *Model {
	m := &Model{
		Layers:   layers.New(),
		cfg:      cfg,
		sidebar:  tree.NewModel(),
		messages: tview.NewTextArea(),
		composer: tview.NewTextArea(),
		main:     flex.NewModel(),
	}
	m.build(workspaceName, conversations)
	return m
}

func (m *Model) build(workspaceName string, conversations []slack.Conversation) {
	root := tree.NewNode("")
	channels := tree.NewNode("Channels").SetExpanded(true)
	dms := tree.NewNode("Direct messages").SetExpanded(true)
	root.AddChild(channels).AddChild(dms)

	sort.Slice(conversations, func(i, j int) bool { return conversations[i].Name < conversations[j].Name })
	for _, conversation := range conversations {
		name := conversation.Name
		parent := channels
		prefix := "# "
		if conversation.IsIM || conversation.IsMPIM {
			parent, prefix = dms, "@ "
		} else if conversation.IsPrivate {
			prefix = "🔒 "
		}
		if name == "" {
			name = conversation.ID
		}
		parent.AddChild(tree.NewNode(prefix + name).SetReference(conversation.ID))
	}

	m.sidebar.SetRoot(root).SetTopLevel(1).SetTitle(workspaceName)
	ui.ConfigureBox(m.sidebar.Box, &m.cfg.Theme)
	ui.FocusBox(m.sidebar.Box, &m.cfg.Theme)

	m.messages.SetText(fmt.Sprintf("Connected to %s.\n\nSelect a conversation to begin.", workspaceName), false)
	m.messages.SetDisabled(true)
	m.messages.SetTitle("Messages")
	ui.ConfigureBox(m.messages.Box, &m.cfg.Theme)

	m.composer.SetPlaceholder(nil)
	m.composer.SetDisabled(true)
	m.composer.SetTitle("Composer")
	ui.ConfigureBox(m.composer.Box, &m.cfg.Theme)

	right := flex.NewModel().SetDirection(flex.DirectionRow).
		AddItem(m.messages, 0, 1, false).
		AddItem(m.composer, 3, 0, false)
	m.main.SetDirection(flex.DirectionColumn).
		AddItem(m.sidebar, 0, m.cfg.Sidebar.WidthPercent, true).
		AddItem(right, 0, 100-m.cfg.Sidebar.WidthPercent, false)
	m.AddLayer(m.main, layers.WithName("main"), layers.WithResize(true), layers.WithVisible(true))
}

func (m *Model) Update(msg tview.Msg) tview.Cmd {
	switch msg := msg.(type) {
	case tview.InitMsg:
		return tview.SetTitle("slacker")
	case tview.KeyMsg:
		if msg.Key() == tcell.KeyCtrlC {
			return tview.Quit()
		}
	}
	return m.Layers.Update(msg)
}

var _ tview.Model = (*Model)(nil)
