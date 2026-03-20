package providers

import "testing"

func TestGetCredentialTypesIncludesOpenAIOAuthBackedByCodex(t *testing.T) {
	var openAIOAuth *CredentialType
	for _, credentialType := range GetCredentialTypes() {
		if credentialType.ID == "openai:oauth" {
			openAIOAuth = &credentialType
			break
		}
	}
	if openAIOAuth == nil {
		t.Fatal("expected openai:oauth credential type")
	}

	if got, want := openAIOAuth.Provider, "openai"; got != want {
		t.Fatalf("expected provider %q, got %q", want, got)
	}
	if got, want := openAIOAuth.BackendProvider, "codex"; got != want {
		t.Fatalf("expected backend provider %q, got %q", want, got)
	}
	if len(openAIOAuth.Env) != 1 || openAIOAuth.Env[0] != "CODEX_TOKEN" {
		t.Fatalf("expected CODEX_TOKEN env, got %v", openAIOAuth.Env)
	}
	if openAIOAuth.OAuth == nil {
		t.Fatal("expected OAuth metadata")
	}
	if got, want := openAIOAuth.OAuth.Provider, "codex"; got != want {
		t.Fatalf("expected oauth provider %q, got %q", want, got)
	}
	if got, want := openAIOAuth.OAuth.Kind, OAuthKindAuthorizationCode; got != want {
		t.Fatalf("expected oauth kind %q, got %q", want, got)
	}
}

func TestGetCredentialTypesPreservesConfiguredToolCredentialMetadata(t *testing.T) {
	var discobotID *CredentialType
	for _, credentialType := range GetCredentialTypes() {
		if credentialType.ID == "discobot:id" {
			discobotID = &credentialType
			break
		}
	}
	if discobotID == nil {
		t.Fatal("expected discobot:id credential type")
	}

	if got, want := discobotID.AuthType, "id"; got != want {
		t.Fatalf("expected auth type %q, got %q", want, got)
	}
	if !discobotID.AutoGenerateSecret {
		t.Fatal("expected auto-generated secret support")
	}
	if got, want := discobotID.Group, CredentialTypeGroupTools; got != want {
		t.Fatalf("expected group %q, got %q", want, got)
	}
}
