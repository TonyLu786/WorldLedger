//go:build windows

package shell

import (
	"fmt"
	"os"
	"testing"
)

// What the whole fallback rests on. A -H=windowsgui build started from Explorer
// has no standard error handle, and if a write to one came back as success then
// Tell would believe it had delivered a message that went nowhere, the bug it
// was written to fix, silently reinstated.
func TestAWriteWithNoStandardErrorBehindItFails(t *testing.T) {
	// Handle zero is what GetStdHandle answers with when the process was given
	// no standard error, which is the state this is about.
	nowhere := os.NewFile(0, "no standard error")
	if nowhere == nil {
		// Then it cannot be written to at all, which is the same answer.
		return
	}
	if _, err := fmt.Fprintln(nowhere, "anybody there?"); err == nil {
		t.Fatal("writing to a handle that does not exist reported success, so Tell cannot tell")
	}
}
