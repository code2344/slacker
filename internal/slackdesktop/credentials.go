package slackdesktop

import "fmt"

type Credentials struct {
	Workspace Workspace
	Token     string
	Cookie    string
}

// Discover reads Slack Desktop without mutating its profile and returns each
// signed-in workspace for which a user token is present.
func Discover() ([]Credentials, error) {
	workspaces, err := Workspaces()
	if err != nil {
		return nil, err
	}
	tokens, err := Tokens()
	if err != nil {
		return nil, err
	}
	cookie, err := Cookie()
	if err != nil {
		return nil, err
	}
	result := make([]Credentials, 0, len(workspaces))
	for _, ws := range workspaces {
		if token := tokens[ws.TeamID]; token != "" {
			result = append(result, Credentials{Workspace: ws, Token: token, Cookie: cookie})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: Slack Desktop has no usable workspace tokens", ErrNotSignedIn)
	}
	return result, nil
}
