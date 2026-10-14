// Package replaypolicy owns the receipt/recovery window shared with the web UI.
package replaypolicy

import (
	_ "embed"
	"encoding/json"
	"time"
)

//go:embed policy.json
var source []byte

type windows struct {
	ReceiptRetentionDays int `json:"receiptRetentionDays"`
	AutoRetryMaxAgeDays  int `json:"autoRetryMaxAgeDays"`
	GCBatchSize          int `json:"gcBatchSize"`
}

var policy = func() windows {
	var p windows
	if err := json.Unmarshal(source, &p); err != nil {
		panic(err)
	}
	if p.AutoRetryMaxAgeDays <= 0 || p.ReceiptRetentionDays <= p.AutoRetryMaxAgeDays || p.GCBatchSize <= 0 {
		panic("invalid replay policy")
	}
	return p
}()

func Retention() time.Duration { return time.Duration(policy.ReceiptRetentionDays) * 24 * time.Hour }
func AutoRetryMaxAge() time.Duration {
	return time.Duration(policy.AutoRetryMaxAgeDays) * 24 * time.Hour
}
func GCBatchSize() int { return policy.GCBatchSize }
