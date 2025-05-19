package gen

import (
	"strings"

	"github.com/google/uuid"
)

// ID returns a unique identifier generated from a UUID, using the last 12 characters.
func ID() string {
	id := uuid.NewString()

	return strings.ToUpper(id[len(id)-12:])
}
