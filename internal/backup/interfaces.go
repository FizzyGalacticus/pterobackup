package backup

import (
	"context"
	"io"
)

// RemoteExecutor runs shell commands on the remote host.
type RemoteExecutor interface {
	Run(ctx context.Context, command string) (stdout string, stderr string, err error)
}

// FileTransfer copies files between local and remote hosts.
type FileTransfer interface {
	Download(ctx context.Context, remotePath, localPath string) error
	Upload(ctx context.Context, localPath, remotePath string) error
}

// SSHSessionFactory produces remote execution and transfer clients.
type SSHSessionFactory interface {
	Connect(ctx context.Context) (RemoteExecutor, FileTransfer, io.Closer, error)
}
