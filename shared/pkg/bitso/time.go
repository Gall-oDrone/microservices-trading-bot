package bitso

import (
	"encoding/json"
	"fmt"
	"time"
)

// Time represents a ISO8601 encoded time value.
type Time time.Time

const iso8601Time = "2006-01-02T15:04:05-0700"

var timeFormats = []string{
	iso8601Time,
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05.000-07:00",
}

func (t *Time) Time() time.Time {
	return time.Time(*t)
}

// UnmarshalJSON implements json.Unmarshal
func (t *Time) UnmarshalJSON(in []byte) error {
	// First try to unmarshal as a string
	var s string
	if err := json.Unmarshal(in, &s); err == nil {
		var err error
		var z time.Time
		for _, timeFormat := range timeFormats {
			z, err = time.Parse(timeFormat, s)
			if err == nil {
				break
			}
		}
		if err != nil {
			return err
		}
		*t = Time(z)
		return nil
	}

	// If string unmarshal fails, try to unmarshal as an object with a timestamp
	var obj struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.Unmarshal(in, &obj); err != nil {
		return err
	}
	*t = Time(time.Unix(obj.Timestamp, 0))
	return nil
}

// String implements fmt.Stringer
func (t Time) String() string {
	return time.Time(t).Format(iso8601Time)
}

// MarshalJSON implements json.Marshaler
func (t Time) MarshalJSON() ([]byte, error) {
	// Always marshal as a string in ISO8601 format
	return json.Marshal(time.Time(t).Format(iso8601Time))
}

// Equal compares two Time values for equality
func (t Time) Equal(other Time) bool {
	t1 := time.Time(t)
	t2 := time.Time(other)

	// Debug: Print both times in different formats
	fmt.Printf("Comparing times:\n")
	fmt.Printf("t1: %v (Unix: %d)\n", t1, t1.Unix())
	fmt.Printf("t2: %v (Unix: %d)\n", t2, t2.Unix())

	// Compare using Unix timestamps to avoid timezone issues
	return t1.Unix() == t2.Unix()
}
