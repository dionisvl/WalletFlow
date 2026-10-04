package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const etherscanURL = "https://api.etherscan.io/v2/api"

// etherscanPage is the max number of rows Etherscan returns per request.
const etherscanPage = 1000

// Etherscan is a minimal Etherscan V2 client.
type Etherscan struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
	Limit   *limiter
}

type etherscanResp struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// evmTx covers fields of txlist, tokentx and txlistinternal.
type evmTx struct {
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

var errNoChainAccess = errors.New("chain not available on this API plan")

func (e *Etherscan) fetch(ctx context.Context, chainID int, action, addr string, startBlock int64) ([]evmTx, error) {
	q := url.Values{
		"chainid":    {strconv.Itoa(chainID)},
		"module":     {"account"},
		"action":     {action},
		"address":    {addr},
		"startblock": {strconv.FormatInt(startBlock, 10)},
		"endblock":   {"9999999999"},
		"page":       {"1"},
		"offset":     {strconv.Itoa(etherscanPage)},
		"sort":       {"asc"},
		"apikey":     {e.Key},
	}
	base := e.BaseURL
	if base == "" {
		base = etherscanURL
	}
	var lastErr error
	for attempt := range 5 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		if err := e.Limit.wait(ctx); err != nil {
			return nil, err
		}
		txs, retry, err := e.get(ctx, base+"?"+q.Encode())
		if err == nil || !retry {
			return txs, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (e *Etherscan) get(ctx context.Context, u string) (txs []evmTx, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode == 429 || resp.StatusCode >= 500, fmt.Errorf("etherscan: HTTP %d", resp.StatusCode)
	}
	var r etherscanResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, true, fmt.Errorf("etherscan: %w", err)
	}
	return parseEtherscan(r)
}

func parseEtherscan(r etherscanResp) (txs []evmTx, retry bool, err error) {
	if r.Status == "1" {
		if err := json.Unmarshal(r.Result, &txs); err != nil {
			return nil, false, fmt.Errorf("etherscan: %w", err)
		}
		return txs, false, nil
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
		return nil, false, fmt.Errorf("%w: %s", errNoChainAccess, msg)
	}
	return nil, false, fmt.Errorf("etherscan: %s %s", r.Message, msg)
}

// evmStream describes one Etherscan action and how to turn its rows into transfers.
type evmStream struct {
	name   string
	action string
}

var evmStreams = []evmStream{
	{"native", "txlist"},
	{"internal", "txlistinternal"},
	{"token", "tokentx"},
}

// syncEVM pulls every stream of one address on one chain, page by page.
func syncEVM(ctx context.Context, st *Store, es *Etherscan, ch Chain, addr string, progress func(string)) (int, error) {
	nativeID, err := st.AssetID(ctx, Asset{Chain: ch.Key, Symbol: ch.Symbol, Decimals: ch.Decimals})
	if err != nil {
		return 0, err
	}
	added := 0
	for _, s := range evmStreams {
		start := st.Cursor(ctx, ch.Key, addr, s.name)
		for {
			txs, err := es.fetch(ctx, ch.ChainID, s.action, addr, start)
			if err != nil {
				return added, err
			}
			ts, maxBlock, err := evmTransfers(ctx, st, ch, s.name, nativeID, txs)
			if err != nil {
				return added, err
			}
			n, err := st.InsertTransfers(ctx, ts)
			if err != nil {
				return added, err
			}
			added += n
			if maxBlock > start {
				// Same block again next time: rows can be split across pages, uid dedupes.
				if err := st.SetCursor(ctx, ch.Key, addr, s.name, maxBlock); err != nil {
					return added, err
				}
			}
			progress(fmt.Sprintf("%s %s %s: +%d", ch.Name, shortAddr(addr), s.name, added))
			if len(txs) < etherscanPage || maxBlock <= start {
				break
			}
			start = maxBlock
		}
	}
	return added, nil
}

func evmTransfers(ctx context.Context, st *Store, ch Chain, stream string, nativeID int64, txs []evmTx) ([]Transfer, int64, error) {
	var out []Transfer
	var maxBlock int64
	for i, x := range txs {
		block, _ := strconv.ParseInt(x.BlockNumber, 10, 64)
		maxBlock = max(maxBlock, block)
		ts, _ := strconv.ParseInt(x.TimeStamp, 10, 64)
		t := Transfer{
			Chain:     ch.Key,
			TxHash:    strings.ToLower(x.Hash),
			TS:        ts,
			From:      strings.ToLower(x.From),
			To:        strings.ToLower(x.To),
			AmountRaw: x.Value,
			FeeRaw:    "0",
			AssetID:   nativeID,
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
				t.AmountRaw = "0"
			}
			t.FeeRaw = mulRaw(x.GasUsed, x.GasPrice)
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
			id, err := st.AssetID(ctx, Asset{Chain: ch.Key, Contract: strings.ToLower(x.Contract), Symbol: tokenSymbol(x.TokenSymbol), Decimals: dec})
			if err != nil {
				return nil, 0, err
			}
			t.AssetID = id
			t.UID = fmt.Sprintf("%s:%s:token:%s", ch.Key, t.TxHash, x.LogIndex)
		}
		out = append(out, t)
	}
	return out, maxBlock, nil
}

func tokenSymbol(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	if r := []rune(s); len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return s
}
