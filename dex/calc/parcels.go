// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package calc

// Parcels calculates the number of parcels associated with the given order
// quantities, lot size and parcel size. Any quantity currently settling
// should be summed in with the makerQty.
func Parcels(makerQty, takerQty, lotSize uint64, parcelSize uint32) float64 {
	parcelWeight := makerQty + takerQty*2
	parcelQty := lotSize * uint64(parcelSize)
	return float64(parcelWeight) / float64(parcelQty)
}
