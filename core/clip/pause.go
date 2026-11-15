package clip

import (
	"context"
	"fmt"
	"time"
)

// Settings keys of the pause state.
const (
	keyPauseActive = "clip.pause.active"
	keyPauseUntil  = "clip.pause.until"
)

// Pause stops creating and applying items until until. A zero until pauses
// until Resume is called.
func (p *Pipeline) Pause(ctx context.Context, until time.Time) error {
	settings := p.db.Settings()

	millis := int64(0)
	if !until.IsZero() {
		millis = until.UnixMilli()
	}
	if err := settings.SetInt(ctx, keyPauseUntil, millis); err != nil {
		return fmt.Errorf("clip: store pause deadline: %w", err)
	}
	if err := settings.SetBool(ctx, keyPauseActive, true); err != nil {
		return fmt.Errorf("clip: store pause state: %w", err)
	}
	return nil
}

// Resume clears the pause.
func (p *Pipeline) Resume(ctx context.Context) error {
	if err := p.db.Settings().SetBool(ctx, keyPauseActive, false); err != nil {
		return fmt.Errorf("clip: store pause state: %w", err)
	}
	return nil
}

// Paused reports whether the pipeline is paused and until when. The time is
// zero for an indefinite pause and when the pipeline is not paused. A pause
// with a deadline ends on its own.
func (p *Pipeline) Paused(ctx context.Context) (bool, time.Time, error) {
	settings := p.db.Settings()

	active, err := settings.GetBool(ctx, keyPauseActive, false)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("clip: read pause state: %w", err)
	}
	if !active {
		return false, time.Time{}, nil
	}

	millis, err := settings.GetInt(ctx, keyPauseUntil, 0)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("clip: read pause deadline: %w", err)
	}
	if millis == 0 {
		return true, time.Time{}, nil
	}

	until := time.UnixMilli(millis)
	if !p.clock.Now().Before(until) {
		return false, time.Time{}, nil
	}
	return true, until, nil
}

// pausedAt reports whether the pipeline is paused at the current time.
func (p *Pipeline) pausedAt(ctx context.Context) (bool, error) {
	paused, _, err := p.Paused(ctx)
	return paused, err
}
