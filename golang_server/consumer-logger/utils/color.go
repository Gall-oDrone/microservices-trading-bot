package utils

import (
	"fmt"
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
