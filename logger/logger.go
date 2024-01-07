package logger

import (
	"fmt"
	"log"
	"reflect"
)

type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

var logLevel = InfoLevel

func SetLogLevel(level LogLevel) {
	logLevel = level
}
func Debug(args ...interface{}) {
	if logLevel <= DebugLevel {
		logOutput("DEBUG", args...)
	}
}

func Info(args ...interface{}) {
	if logLevel <= InfoLevel {
		logOutput("INFO", args...)
	}
}

func Warn(args ...interface{}) {
	if logLevel <= WarnLevel {
		logOutput("WARN", args...)
	}
}

func Error(args ...interface{}) {
	if logLevel <= ErrorLevel {
		logOutput("ERROR", args...)
	}
}

func logOutput(level string, args ...interface{}) {
	logMsg := fmt.Sprintf("[%s]: ", level)

	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			logMsg += v + " "
		case error:
			logMsg += v.Error() + " "
		default:
			val := reflect.ValueOf(v)
			kind := val.Kind()

			if kind == reflect.Array || kind == reflect.Slice {
				for i := 0; i < val.Len(); i++ {
					logMsg += fmt.Sprintf("%v ", val.Index(i).Interface())
				}
			} else {
				logMsg += fmt.Sprintf("%v ", v)
			}
		}
	}

	log.Println(logMsg)
}
