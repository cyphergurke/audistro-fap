package secretbox

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	master := bytes.Repeat([]byte{0x11}, 32)
	plain := []byte("secret-value")

	blob, err := Encrypt(master, plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(blob, plain) {
		t.Fatal("cipher blob leaks plaintext")
	}

	out, err := Decrypt(master, blob)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("decrypt mismatch: got %q want %q", out, plain)
	}
}

func TestDecryptRejectsShortBlob(t *testing.T) {
	master := bytes.Repeat([]byte{0x22}, 32)
	if _, err := Decrypt(master, []byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short blob")
	}
}
