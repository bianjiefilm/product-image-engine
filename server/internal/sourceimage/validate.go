package sourceimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func identifier(s string) bool {
	if s == "" || len(s) > 256 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (s Scope) Valid() bool {
	for _, v := range []string{s.AppID, s.TenantID, s.ProjectID, s.UserID, s.PrincipalAccountID, s.PayerAccountID} {
		if !identifier(v) {
			return false
		}
	}
	return true
}
func Fingerprint(in Intent) (string, error) {
	if !in.Scope.Valid() || !identifier(in.RequestKey) || !identifier(in.PricingVersion) || in.Mode != "text_generate" || in.Provider != "modelxing-qwen-image-2.0-v1" || in.Model != "qwen-image-2.0" || in.Capability != "image.generate" || in.Size != "1024*1024" || in.Quantity != 1 || !utf8.ValidString(in.Prompt) || len(in.Prompt) > 16<<10 || strings.TrimSpace(in.Prompt) == "" {
		return "", ErrInvalid
	}
	return digest(in), nil
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func QuoteHash(q Quote) string { return digest(q) }
func ValidateQuote(r Run, q Quote) error {
	if !identifier(q.UsageID) || !identifier(q.QuoteID) || q.UsageKey != r.UsageKey || q.BusinessRef != r.BusinessRef || q.PricingVersion != r.Intent.PricingVersion || q.Currency != "CNY" || q.Quantity != r.Intent.Quantity || q.Quantity <= 0 || q.UnitPriceMinor <= 0 || q.UnitPriceMinor > math.MaxInt64/q.Quantity || q.AmountMinor != q.UnitPriceMinor*q.Quantity || q.QuotedAtUnix <= 0 || q.QuotedAtUnix > math.MaxInt64-300 || q.ExpiresAtUnix != q.QuotedAtUnix+300 {
		return ErrInvalid
	}
	return nil
}

// ParseMinor accepts canonical JSON integer tokens without a float64 round trip.
func ParseMinor(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || (len(raw) > 1 && raw[0] == '0') {
		return 0, ErrInvalid
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			return 0, ErrInvalid
		}
	}
	v, e := strconv.ParseInt(string(raw), 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	return v, nil
}
