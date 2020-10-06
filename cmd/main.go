package cmd

import (
	"flag"
	"fmt"

	"github.com/networkop/cloudroutesync/pkg/monitor"
	"github.com/networkop/cloudroutesync/pkg/reconciler"
	"github.com/networkop/cloudroutesync/pkg/route"
	"github.com/sirupsen/logrus"
)

var (
	cloud           = flag.String("cloud", "", "public cloud providers [azure|aws|gcp]")
	netlinkPollSec  = flag.Int("netlink", 10, "netlink polling interval in seconds")
	cloudSyncSec    = flag.Int("sync", 10, "cloud routing table sync interval in seconds")
	enableSync      = flag.Bool("event", false, "enable event-based sync (default is periodic, controlled by 'sync')")
	debug           = flag.Bool("debug", false, "enable debug logging")
	supportedClouds = struct {
		azure string
	}{
		azure: "azure",
	}
)

func Run() error {

	flag.Parse()

	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	var client reconciler.CloudClient

	switch *cloud {
	case supportedClouds.azure:
		logrus.Info("Running on Azure")
		client = reconciler.NewAzureClient()
	default:
		flag.Usage()
		return fmt.Errorf("Unsupported/Undefined cloud provider: %v", *cloud)
	}

	syncCh := make(chan bool)

	rt := route.New(syncCh)

	go monitor.Start(rt, *netlinkPollSec)

	go client.Reconcile(rt, *enableSync, *cloudSyncSec)

	select {}
}
