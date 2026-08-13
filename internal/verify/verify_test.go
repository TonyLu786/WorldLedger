package verify

import (
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func obs(at time.Time, contributor, digest string) model.Observation {
	return model.Observation{ObservedAt: at, Source: model.Source{Contributor: contributor}, StateDigest: digest}
}

func TestCorroboratedWindow(t *testing.T) {
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	windows := BuildWindows([]model.Observation{
		obs(t0, "alice", "state-a"),
		obs(t0.Add(2*time.Second), "bob", "state-a"),
	}, 10*time.Second)
	if len(windows) != 1 || windows[0].Status != "corroborated" {
		t.Fatalf("expected corroborated, got %#v", windows)
	}
}

func TestConflictWindow(t *testing.T) {
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	windows := BuildWindows([]model.Observation{
		obs(t0, "alice", "state-a"),
		obs(t0.Add(time.Second), "bob", "state-b"),
	}, 10*time.Second)
	if len(windows) != 1 || windows[0].Status != "conflict" {
		t.Fatalf("expected conflict, got %#v", windows)
	}
}
