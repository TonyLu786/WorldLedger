package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/policy"
)

// Declaring is the one step this application does not do for anybody.
//
// Everything else here exists to remove a decision the person never wanted to
// make. This is a decision they do want to make, and it is the reason the rest
// is safe to automate: an archive records where somebody went and when, and it
// does not leave the machine until a named person has said what may happen to
// it. Making that a checkbox with a default would be removing the only step
// that carries responsibility.
//
// So the wording is plain, all four choices are shown with what they mean, and
// nothing is preselected.

// Choice is one disposition, described for somebody who has not read the trust
// model.
type Choice struct {
	Value       string `json:"value"`
	Title       string `json:"title"`
	Meaning     string `json:"meaning"`
	NeedsExpiry bool   `json:"needs_expiry,omitempty"`
}

// Choices are given by the application rather than written into the page, so
// that the set the person sees cannot drift from the set the archive accepts.
func Choices() []Choice {
	return []Choice{
		{
			Value:   string(policy.Private),
			Title:   "Keep it to myself",
			Meaning: "Nothing is shared. You can still make worlds and look at your own recordings.",
		},
		{
			Value:       string(policy.Embargoed),
			Title:       "Not yet",
			Meaning:     "Held back until a date you choose, then treated as shareable.",
			NeedsExpiry: true,
		},
		{
			Value:   string(policy.Research),
			Title:   "For study",
			Meaning: "May be shared with people examining how worlds change, not published openly.",
		},
		{
			Value:   string(policy.Public),
			Title:   "Anyone may have it",
			Meaning: "You are stating that the server's community accepted this. Say this only if it is true.",
		},
	}
}

type declaration struct {
	Server      string `json:"server"`
	Disposition string `json:"disposition"`
	DeclaredBy  string `json:"declared_by"`
	Until       string `json:"until,omitempty"`
	Note        string `json:"note,omitempty"`
}

func handleChoices(w http.ResponseWriter, r *http.Request) {
	app.WriteJSON(w, http.StatusOK, Choices())
}

func handleDeclare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"a declaration has to be made deliberately", "use the form on the decide screen")
		return
	}

	var request declaration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		app.WriteFailure(w, http.StatusBadRequest,
			"the declaration could not be read", "try again")
		return
	}

	disposition, err := policy.ParseDisposition(request.Disposition)
	if err != nil {
		app.WriteFailure(w, http.StatusBadRequest,
			"that is not one of the choices", "pick one of the four options")
		return
	}
	// A declaration with nobody's name on it is not a declaration. The archive
	// enforces this too; saying it here means the person is told which field,
	// rather than being handed the store's own message.
	if request.DeclaredBy == "" {
		app.WriteFailure(w, http.StatusBadRequest,
			"a declaration has to say who is making it",
			"put your name in, the same one you record under")
		return
	}
	if request.Server == "" {
		app.WriteFailure(w, http.StatusBadRequest,
			"no server was chosen", "pick the server this applies to")
		return
	}

	declared := policy.ServerPolicy{
		Server:      model.NormalizeToken(request.Server),
		Disposition: disposition,
		DeclaredBy:  request.DeclaredBy,
		DeclaredAt:  time.Now().UTC(),
		Note:        request.Note,
	}
	if disposition == policy.Embargoed {
		if request.Until == "" {
			app.WriteFailure(w, http.StatusBadRequest,
				"holding something back needs a date to hold it until",
				"choose the date it stops being held back")
			return
		}
		until, err := time.Parse("2006-01-02", request.Until)
		if err != nil {
			app.WriteFailure(w, http.StatusBadRequest,
				"that date could not be read", "use the date picker")
			return
		}
		until = until.UTC()
		declared.EmbargoUntil = &until
	}

	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}
	if err := policy.NewStore(a.Root).Declare(declared); err != nil {
		app.WriteFailure(w, http.StatusBadRequest, err.Error(),
			"check the choice and the name, then try again")
		return
	}

	app.WriteJSON(w, http.StatusOK, map[string]any{
		"server":      declared.Server,
		"disposition": string(declared.Disposition),
		"declared_by": declared.DeclaredBy,
	})
}
