package service

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

// linkedUser returns a principal whose fixture data has been synced.
func linkedUser(t *testing.T, svc *Service, fakeToken string, name string) Principal {
	t.Helper()
	p := LoginTestUser(t, svc, name)
	if err := svc.LinkSimpleFIN(context.Background(), p, fakeToken); err != nil {
		t.Fatal(err)
	}
	return p
}

func findItem(t *testing.T, items []InboxItem, payee string) InboxItem {
	t.Helper()
	for _, it := range items {
		if it.Tx.Payee == payee {
			return it
		}
	}
	t.Fatalf("no inbox item %q", payee)
	return InboxItem{}
}

func TestInboxFlagDismissUnflag(t *testing.T) {
	svc, fake := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	fake.Set(fixtureSet())
	ctx := context.Background()
	cat, _ := svc.AddCategory(ctx, "Groceries")
	svc.AddHouseholdRule(ctx, domain.HouseholdRule{Pattern: "sobeys", CategoryID: cat, SuggestHousehold: true})
	p := linkedUser(t, svc, fake.SetupToken(), "alice")

	inbox, err := svc.Inbox(ctx, p)
	if err != nil || !inbox.Linked || len(inbox.Items) != 2 {
		t.Fatalf("inbox %+v %v", inbox, err)
	}
	// Unsuggested first: NETFLIX has no rule, SOBEYS does.
	if inbox.Items[0].Tx.Payee != "NETFLIX.COM" || inbox.Items[1].SuggestedCategory != "Groceries" {
		t.Fatalf("order/suggestion wrong: %+v", inbox.Items)
	}
	sob := findItem(t, inbox.Items, "SOBEYS #1234")

	// Partial amount validation.
	if _, err := svc.Flag(ctx, p, sob.IDHash, cat, 9999, ""); err != ErrInvalidAmount {
		t.Fatalf("over amount: %v", err)
	}
	if _, err := svc.Flag(ctx, p, sob.IDHash, cat, -100, ""); err != ErrInvalidAmount {
		t.Fatalf("wrong sign: %v", err)
	}
	entryID, err := svc.Flag(ctx, p, sob.IDHash, cat, 4000, "minus the wine")
	if err != nil {
		t.Fatal(err)
	}
	e, _ := svc.store.Entry(ctx, entryID)
	if e.Amount != 4000 || e.PayerID != p.UserID || e.Source != domain.SourceSync || e.SourceRef != sob.IDHash || e.Note != "minus the wine" {
		t.Fatalf("entry %+v", e)
	}
	if _, err := svc.Flag(ctx, p, sob.IDHash, cat, 0, ""); err != ErrInvalidState {
		t.Fatalf("double flag: %v", err)
	}
	inbox, _ = svc.Inbox(ctx, p)
	if len(inbox.Items) != 1 {
		t.Fatal("flagged item should leave inbox")
	}

	// Edit the flagged transaction: entry follows.
	if err := svc.EditFlagged(ctx, p, sob.IDHash, cat, 4321, "full"); err != nil {
		t.Fatal(err)
	}
	e, _ = svc.store.Entry(ctx, entryID)
	if e.Amount != 4321 || e.Note != "full" {
		t.Fatalf("edited %+v", e)
	}

	// Unflag deletes the entry and returns the transaction to the inbox.
	if err := svc.Unflag(ctx, p, sob.IDHash); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Entry(ctx, entryID); err == nil {
		t.Fatal("entry should be deleted")
	}
	inbox, _ = svc.Inbox(ctx, p)
	if len(inbox.Items) != 2 {
		t.Fatal("unflagged item should be back")
	}

	// Dismiss and restore.
	nf := findItem(t, inbox.Items, "NETFLIX.COM")
	svc.Dismiss(ctx, p, nf.IDHash)
	inbox, _ = svc.Inbox(ctx, p)
	if len(inbox.Items) != 1 {
		t.Fatal("dismissed should leave inbox")
	}
	svc.Restore(ctx, p, nf.IDHash)
	inbox, _ = svc.Inbox(ctx, p)
	if len(inbox.Items) != 2 {
		t.Fatal("restored should be back")
	}

	// Full-amount flag then shared delete resets to new.
	id, _ := svc.Flag(ctx, p, sob.IDHash, cat, 0, "")
	e, _ = svc.store.Entry(ctx, id)
	if e.Amount != 4321 {
		t.Fatalf("full amount should be 43.21, got %d", e.Amount)
	}
	if err := svc.DeleteEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	row, _ := svc.store.Transaction(ctx, p.UserID, sob.IDHash)
	if row.State != "new" {
		t.Fatalf("deleted entry must reset tx, state=%s", row.State)
	}
}

func TestAcceptAllAndReapply(t *testing.T) {
	svc, fake := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	fake.Set(fixtureSet())
	ctx := context.Background()
	p := linkedUser(t, svc, fake.SetupToken(), "alice")

	n, _ := svc.AcceptAllSuggestions(ctx, p)
	if n != 0 {
		t.Fatal("nothing suggested yet")
	}
	cat, _ := svc.AddCategory(ctx, "Groceries")
	svc.AddHouseholdRule(ctx, domain.HouseholdRule{Pattern: "sobeys", CategoryID: cat, SuggestHousehold: true})
	svc.AddHouseholdRule(ctx, domain.HouseholdRule{Pattern: "netflix", CategoryID: cat, SuggestHousehold: false})
	if err := svc.ReapplyRules(ctx, p); err != nil {
		t.Fatal(err)
	}
	n, err := svc.AcceptAllSuggestions(ctx, p)
	if err != nil || n != 1 {
		t.Fatalf("accept all = %d %v (only household suggestions with a category are accepted)", n, err)
	}
	entries, _ := svc.store.ListEntries(ctx, domain.MonthOf(fixedNow))
	if len(entries) != 1 || entries[0].Amount != 4321 {
		t.Fatalf("entries %+v", entries)
	}
}

func TestHistory(t *testing.T) {
	svc, fake := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	fake.Set(fixtureSet())
	ctx := context.Background()
	p := linkedUser(t, svc, fake.SetupToken(), "alice")
	cat, _ := svc.AddCategory(ctx, "Groceries")
	inbox, _ := svc.Inbox(ctx, p)
	sob := findItem(t, inbox.Items, "SOBEYS #1234")
	svc.Flag(ctx, p, sob.IDHash, cat, 0, "")

	items, err := svc.History(ctx, p, domain.MonthOf(fixedNow), "")
	if err != nil || len(items) != 4 { // c1, c2, v1, v2 all in September
		t.Fatalf("history %d %v", len(items), err)
	}
	var flagged, dismissed int
	for _, it := range items {
		if it.Entry != nil {
			flagged++
		}
		if it.Tx.State == domain.TxDismissed {
			dismissed++
		}
	}
	if flagged != 1 || dismissed != 2 {
		t.Fatalf("flagged=%d dismissed=%d", flagged, dismissed)
	}
	items, _ = svc.History(ctx, p, domain.MonthOf(fixedNow), "netflix")
	if len(items) != 1 {
		t.Fatalf("search %d", len(items))
	}
}
