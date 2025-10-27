package processor

import (
	"time"
)

// BookFilter filters events by book
type BookFilter struct {
	name  string
	books map[string]bool
}

// NewBookFilter creates a new book filter
func NewBookFilter(name string, books []string) *BookFilter {
	bookMap := make(map[string]bool)
	for _, book := range books {
		bookMap[book] = true
	}

	return &BookFilter{
		name:  name,
		books: bookMap,
	}
}

// ShouldProcess returns true if the event's book is in the allowed list
func (f *BookFilter) ShouldProcess(event *ProcessedEvent) bool {
	return f.books[event.Book]
}

// GetName returns the filter name
func (f *BookFilter) GetName() string {
	return f.name
}

// AddBook adds a book to the filter
func (f *BookFilter) AddBook(book string) {
	f.books[book] = true
}

// RemoveBook removes a book from the filter
func (f *BookFilter) RemoveBook(book string) {
	delete(f.books, book)
}

// TypeFilter filters events by type
type TypeFilter struct {
	name  string
	types map[EventType]bool
}

// NewTypeFilter creates a new type filter
func NewTypeFilter(name string, types []EventType) *TypeFilter {
	typeMap := make(map[EventType]bool)
	for _, eventType := range types {
		typeMap[eventType] = true
	}

	return &TypeFilter{
		name:  name,
		types: typeMap,
	}
}

// ShouldProcess returns true if the event's type is in the allowed list
func (f *TypeFilter) ShouldProcess(event *ProcessedEvent) bool {
	return f.types[event.Type]
}

// GetName returns the filter name
func (f *TypeFilter) GetName() string {
	return f.name
}

// AddType adds a type to the filter
func (f *TypeFilter) AddType(eventType EventType) {
	f.types[eventType] = true
}

// RemoveType removes a type from the filter
func (f *TypeFilter) RemoveType(eventType EventType) {
	delete(f.types, eventType)
}

// TimeFilter filters events by time range
type TimeFilter struct {
	name      string
	startTime time.Time
	endTime   time.Time
}

// NewTimeFilter creates a new time filter
func NewTimeFilter(name string, startTime, endTime time.Time) *TimeFilter {
	return &TimeFilter{
		name:      name,
		startTime: startTime,
		endTime:   endTime,
	}
}

// ShouldProcess returns true if the event's timestamp is within the time range
func (f *TimeFilter) ShouldProcess(event *ProcessedEvent) bool {
	return event.Timestamp.After(f.startTime) && event.Timestamp.Before(f.endTime)
}

// GetName returns the filter name
func (f *TimeFilter) GetName() string {
	return f.name
}

// SetTimeRange updates the time range
func (f *TimeFilter) SetTimeRange(startTime, endTime time.Time) {
	f.startTime = startTime
	f.endTime = endTime
}

// RateLimitFilter filters events based on rate limiting
type RateLimitFilter struct {
	name        string
	maxEvents   int
	timeWindow  time.Duration
	eventCounts map[string]int
	lastReset   time.Time
}

// NewRateLimitFilter creates a new rate limit filter
func NewRateLimitFilter(name string, maxEvents int, timeWindow time.Duration) *RateLimitFilter {
	return &RateLimitFilter{
		name:        name,
		maxEvents:   maxEvents,
		timeWindow:  timeWindow,
		eventCounts: make(map[string]int),
		lastReset:   time.Now(),
	}
}

// ShouldProcess returns true if the event is within rate limits
func (f *RateLimitFilter) ShouldProcess(event *ProcessedEvent) bool {
	now := time.Now()

	// Reset counters if time window has passed
	if now.Sub(f.lastReset) >= f.timeWindow {
		f.eventCounts = make(map[string]int)
		f.lastReset = now
	}

	// Check rate limit for this book
	if f.eventCounts[event.Book] >= f.maxEvents {
		return false
	}

	// Increment counter
	f.eventCounts[event.Book]++
	return true
}

// GetName returns the filter name
func (f *RateLimitFilter) GetName() string {
	return f.name
}

// SetRateLimit updates the rate limit
func (f *RateLimitFilter) SetRateLimit(maxEvents int, timeWindow time.Duration) {
	f.maxEvents = maxEvents
	f.timeWindow = timeWindow
}

// CompositeFilter combines multiple filters with AND logic
type CompositeFilter struct {
	name    string
	filters []EventFilter
}

// NewCompositeFilter creates a new composite filter
func NewCompositeFilter(name string, filters ...EventFilter) *CompositeFilter {
	return &CompositeFilter{
		name:    name,
		filters: filters,
	}
}

// ShouldProcess returns true if all filters allow the event
func (f *CompositeFilter) ShouldProcess(event *ProcessedEvent) bool {
	for _, filter := range f.filters {
		if !filter.ShouldProcess(event) {
			return false
		}
	}
	return true
}

// GetName returns the filter name
func (f *CompositeFilter) GetName() string {
	return f.name
}

// AddFilter adds a filter to the composite filter
func (f *CompositeFilter) AddFilter(filter EventFilter) {
	f.filters = append(f.filters, filter)
}

// RemoveFilter removes a filter from the composite filter
func (f *CompositeFilter) RemoveFilter(name string) {
	for i, filter := range f.filters {
		if filter.GetName() == name {
			f.filters = append(f.filters[:i], f.filters[i+1:]...)
			break
		}
	}
}

// VolumeFilter filters events based on volume thresholds
type VolumeFilter struct {
	name        string
	minVolume   float64
	maxVolume   float64
	volumeField string // "amount", "value", etc.
}

// NewVolumeFilter creates a new volume filter
func NewVolumeFilter(name string, minVolume, maxVolume float64, volumeField string) *VolumeFilter {
	return &VolumeFilter{
		name:        name,
		minVolume:   minVolume,
		maxVolume:   maxVolume,
		volumeField: volumeField,
	}
}

// ShouldProcess returns true if the event's volume is within the threshold
func (f *VolumeFilter) ShouldProcess(event *ProcessedEvent) bool {
	// Extract volume from metadata
	if volume, ok := event.Metadata[f.volumeField].(float64); ok {
		return volume >= f.minVolume && volume <= f.maxVolume
	}

	// If volume field not found, allow the event
	return true
}

// GetName returns the filter name
func (f *VolumeFilter) GetName() string {
	return f.name
}

// SetVolumeThreshold updates the volume threshold
func (f *VolumeFilter) SetVolumeThreshold(minVolume, maxVolume float64) {
	f.minVolume = minVolume
	f.maxVolume = maxVolume
}
