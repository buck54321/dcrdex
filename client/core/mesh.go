package core

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/feerates"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func (c *Core) handleMeshNotification(noteI any) {
	switch n := noteI.(type) {
	case *mj.Broadcast:
		c.handleMeshBroadcast(n)

	}
}

func (c *Core) handleMeshBroadcast(bcast *mj.Broadcast) {
	c.log.Tracef("Received mesh broadcast: %s", bcast.Subject)

	switch bcast.Topic {
	case mj.TopicMarket:
		c.handleMarketBroadcast(bcast)
	case mj.TopicFeeRateEstimate:
		c.handleFeeRateEstimateBroadcast(bcast)
	case mj.TopicFiatRate:
		c.handleFiatRateBroadcast(bcast)
	default:
		c.log.Warnf("Unknown broadcast topic: %s", bcast.Topic)
	}

	// // Emit the broadcast to the subscribers.
	// c.meshEmit(bcast)
}

func (c *Core) handleMarketBroadcast(bcast *mj.Broadcast) {
	c.log.Debugf("Handling market broadcast for subject %s", bcast.Subject)

	switch bcast.MessageType {
	case mj.MessageTypeTrollBox:
		var troll mj.Troll
		if err := json.Unmarshal(bcast.Payload, &troll); err != nil {
			c.log.Errorf("error unmarshaling trollbox message: %v", err)
			return
		}
		fmt.Printf("trollbox message for market: %s\n", troll.Msg)
	case mj.MessageTypeNewOrder:
		var ord tanka.Order
		if err := json.Unmarshal(bcast.Payload, &ord); err != nil {
			c.log.Errorf("error unmarshaling new order: %v", err)
			return
		}
		fmt.Printf("new order for market %q\n", bcast.Subject)
	case mj.MessageTypeNewSubscriber:
		var ns mj.NewSubscriber
		if err := json.Unmarshal(bcast.Payload, &ns); err != nil {
			c.log.Errorf("error decoding new_subscriber payload: %v", err)
		}
		fmt.Printf("new subscriber for market %q: %s\n", bcast.Subject, ns.PeerID)
	default:
		c.log.Warnf("Unknown market broadcast message type: %s", bcast.MessageType)
	}
}

func (c *Core) handleFeeRateEstimateBroadcast(bcast *mj.Broadcast) {
	c.log.Tracef("Handling fee rate estimate broadcast for subject %s", bcast.Subject)

	var feeRate map[uint32]*feerates.Estimate
	if err := json.Unmarshal(bcast.Payload, &feeRate); err != nil {
		c.log.Errorf("Error unmarshaling fee rate estimate: %v", err)
		return
	}

	// Process the fee rate estimate.
	c.processMeshFeeRateEstimate(feeRate)
}

func (c *Core) processMeshFeeRateEstimate(feeRate map[uint32]*feerates.Estimate) {
	c.log.Tracef("Processing fee rate estimate: %v", feeRate)

	c.meshFeeRatesMtx.Lock()
	defer c.meshFeeRatesMtx.Unlock()
	for chainID, r := range feeRate {
		oldRate, exists := c.meshFeeRates[chainID]
		if exists && oldRate.LastUpdated.After(r.LastUpdated) {
			c.log.Debugf("Ignoring older fee rate estimate for chain %d: %v", chainID, r)
			continue
		}
		c.meshFeeRates[chainID] = r
	}
}

func (c *Core) handleFiatRateBroadcast(bcast *mj.Broadcast) {
	c.log.Tracef("Handling fiat rate broadcast for subject %s", bcast.Subject)

	var rate map[string]*fiatrates.FiatRateInfo
	if err := json.Unmarshal(bcast.Payload, &rate); err != nil {
		c.log.Errorf("Error unmarshaling fiat rate message: %v", err)
		return
	}

	c.meshFiatRatesMtx.Lock()
	defer c.meshFiatRatesMtx.Unlock()
	for assetID, r := range rate {
		oldRate, exists := c.meshFiatRates[assetID]
		if exists && oldRate.LastUpdate.After(r.LastUpdate) {
			c.log.Debugf("Ignoring older fiat rate for asset %s: %v", assetID, r)
			continue
		}
		c.meshFiatRates[assetID] = r
	}
}

