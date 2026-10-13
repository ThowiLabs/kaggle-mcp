package projectstate

import (
	"sort"
	"time"
)

const HistoryPageSize = 20

// HistoryPage is a bounded, immutable view of the audit trail. Page 1
// always contains the newest events; pagination never returns the full log.
type HistoryPage struct {
	Items       []Event   `json:"items"`
	Page        int       `json:"page"`
	PageSize    int       `json:"page_size"`
	Total       int       `json:"total"`
	TotalPages  int       `json:"total_pages"`
	HasPrev     bool      `json:"has_prev"`
	HasNext     bool      `json:"has_next"`
	PrevPage    int       `json:"prev_page"`
	NextPage    int       `json:"next_page"`
	PageNumbers []int     `json:"page_numbers"`
	CheckedAt   time.Time `json:"checked_at"`
}

func (s *Store) HistoryPage(number int) HistoryPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.state.History)
	pages := (total + HistoryPageSize - 1) / HistoryPageSize
	if pages == 0 {
		pages = 1
	}
	if number < 1 {
		number = 1
	}
	if number > pages {
		number = pages
	}
	// This array is capped by Store.Record at 1,000. Sort a copy by
	// timestamp rather than assuming imports always preserve append order.
	// Reverse insertion order first: two events may share a timestamp at
	// the platform clock resolution. The later recorded event is newer.
	ordered := make([]Event, total)
	for i := range ordered {
		ordered[i] = s.state.History[total-1-i]
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].When.After(ordered[j].When)
	})
	start := (number - 1) * HistoryPageSize
	end := start + HistoryPageSize
	if end > total {
		end = total
	}
	items := append([]Event{}, ordered[start:end]...)
	result := HistoryPage{
		Items: items, Page: number, PageSize: HistoryPageSize, Total: total,
		TotalPages: pages, HasPrev: number > 1, HasNext: number < pages,
		CheckedAt: time.Now().UTC(),
	}
	if result.HasPrev {
		result.PrevPage = number - 1
	}
	if result.HasNext {
		result.NextPage = number + 1
	}
	// First, last and a small window remain reachable directly.
	seen := map[int]bool{}
	for _, candidate := range []int{1, number - 2, number - 1, number, number + 1, number + 2, pages} {
		if candidate >= 1 && candidate <= pages && !seen[candidate] {
			result.PageNumbers = append(result.PageNumbers, candidate)
			seen[candidate] = true
		}
	}
	sort.Ints(result.PageNumbers)
	return result
}
