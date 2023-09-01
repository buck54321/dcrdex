// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import "testing"

func TestSubscribeCEXUpdates(t *testing.T) {
	bn := &binance{
		cexUpdaters: make(map[chan interface{}]struct{}),
	}
	_, unsub0 := bn.SubscribeCEXUpdates()
	bn.SubscribeCEXUpdates()
	unsub0()
	bn.SubscribeCEXUpdates()
	if len(bn.cexUpdaters) != 2 {
		t.Fatalf("wrong number of updaters. wanted 2, got %d", len(bn.cexUpdaters))
	}

}
