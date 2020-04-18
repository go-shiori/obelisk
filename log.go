package obelisk

import (
	"github.com/sirupsen/logrus"
)

func (arc *Archiver) logURL(url, parentURL string, isCached bool) {
	if !arc.EnableLog {
		return
	}

	fields := logrus.Fields{}
	if isCached {
		fields["cached"] = true
	}
	if arc.EnableVerboseLog {
		fields["parent"] = parentURL
	}

	logrus.WithFields(fields).Println(url)
}
