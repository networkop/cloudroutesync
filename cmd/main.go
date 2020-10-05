package cmd

import (
	"flag"
	"fmt"

	"github.com/networkop/cloudroutersync/pkg/monitor"
	"github.com/networkop/cloudroutersync/pkg/reconciler"
	"github.com/networkop/cloudroutersync/pkg/route"
	"github.com/sirupsen/logrus"
)

var (
	cloud           = flag.String("cloud", "", "public cloud providers [azure|aws|gcp]")
	netlinkPollSec  = flag.Int("netlink", 10, "netlink polling interval in seconds")
	cloudSyncSec    = flag.Int("sync", 10, "cloud routing table sync interval in seconds")
	enableSync      = flag.Bool("event", false, "enable event-based sync (default is periodic, controlled by 'sync')")
	supportedClouds = struct {
		azure string
	}{
		azure: "azure",
	}
)

func Run() error {
	logrus.Info("Starting Virtual Cloud Router")

	flag.Parse()

	var client reconciler.CloudClient

	switch *cloud {
	case supportedClouds.azure:
		logrus.Info("Running on Azure")
		client = reconciler.NewAzureClient()
	default:
		flag.Usage()
		return fmt.Errorf("Unsupported cloud provider: %v", cloud)
	}

	syncCh := make(chan bool)

	rt := route.New(syncCh)

	go monitor.Start(rt, *netlinkPollSec)

	go client.Reconcile(rt, *enableSync, *cloudSyncSec)

	select {}
}
