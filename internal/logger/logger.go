package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
var debugEnabled bool

// SetDebugMode 设置调试模式
func SetDebugMode(enabled bool) {
	debugEnabled = enabled
}

func formatTimestamp() string {
	return time.Now().Format("15:04:05.000")
}

func Info(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorBlue, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Success(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorGreen, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Warning(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorYellow, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func Error(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorRed, category, ColorReset)
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

func Debug(category string, message string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorPurple, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

// DebugMultiline 显示多行调试信息，添加缩进提高可读性
func DebugMultiline(category string, title string, content string) {
	if !debugEnabled {
		return
	}
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%5s%s", ColorPurple, category, ColorReset)
	
	// 标题行
	log.Printf("%s [%s] %s:", timestamp, categoryFormatted, title)
	
	// 内容行，每行添加2空格缩进
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" { // 跳过空行
			log.Printf("  %s", line)
		}
	}
}

// FormatJSON 格式化JSON数据，添加缩进
func FormatJSON(jsonBytes []byte) string {
	var buf bytes.Buffer
	err := json.Indent(&buf, jsonBytes, "", "  ")
	if err != nil {
		// 如果格式化失败，返回原始字符串
		return string(jsonBytes)
	}
	return buf.String()
}

// DebugJSON 显示格式化的JSON调试信息
func DebugJSON(category string, title string, jsonBytes []byte) {
	if !debugEnabled {
		return
	}
	formattedJSON := FormatJSON(jsonBytes)
	DebugMultiline(category, fmt.Sprintf("%s (%d bytes)", title, len(jsonBytes)), formattedJSON)
}
