package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const tronGridURL = "https://api.trongrid.io"

// TronGrid is a minimal TronGrid v1 client.
type TronGrid struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
	Limit   *limiter
}

type tronPage[T any] struct {
	Data    []T    `json:"data"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Meta    struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	} `json:"meta"`
}

type tronTx struct {
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

type trc20Tx struct {
	TransactionID  string `json:"transaction_id"`
	BlockTimestamp int64  `json:"block_timestamp"`
	From           string `json:"from"`
	To             string `json:"to"`
	Type           string `json:"type"`
	Value          string `json:"value"`
	TokenInfo      struct {
		Symbol   string `json:"symbol"`
		Address  string `json:"address"`
		Decimals int    `json:"decimals"`
	} `json:"token_info"`
}

// getJSON fetches u into v with retries on rate limits and server errors.
func (g *TronGrid) getJSON(ctx context.Context, u string, v any) error {
	var lastErr error
	for attempt := range 5 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		if err := g.Limit.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		if g.Key != "" {
			req.Header.Set("TRON-PRO-API-KEY", g.Key)
		}
		resp, err := g.HTTP.Do(req)
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

func (g *TronGrid) firstURL(addr, path string, minTS int64) string {
	base := g.BaseURL
	if base == "" {
		base = tronGridURL
	}
	q := url.Values{
		"limit":          {"200"},
		"only_confirmed": {"true"},
		"order_by":       {"block_timestamp,asc"},
		"min_timestamp":  {strconv.FormatInt(minTS, 10)},
	}
	return base + "/v1/accounts/" + addr + path + "?" + q.Encode()
}

// tronStreamSync walks the pages of one TronGrid stream starting at the saved cursor.
func tronStreamSync[T any](ctx context.Context, st *Store, g *TronGrid, addr, stream, path string,
	convert func([]T) ([]Transfer, int64, error), progress func(int)) (int, error) {
	cursor := st.Cursor(ctx, "tron", addr, stream)
	u := g.firstURL(addr, path, cursor)
	added := 0
	for u != "" {
		var p tronPage[T]
		if err := g.getJSON(ctx, u, &p); err != nil {
			return added, err
		}
		if !p.Success && p.Error != "" {
			return added, fmt.Errorf("trongrid: %s", p.Error)
		}
		ts, maxTS, err := convert(p.Data)
		if err != nil {
			return added, err
		}
		n, err := st.InsertTransfers(ctx, ts)
		if err != nil {
			return added, err
		}
		added += n
		if maxTS > cursor {
			cursor = maxTS
			if err := st.SetCursor(ctx, "tron", addr, stream, cursor); err != nil {
				return added, err
			}
		}
		progress(added)
		u = p.Meta.Links.Next
	}
	return added, nil
}

func syncTron(ctx context.Context, st *Store, g *TronGrid, addr string, progress func(string)) (int, error) {
	trxID, err := st.AssetID(ctx, Asset{Chain: "tron", Symbol: "TRX", Decimals: 6})
	if err != nil {
		return 0, err
	}
	n1, err := tronStreamSync(ctx, st, g, addr, "native", "/transactions",
		func(txs []tronTx) ([]Transfer, int64, error) { return tronTransfers(txs, trxID) },
		func(n int) { progress(fmt.Sprintf("Tron %s TRX: +%d", shortAddr(addr), n)) })
	if err != nil {
		return n1, err
	}
	n2, err := tronStreamSync(ctx, st, g, addr, "trc20", "/transactions/trc20",
		func(txs []trc20Tx) ([]Transfer, int64, error) { return trc20Transfers(ctx, st, txs) },
		func(n int) { progress(fmt.Sprintf("Tron %s TRC20: +%d", shortAddr(addr), n)) })
	return n1 + n2, err
}

func tronTransfers(txs []tronTx, trxID int64) ([]Transfer, int64, error) {
	var out []Transfer
	var maxTS int64
	for _, x := range txs {
		maxTS = max(maxTS, x.BlockTimestamp)
		if x.TxID == "" || len(x.RawData.Contract) == 0 {
			continue // internal tx entries have another shape
		}
		c := x.RawData.Contract[0]
		v := c.Parameter.Value
		t := Transfer{
			UID:     "tron:" + x.TxID + ":native",
			Chain:   "tron",
			TxHash:  x.TxID,
			TS:      x.BlockTimestamp / 1000,
			From:    tronFromHex(v.OwnerAddress),
			AssetID: trxID,
			FeeRaw:  "0",
		}
		switch c.Type {
		case "TransferContract":
			t.To = tronFromHex(v.ToAddress)
			t.AmountRaw = strconv.FormatInt(v.Amount, 10)
		case "TriggerSmartContract":
			t.To = tronFromHex(v.ContractAddress)
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
	return out, maxTS, nil
}

func trc20Transfers(ctx context.Context, st *Store, txs []trc20Tx) ([]Transfer, int64, error) {
	var out []Transfer
	var maxTS int64
	for _, x := range txs {
		maxTS = max(maxTS, x.BlockTimestamp)
		id, err := st.AssetID(ctx, Asset{Chain: "tron", Contract: x.TokenInfo.Address,
			Symbol: tokenSymbol(x.TokenInfo.Symbol), Decimals: x.TokenInfo.Decimals})
		if err != nil {
			return nil, 0, err
		}
		amount := x.Value
		if amount == "" {
			amount = "0"
		}
		out = append(out, Transfer{
			// TronGrid has no log index here, so the transfer itself is the key.
			UID:       fmt.Sprintf("tron:%s:trc20:%s:%s:%s:%s", x.TransactionID, x.TokenInfo.Address, x.From, x.To, amount),
			Chain:     "tron",
			TxHash:    x.TransactionID,
			TS:        x.BlockTimestamp / 1000,
			From:      x.From,
			To:        x.To,
			AssetID:   id,
			AmountRaw: amount,
			FeeRaw:    "0",
		})
	}
	return out, maxTS, nil
}
