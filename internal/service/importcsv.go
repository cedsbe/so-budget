package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

type ImportRow struct {
	Line        int
	Payer       string
	Payee       string
	Description string
	Type        string
	Date        time.Time
	Amount      domain.Cents
	Err         string
}

type ImportPreview struct {
	Rows   []ImportRow
	Payers []string
	Types  []string
	Errors int
}

type ImportMapping struct {
	Payers map[string]int64
	Types  map[string]int64 // 0 = create a category with the type's name
	Negate bool
}

var importDateLayouts = []string{"2006-01-02", "01/02/2006", "2006/01/02", "1/2/2006", "2 Jan 2006", "Jan 2, 2006"}

func parseImportDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range importDateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

func ParseImportCSV(r io.Reader) (ImportPreview, error) {
	var p ImportPreview
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return p, fmt.Errorf("cannot read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))] = i
	}
	for _, need := range []string{"payer", "date", "payee", "amount", "type"} {
		if _, ok := col[need]; !ok {
			return p, fmt.Errorf("missing column %q", need)
		}
	}
	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	payers, types := map[string]bool{}, map[string]bool{}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return p, fmt.Errorf("line %d: %w", line, err)
		}
		if len(rec) == 0 || strings.Join(rec, "") == "" {
			continue
		}
		row := ImportRow{Line: line, Payer: get(rec, "payer"), Payee: get(rec, "payee"), Description: get(rec, "description"), Type: get(rec, "type")}
		if row.Date, err = parseImportDate(get(rec, "date")); err != nil {
			row.Err = err.Error()
		} else if row.Amount, err = domain.ParseCents(get(rec, "amount")); err != nil {
			row.Err = err.Error()
		} else if row.Payer == "" || row.Type == "" {
			row.Err = "payer and type are required"
		}
		if row.Err != "" {
			p.Errors++
		} else {
			payers[row.Payer], types[row.Type] = true, true
		}
		p.Rows = append(p.Rows, row)
	}
	for k := range payers {
		p.Payers = append(p.Payers, k)
	}
	for k := range types {
		p.Types = append(p.Types, k)
	}
	sort.Strings(p.Payers)
	sort.Strings(p.Types)
	return p, nil
}

func (s *Service) CommitImport(ctx context.Context, p ImportPreview, m ImportMapping) (int, error) {
	for _, name := range p.Payers {
		if m.Payers[name] == 0 {
			return 0, fmt.Errorf("payer %q is not mapped to a user", name)
		}
	}
	n := 0
	err := s.store.WithTx(ctx, func(tx *store.Store) error {
		cats := map[string]int64{}
		for _, name := range p.Types {
			id := m.Types[name]
			if id == 0 {
				existing, err := tx.CategoryByName(ctx, name)
				if err == nil {
					id = existing.ID
				} else {
					if id, err = tx.CreateCategory(ctx, name); err != nil {
						return err
					}
				}
			}
			cats[name] = id
		}
		for _, row := range p.Rows {
			if row.Err != "" {
				continue
			}
			amt := row.Amount
			if m.Negate {
				amt = -amt
			}
			note := row.Payee
			if row.Description != "" {
				note += " — " + row.Description
			}
			e := domain.HouseholdEntry{PayerID: m.Payers[row.Payer], Date: row.Date, Amount: amt, CategoryID: cats[row.Type], Note: note, Source: domain.SourceImport}
			if _, err := tx.CreateEntry(ctx, e); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
