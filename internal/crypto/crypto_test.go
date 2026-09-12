package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	k, _ := NewKey()
	box, err := Seal(k, []byte("transactions:abc"), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(box, []byte("hello")) {
		t.Fatal("ciphertext leaks plaintext")
	}
	pt, err := Open(k, []byte("transactions:abc"), box)
	if err != nil || string(pt) != "hello" {
		t.Fatalf("open: %q %v", pt, err)
	}
	if _, err := Open(k, []byte("transactions:other"), box); err != ErrDecrypt {
		t.Fatalf("aad mismatch should fail, got %v", err)
	}
	k2, _ := NewKey()
	if _, err := Open(k2, []byte("transactions:abc"), box); err != ErrDecrypt {
		t.Fatalf("wrong key should fail, got %v", err)
	}
	if _, err := Open(k, []byte("transactions:abc"), box[:5]); err != ErrDecrypt {
		t.Fatalf("truncated should fail, got %v", err)
	}
}

func TestWrapUnwrapWithDerivedKey(t *testing.T) {
	dek, _ := NewKey()
	salt, _ := NewSalt()
	kek := Derive("correct horse", salt, TestParams)
	box, err := Wrap(dek, kek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unwrap(box, Derive("correct horse", salt, TestParams))
	if err != nil || got != dek {
		t.Fatalf("unwrap: %v", err)
	}
	if _, err := Unwrap(box, Derive("wrong", salt, TestParams)); err != ErrDecrypt {
		t.Fatalf("wrong password should fail, got %v", err)
	}
}

func TestRecoveryCode(t *testing.T) {
	c, err := NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(c, "-")
	if len(parts) != 8 {
		t.Fatalf("want 8 groups, got %q", c)
	}
	for _, p := range parts {
		if len(p) != 4 {
			t.Fatalf("bad group in %q", c)
		}
	}
	if NormalizeRecoveryCode(" "+strings.ToLower(c)+" ") != strings.ReplaceAll(c, "-", "") {
		t.Fatal("normalize wrong")
	}
}

func TestHashID(t *testing.T) {
	k1, _ := NewKey()
	k2, _ := NewKey()
	a := HashID(k1, []byte("pepper"), "tx-1")
	if a != HashID(k1, []byte("pepper"), "tx-1") {
		t.Fatal("not deterministic")
	}
	if a == HashID(k2, []byte("pepper"), "tx-1") || a == HashID(k1, []byte("other"), "tx-1") || a == HashID(k1, []byte("pepper"), "tx-2") {
		t.Fatal("hash should depend on key, pepper and id")
	}
	if len(a) != 64 {
		t.Fatalf("want hex sha256, got %q", a)
	}
}

func TestRandomToken(t *testing.T) {
	tok, err := RandomToken(16)
	if err != nil || len(tok) != 32 {
		t.Fatalf("got %q %v", tok, err)
	}
}
