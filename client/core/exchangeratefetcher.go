// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"fmt"

	"net/http"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

const (
	// DefaultFiatCurrency is the currency for displaying assets fiat value.
	DefaultFiatCurrency = "USD"
	// fiatRateRequestInterval is the amount of time between calls to the exchange API.
	fiatRateRequestInterval = 5 * time.Minute
	// fiatRateDataExpiry : Any data older than fiatRateDataExpiry will be discarded.
	fiatRateDataExpiry = 60 * time.Minute

	// Tokens. Used to identify exchange rate source.
	messari       = "messari"
	coinpaprika   = "coinpaprika"
	dcrdataDotOrg = "dcrdata"
)

var (
	dcrDataURL     = "https://explorer.dcrdata.org/api/exchangerate"
	coinpaprikaURL = "https://api.coinpaprika.com/v1/tickers/%s"
	messariURL     = "https://data.messari.io/api/v1/assets/%s/metrics/market-data"
	btcBipId, _    = dex.BipSymbolID("btc")
	dcrBipId, _    = dex.BipSymbolID("dcr")
)

// exchangeRateFetchers is the list of all supported exchange rate fetchers.
var exchangeRateFetchers = map[string]rateFetcher{
	coinpaprika:   fetchCoinpaprikaRates,
	dcrdataDotOrg: fetchDcrdataRates,
	messari:       fetchMassariRates,
}

// fiatRateInfo holds the fiat exchange rate and the last update time for an
// asset.
type fiatRateInfo struct {
	rate       float64
	lastUpdate time.Time
}

// rateFetcher can fetch exchange rates for assets from an API.
type rateFetcher func(context context.Context, logger dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64

type commonSource struct {
	mtx         sync.RWMutex
	lastRequest time.Time
	log         dex.Logger
	fetchRates  rateFetcher
	fiatRates   map[uint32]*fiatRateInfo
}

// logRequest sets the lastRequest time.Time.
func (source *commonSource) logRequest() {
	source.mtx.Lock()
	defer source.mtx.Unlock()
	source.lastRequest = time.Now()
}

// lastTry is the last time the exchange rate fecther made a request.
func (source *commonSource) lastTry() time.Time {
	source.mtx.RLock()
	defer source.mtx.RUnlock()
	return source.lastRequest
}

// isExpired checks the last update time for all exchange rates against the
// provided expiryTime.
func (source *commonSource) isExpired(expiryTime time.Duration) bool {
	source.mtx.Lock()
	defer source.mtx.Unlock()
	var expiredCount int
	for _, rateInfo := range source.fiatRates {
		if time.Since(rateInfo.lastUpdate) > expiryTime {
			expiredCount++
		}
	}
	totalFiatRate := len(source.fiatRates)
	return expiredCount == totalFiatRate && totalFiatRate != 0
}

// assetRate returns the exchange rate information for the assetID specified.
// found is true is the rate source has a value for the assetID specified.
func (source *commonSource) assetRate(assetID uint32) (fiatRateInfo, bool) {
	source.mtx.Lock()
	defer source.mtx.Unlock()
	rateInfo, found := source.fiatRates[assetID]
	if !found {
		return fiatRateInfo{}, false
	}
	return *rateInfo, true
}

// refreshRates updates the last update time and the rate information for asset.
func (source *commonSource) refreshRates(ctx context.Context, assets map[uint32]*SupportedAsset) {
	source.logRequest()
	fiatRates := source.fetchRates(ctx, source.log, assets)
	source.mtx.Lock()
	for assetID, fiatRate := range fiatRates {
		source.fiatRates[assetID] = &fiatRateInfo{
			rate:       fiatRate,
			lastUpdate: time.Now(),
		}
	}
	source.mtx.Unlock()
}

// Used to initialize an exchange rate source.
func newcommonSource(logger dex.Logger, fetcher rateFetcher) *commonSource {
	return &commonSource{
		log:        logger,
		fetchRates: fetcher,
		fiatRates:  make(map[uint32]*fiatRateInfo),
	}
}

// fetchCoinpaprikaRates retrieves and parses exchange rate data from the
// coinpaprika API. coinpaprika response models the JSON data returned from the
// coinpaprika API. See https://api.coinpaprika.com/#operation/getTickersById
// for sample request and response information.
func fetchCoinpaprikaRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	fiatRates := make(map[uint32]float64, 0)
	for assetID, asset := range assets {
		if asset.Wallet == nil {
			// we don't want to fetch rates for assets with no wallet.
			continue
		}

		res := new(struct {
			Name   string `json:"name"`
			Symbol string `json:"symbol"`
			Quotes struct {
				Currency struct {
					Price float64 `json:"price"`
				} `json:"USD"`
			} `json:"quotes"`
		})

		slug := fmt.Sprintf("%s-%s", asset.Symbol, asset.Info.Name)
		// Special handling for asset names with multiple space, e.g Bitcoin Cash.
		slug = strings.ToLower(strings.ReplaceAll(slug, " ", "-"))
		reqStr := fmt.Sprintf(coinpaprikaURL, slug)

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqStr, nil)
		if err != nil {
			log.Errorf("%s: NewRequestWithContext error: %v", coinpaprika, err)
			continue
		}

		resp, err := http.DefaultClient.Do(request)
		if err != nil {
			log.Errorf("%s: request failed: %v", coinpaprika, err)
			continue
		}
		defer resp.Body.Close()

		// Read the raw bytes and close the response.
		err = json.NewDecoder(resp.Body).Decode(res)
		if err != nil {
			log.Errorf("%s: failed to decode json from %s: %v", coinpaprika, request.URL.String(), err)
			continue
		}

		fiatRates[assetID] = res.Quotes.Currency.Price
	}
	return fiatRates
}

