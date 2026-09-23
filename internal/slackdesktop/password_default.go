//go:build !darwin && !linux && !windows

package slackdesktop

// Slack Desktop's cookie encryption is currently implemented for its three
// supported desktop platforms. Other systems can use manual session import.
func keyringPasswords() ([][]byte, error) {
	return nil, ErrNoSecretService
}
