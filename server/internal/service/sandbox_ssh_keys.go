package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/encryption"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/store"
)

const sandboxSSHKeyFilename = "discobot_sandbox"

type sessionSSHKeyData struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Algorithm  string `json:"algorithm"`
	Filename   string `json:"filename"`
}

func ensureSessionSSHKey(ctx context.Context, s *store.Store, cfg *config.Config, session *model.Session) (*sandbox.SSHKeyProvision, error) {
	encryptor, err := encryption.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox ssh key encryptor: %w", err)
	}

	if len(session.SSHKeyEncryptedData) > 0 {
		return decryptSessionSSHKey(encryptor, session.SSHKeyEncryptedData)
	}

	keyData, err := generateSessionSSHKey()
	if err != nil {
		return nil, err
	}

	encrypted, err := encryptor.EncryptJSON(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt sandbox ssh key: %w", err)
	}

	if err := s.UpdateSessionSSHKey(ctx, session.ID, encrypted); err != nil {
		return nil, fmt.Errorf("failed to persist sandbox ssh key: %w", err)
	}
	session.SSHKeyEncryptedData = encrypted

	return keyData.toProvision(), nil
}

func decryptSessionSSHKey(encryptor *encryption.Encryptor, encrypted []byte) (*sandbox.SSHKeyProvision, error) {
	var keyData sessionSSHKeyData
	if err := encryptor.DecryptJSON(encrypted, &keyData); err != nil {
		return nil, fmt.Errorf("failed to decrypt sandbox ssh key: %w", err)
	}
	if strings.TrimSpace(keyData.PrivateKey) == "" || strings.TrimSpace(keyData.PublicKey) == "" {
		return nil, fmt.Errorf("sandbox ssh key payload is incomplete")
	}
	if keyData.Filename == "" {
		keyData.Filename = sandboxSSHKeyFilename
	}
	return keyData.toProvision(), nil
}

func generateSessionSSHKey() (*sessionSSHKeyData, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate sandbox ssh key: %w", err)
	}

	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sandbox ssh private key: %w", err)
	}

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sandbox ssh public key: %w", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyDER})
	return &sessionSSHKeyData{
		PrivateKey: string(privateKeyPEM),
		PublicKey:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))),
		Algorithm:  publicKey.Type(),
		Filename:   sandboxSSHKeyFilename,
	}, nil
}

func (k *sessionSSHKeyData) toProvision() *sandbox.SSHKeyProvision {
	if k == nil {
		return nil
	}
	return &sandbox.SSHKeyProvision{
		Filename:   k.Filename,
		PrivateKey: k.PrivateKey,
		PublicKey:  k.PublicKey,
		Algorithm:  k.Algorithm,
	}
}