// fetchDcrdataRates retrieves and parses exchange rate data from dcrdataDotOrg
// exchange rate API.
func fetchDcrdataRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	assetBTC := assets[btcBipId]
	assetDCR := assets[dcrBipId]
	noBTCAsset := assetBTC == nil || assetBTC.Wallet == nil
	noDCRAsset := assetDCR == nil || assetDCR.Wallet == nil
	if noBTCAsset && noDCRAsset {
		return nil
	}

	fiatRates := make(map[uint32]float64)
	res := new(struct {
		Currency string  `json:"btcIndex"`
		DcrPrice float64 `json:"dcrPrice"`
		BtcPrice float64 `json:"btcPrice"`
	})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, dcrDataURL, nil)
	if err != nil {
		log.Errorf("%s: NewRequestWithContext error: %v", dcrdataDotOrg, err)
		return nil
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Errorf("%s: request failed: %v", dcrdataDotOrg, err)
		return nil
	}
	defer resp.Body.Close()

	// Read the raw bytes and close the response.
	err = json.NewDecoder(resp.Body).Decode(res)
	if err != nil {
		log.Errorf("%s: failed to decode json from %s: %v", dcrdataDotOrg, request.URL.String(), err)
		return nil
	}

	if !noBTCAsset {
		fiatRates[btcBipId] = res.BtcPrice
	}
	if !noDCRAsset {
		fiatRates[dcrBipId] = res.DcrPrice
	}

	return fiatRates
}

// fetchMassariRates retrieves and parses exchange rate data from the massari
// API. It returns the last error if any. messari response models the JSON data
// returned from the messari API. See
// https://messari.io/api/docs#operation/Get%20Asset%20Metrics for sample
// request and response information.
func fetchMassariRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	fiatRates := make(map[uint32]float64)
	for assetID, asset := range assets {
		if asset.Wallet == nil {
			// we don't want to fetch rate for assets with no wallet.
			continue
		}

		res := new(struct {
			Data struct {
				Name       string `json:"name"`
				Symbol     string `json:"symbol"`
				MarketData struct {
					Price float64 `json:"price_usd"`
				} `json:"market_data"`
			} `json:"data"`
		})

		slug := strings.ToLower(asset.Symbol)
		reqStr := fmt.Sprintf(messariURL, slug)

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqStr, nil)
		if err != nil {
			log.Errorf("%s: NewRequestWithContext error: %v", dcrdataDotOrg, err)
			continue
		}

		resp, err := http.DefaultClient.Do(request)
		if err != nil {
			log.Errorf("%s: request error: %v", messari, err)
			continue
		}
		defer resp.Body.Close()

		// Read the raw bytes and close the response.
		err = json.NewDecoder(resp.Body).Decode(res)
		if err != nil {
			log.Errorf("%s: failed to decode json from %s: %v", messari, request.URL.String(), err)
			continue
		}

		fiatRates[assetID] = res.Data.MarketData.Price
	}
	return fiatRates
}
