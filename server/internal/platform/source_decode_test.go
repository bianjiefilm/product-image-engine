package platform

import (
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"strings"
	"testing"
)

func TestSourceDecodeSingleMeaning(t *testing.T) {
	for _, raw := range []string{`{"app_id":"a","app_id":"b"}`, `{"app_id":"a","App_ID":"a"}`, `{"nested":{"amount_minor":1,"Amount_Minor":1}}`, `{"n":1} {}`, `{"n":null}`, `{"n":1.0}`, `{"n":1e2}`, `{"n":-1}`, `{"n":9223372036854775808}`} {
		var dst struct {
			App    string           `json:"app_id"`
			N      int64            `json:"n"`
			Nested map[string]int64 `json:"nested"`
		}
		if err := DecodeSourceJSON([]byte(raw), &dst); !errors.Is(err, si.ErrInvariant) {
			t.Errorf("accepted ambiguous/invalid %s: %v", raw, err)
		}
	}
	var dst struct {
		N int64 `json:"n"`
	}
	if err := DecodeSourceJSON([]byte(`{"n":9007199254740993}`), &dst); err != nil || dst.N != 9007199254740993 {
		t.Fatal(dst, err)
	}
}
func TestSourceDecodeRejectsMalformedKnownFieldTypes(t *testing.T) {
	r := sourceWireRun()
	for _, pair := range [][2]string{{`"hold_id":""`, `"hold_id":{}`}, {`"created_at_unix":`, `"created_at_unix":null,"ignored":`}, {`"quoted_unit_price_minor":25`, `"quoted_unit_price_minor":null`}} {
		raw := strings.Replace(string(sourceBillJSON(r)), pair[0], pair[1], 1)
		if _, e := decodeSourceBill([]byte(raw), r); !errors.Is(e, si.ErrInvariant) {
			t.Fatal("malformed known field accepted", pair[1], e)
		}
	}
}
