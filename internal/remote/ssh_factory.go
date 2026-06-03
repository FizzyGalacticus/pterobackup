package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/FizzyGalacticus/pterobackup/internal/backup"
	"github.com/FizzyGalacticus/pterobackup/internal/domain"
)

var _ backup.SSHSessionFactory = (*SSHFactory)(nil)

type SSHFactory struct {
	cfg domain.SSHConfig
}

func NewSSHFactory(cfg domain.SSHConfig) *SSHFactory {
	return &SSHFactory{cfg: cfg}
}

func (f *SSHFactory) Connect(ctx context.Context) (backup.RemoteExecutor, backup.FileTransfer, io.Closer, error) {
	sshConfig, err := buildClientConfig(f.cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	addr := fmt.Sprintf("%s:%d", f.cfg.Host, f.cfg.Port)
	dialer := sshDialer{config: sshConfig, addr: addr}
	client, err := dialer.Dial(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial ssh %s: %w", addr, err)
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, fmt.Errorf("create sftp client: %w", err)
	}

	closer := multiCloser{closers: []ioCloser{sftpClient, client}}

	return &executor{client: client}, &transfer{client: sftpClient}, closer, nil
}

type ioCloser interface {
	Close() error
}

type multiCloser struct {
	closers []ioCloser
}

func (m multiCloser) Close() error {
	var result error
	for _, c := range m.closers {
		if err := c.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}

	return result
}

type sshDialer struct {
	config *ssh.ClientConfig
	addr   string
}

func (d sshDialer) Dial(ctx context.Context) (*ssh.Client, error) {
	type dialResult struct {
		client *ssh.Client
		err    error
	}

	ch := make(chan dialResult, 1)
	go func() {
		client, err := ssh.Dial("tcp", d.addr, d.config)
		ch <- dialResult{client: client, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.client, res.err
	}
}

func buildClientConfig(cfg domain.SSHConfig) (*ssh.ClientConfig, error) {
	authMethod, err := authMethod(cfg)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}, nil
}

func authMethod(cfg domain.SSHConfig) (ssh.AuthMethod, error) {
	switch cfg.AuthMethod {
	case domain.AuthMethodPassword:
		if cfg.Password == "" {
			return nil, errors.New("password auth requires password")
		}

		return ssh.Password(cfg.Password), nil
	case domain.AuthMethodKey:
		keyData := cfg.PrivateKeyValue
		if keyData == "" {
			data, err := os.ReadFile(cfg.PrivateKeyPath)
			if err != nil {
				return nil, fmt.Errorf("read private key: %w", err)
			}
			keyData = string(data)
		}

		signer, err := ssh.ParsePrivateKey([]byte(keyData))
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}

		return ssh.PublicKeys(signer), nil
	default:
		return nil, errors.New("unknown auth method")
	}
}
