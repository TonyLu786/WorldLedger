package shell

import (
	"fmt"
	"os"
)

// Tell puts a message where the person will actually see it.
//
// Standard error is the right place when there is one, and the write is how
// this finds out whether there is. The Windows build of this program is linked
// with -H=windowsgui, which is what keeps a console window from appearing
// behind the application; started that way from Explorer it has no standard
// error at all and everything written there fails. That is fine for a note
// nobody has to act on. It is not fine for "the program is running and here is
// the address you have to open yourself", which is the one message the shipped
// build could not deliver.
//
// Asking the write, rather than asking the operating system, is deliberate.
// The question is whether the message arrived, and the proxies for it each
// answer something else and are wrong somewhere: this was first written to
// check for a console window, and output redirected to a pipe has no console,
// prints perfectly well, and would have got a dialog box.
func Tell(message string) {
	if _, err := fmt.Fprintln(os.Stderr, message); err == nil {
		return
	}
	showElsewhere(message)
}
