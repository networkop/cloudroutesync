package reconciler

import (
	"github.com/networkop/cloudroutesync/pkg/route"
)

const uniquePrefix = "cloudroutesync"

// CloudClient defines generic Cloud Client interface
type CloudClient interface {
	Reconcile(*route.Table, bool, int)
}
