package obelisk

import (
	"github.com/sirupsen/logrus"
)

func (arc *archiver) log(args ...interface{}) {
	if arc.config.EnableLog {
		logrus.Println(args...)
	}
}

func (arc *archiver) logURL(url, parentURL string, isCached bool) {
	if !arc.config.EnableLog {
		return
	}

	fields := logrus.Fields{}
	if arc.config.LogParentURL {
		fields["parent"] = parentURL
	}

	if isCached {
		fields["cached"] = true
	}

	logrus.WithFields(fields).Println(url)
}
