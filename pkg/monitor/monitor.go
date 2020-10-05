package monitor

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/jsimonetti/rtnetlink"
	"github.com/networkop/cloudroutersync/pkg/route"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// Start monitoring local routing table
func Start(rt *route.Table, pollInterval int) {

	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	for {
		msg, err := conn.Route.List()
		if err != nil {
			logrus.Errorf("Failed to list routes :%s", err)
		}

		currentRT := parseNetlinkRT(msg)

		//updatedRT := setNextHopSelf(currentRT, rt.DefaultIntf, rt.DefaultIP)

		rt.Update(currentRT)
		rt.Print()

		time.Sleep(time.Duration(pollInterval) * time.Second)
	}

}

func parseNetlinkRT(routes []rtnetlink.RouteMessage) map[string]net.IP {
	result := make(map[string]net.IP)

	for _, r := range routes {
		// Narrowing down to only the routes we _need_
		if r.Table != unix.RT_TABLE_MAIN && r.Scope != unix.RT_SCOPE_UNIVERSE && r.Type != unix.RTN_UNICAST && r.Family != unix.AF_INET {
			continue
		}
		attrs := r.Attributes

		if attrs.Dst != nil && attrs.Gateway != nil {
			prefix := fmt.Sprintf("%s/%d", attrs.Dst.String(), r.DstLength)
			result[prefix] = attrs.Gateway
		}

	}

	return result
}
