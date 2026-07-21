package rules

import "testing"

func TestFingerprintNormalizesVolatileValues(t *testing.T) {
	first := Fingerprint("p", 429, "", `{"error":"rate limited","request_id":"550e8400-e29b-41d4-a716-446655440000","ts":"2026-06-12T10:00:00Z"}`)
	second := Fingerprint("p", 429, "", `{"error":"rate limited","request_id":"650e8400-e29b-41d4-a716-446655440111","ts":"2026-06-13T11:30:00Z"}`)

	if first != second {
		t.Fatal("fingerprint must normalize UUIDs, timestamps, and numbers")
	}
}

func TestFingerprintDistinguishesDifferentFailures(t *testing.T) {
	rateLimit := Fingerprint("p", 429, "", `{"error":"rate limited"}`)
	trialEnded := Fingerprint("p", 429, "", `{"error":"trial ended"}`)

	if rateLimit == trialEnded {
		t.Fatal("different errors must have different fingerprints")
	}
}
