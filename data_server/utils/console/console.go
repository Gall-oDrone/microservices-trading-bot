package console

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"
)

func messageColor(action string) string {
	switch action {
	case "red":
		return "\033[31m" //colorRed
	case "green":
		return "\033[32m" //colorGreen
	case "yellow":
		return "\033[33m" //colorYellow
	case "blue":
		return "\033[34m" //colorBlue
	case "purple":
		return "\033[35m" //colorPurple
	case "cyan":
		return "\033[36m" //colorCyan
	case "white":
		return "\033[37m" //colorWhite
	default:
		return "\033[0m" //colorReset
	}
}

func SetDefaultColor() {
	fmt.Printf(string(messageColor("")), "")
}
func Pretty(data interface{}, color string) {
	b, err := json.MarshalIndent(data, "", "")
	msg_color := messageColor(color)
	if err != nil {
		msg_color = messageColor("red")
		log.Println(string(msg_color), err)
		return
	}
	fmt.Println(string(msg_color), string(b))
	SetDefaultColor()
}

func PrettyController(controller, request, color string) {
	json_format := map[string]interface{}{
		"Controller": controller,
		"Request":    request,
		"Time":       time.Now(),
	}
	Pretty(json_format, color)
}

func FancyHandleError(err error) (b bool) {
	if err != nil {
		// notice that we're using 1, so it will actually log the where
		// the error happened, 0 = this function, we don't want that.
		pc, fn, line, _ := runtime.Caller(1)
		msg_color := messageColor("red")
		log.Printf(string(msg_color), "[error] in %s[%s:%d] %v", runtime.FuncForPC(pc).Name(), fn, line, err)
		SetDefaultColor()
		b = true
	}
	return
}
