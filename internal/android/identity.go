//go:build android

package android

import (
	"context"
	_ "embed"
	"os/exec"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
)

//go:embed identity.sh
var identityScript string

type Identity struct {
	ID   string
	Name string
}

func ReadIdentity(ctx context.Context) (Identity, error) {
	output, err := exec.CommandContext(ctx, "/system/bin/sh", "-c", identityScript).Output()
	if err != nil {
		return Identity{}, E.Cause(err, "read the identity of the device")
	}
	id, name, found := strings.Cut(strings.TrimSuffix(string(output), "\n"), "\n")
	if !found {
		return Identity{}, E.New("unexpected output of the identity script: ", string(output))
	}
	return Identity{ID: strings.TrimSpace(id), Name: strings.TrimSpace(name)}, nil
}
