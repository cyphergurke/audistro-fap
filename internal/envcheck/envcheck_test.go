package envcheck

import "testing"

func TestValidateSkipsWhenModeUnset(t *testing.T) {
	t.Setenv("AUDISTRO_ENV", "")

	if err := Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateFailsWhenProdEnvMissing(t *testing.T) {
	t.Setenv("AUDISTRO_ENV", "prod")
	t.Setenv("FAP_MASTER_KEY_HEX", "")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")
	t.Setenv("FAP_ADMIN_TOKEN", "admin")

	if err := Validate(); err == nil || err.Error() != "envcheck: missing required env: FAP_MASTER_KEY_HEX" {
		t.Fatalf("expected missing master key error, got %v", err)
	}
}

func TestValidateAcceptsProdEnv(t *testing.T) {
	t.Setenv("AUDISTRO_ENV", "prod")
	t.Setenv("FAP_MASTER_KEY_HEX", "2222222222222222222222222222222222222222222222222222222222222222")
	t.Setenv("FAP_WEBHOOK_SECRET", "secret")
	t.Setenv("FAP_ADMIN_TOKEN", "admin")

	if err := Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
