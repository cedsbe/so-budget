package domain

import (
	"testing"
	"time"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

func TestDetectTransfers(t *testing.T) {
	txs := []Transaction{
		{ID: "a", AccountID: "chq", Posted: day(1), Amount: -50000, Payee: "TFR-TO C/C"},
		{ID: "b", AccountID: "sav", Posted: day(2), Amount: 50000, Payee: "TFR-FR C/C"},
		{ID: "c", AccountID: "chq", Posted: day(3), Amount: -50000, Payee: "SOBEYS"}, // no partner in window (d is too far)
		{ID: "d", AccountID: "sav", Posted: day(9), Amount: 50000, Payee: "DEPOSIT"},
		{ID: "e", AccountID: "chq", Posted: day(4), Amount: -1000, Payee: "X"},
		{ID: "f", AccountID: "chq", Posted: day(4), Amount: 1000, Payee: "Y"}, // same account: not a transfer
	}
	got := DetectTransfers(txs, 3*24*time.Hour)
	want := map[string]bool{"a": true, "b": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("missing %s", id)
		}
	}
}

func TestDetectTransfersPairsEachOnce(t *testing.T) {
	txs := []Transaction{
		{ID: "a", AccountID: "chq", Posted: day(1), Amount: -100},
		{ID: "b", AccountID: "sav", Posted: day(1), Amount: 100},
		{ID: "c", AccountID: "sav", Posted: day(1), Amount: 100},
	}
	got := DetectTransfers(txs, 24*time.Hour)
	if len(got) != 2 || !got["a"] {
		t.Fatalf("got %v", got)
	}
}
