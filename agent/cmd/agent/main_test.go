package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSandboxSSHKeyFiles(t *testing.T) {
	srcDir := t.TempDir()
	homeDir := t.TempDir()

	privateKey := []byte("PRIVATE KEY DATA\n")
	publicKey := []byte("ecdsa-sha2-nistp256 AAAATEST discobot\n")

	if err := os.WriteFile(filepath.Join(srcDir, sandboxSSHKeyName), privateKey, 0600); err != nil {
		t.Fatalf("failed to write staged private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, sandboxSSHKeyName+".pub"), publicKey, 0644); err != nil {
		t.Fatalf("failed to write staged public key: %v", err)
	}

	if err := installSandboxSSHKeyFiles(srcDir, homeDir, -1, -1); err != nil {
		t.Fatalf("installSandboxSSHKeyFiles failed: %v", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	privateDst := filepath.Join(sshDir, sandboxSSHKeyName)
	publicDst := filepath.Join(sshDir, sandboxSSHKeyName+".pub")

	privateData, err := os.ReadFile(privateDst)
	if err != nil {
		t.Fatalf("failed to read installed private key: %v", err)
	}
	if string(privateData) != string(privateKey) {
		t.Fatalf("unexpected private key contents: %q", string(privateData))
	}

	publicData, err := os.ReadFile(publicDst)
	if err != nil {
		t.Fatalf("failed to read installed public key: %v", err)
	}
	if string(publicData) != string(publicKey) {
		t.Fatalf("unexpected public key contents: %q", string(publicData))
	}

	sshInfo, err := os.Stat(sshDir)
	if err != nil {
		t.Fatalf("failed to stat ssh dir: %v", err)
	}
	if got := sshInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("ssh dir mode = %o, want 700", got)
	}

	privateInfo, err := os.Stat(privateDst)
	if err != nil {
		t.Fatalf("failed to stat installed private key: %v", err)
	}
	if got := privateInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("private key mode = %o, want 600", got)
	}

	publicInfo, err := os.Stat(publicDst)
	if err != nil {
		t.Fatalf("failed to stat installed public key: %v", err)
	}
	if got := publicInfo.Mode().Perm(); got != 0644 {
		t.Fatalf("public key mode = %o, want 644", got)
	}

	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatalf("expected staging dir to be removed, err=%v", err)
	}

	if _, err := os.Stat(filepath.Join(sshDir, "id_ecdsa")); !os.IsNotExist(err) {
		t.Fatalf("expected default ssh identity to remain absent, err=%v", err)
	}
}

func TestInstallSandboxSSHKeyFilesSkipsWhenNoKeyStaged(t *testing.T) {
	srcDir := t.TempDir()
	homeDir := t.TempDir()

	if err := installSandboxSSHKeyFiles(srcDir, homeDir, -1, -1); err != nil {
		t.Fatalf("installSandboxSSHKeyFiles returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(homeDir, ".ssh")); !os.IsNotExist(err) {
		t.Fatalf("expected .ssh dir to remain absent, err=%v", err)
	}
}
