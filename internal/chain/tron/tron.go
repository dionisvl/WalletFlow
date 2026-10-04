// Package tron pulls TRX and TRC20 transfers from the TronGrid v1 API.
package tron

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"walletflow/internal/address"
	"walletflow/internal/chain"
	"walletflow/internal/ledger"
)

const defaultURL = "https://api.trongrid.io"

// Key is the chain key of Tron.
const Key = "tron"

// Client is a minimal TronGrid client. The API key is optional.
type Client struct {
	Key     string
	BaseURL string // empty = TronGrid
	HTTP    *http.Client
	Limit   *chain.Limiter
}

type page[T any] struct {
	Data    []T    `json:"data"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Meta    struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	} `json:"meta"`
}

type tx struct {
	TxID           string `json:"txID"`
	BlockTimestamp int64  `json:"block_timestamp"`
	Ret            []struct {
		ContractRet string `json:"contractRet"`
		Fee         int64  `json:"fee"`
	} `json:"ret"`
	RawData struct {
		Contract []struct {
			Type      string `json:"type"`
			Parameter struct {
				Value struct {
					Amount          int64  `json:"amount"`
					CallValue       int64  `json:"call_value"`
					OwnerAddress    string `json:"owner_address"`
					ToAddress       string `json:"to_address"`
					ContractAddress string `json:"contract_address"`
				} `json:"value"`
			} `json:"parameter"`
		} `json:"contract"`
	} `json:"raw_data"`
}

type trc20 struct {
	TransactionID  string `json:"transaction_id"`
	BlockTimestamp int64  `json:"block_timestamp"`
	From           string `json:"from"`
	To             string `json:"to"`
	Value          string `json:"value"`
	TokenInfo      struct {
		Symbol   string `json:"symbol"`
		Address  string `json:"address"`
		Decimals int    `json:"decimals"`
	} `json:"token_info"`
}

var trx = ledger.Asset{Chain: Key, Symbol: "TRX", Decimals: 6}

// Pull fetches new TRX and TRC20 transfers of addr, saving page by page.
func (c *Client) Pull(ctx context.Context, addr string, cur chain.Cursors, emit chain.Emit, progress func(string)) (int, error) {
	n1, err := pullStream(ctx, c, addr, "native", "/transactions", cur, emit, txTransfers,
		func(n int) { progress(fmt.Sprintf("Tron %s TRX: +%d", address.Short(addr), n)) })
	if err != nil {
		return n1, err
	}
	n2, err := pullStream(ctx, c, addr, "trc20", "/transactions/trc20", cur, emit, trc20Transfers,
		func(n int) { progress(fmt.Sprintf("Tron %s TRC20: +%d", address.Short(addr), n)) })
	return n1 + n2, err
}

// pullStream walks the pages of one stream starting at the saved cursor (ms timestamp).
func pullStream[T any](ctx context.Context, c *Client, addr, stream, path string, cur chain.Cursors, emit chain.Emit,
	convert func([]T) ([]ledger.Transfer, int64), progress func(int)) (int, error) {
	cursor := cur.Cursor(ctx, Key, addr, stream)
	u := c.firstURL(addr, path, cursor, "asc")
	added := 0
	for u != "" {
		var p page[T]
		if err := c.getJSON(ctx, u, &p); err != nil {
			return added, err
		}
		if !p.Success && p.Error != "" {
			return added, fmt.Errorf("trongrid: %s", p.Error)
		}
		ts, maxTS := convert(p.Data)
		n, err := emit(ctx, ts)
		if err != nil {
			return added, err
		}
		added += n
		if maxTS > cursor {
			cursor = maxTS
			if err := cur.SetCursor(ctx, Key, addr, stream, cursor); err != nil {
				return added, err
			}
		}
		progress(added)
		u = p.Meta.Links.Next
	}
	return added, nil
}

// PerDay estimates transfers per day of addr from its newest page of TRX and
// TRC20 transfers, without downloading the history.
func (c *Client) PerDay(ctx context.Context, addr string) (float64, error) {
	var txs page[tx]
	if err := c.getJSON(ctx, c.firstURL(addr, "/transactions", 0, "desc"), &txs); err != nil {
		return 0, err
	}
	var toks page[trc20]
	if err := c.getJSON(ctx, c.firstURL(addr, "/transactions/trc20", 0, "desc"), &toks); err != nil {
		return 0, err
	}
	stamps := func(n int, at func(int) int64) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = at(i) / 1000
		}
		return out
	}
	r1 := chain.PerDay(stamps(len(txs.Data), func(i int) int64 { return txs.Data[i].BlockTimestamp }), len(txs.Data) >= pageLimit)
	r2 := chain.PerDay(stamps(len(toks.Data), func(i int) int64 { return toks.Data[i].BlockTimestamp }), len(toks.Data) >= pageLimit)
	return max(r1, r2), nil
}

// pageLimit is the page size we ask TronGrid for.
const pageLimit = 200

func (c *Client) firstURL(addr, path string, minTS int64, order string) string {
	base := c.BaseURL
	if base == "" {
		base = defaultURL
	}
	q := url.Values{
		"limit":          {strconv.Itoa(pageLimit)},
		"only_confirmed": {"true"},
		"order_by":       {"block_timestamp," + order},
		"min_timestamp":  {strconv.FormatInt(minTS, 10)},
	}
	return base + "/v1/accounts/" + addr + path + "?" + q.Encode()
}

// getJSON fetches u into v with retries on rate limits and server errors.
func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	var lastErr error
	for attempt := range 5 {
		if attempt > 0 {
			if err := chain.Backoff(ctx, attempt); err != nil {
				return err
			}
		}
		if err := c.Limit.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		if c.Key != "" {
			req.Header.Set("TRON-PRO-API-KEY", c.Key)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("trongrid: HTTP %d", resp.StatusCode)
			continue
		}
		err = json.NewDecoder(resp.Body).Decode(v)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("trongrid: HTTP %d", resp.StatusCode)
		}
		if err != nil {
			return fmt.Errorf("trongrid: %w", err)
		}
		return nil
	}
	return lastErr
}

func txTransfers(txs []tx) ([]ledger.Transfer, int64) {
	var out []ledger.Transfer
	var maxTS int64
	for _, x := range txs {
		maxTS = max(maxTS, x.BlockTimestamp)
		if x.TxID == "" || len(x.RawData.Contract) == 0 {
			continue // internal tx entries have another shape
		}
		c := x.RawData.Contract[0]
		v := c.Parameter.Value
		t := ledger.Transfer{
			UID:    Key + ":" + x.TxID + ":native",
			Chain:  Key,
			TxHash: x.TxID,
			TS:     x.BlockTimestamp / 1000,
			From:   address.TronFromHex(v.OwnerAddress),
			Asset:  trx,
			FeeRaw: "0",
		}
		switch c.Type {
		case "TransferContract":
			t.To = address.TronFromHex(v.ToAddress)
			t.AmountRaw = strconv.FormatInt(v.Amount, 10)
		case "TriggerSmartContract":
			t.To = address.TronFromHex(v.ContractAddress)
			t.AmountRaw = strconv.FormatInt(v.CallValue, 10)
		default:
			continue
		}
		if len(x.Ret) > 0 {
			t.FeeRaw = strconv.FormatInt(x.Ret[0].Fee, 10)
			if x.Ret[0].ContractRet != "" && x.Ret[0].ContractRet != "SUCCESS" {
				t.AmountRaw = "0"
			}
		}
		out = append(out, t)
	}
	return out, maxTS
}

func trc20Transfers(txs []trc20) ([]ledger.Transfer, int64) {
	var out []ledger.Transfer
	var maxTS int64
	for _, x := range txs {
		maxTS = max(maxTS, x.BlockTimestamp)
		amount := x.Value
		if amount == "" {
			amount = "0"
		}
		out = append(out, ledger.Transfer{
			// TronGrid has no log index here, so the transfer itself is the key.
			UID:       fmt.Sprintf("%s:%s:trc20:%s:%s:%s:%s", Key, x.TransactionID, x.TokenInfo.Address, x.From, x.To, amount),
			Chain:     Key,
			TxHash:    x.TransactionID,
			TS:        x.BlockTimestamp / 1000,
			From:      x.From,
			To:        x.To,
			Asset:     ledger.Asset{Chain: Key, Contract: x.TokenInfo.Address, Symbol: ledger.CleanSymbol(x.TokenInfo.Symbol), Decimals: x.TokenInfo.Decimals},
			AmountRaw: amount,
			FeeRaw:    "0",
		})
	}
	return out, maxTS
}
