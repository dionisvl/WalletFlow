// Package evm pulls transfers of EVM chains from the Etherscan V2 API.
package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dionisvl/WalletFlow/internal/address"
	"github.com/dionisvl/WalletFlow/internal/chain"
	"github.com/dionisvl/WalletFlow/internal/ledger"
)

const defaultURL = "https://api.etherscan.io/v2/api"

// pageSize is the number of rows asked per request.
const pageSize = 1000

// ErrNoChainAccess means the API plan does not cover the chain.
var ErrNoChainAccess = errors.New("chain not available on this API plan")

// Client is a minimal Etherscan V2 client. One key covers every EVM chain.
type Client struct {
	Key     string
	BaseURL string // empty = Etherscan
	HTTP    *http.Client
	Limit   *chain.Limiter
}

type response struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// row covers fields of txlist, tokentx and txlistinternal.
type row struct {
	BlockNumber  string `json:"blockNumber"`
	TimeStamp    string `json:"timeStamp"`
	Hash         string `json:"hash"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
	GasPrice     string `json:"gasPrice"`
	GasUsed      string `json:"gasUsed"`
	IsError      string `json:"isError"`
	Contract     string `json:"contractAddress"`
	TokenSymbol  string `json:"tokenSymbol"`
	TokenDecimal string `json:"tokenDecimal"`
	LogIndex     string `json:"logIndex"`
	TraceID      string `json:"traceId"`
}

// streams are the Etherscan actions we read, by cursor stream name.
var streams = []struct{ name, action string }{
	{"native", "txlist"},
	{"internal", "txlistinternal"},
	{"token", "tokentx"},
}

// Pull fetches everything new for addr on ch, saving page by page.
func (c *Client) Pull(ctx context.Context, ch chain.Chain, addr string, cur chain.Cursors, emit chain.Emit, progress func(string)) (int, error) {
	added := 0
	for _, s := range streams {
		start := cur.Cursor(ctx, ch.Key, addr, s.name)
		for {
			rows, err := c.fetch(ctx, ch.ChainID, s.action, addr, start, "asc")
			if err != nil {
				return added, err
			}
			ts, maxBlock := toTransfers(ch, s.name, rows)
			n, err := emit(ctx, ts)
			if err != nil {
				return added, err
			}
			added += n
			if maxBlock > start {
				// Next run starts at the same block: rows may be split across pages, uid dedupes.
				if err := cur.SetCursor(ctx, ch.Key, addr, s.name, maxBlock); err != nil {
					return added, err
				}
			}
			progress(fmt.Sprintf("%s %s %s: +%d", ch.Name, address.Short(addr), s.name, added))
			if len(rows) < pageSize || maxBlock <= start {
				break
			}
			start = maxBlock
		}
	}
	return added, nil
}

// PerDay estimates transfers per day of addr on ch from its newest page of
// native and token transfers, without downloading the history.
func (c *Client) PerDay(ctx context.Context, ch chain.Chain, addr string) (float64, error) {
	var rate float64
	for _, action := range []string{"txlist", "tokentx"} {
		rows, err := c.fetch(ctx, ch.ChainID, action, addr, 0, "desc")
		if err != nil {
			return 0, err
		}
		ts := make([]int64, len(rows))
		for i, r := range rows {
			ts[i], _ = strconv.ParseInt(r.TimeStamp, 10, 64)
		}
		rate = max(rate, chain.PerDay(ts, len(rows) >= pageSize))
	}
	return rate, nil
}

func (c *Client) fetch(ctx context.Context, chainID int, action, addr string, startBlock int64, sort string) ([]row, error) {
	q := url.Values{
		"chainid":    {strconv.Itoa(chainID)},
		"module":     {"account"},
		"action":     {action},
		"address":    {addr},
		"startblock": {strconv.FormatInt(startBlock, 10)},
		"endblock":   {"9999999999"},
		"page":       {"1"},
		"offset":     {strconv.Itoa(pageSize)},
		"sort":       {sort},
		"apikey":     {c.Key},
	}
	base := c.BaseURL
	if base == "" {
		base = defaultURL
	}
	var lastErr error
	for attempt := range 5 {
		if attempt > 0 {
			if err := chain.Backoff(ctx, attempt); err != nil {
				return nil, err
			}
		}
		if err := c.Limit.Wait(ctx); err != nil {
			return nil, err
		}
		rows, retry, err := c.get(ctx, base+"?"+q.Encode())
		if err == nil || !retry {
			return rows, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *Client) get(ctx context.Context, u string) (rows []row, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode == 429 || resp.StatusCode >= 500, fmt.Errorf("etherscan: HTTP %d", resp.StatusCode)
	}
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, true, fmt.Errorf("etherscan: %w", err)
	}
	return parse(r)
}

func parse(r response) (rows []row, retry bool, err error) {
	if r.Status == "1" {
		if err := json.Unmarshal(r.Result, &rows); err != nil {
			return nil, false, fmt.Errorf("etherscan: %w", err)
		}
		return rows, false, nil
	}
	if strings.HasPrefix(r.Message, "No transactions found") {
		return nil, false, nil
	}
	var msg string
	json.Unmarshal(r.Result, &msg)
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "rate limit"):
		return nil, true, fmt.Errorf("etherscan: %s", msg)
	case strings.Contains(low, "not supported for this chain") || strings.Contains(low, "upgrade your api plan"):
		return nil, false, ErrNoChainAccess
	}
	return nil, false, fmt.Errorf("etherscan: %s %s", r.Message, msg)
}

func toTransfers(ch chain.Chain, stream string, rows []row) ([]ledger.Transfer, int64) {
	native := ledger.Asset{Chain: ch.Key, Symbol: ch.Symbol, Decimals: ch.Decimals}
	var out []ledger.Transfer
	var maxBlock int64
	for i, x := range rows {
		block, _ := strconv.ParseInt(x.BlockNumber, 10, 64)
		maxBlock = max(maxBlock, block)
		ts, _ := strconv.ParseInt(x.TimeStamp, 10, 64)
		t := ledger.Transfer{
			Chain:     ch.Key,
			TxHash:    strings.ToLower(x.Hash),
			TS:        ts,
			From:      strings.ToLower(x.From),
			To:        strings.ToLower(x.To),
			AmountRaw: x.Value,
			FeeRaw:    "0",
			Asset:     native,
		}
		if t.AmountRaw == "" {
			t.AmountRaw = "0"
		}
		switch stream {
		case "native":
			if t.To == "" {
				t.To = strings.ToLower(x.Contract) // contract creation
			}
			if x.IsError == "1" {
				t.AmountRaw = "0" // failed: only the fee was spent
			}
			t.FeeRaw = ledger.MulRaw(x.GasUsed, x.GasPrice)
			t.UID = fmt.Sprintf("%s:%s:native", ch.Key, t.TxHash)
		case "internal":
			if x.IsError == "1" {
				continue
			}
			idx := x.TraceID
			if idx == "" {
				idx = strconv.Itoa(i) + ":" + t.From + ":" + t.To + ":" + t.AmountRaw
			}
			t.UID = fmt.Sprintf("%s:%s:internal:%s", ch.Key, t.TxHash, idx)
		case "token":
			dec, _ := strconv.Atoi(x.TokenDecimal)
			t.Asset = ledger.Asset{Chain: ch.Key, Contract: strings.ToLower(x.Contract), Symbol: ledger.CleanSymbol(x.TokenSymbol), Decimals: dec}
			t.UID = fmt.Sprintf("%s:%s:token:%s", ch.Key, t.TxHash, x.LogIndex)
		}
		out = append(out, t)
	}
	return out, maxBlock
}
