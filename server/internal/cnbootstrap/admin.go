package cnbootstrap

import (
	"io"
	"net/http"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/operations"
)

type cnDeploymentHandler struct {
	http.Handler
	admin      http.Handler
	database   *accountstore.Database
	operations *operations.Runtime
	accounts   *accountstore.Accounts
}

func (handler *cnDeploymentHandler) AdminHandler() http.Handler {
	return handler.admin
}

func (handler *cnDeploymentHandler) IPBlocked(remoteAddr string) bool {
	return handler.operations.IPBlocked(remoteAddr)
}

// Close is called by the service owner after HTTP/Admin/BattleSv have drained.
func (handler *cnDeploymentHandler) Close() error {
	if closer, ok := handler.admin.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			return err
		}
	}
	if handler.operations != nil {
		handler.operations.CloseMaintenance()
	}
	return handler.database.Close()
}

// Activate is called once by the service owner after all listeners are bound.
// Merely assembling a second handler must not revoke a running server's users.
func (handler *cnDeploymentHandler) Activate() error {
	return handler.accounts.InvalidateSessions()
}
