package main

import (
	"os"

	"github.com/networkop/cloudroutesync/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.Info("Starting Virtual Cloud Router")

	if err := cmd.Run(); err != nil {
		logrus.Info(err)
		os.Exit(1)
	}

}
