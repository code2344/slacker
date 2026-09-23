package keyring

import (
	"github.com/code2344/slacker/internal/consts"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = consts.Name
	keyringPrefix  = "workspace:"
)

func GetWorkspace(teamID string) (string, error) {
	return keyring.Get(keyringService, keyringPrefix+teamID)
}

func SetWorkspace(teamID, secret string) error {
	return keyring.Set(keyringService, keyringPrefix+teamID, secret)
}

func DeleteWorkspace(teamID string) error {
	return keyring.Delete(keyringService, keyringPrefix+teamID)
}
