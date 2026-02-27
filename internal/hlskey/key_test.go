package hlskey

import "testing"

func TestDevAES128KeyDeterministic(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	got := DevAES128Key(masterKey, "asset-1")
	want := [16]byte{0x12, 0x7d, 0x87, 0xd4, 0x73, 0x78, 0x64, 0xe2, 0x85, 0xf0, 0x01, 0x88, 0xe9, 0xe8, 0x8e, 0x00}
	if got != want {
		t.Fatalf("unexpected key: got %x want %x", got, want)
	}
}
