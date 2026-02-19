package monitor

import (
	"fmt"
	"sync"
	"time"
)

// SlotEntry represents a single slot observation with timestamp.
type SlotEntry struct {
	Slot      uint64
	Timestamp time.Time
}

// StuckInfo contains details about a stuck slot condition.
type StuckInfo struct {
	StuckSlot        uint64
	FirstObservedAt  time.Time
	LastObservedAt   time.Time
	Duration         time.Duration
	ObservationCount int
}

// SlotHistory tracks recent slot observations and detects stuck conditions.
type SlotHistory struct {
	mu             sync.Mutex
	entries        []SlotEntry
	maxEntries     int
	stuckThreshold time.Duration
}

// NewSlotHistory creates a new slot history tracker.
func NewSlotHistory(stuckThreshold time.Duration) *SlotHistory {
	return &SlotHistory{
		entries:        make([]SlotEntry, 0, 20),
		maxEntries:     20,
		stuckThreshold: stuckThreshold,
	}
}

// Record adds a new slot observation to the history.
func (h *SlotHistory) Record(slot uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := SlotEntry{
		Slot:      slot,
		Timestamp: time.Now(),
	}

	h.entries = append(h.entries, entry)

	// Trim to max entries (circular buffer behavior)
	if len(h.entries) > h.maxEntries {
		h.entries = h.entries[len(h.entries)-h.maxEntries:]
	}
}

// IsStuck checks if the slot has been stuck beyond the threshold.
// Returns true and stuck info if stuck, false and nil otherwise.
func (h *SlotHistory) IsStuck() (bool, *StuckInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Need at least 2 observations to detect stuck condition
	if len(h.entries) < 2 {
		return false, nil
	}

	// Check if the most recent slot matches the previous slot
	lastEntry := h.entries[len(h.entries)-1]

	// Find the first occurrence of this slot going backwards
	var firstOccurrence *SlotEntry
	var count int
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].Slot == lastEntry.Slot {
			firstOccurrence = &h.entries[i]
			count++
		} else {
			break
		}
	}

	// If we only saw this slot once, not stuck
	if firstOccurrence == nil || count < 2 {
		return false, nil
	}

	// Check if stuck duration exceeds threshold
	duration := lastEntry.Timestamp.Sub(firstOccurrence.Timestamp)
	if duration < h.stuckThreshold {
		return false, nil
	}

	return true, &StuckInfo{
		StuckSlot:        lastEntry.Slot,
		FirstObservedAt:  firstOccurrence.Timestamp,
		LastObservedAt:   lastEntry.Timestamp,
		Duration:         duration,
		ObservationCount: count,
	}
}

// GetLatestSlot returns the most recent slot observation, or 0 if no observations.
func (h *SlotHistory) GetLatestSlot() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.entries) == 0 {
		return 0
	}
	return h.entries[len(h.entries)-1].Slot
}

// HasProgressed returns true if the slot has changed since the last observation.
func (h *SlotHistory) HasProgressed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.entries) < 2 {
		return false
	}

	return h.entries[len(h.entries)-1].Slot != h.entries[len(h.entries)-2].Slot
}

// String returns a human-readable representation of the stuck info.
func (s *StuckInfo) String() string {
	return fmt.Sprintf("Stuck on slot %d for %s (observed %d times, first at %s)",
		s.StuckSlot,
		s.Duration.Round(time.Second),
		s.ObservationCount,
		s.FirstObservedAt.Format("15:04:05"))
}
