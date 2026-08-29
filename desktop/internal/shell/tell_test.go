package shell

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Tell has one job: the message gets somewhere. Where that is depends on the
// build, and the half that can be checked without putting a window on
// somebody's screen is the printing half. The other half is kept small enough
// to read instead.

func TestTellPrintsWhenThereIsSomewhereToPrint(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = previous }()

	Tell("open this address yourself:\nhttp://127.0.0.1:1234/")
	write.Close()

	printed, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(printed), "http://127.0.0.1:1234/") {
		t.Errorf("the address never reached standard error; got %q", string(printed))
	}
}
