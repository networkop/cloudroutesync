package main

import (
	"flag"
	"github.com/networkop/vcr/pkg/monitor"
	"github.com/networkop/vcr/pkg/reconciler"
	"github.com/networkop/vcr/pkg/route"
	"github.com/sirupsen/logrus"
)

var (
	cloud     = flag.String("cloud", "", "public cloud providers [azure|aws|gcp]")
	supportedClouds = struct {
		azure string
	}{
		azure: "azure",
	}
)

var 

func main() {
	logrus.Info("Starting Virtual Cloud Router")

	flag.Parse()

	var cloud reconciler.CloudClient

	switch cloud {
	case supportedClouds.azure:
		logrus.Info("Running on Azure")
		cloud = reconciler.NewAzureClient()
	default:
		return fmt.Errorf("Unsupported cloud provider: %v", cloud)
	}

	syncCh := make(chan bool, 1)

	errCh := make(chan error)

	rt := route.New()

	go monitor.Start(rt, syncCh)

	go cloud.Reconcile(rt, syncCh)

	select {}
}
