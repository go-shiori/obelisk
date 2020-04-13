package obelisk

import "github.com/sirupsen/logrus"

func (arc *archiver) log(args ...interface{}) {
	if arc.config.EnableLog {
		logrus.Println(args...)
	}
}

func (arc *archiver) logf(format string, args ...interface{}) {
	if arc.config.EnableLog {
		logrus.Printf(format, args...)
	}
}
