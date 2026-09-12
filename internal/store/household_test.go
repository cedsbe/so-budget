package store

import (
	"context"
	"testing"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestCategoriesAndHouseholdRules(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	id, err := s.CreateCategory(ctx, "Groceries")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, "Groceries"); err == nil {
		t.Fatal("duplicate category must fail")
	}
	s.CreateCategory(ctx, "Rent")
	s.UpdateCategory(ctx, domain.Category{ID: id, Name: "Food", Active: false})
	all, _ := s.ListCategories(ctx, false)
	active, _ := s.ListCategories(ctx, true)
	if len(all) != 2 || len(active) != 1 || active[0].Name != "Rent" {
		t.Fatalf("all=%+v active=%+v", all, active)
	}
	if c, err := s.CategoryByName(ctx, "food"); err != nil || c.ID != id {
		t.Fatalf("by name should be case-insensitive: %+v %v", c, err)
	}

	rid, err := s.CreateHouseholdRule(ctx, domain.HouseholdRule{Pattern: "sobeys", CategoryID: id, SuggestHousehold: true})
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateHouseholdRule(ctx, domain.HouseholdRule{ID: rid, Pattern: "sobeys #", CategoryID: id, SuggestHousehold: false})
	rules, _ := s.ListHouseholdRules(ctx)
	if len(rules) != 1 || rules[0].Pattern != "sobeys #" || rules[0].SuggestHousehold {
		t.Fatalf("rules %+v", rules)
	}
	s.DeleteHouseholdRule(ctx, rid)
	if rules, _ := s.ListHouseholdRules(ctx); len(rules) != 0 {
		t.Fatal("delete failed")
	}
}
