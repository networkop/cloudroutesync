package main

import (
	"flag"

	"github.com/networkop/cloudrouter/pkg/monitor"
	"github.com/networkop/cloudrouter/pkg/reconciler"
	"github.com/networkop/cloudrouter/pkg/route"
	"github.com/sirupsen/logrus"
)

var (
	cloud           = flag.String("cloud", "", "public cloud providers [azure|aws|gcp]")
	supportedClouds = struct {
		azure string
	}{
		azure: "azure",
	}
)

func main() {
	logrus.Info("Starting Virtual Cloud Router")

	flag.Parse()

	var client reconciler.CloudClient

	switch *cloud {
	case supportedClouds.azure:
		logrus.Info("Running on Azure")
		client = reconciler.NewAzureClient()
	default:
		logrus.Errorf("Unsupported cloud provider: %v", cloud)
		return
	}

	syncCh := make(chan bool, 1)

	rt := route.New()

	go monitor.Start(rt, syncCh)

	go client.Reconcile(rt, syncCh)

	select {}
}
