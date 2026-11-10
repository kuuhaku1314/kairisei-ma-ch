package accountstore

import (
	"errors"
	"fmt"
	"os"
)

func (storage *Database) ensureCurrentDatabase() error {
	if info, err := os.Stat(storage.databasePath); err == nil {
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return errors.New("CN state database must be a non-empty regular file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat CN state database: %w", err)
	}

	return nil
}
