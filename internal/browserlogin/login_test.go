package browserlogin

import "testing"

func TestSessionsUsesTeamKeyAndURLFallbacks(t *testing.T) {
	config := localConfig{Teams: map[string]struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Domain string `json:"domain"`
		URL    string `json:"url"`
		Token  string `json:"token"`
	}{
		"T123": {Name: "Example", URL: "https://example.slack.com/", Token: "xoxc-test"},
	}}
	got, err := sessions(config, "xoxd-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TeamID != "T123" || got[0].Domain != "example" || got[0].Cookie != "xoxd-test" {
		t.Fatalf("unexpected sessions: %#v", got)
	}
}

func TestSessionsRejectsIncompleteData(t *testing.T) {
	if _, err := sessions(localConfig{}, "xoxd-test"); err == nil {
		t.Fatal("expected incomplete browser data to be rejected")
	}
}
