package monitor

import (
	"bytes"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/networkop/vcr/pkg/route"
	"github.com/sirupsen/logrus"
)

var (
	ipCmd     = "ip"
	showRoute = []string{"route", "show", "table", "main"}
	monRoute  = []string{"monitor", "route"}
)

// Start monitoring local routing table
func Start(rt *route.Table, syncCh chan bool) {

	// Periodically check routing table
	// TODO: replace with ip mon
	go func() {
		for {
			currentRT, err := parseCurrent()
			if err != nil {
				logrus.Infof("Failed to parse current route table")
			}

			rt.Update(currentRT)

			time.Sleep(time.Second * 2)
			rt.Print()
		}
	}()

}

func parseCurrent() (map[string]net.IP, error) {
	cmd := exec.Command(ipCmd, showRoute...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		logrus.Infof("Failed to execute command %s", cmd)
	}

	routes := make(map[string]net.IP)

	for _, routeStr := range strings.Split(out.String(), "\n") {
		//fmt.Println(routeStr)
		if !strings.Contains(routeStr, "via") {
			logrus.Debugf("Directly-connected route, skipping: %s", routeStr)
			continue
		}
		parts := strings.Split(routeStr, " ")
		prefixStr, nhStr := parts[0], parts[2]

		// We don't want to inject default
		//if prefixStr == "default" {
		//	prefixStr = "0.0.0.0/0"
		//}

		_, prefix, err := net.ParseCIDR(prefixStr)
		if err != nil {
			hostRoute := net.ParseIP(prefixStr)
			if hostRoute == nil {
				logrus.Infof("Failed to parse prefix: %s", prefixStr)
				continue
			}
			prefix = &net.IPNet{IP: hostRoute, Mask: net.CIDRMask(32, 32)}
		}
		nh := net.ParseIP(nhStr)
		if nh == nil {
			logrus.Infof("Failed to parse nexthop: %s", nhStr)
			continue
		}
		//logrus.Infof("Adding route %s via %s", prefix.String(), nh.String())
		routes[prefix.String()] = nh
	}

	return routes, nil
}
