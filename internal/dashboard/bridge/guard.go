package bridge

import (
	"fmt"

	"AegisClaw/internal/dashboard/contracts"
)

// Guard validates bridge actions before the portal invokes the vsock client.
type Guard struct{}

func NewGuard() *Guard { return &Guard{} }

// Validate returns an error if action is not on the portal allow-list.
func (g *Guard) Validate(action string) error {
	if contracts.IsAllowedBridgeAction(action) {
		return nil
	}
	return fmt.Errorf("bridge action not permitted: %s", action)
}

// NeedsConfirmation reports whether the UI must obtain user confirmation first.
func (g *Guard) NeedsConfirmation(action string) bool {
	return contracts.RequiresConfirmation(action)
}