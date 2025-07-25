// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/feeratefetcher"
)

// PaidSourceConfig are keys for sources that require payment or registration.
type PaidSourceConfig struct {
	TatumKey       string `json:"tatumKey"`
	BlockdaemonKey string `json:"blockdaemonKey"`
}

func NewFeeFetcher(cfg *PaidSourceConfig, log dex.Logger) *feeratefetcher.FeeRateFetcher {
	feeSources := make([]*feeratefetcher.SourceConfig, len(freeFeeSources), len(freeFeeSources)+1)
	copy(feeSources, freeFeeSources)
	if cfg.TatumKey != "" {
		feeSources = append(feeSources, tatumFeeRateFetcher(cfg.TatumKey))
	}
	if cfg.BlockdaemonKey != "" {
		feeSources = append(feeSources, blockDaemonFeeRateFetcher(cfg.BlockdaemonKey))
	}
	return feeratefetcher.NewFeeRateFetcher(feeSources, log)
}

var freeFeeSources = []*feeratefetcher.SourceConfig{
	{ // https://mempool.space/docs/api/rest#get-recommended-fees
		Name:   "mempool.space",
		Rank:   1,
		Period: time.Minute * 2, // Rate limit might be 1 per 10 seconds.
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://mempool.space/api/v1/fees/recommended"
			var res struct {
				FastestFee uint64 `json:"fastestFee"`
			}
			var code int
			if err := dexnet.Get(ctx, uri, &res, dexnet.WithStatusFunc(func(respCode int) {
				code = respCode
			})); err != nil {
				if code == http.StatusTooManyRequests { // 429 per docs
					return 0, time.Minute * 30, errors.New("exceeded request limit")
				}
				return 0, time.Minute * 2, err
			}
			return res.FastestFee, 0, nil
		},
	},
	{ // https://bitcoiner.live/doc/api
		Name:   "bitcoiner.live",
		Rank:   2,
		Period: time.Minute * 5, // Data is refreshed every 5 minutes
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://bitcoiner.live/api/fees/estimates/latest"
			var res struct {
				Estimates map[string]struct {
					SatsPerVB float64 `json:"sat_per_vbyte"`
				} `json:"estimates"`
			}
			if err := dexnet.Get(ctx, uri, &res); err != nil {
				return 0, time.Minute * 10, err
			}
			if res.Estimates == nil {
				return 0, time.Minute * 10, errors.New("no estimates returned")
			}
			// Using 30 minutes estimate. There is also a 60, 120, and higher
			r, found := res.Estimates["30"]
			if !found {
				return 0, time.Minute * 10, errors.New("no 30-minute estimate returned")
			}
			return uint64(math.Round(r.SatsPerVB)), 0, nil
		},
	},
	{
		// https://api.blockcypher.com/v1/btc/main
		// Also have ltc, dash, doge
		Name:   "blockcypher.com",
		Rank:   2,
		Period: time.Minute * 3, // 100 requests/hr => 0.6 minutes
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://api.blockcypher.com/v1/btc/main"
			var res struct {
				MediumPerKB uint64 `json:"medium_fee_per_kb"`
			}
			var code int
			if err := dexnet.Get(ctx, uri, &res, dexnet.WithStatusFunc(func(respCode int) {
				code = respCode
			})); err != nil {
				if code == http.StatusTooManyRequests { // 429 per docs
					// There's a X-Ratelimit-Remaining response header that
					// could potentially be used to caculate a proper delay here.
					return 0, time.Minute * 30, errors.New("exceeded request limit")
				}
				return 0, time.Minute * 10, err
			}
			return uint64(math.Round(float64(res.MediumPerKB) / 1e3)), 0, nil
		},
	},
	{ // undocumented. source is somehow related to blockchain.com
		Name:   "blockchain.info",
		Rank:   3,
		Period: time.Minute * 3, // Rate limit might be 1 per 10 seconds.
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://api.blockchain.info/mempool/fees"
			var res struct {
				Regular  uint64 `json:"regular"` // Might be a little low
				Priority uint64 `json:"priority"`
			}
			if err := dexnet.Get(ctx, uri, &res); err != nil {
				return 0, time.Minute * 10, err
			}
			return res.Priority, 0, nil
		},
	},
	{
		// undocumented. Probably just estimatesmartfee underneath
		Name:   "bitcoinfees.net",
		Rank:   3,
		Period: time.Minute * 3,
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://bitcoinfees.net/api.json"
			var res struct {
				FeePerKBByBlockTarget map[string]uint64 `json:"fee_by_block_target"`
			}
			if err := dexnet.Get(ctx, uri, &res); err != nil {
				return 0, time.Minute * 10, err
			}
			if res.FeePerKBByBlockTarget == nil {
				return 0, time.Minute * 10, errors.New("no estimates returned")
			}
			// Using 30 minutes estimate. There is also a 60, 120, and higher
			r, found := res.FeePerKBByBlockTarget["1"]
			if !found {
				return 0, time.Minute * 10, errors.New("no 1-block estimate returned")
			}
			return uint64(math.Round(float64(r) / 1e3)), 0, nil
		},
	},
	{
		// https://blockchair.com/api/docs#link_M0
		Name:   "blockchair.com",
		Rank:   4,               // blockchair sometimes returns zero. Use only as a last resort.
		Period: time.Minute * 3, // 1440 per day => 1 request / minute
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://api.blockchair.com/bitcoin/stats"
			var res struct {
				Data struct {
					SatsPerByte uint64 `json:"suggested_transaction_fee_per_byte_sat"`
				} `json:"data"`
			}
			var code int
			if err := dexnet.Get(ctx, uri, &res, dexnet.WithStatusFunc(func(respCode int) {
				code = respCode
			})); err != nil {
				switch code {
				case http.StatusTooManyRequests, http.StatusPaymentRequired:
					return 0, time.Minute * 30, errors.New("exceeded request limit")
				case http.StatusServiceUnavailable, 430, 434:
					return 0, time.Hour * 24, errors.New("banned from api")
				}
				return 0, time.Minute * 10, err
			}
			return res.Data.SatsPerByte, 0, nil
		},
	},
}

