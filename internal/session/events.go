package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/observability"
	"golang.org/x/sys/unix"
)

// ReplayObservations returns only retained metadata. A gap explicitly tells a
// reconnecting consumer that its cursor predates retained history.
func ReplayObservations(value Session, after uint64) observability.Page {
	normalizeObservationHistory(&value)
	page := observability.Page{SchemaVersion: 1, SessionID: value.SessionID, LatestSequence: value.ObservationSequence, Events: []observability.Event{}}
	if len(value.Observability) > 0 {
		page.OldestSequence = value.Observability[0].Sequence
	}
	page.Gap = after > page.LatestSequence || (page.OldestSequence > 0 && after < page.OldestSequence-1)
	for _, event := range value.Observability {
		if event.Sequence > after || page.Gap {
			page.Events = append(page.Events, event)
		}
	}
	return page
}

func normalizeObservationHistory(value *Session) {
	// Copy before migrating legacy events so read-only projections cannot mutate
	// another goroutine's snapshot backing array.
	value.Observability = append([]observability.Event(nil), value.Observability...)
	var last uint64
	for i := range value.Observability {
		event := &value.Observability[i]
		if event.Sequence == 0 {
			event.Sequence = last + 1
		}
		if event.SchemaVersion == 0 {
			event.SchemaVersion = 1
		}
		last = event.Sequence
	}
	if value.ObservationSequence < last {
		value.ObservationSequence = last
	}
}

// Watch coalesces filesystem changes into a one-slot notification channel.
// It watches atomic replacement, including writes from worker processes; it
// never polls session files. The caller reads the current bounded snapshot.
func (s Store) Watch(ctx context.Context, id string) (<-chan struct{}, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	add := func(path string) error {
		_, err := unix.InotifyAddWatch(fd, path, unix.IN_MOVED_TO|unix.IN_CLOSE_WRITE|unix.IN_CREATE|unix.IN_DELETE|unix.IN_DELETE_SELF)
		return err
	}
	if err = add(s.Root); err != nil {
		unix.Close(fd)
		return nil, err
	}
	for _, dir := range []string{s.nativeDir(), s.frontendDir()} {
		if err = add(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			unix.Close(fd)
			return nil, err
		}
	}
	changes := make(chan struct{}, 1)
	go func() {
		defer close(changes)
		defer unix.Close(fd)
		buffer := make([]byte, 64<<10)
		for ctx.Err() == nil {
			ready, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 200)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return
			}
			if ready == 0 {
				continue
			}
			_, err = unix.Read(fd, buffer)
			if errors.Is(err, unix.EAGAIN) {
				continue
			}
			if err != nil {
				return
			}
			// Namespace creation/migration may occur while the subscription is alive.
			for _, dir := range []string{s.nativeDir(), s.frontendDir()} {
				if filepath.Dir(dir) == s.Root {
					_ = add(dir)
				}
			}
			select {
			case changes <- struct{}{}:
			default:
			}
		}
	}()
	return changes, nil
}
