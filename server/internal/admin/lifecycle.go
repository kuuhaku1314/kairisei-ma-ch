package admin

import "net/http"

// Close cancels the sole background image task and waits before its database
// owner closes SQLite. No task may be admitted after this handler is closed.
type lifecycleHandler struct {
	http.Handler
	images *imageSyncState
}

func (h *lifecycleHandler) Close() error {
	h.images.mu.Lock()
	h.images.closed = true
	cancel, done := h.images.cancel, h.images.done
	h.images.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}
