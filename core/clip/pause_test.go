package clip

import (
	"testing"
	"time"
)

func TestPauseStopsCaptureAndApply(t *testing.T) {
	f := newFixture(t)

	if err := f.pipe.Pause(t.Context(), time.Time{}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	paused, until, err := f.pipe.Paused(t.Context())
	if err != nil {
		t.Fatalf("read pause state: %v", err)
	}
	if !paused || !until.IsZero() {
		t.Fatalf("paused %t until %s, want an indefinite pause", paused, until)
	}

	f.emit(textSnapshot("paused", ""))
	f.expectNoItem()
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("paused content reached the history")
	}

	if err := f.pipe.Apply(t.Context(), testPeer, textItem("incoming", "")); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("clipboard was written while paused")
	}

	if err := f.pipe.Resume(t.Context()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	f.emit(textSnapshot("live", ""))
	if item := f.waitItem(); item.Seq != 1 {
		t.Fatalf("captured seq %d, want 1", item.Seq)
	}

	if err := f.pipe.Apply(t.Context(), testPeer, textItem("incoming", "")); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); !ok {
		t.Fatal("clipboard was not written after the resume")
	}
}

func TestPauseUntilExpires(t *testing.T) {
	f := newFixture(t)

	deadline := f.clock.Now().Add(time.Minute)
	if err := f.pipe.Pause(t.Context(), deadline); err != nil {
		t.Fatalf("pause: %v", err)
	}

	paused, until, err := f.pipe.Paused(t.Context())
	if err != nil {
		t.Fatalf("read pause state: %v", err)
	}
	if !paused || !until.Equal(deadline) {
		t.Fatalf("paused %t until %s, want %s", paused, until, deadline)
	}

	f.clock.Advance(30 * time.Second)
	if paused, _, err := f.pipe.Paused(t.Context()); err != nil || !paused {
		t.Fatalf("pause ended early: paused %t, error %v", paused, err)
	}

	f.clock.Advance(31 * time.Second)
	if paused, _, err := f.pipe.Paused(t.Context()); err != nil || paused {
		t.Fatalf("pause did not end: paused %t, error %v", paused, err)
	}

	f.emit(textSnapshot("live", ""))
	if item := f.waitItem(); item.Seq != 1 {
		t.Fatalf("captured seq %d, want 1", item.Seq)
	}
}

func TestPauseSurvivesRestart(t *testing.T) {
	f := newFixture(t)

	if err := f.pipe.Pause(t.Context(), time.Time{}); err != nil {
		t.Fatalf("pause: %v", err)
	}

	f.stop()
	f.start()

	paused, _, err := f.pipe.Paused(t.Context())
	if err != nil {
		t.Fatalf("read pause state: %v", err)
	}
	if !paused {
		t.Fatal("pause was lost")
	}

	if err := f.pipe.Resume(t.Context()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	paused, _, err = f.pipe.Paused(t.Context())
	if err != nil {
		t.Fatalf("read pause state: %v", err)
	}
	if paused {
		t.Fatal("resume did not clear the pause")
	}
}
