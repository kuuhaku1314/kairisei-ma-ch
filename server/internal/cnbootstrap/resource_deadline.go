package cnbootstrap

import (
	"net/http"
	"time"
)

// API writes retain the server's 60-second deadline. A registered download gets
// one minute plus its size at 64 KiB/s, without buffering it or changing ranges.
func allowResourceTransfer(w http.ResponseWriter, size int64) {
	seconds := int64(60) + max(size, 0)/(64*1024)
	// Saturate before converting file bytes into a time.Duration.
	seconds = min(seconds, int64((1<<63-1)/int64(time.Second)))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(seconds) * time.Second))
}
