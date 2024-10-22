package utils

import (
	"bytes"
	"os"
	"testing"
)

// TestPrint tests the Print method of the ColoredMessage struct
func TestPrint(t *testing.T) {
	tests := []struct {
		message        string
		color          string
		expectedOutput string
	}{
		{"Hello, World!", "green", "\033[32mHello, World!\033[0m\n"},
		{"This is an error message.", "red", "\033[31mThis is an error message.\033[0m\n"},
		{"Warning!", "yellow", "\033[33mWarning!\033[0m\n"},
		{"Unsupported color", "unsupported", "Unsupported color\n"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			var buf bytes.Buffer
			// Save the original stdout
			originalStdout := os.Stdout
			// Redirect stdout to the buffer
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("Failed to create pipe: %v", err)
			}
			os.Stdout = w

			cm := NewColoredMessage(tt.message, tt.color)
			cm.Print()

			// Close the writer and restore stdout
			w.Close()
			os.Stdout = originalStdout

			// Read the output from the reader
			_, err = buf.ReadFrom(r)
			if err != nil {
				t.Fatalf("Failed to read from buffer: %v", err)
			}

			if buf.String() != tt.expectedOutput {
				t.Errorf("Print() = %q, want %q", buf.String(), tt.expectedOutput)
			}
		})
	}
}
