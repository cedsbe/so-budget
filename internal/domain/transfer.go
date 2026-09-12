package domain

import "time"

// DetectTransfers returns the ids of transactions that pair up as internal
// transfers: equal amount, opposite sign, different accounts, posted within
// window of each other. Each transaction is used at most once.
func DetectTransfers(txs []Transaction, window time.Duration) map[string]bool {
	used := map[string]bool{}
	for i := range txs {
		a := txs[i]
		if used[a.ID] || a.Amount == 0 {
			continue
		}
		for j := i + 1; j < len(txs); j++ {
			b := txs[j]
			if used[b.ID] || a.AccountID == b.AccountID || a.Amount != -b.Amount {
				continue
			}
			d := a.Posted.Sub(b.Posted)
			if d < 0 {
				d = -d
			}
			if d <= window {
				used[a.ID], used[b.ID] = true, true
				break
			}
		}
	}
	return used
}
