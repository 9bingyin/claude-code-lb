package logger

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// 颜色常量
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorGray   = "\033[37m"
	ColorBold   = "\033[1m"
)

var logMutex sync.Mutex

func formatTimestamp() string {
	return time.Now().Format("15:04:05.000")
}

func Info(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorBlue, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Success(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorGreen, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Warning(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorYellow, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Error(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorRed, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Auth(success bool, message string, args ...interface{}) {
	if success {
		Success("AUTH", message, args...)
	} else {
		Error("AUTH", message, args...)
	}
}