package main

import (
	"github.com/networkop/cloudroutesync/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.Info("Starting Virtual Cloud Router")

	logrus.Fatal(cmd.Run())
}
