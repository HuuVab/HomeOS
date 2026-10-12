package syncclient

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStateSurvivesRestartAndTracksRetries(t *testing.T) {
	root := t.TempDir()
	state, err := OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveBaseline(Baseline{Path: "notes.txt", Kind: "file", RemoteVersion: 4, LocalHash: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := state.Queue(Pending{Path: "notes.txt", Operation: "update", BaseVersion: 4, LocalHash: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := state.Retry("notes.txt", errors.New("offline")); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkConflict("notes.txt", "remote changed"); err != nil {
		t.Fatal(err)
	}
	if err := state.SetMeta("cursor", "12"); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	state, err = OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	baselines, err := state.Baselines()
	if err != nil || baselines["notes.txt"].RemoteVersion != 4 {
		t.Fatalf("baselines=%+v err=%v", baselines, err)
	}
	pending, err := state.Pending()
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError != "offline" || pending[0].NextAttempt < time.Now().Unix() {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	conflicts, err := state.Conflicts()
	if err != nil || len(conflicts) != 1 || conflicts[0].Reason != "remote changed" {
		t.Fatalf("conflicts=%+v err=%v", conflicts, err)
	}
	cursor, err := state.Meta("cursor")
	if err != nil || cursor != "12" {
		t.Fatalf("cursor=%q err=%v", cursor, err)
	}
	if err := state.SaveBaseline(Baseline{Path: "notes.txt", Kind: "file", RemoteVersion: 5, LocalHash: "new"}); err != nil {
		t.Fatal(err)
	}
	pending, err = state.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after success=%+v err=%v", pending, err)
	}
	conflicts, err = state.Conflicts()
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("conflicts after success=%+v err=%v", conflicts, err)
	}
	if _, err := OpenState(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected missing sync root to be rejected")
	}
}
