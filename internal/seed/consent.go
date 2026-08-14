package seed

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Notice is shown before the tool will do anything. It is deliberately shown in
// full every time rather than reduced to a stored preference: the person running
// the command is the person who carries the consequences, and that person can
// change between runs.
const Notice = `WORLD SEED RECOVERY - READ BEFORE USE

What this does
  It searches for world generation parameters consistent with structures you
  supply. A recovered seed does not describe only what you observed. It makes the
  entire world computable, including regions nobody has visited: strongholds,
  ancient cities, buried treasure, slime chunks and spawners all become known.

Who is responsible
  You are. Running this tool is your decision and the consequences are yours.
  The WorldLedger project supplies the software and claims no authority to
  permit its use against any particular world. Nothing here grants you
  permission you do not already hold.

Before you use it on a world you do not own
  Recovering a seed can breach a server's rules, its terms of use, or a
  competition's rules, even when every input came from ordinary client
  visibility. Being technically able to observe something is not permission to
  derive from it or to publish what you derived.
  A recovered seed affects everyone on that server, not only you. They did not
  agree to it and cannot undo it.

Intended use
  Worlds you own, worlds whose operator has agreed, single player worlds whose
  seed was lost, and generation research on worlds you created.

No warranty
  Results are candidates, not answers. This software is provided without
  warranty of any kind, and the project accepts no liability for any use of it
  or of anything derived from it. See LICENSE.`

// Acknowledgement records who accepted the notice. It is written into every
// result, so an output file cannot circulate without the record of who produced
// it and under what claim.
type Acknowledgement struct {
	Operator   string    `json:"operator"`
	AcceptedAt time.Time `json:"accepted_at"`
	Statement  string    `json:"statement"`
}

// StatementOfResponsibility is what the operator is asserting by proceeding.
const StatementOfResponsibility = "I am responsible for this use, and I own this world or have its operator's permission."

var (
	// ErrNotAccepted is returned when the terms were not accepted.
	ErrNotAccepted = errors.New("the responsibility notice has not been accepted")
	// ErrNoOperator is returned when nobody is named as responsible.
	ErrNoOperator = errors.New("an operator must be named as responsible for this use")
)

// Accept validates an acceptance. It refuses an anonymous one: an
// acknowledgement nobody signed records nothing.
func Accept(operator string, accepted bool) (Acknowledgement, error) {
	if !accepted {
		return Acknowledgement{}, ErrNotAccepted
	}
	operator = strings.TrimSpace(operator)
	if operator == "" {
		return Acknowledgement{}, ErrNoOperator
	}
	if strings.ContainsAny(operator, "\r\n") {
		return Acknowledgement{}, fmt.Errorf("operator must be a single line")
	}
	return Acknowledgement{
		Operator:   operator,
		AcceptedAt: time.Now().UTC(),
		Statement:  StatementOfResponsibility,
	}, nil
}
