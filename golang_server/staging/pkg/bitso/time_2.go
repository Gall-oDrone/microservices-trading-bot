package bitso

import (
	"encoding/json"
	"time"
)

// CustomTime is a custom time type that handles JSON unmarshaling
type CustomTime struct {
	time.Time
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (t *CustomTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsedTime, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	t.Time = parsedTime
	return nil
}

// MarshalJSON implements the json.Marshaler interface
func (t CustomTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format(time.RFC3339))
}

// String implements fmt.Stringer
func (t CustomTime) String() string {
	return t.Time.Format(time.RFC3339)
}

// ToBitsoTime converts CustomTime to bitso.Time
func (t CustomTime) ToBitsoTime() Time {
	return Time(t.Time)
}

// FromBitsoTime creates a CustomTime from bitso.Time
func FromBitsoTime(t Time) CustomTime {
	return CustomTime{Time: time.Time(t)}
}