func tatumFeeRateFetcher(apiKey string) *feeratefetcher.SourceConfig {
	return &feeratefetcher.SourceConfig{
		Name:   "tatum",
		Rank:   1,
		Period: time.Minute * 1, // 1M credit / mo => 3 req / sec
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://api.tatum.io/v3/blockchain/fee/BTC"
			var res struct {
				Fast   float64 `json:"fast"` // Might be a little high
				Medium float64 `json:"medium"`
			}
			var code int
			withCode := dexnet.WithStatusFunc(func(respCode int) {
				code = respCode
			})
			withApiKey := dexnet.WithRequestHeader("x-api-key", apiKey)
			if err := dexnet.Get(ctx, uri, &res, withCode, withApiKey); err != nil {
				if code == http.StatusForbidden {
					return 0, time.Minute * 30, errors.New("exceeded request limit")
				}
				return 0, time.Minute * 10, err
			}
			return uint64(math.Round(res.Medium)), 0, nil
		},
	}
}

func blockDaemonFeeRateFetcher(apiKey string) *feeratefetcher.SourceConfig {
	// https://docs.blockdaemon.com/reference/getfeeestimate
	return &feeratefetcher.SourceConfig{
		Name:   "blockdaemon",
		Rank:   1,
		Period: time.Minute * 2, // 25 reqs/second, 3M compute units, request is 50 compute units => 1 req / 43 secs
		F: func(ctx context.Context) (rate uint64, errDelay time.Duration, err error) {
			const uri = "https://svc.blockdaemon.com/universal/v1/bitcoin/mainnet/tx/estimate_fee"
			var res struct {
				Fees struct {
					Fast   uint64 `json:"fast"` // a little high
					Medium uint64 `json:"medium"`
				} `json:"estimated_fees"`
			}
			var code int
			withCode := dexnet.WithStatusFunc(func(respCode int) {
				code = respCode
			})
			withApiKey := dexnet.WithRequestHeader("X-API-Key", apiKey)
			if err := dexnet.Get(ctx, uri, &res, withCode, withApiKey); err != nil {
				if code == http.StatusTooManyRequests {
					return 0, time.Minute * 30, errors.New("exceeded request limit")
				}
				return 0, time.Minute * 10, err
			}
			return res.Fees.Medium, 0, nil
		},
	}
}

// BitcoreRateFetcher generates a rate fetching function for the bitcore.io API.
func BitcoreRateFetcher(ticker string) func(ctx context.Context, net dex.Network) (uint64, error) {
	const uriTemplate = "https://api.bitcore.io/api/%s/%s/fee/1"
	mainnetURI, testnetURI := fmt.Sprintf(uriTemplate, ticker, "mainnet"), fmt.Sprintf(uriTemplate, ticker, "testnet")

	return func(ctx context.Context, net dex.Network) (uint64, error) {
		var uri string
		if net == dex.Testnet {
			uri = testnetURI
		} else {
			uri = mainnetURI
		}
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		var resp struct {
			RatePerKB float64 `json:"feerate"`
		}
		if err := dexnet.Get(ctx, uri, &resp); err != nil {
			return 0, err
		}
		if resp.RatePerKB <= 0 {
			return 0, fmt.Errorf("zero or negative fee rate")
		}
		return uint64(math.Round(resp.RatePerKB * 1e5)), nil // 1/kB => 1/B
	}
}
