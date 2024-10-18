package obelisk

import (
	"bytes"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestArchiver_LogURL(t *testing.T) {
	arc := &Archiver{
		EnableLog:        true,
		EnableVerboseLog: true,
	}

	url := "https://example.com"
	parentURL := "https://parent.com"
	isCached := true

	// Capture log output
	var logOutput bytes.Buffer
	logrus.SetOutput(&logOutput)

	arc.logURL(url, parentURL, isCached)
	assert.Contains(t, logOutput.String(), url)

	// clear log output
	logrus.SetOutput(os.Stdout)

	arc = &Archiver{
		EnableLog:        false,
		EnableVerboseLog: false,
	}
	arc.logURL(url, parentURL, isCached)
	assert.Contains(t, logOutput.String(), url)

}

func TestArchiver_LogURLdisable(t *testing.T) {
	arc := &Archiver{
		EnableLog:        false,
		EnableVerboseLog: false,
	}

	url := "https://example.com"
	parentURL := "https://parent.com"
	isCached := true

	// Capture log output
	var logOutput bytes.Buffer
	logrus.SetOutput(&logOutput)

	arc.logURL(url, parentURL, isCached)
	assert.NotContains(t, logOutput.String(), url)
}
