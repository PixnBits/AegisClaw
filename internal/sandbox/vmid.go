package sandbox

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidVMID is returned when an id is not safe as one file name under
// the state directory (vmkey, fc-<id>.*, per-VM rootfs).
var ErrInvalidVMID = errors.New("invalid vm id")

// maxStrictVMIDBytes is the StartVM / ensure_role cap.
// The longest fixed id is court-persona-security-architect (32 bytes).
// project-manager- plus a generated plan-* channel is about 33.
// chatstore session ids are lowercase hex (unix nano, at most 20 bytes);
// agent- or memory- plus that id stays well under 64.
const maxStrictVMIDBytes = 64

// vmIDStrict is the single path-segment rule. Dots and underscores are out
// so ".." cannot hide in a name. Uppercase is out (callers are not
// lowercased). "--" and a trailing "-" are rejected below; the class
// itself allows a hyphen.
var vmIDStrict = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidateVMID reports whether id may be interpolated into a state-dir path.
// Firecracker Start calls this before it joins fc-<id>.* paths. runtime.ValidateVMID
// is the same check for StartVM's <id>.vmkey write.
func ValidateVMID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty", ErrInvalidVMID)
	}
	if len(id) > maxStrictVMIDBytes {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidVMID, maxStrictVMIDBytes)
	}
	if !vmIDStrict.MatchString(id) {
		return fmt.Errorf("%w: must match %s", ErrInvalidVMID, vmIDStrict.String())
	}
	if strings.HasSuffix(id, "-") || strings.Contains(id, "--") {
		return fmt.Errorf("%w: empty dash segment", ErrInvalidVMID)
	}
	return nil
}
