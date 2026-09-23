package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/code2344/slacker/internal/consts"
	"github.com/code2344/slacker/internal/keyring"
)

const metadataFile = "workspaces.json"

var ErrNoActiveWorkspace = errors.New("no active workspace")

type Metadata struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	APIURL string `json:"api_url,omitempty"`
}

type Secret struct {
	Token   string `json:"token"`
	Cookie  string `json:"cookie"`
	CookieS string `json:"cookie_s,omitempty"`
}

type State struct {
	Active     string              `json:"active,omitempty"`
	Workspaces map[string]Metadata `json:"workspaces"`
}

type Store struct{ path string }

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, consts.Name, metadataFile)
}

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (State, error) {
	state := State{Workspaces: map[string]Metadata{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read workspace metadata: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode workspace metadata: %w", err)
	}
	if state.Workspaces == nil {
		state.Workspaces = map[string]Metadata{}
	}
	return state, nil
}

func (s *Store) Save(state State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace metadata: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write workspace metadata: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace workspace metadata: %w", err)
	}
	return nil
}

func (s *Store) Put(meta Metadata, secret Secret, makeActive bool) error {
	if meta.TeamID == "" || secret.Token == "" || secret.Cookie == "" {
		return errors.New("workspace ID, token, and cookie are required")
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	if err := keyring.SetWorkspace(meta.TeamID, string(encoded)); err != nil {
		return fmt.Errorf("save workspace credentials in keyring: %w", err)
	}
	state, err := s.Load()
	if err != nil {
		return err
	}
	state.Workspaces[meta.TeamID] = meta
	if makeActive || state.Active == "" {
		state.Active = meta.TeamID
	}
	return s.Save(state)
}

func (s *Store) Secret(teamID string) (Secret, error) {
	encoded, err := keyring.GetWorkspace(teamID)
	if err != nil {
		return Secret{}, fmt.Errorf("load workspace credentials from keyring: %w", err)
	}
	var secret Secret
	if err := json.Unmarshal([]byte(encoded), &secret); err != nil {
		return Secret{}, fmt.Errorf("decode workspace credentials: %w", err)
	}
	return secret, nil
}

func (s *Store) Active() (Metadata, Secret, error) {
	state, err := s.Load()
	if err != nil {
		return Metadata{}, Secret{}, err
	}
	meta, ok := state.Workspaces[state.Active]
	if !ok {
		return Metadata{}, Secret{}, fmt.Errorf("%w; run `slacker workspace add`", ErrNoActiveWorkspace)
	}
	secret, err := s.Secret(meta.TeamID)
	return meta, secret, err
}

func (s *Store) Use(selector string) error {
	state, err := s.Load()
	if err != nil {
		return err
	}
	teamID, err := resolve(state, selector)
	if err != nil {
		return err
	}
	state.Active = teamID
	return s.Save(state)
}

func (s *Store) Remove(selector string) error {
	state, err := s.Load()
	if err != nil {
		return err
	}
	teamID, err := resolve(state, selector)
	if err != nil {
		return err
	}
	if err := keyring.DeleteWorkspace(teamID); err != nil {
		return fmt.Errorf("remove workspace credentials from keyring: %w", err)
	}
	delete(state.Workspaces, teamID)
	if state.Active == teamID {
		state.Active = ""
		for id := range state.Workspaces {
			state.Active = id
			break
		}
	}
	return s.Save(state)
}

func (s *Store) List() ([]Metadata, string, error) {
	state, err := s.Load()
	if err != nil {
		return nil, "", err
	}
	items := make([]Metadata, 0, len(state.Workspaces))
	for _, meta := range state.Workspaces {
		items = append(items, meta)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, state.Active, nil
}

func resolve(state State, selector string) (string, error) {
	if _, ok := state.Workspaces[selector]; ok {
		return selector, nil
	}
	match := ""
	for id, meta := range state.Workspaces {
		if meta.Name == selector || meta.Domain == selector {
			if match != "" {
				return "", fmt.Errorf("workspace %q is ambiguous; use its team ID", selector)
			}
			match = id
		}
	}
	if match == "" {
		return "", fmt.Errorf("workspace %q not found", selector)
	}
	return match, nil
}
