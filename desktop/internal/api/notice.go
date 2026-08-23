package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/home"
)

// What the application says for itself before it does anything.
//
// Every other surface of this project carries this. NOTICE has it in full, the
// README points at it, the site has a paragraph of it. The window -- which is
// now how most people will use any of it, and the only one somebody reaches by
// double-clicking a file -- had none of it at all, which is the wrong way round:
// a person reading the repository has already chosen to read.
//
// It is deliberately short and deliberately calm. The full terms are in NOTICE
// and in the licence and govern regardless; what belongs here is the part a
// person needs in order to decide, in the words they would use. Three things
// are worth their attention -- what is kept, what is not, and that the decision
// about any of it is theirs -- and burying those in a page of legal text is the
// same as not saying them.

// noticeParagraph is one block of the notice, with a heading the page can show
// apart from the body.
type noticeParagraph struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

type noticeAnswer struct {
	// Accepted is whether somebody has already read this on this machine. The
	// notice is shown once rather than every launch: a message that appears
	// every time is one people learn to dismiss without reading, which is worse
	// than not showing it.
	Accepted   bool              `json:"accepted"`
	AcceptedAt string            `json:"accepted_at,omitempty"`
	Paragraphs []noticeParagraph `json:"paragraphs"`
}

var noticeText = []noticeParagraph{
	{
		Heading: "What this keeps",
		Body: "While you play on a multiplayer server, this keeps a copy of the world your own " +
			"Minecraft client is being shown: the blocks, biomes and signs it draws for you, as it " +
			"draws them. It is kept on this computer, in a folder this application names on every " +
			"screen. Nothing is sent anywhere.",
	},
	{
		Heading: "What it does not do",
		Body: "It does not change the server, the world, or your game. It does not read anything " +
			"your client was not sent, so places you never went and chests you never opened are " +
			"simply absent rather than guessed at. It has no server side and needs no permission " +
			"from anyone to run.",
	},
	{
		Heading: "The part that is yours",
		Body: "What you record is a record of where you went and when, and a server's players and " +
			"operators may not know it exists. Nothing here can be turned into a world or shared " +
			"until a named person has said what may happen to it, and that step is not done for " +
			"you. Being able to do something is not the same as being allowed to.",
	},
	{
		Heading: "Responsibility",
		Body: "This is a general-purpose tool, provided as it is, with no warranty of any kind. " +
			"Responsibility for what you do with it rests with you, including keeping to the rules " +
			"of any server, service or community you take part in, and to the law where you are. " +
			"Nothing this application says or produces is permission, and nothing in it is legal " +
			"advice. The full terms are in the NOTICE and LICENSE files that ship beside it.",
	},
}

// NoticeText is what the application says for itself, exported so that what it
// must not stop saying can be asserted from outside this package.
func NoticeText() []noticeParagraph { return noticeText }

// noticePath is where acceptance is recorded. It sits with the archive rather
// than in the browser's storage: a window's storage can be cleared by things
// that have nothing to do with this application, and asking somebody the same
// question again because a cache was emptied reads as an application that does
// not remember its own conversations.
func noticePath(dir string) string { return filepath.Join(dir, "notice-accepted.json") }

type noticeRecord struct {
	AcceptedAt string `json:"accepted_at"`
}

func handleNotice(w http.ResponseWriter, r *http.Request) {
	dir, err := home.Dir()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"there is nowhere to keep the application's own files")
		return
	}

	if r.Method == http.MethodPost {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			app.WriteFailure(w, http.StatusInternalServerError,
				"the application's folder could not be created: "+err.Error(),
				"check that there is room on the disk")
			return
		}
		record, err := json.Marshal(noticeRecord{AcceptedAt: time.Now().UTC().Format(time.RFC3339)})
		if err != nil {
			app.WriteFailure(w, http.StatusInternalServerError, err.Error(), "try again")
			return
		}
		if err := os.WriteFile(noticePath(dir), record, 0o644); err != nil {
			app.WriteFailure(w, http.StatusInternalServerError,
				"that could not be written down: "+err.Error(),
				"check that there is room on the disk")
			return
		}
		app.WriteJSON(w, http.StatusOK, noticeAnswer{Accepted: true, Paragraphs: noticeText})
		return
	}

	answer := noticeAnswer{Paragraphs: noticeText}
	if raw, err := os.ReadFile(noticePath(dir)); err == nil {
		var record noticeRecord
		// A record that cannot be parsed is treated as no record. Showing the
		// notice again costs somebody one click; skipping it on the strength of
		// a file we could not read costs the thing the notice is for.
		if json.Unmarshal(raw, &record) == nil && record.AcceptedAt != "" {
			answer.Accepted = true
			answer.AcceptedAt = record.AcceptedAt
		}
	}
	app.WriteJSON(w, http.StatusOK, answer)
}
