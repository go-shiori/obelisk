package obelisk

import (
	"github.com/sirupsen/logrus"
)

func (arc *archiver) logURL(url, parentURL string, isCached bool) {
	if !arc.config.EnableLog {
		return
	}

	fields := logrus.Fields{"cached": isCached}
	if arc.config.EnableVerboseLog {
		fields["parent"] = parentURL
	}

	logrus.WithFields(fields).Println(url)
}
