package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

func TestPublicKeyFromSSHConfig_NoKey(t *testing.T) {
	t.Parallel()

	pub, hasKey, err := publicKeyFromSSHConfig(domain.SSHConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasKey {
		t.Fatalf("expected hasKey=false")
	}
	if pub != "" {
		t.Fatalf("expected empty public key, got %q", pub)
	}
}

func TestPublicKeyFromSSHConfig_FromPrivateKeyValue(t *testing.T) {
	t.Parallel()

	privatePEM := testPrivateKeyPEM(t)
	pub, hasKey, err := publicKeyFromSSHConfig(domain.SSHConfig{PrivateKeyValue: privatePEM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasKey {
		t.Fatalf("expected hasKey=true")
	}
	if !strings.HasPrefix(pub, "ssh-rsa ") {
		t.Fatalf("expected ssh-rsa public key, got %q", pub)
	}
}

func TestPublicKeyFromSSHConfig_FromPrivateKeyPath(t *testing.T) {
	t.Parallel()

	privatePEM := testPrivateKeyPEM(t)
	keyPath := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(keyPath, []byte(privatePEM), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	pub, hasKey, err := publicKeyFromSSHConfig(domain.SSHConfig{PrivateKeyPath: keyPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasKey {
		t.Fatalf("expected hasKey=true")
	}
	if !strings.HasPrefix(pub, "ssh-rsa ") {
		t.Fatalf("expected ssh-rsa public key, got %q", pub)
	}
}

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}

	return string(pem.EncodeToMemory(block))
}

func TestGenerateRSAKeypair(t *testing.T) {
	t.Parallel()

	privateKey, publicKey, err := generateRSAKeypair()
	if err != nil {
		t.Fatalf("generateRSAKeypair error: %v", err)
	}

	if !strings.Contains(privateKey, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("expected PEM private key, got %q", privateKey)
	}

	if !strings.HasPrefix(publicKey, "ssh-rsa ") {
		t.Fatalf("expected ssh-rsa public key, got %q", publicKey)
	}
}
