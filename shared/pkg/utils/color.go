package utils

import (
	"fmt"
	"strings"
)

type ColoredMessage struct {
	Message string
	Color   string
}

// ANSI escape codes for colors
var colorCodes = map[string]string{
	"black":   "\033[30m",
	"red":     "\033[31m",
	"green":   "\033[32m",
	"yellow":  "\033[33m",
	"blue":    "\033[34m",
	"magenta": "\033[35m",
	"cyan":    "\033[36m",
	"white":   "\033[37m",
	"reset":   "\033[0m",
}

func NewColoredMessage(message, color string) *ColoredMessage {
	return &ColoredMessage{
		Message: message,
		Color:   color,
	}
}

func (cm *ColoredMessage) Print() {
	colorCode, exists := colorCodes[cm.Color]
	if !exists {
		colorCode = colorCodes["reset"]
	}

	fmt.Printf("%s%s%s\n", colorCode, cm.Message, colorCodes["reset"])
}

// ColorizePnL returns a colored string based on the PnL value
func ColorizePnL(value float64, format string, args ...interface{}) string {
	message := fmt.Sprintf(format, args...)
	if value > 0 {
		return fmt.Sprintf("%s%s%s", colorCodes["green"], message, colorCodes["reset"])
	} else if value < 0 {
		return fmt.Sprintf("%s%s%s", colorCodes["red"], message, colorCodes["reset"])
	}
	return message
}

// ColorizeTabularData returns a colored tabular string with colored PnL values
func ColorizeTabularData(data string) string {
	lines := strings.Split(data, "\n")
	var coloredLines []string

	for i, line := range lines {
		if line == "" {
			continue
		}

		// Skip header line
		if i == 0 {
			coloredLines = append(coloredLines, line)
			continue
		}

		// Split the line into columns
		columns := strings.Split(line, "\t")
		if len(columns) < 7 { // Skip lines without enough columns
			coloredLines = append(coloredLines, line)
			continue
		}

		// Try to parse PnL values
		var coloredColumns []string
		for i, col := range columns {
			// Check if this column contains a PnL value
			if strings.Contains(col, "%") || (i >= 5 && i <= 7) {
				// Try to parse the numeric value
				var value float64
				// Remove any non-numeric characters except decimal point and minus sign
				cleanCol := strings.Map(func(r rune) rune {
					if (r >= '0' && r <= '9') || r == '.' || r == '-' {
						return r
					}
					return -1
				}, col)
				fmt.Sscanf(cleanCol, "%f", &value)
				coloredColumns = append(coloredColumns, ColorizePnL(value, "%s", col))
			} else {
				coloredColumns = append(coloredColumns, col)
			}
		}

		coloredLines = append(coloredLines, strings.Join(coloredColumns, "\t"))
	}

	return strings.Join(coloredLines, "\n")
}
