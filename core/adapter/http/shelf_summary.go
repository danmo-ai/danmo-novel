package httpapi

import (
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// shelfReadTimeout bounds how long the shelf waits on one book's metadata.
//
// os.ReadFile has no timeout, and in an iCloud library a file whose content is
// not on this device blocks until the download finishes. A whole-library shelf
// must never inherit that wait: past the deadline the book falls back to its
// registry row, which keeps every title on the shelf.
const shelfReadTimeout = 3 * time.Second

// shelfSummary loads one book's shelf entry, giving up after timeout.
//
// ok is false when the book could not be read in time (or at all); the caller
// then uses the stored summary instead. The read goroutine is deliberately left
// running: it holds no shared state and finishes once iCloud delivers the file,
// whereas cancelling a blocked syscall is not possible on all platforms.
func shelfSummary(b domain.BookSummary, timeout time.Duration) (domain.BookSummary, bool) {
	type result struct {
		summary domain.BookSummary
		err     error
	}
	done := make(chan result, 1)
	go func() {
		summary, err := books.LoadBookSummary(b.RootPath, b.ReadOnly)
		done <- result{summary: summary, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res, open := <-done:
		if !open || res.err != nil {
			return domain.BookSummary{}, false
		}
		return res.summary, true
	case <-timer.C:
		return domain.BookSummary{}, false
	}
}