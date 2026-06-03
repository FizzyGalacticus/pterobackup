package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/pkg/sftp"
)

type transfer struct {
	client *sftp.Client
}

func (t *transfer) Download(_ context.Context, remotePath, localPath string) error {
	src, err := t.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file %s: %w", remotePath, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file %s: %w", localPath, err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy remote %s to local %s: %w", remotePath, localPath, err)
	}

	return nil
}

func (t *transfer) Upload(_ context.Context, localPath, remotePath string) error {
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file %s: %w", localPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := t.client.MkdirAll(path.Dir(remotePath)); err != nil {
		return fmt.Errorf("create remote directory for %s: %w", remotePath, err)
	}

	dst, err := t.client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file %s: %w", remotePath, err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy local %s to remote %s: %w", localPath, remotePath, err)
	}

	return nil
}
