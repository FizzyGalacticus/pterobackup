package remote

import (
	"bytes"
	"context"
	"fmt"

	"golang.org/x/crypto/ssh"
)

type executor struct {
	client *ssh.Client
}

func (e *executor) Run(_ context.Context, command string) (string, string, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("create ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(command)

	return stdout.String(), stderr.String(), err
}
