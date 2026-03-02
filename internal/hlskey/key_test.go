package hlskey

import "testing"

func TestAES128KeyDeterministic(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	got := AES128Key(masterKey, "asset-1")
	want := [16]byte{0x1d, 0x14, 0x16, 0xc8, 0xd4, 0x7d, 0x28, 0x8d, 0x70, 0x21, 0x70, 0x63, 0x9d, 0x15, 0x2a, 0x62}
	if got != want {
		t.Fatalf("unexpected key: got %x want %x", got, want)
	}
}