func (c *Core) coreMesh() *Mesh {
	c.meshMtx.RLock()
	mesh := c.mesh
	meshCM := c.meshCM
	c.meshMtx.RUnlock()
	if mesh == nil || !meshCM.On() {
		return nil
	}
	mktIDs, err := mesh.GetMarkets()
	if err != nil {
		c.log.Errorf("error getting markets from mesh: %v", err)
		return nil
	}
	mkts := make(map[string]*MeshMarket, len(mktIDs))
	for _, mktID := range mktIDs {
		symbols := strings.Split(mktID, "_")
		if len(symbols) != 2 {
			c.log.Debugf("Invalid market ID %s, expected format BASE_QUOTE", mktID)
			continue
		}
		baseSymbol, quoteSymbol := symbols[0], symbols[1]
		baseID, found := dex.BipSymbolID(baseSymbol)
		if !found {
			c.log.Debugf("Unknown base symbol %s in market %s", baseSymbol, mktID)
			continue
		}
		quoteID, found := dex.BipSymbolID(quoteSymbol)
		if !found {
			c.log.Debugf("Unknown quote symbol %s in market %s", quoteSymbol, mktID)
			continue
		}
		mkt := &MeshMarket{
			BaseID:  baseID,
			QuoteID: quoteID,
		}
		mkts[mktID] = mkt
	}
	var assetVersions map[uint32]uint32
	if av, ok := c.meshAssetVersions.Load().(map[uint32]uint32); ok {
		assetVersions = av
	}
	c.meshFeeRatesMtx.RLock()
	feeRates := make(map[uint32]*feerates.Estimate, len(c.meshFeeRates))
	maps.Copy(feeRates, c.meshFeeRates)
	c.meshFeeRatesMtx.RUnlock()
	return &Mesh{
		Markets:       mkts,
		AssetVersions: assetVersions,
		FeeRates:      feeRates,
	}

}
func (c *Core) connectMesh() {
	if c.net != dex.Simnet {
		return
	}
	var err error
	if err = c.meshCM.ConnectOnce(c.ctx); err != nil {
		c.log.Errorf("error connecting mesh: %v", err)
		return
	}
	defer func() {
		if err != nil {
			c.log.Errorf("Failed to initialize mesh subscriptions. Closing connection:", err)
			c.meshCM.Disconnect()
		}
	}()

	c.log.Infof("Connected to Mesh")

	c.meshAssetVersions.Store(c.mesh.AssetVersions())

	if err = c.mesh.SubscribeToFeeRateEstimates(); err != nil {
		c.log.Errorf("error subscribing to mesh fee rate estimates: %v", err)
		return
	}
	if err := c.mesh.SubscribeToFiatRates(); err != nil {
		c.log.Error("error subscribing to mesh fiat rates: %v", err)
		return
	}

	if _, err := c.mesh.GetMarkets(); err != nil {
		c.log.Errorf("error getting markets from mesh: %v", err)
	}
}

func deriveMeshPriv(seed []byte) (*secp256k1.PrivateKey, error) {
	xKey, err := keygen.GenDeepChild(seed, []uint32{hdKeyPurposeBonds})
	if err != nil {
		return nil, err
	}
	privB, err := xKey.SerializedPrivKey()
	if err != nil {
		return nil, err
	}
	return secp256k1.PrivKeyFromBytes(privB), nil
}

func (c *Core) MakeMeshMarket(baseID, quoteID uint32) (*OrderBook, error) {
	mesh := c.getMesh()

	if err := mesh.SubscribeMarket(baseID, quoteID); err != nil {
		return nil, fmt.Errorf("error making new mesh market: %w", err)
	}

	mktID, err := dex.MarketName(baseID, quoteID)
	if err != nil {
		return nil, fmt.Errorf("unknown asset pair (%d, %d)", baseID, quoteID)
	}

	c.meshBookNoteMtx.Lock()
	defer c.meshBookNoteMtx.Unlock()

	ob, err := mesh.Book(mktID)
	if err != nil {
		return nil, fmt.Errorf("error getting mesh orderbook: %w", err)
	}

	return meshBookToMiniBook(baseID, quoteID, ob)
}

func meshBookToMiniBook(baseID, quoteID uint32, mords []*tanka.Order) (*OrderBook, error) {
	bui, _ := asset.UnitInfo(baseID)
	qui, _ := asset.UnitInfo(quoteID)
	ob := &OrderBook{
		Buys:  make([]*MiniOrder, 0, len(mords)/2),
		Sells: make([]*MiniOrder, 0, len(mords)/2),
	}
	convertMord := func(mord *tanka.Order) *MiniOrder {
		oid := mord.ID()
		return &MiniOrder{
			Qty:       float64(mord.Qty) / float64(bui.Conventional.ConversionFactor),
			QtyAtomic: mord.Qty,
			Rate:      calc.ConventionalRate(mord.Rate, bui, qui),
			MsgRate:   mord.Rate,
			Sell:      mord.Sell,
			Token:     token(oid[32:38]),
		}
	}
	for _, mord := range mords {
		if mord.Sell {
			ob.Sells = append(ob.Sells, convertMord(mord))
		} else {
			ob.Buys = append(ob.Buys, convertMord(mord))
		}
	}
	sort.Slice(ob.Sells, func(i, j int) bool {
		return ob.Sells[i].Rate < ob.Sells[j].Rate
	})
	sort.Slice(ob.Buys, func(i, j int) bool {
		return ob.Buys[i].Rate > ob.Buys[j].Rate
	})
	return ob, nil
}
