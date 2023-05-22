package log

import (
	"log"
	"os"
)

func New(level Level) Interface {
	var debug *log.Logger
	if level <= Debug {
		debug = log.New(os.Stderr, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		debug = nil
	}

	var info *log.Logger
	if level <= Info {
		info = log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		info = nil
	}

	var warning *log.Logger
	if level <= Warning {
		warning = log.New(os.Stderr, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		warning = nil
	}

	var error *log.Logger
	if level <= Error {
		error = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		error = nil
	}

	return &logger{debug: debug, info: info, warning: warning, error: error, level: level}
}

type Level int8

const (
	Debug Level = iota
	Info
	Warning
	Error
	Silent
)

type logger struct {
	debug   *log.Logger
	info    *log.Logger
	warning *log.Logger
	error   *log.Logger
	level   Level
}

func (logger *logger) Debug(format string, v ...any) {
	if logger.debug != nil {
		logger.debug.Printf(format, v...)
	}
}

func (logger *logger) Info(format string, v ...any) {
	if logger.info != nil {
		logger.info.Printf(format, v...)
	}
}

func (logger *logger) Warning(format string, v ...any) {
	if logger.warning != nil {
		logger.warning.Printf(format, v...)
	}
}

func (logger *logger) Error(format string, v ...any) {
	if logger.error != nil {
		logger.error.Printf(format, v...)
	}
}

func (logger *logger) GetLevel() Level {
	return logger.level
}

type Interface interface {
	GetLevel() Level
	Debug(string, ...any)
	Info(string, ...any)
	Warning(string, ...any)
	Error(string, ...any)
}
