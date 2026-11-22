package agentrpc

import (
	"testing"

	providerchecksv1 "github.com/rudolfkova/mytonprovider-backend/contracts/gen/go/providerchecks/v1"
)

func TestMergeStorageRatesRows_PrefersOkResult(t *testing.T) {
	pubkeys := []string{"pub-1"}
	responses := [][]*providerchecksv1.StorageRatesResult{
		{
			{
				ProviderPubkey: "pub-1",
				Ok:             false,
				Details:        "context deadline exceeded",
			},
		},
		{
			{
				ProviderPubkey: "PUB-1",
				Ok:             true,
				Available:      true,
				MinSpan:        10,
				MaxSpan:        20,
			},
		},
	}

	merged := mergeStorageRatesRows(pubkeys, responses)
	row, ok := merged["PUB-1"]
	if !ok {
		t.Fatalf("expected merged row for PUB-1")
	}
	if !row.OK {
		t.Fatalf("expected merged row to be ok=true")
	}
	if !row.Available {
		t.Fatalf("expected available=true")
	}
	if row.MinSpan != 10 || row.MaxSpan != 20 {
		t.Fatalf("unexpected merged spans: min=%d max=%d", row.MinSpan, row.MaxSpan)
	}
}

func TestMergeStorageRatesRows_KeepsFailureWhenNoOk(t *testing.T) {
	pubkeys := []string{"pub-1"}
	responses := [][]*providerchecksv1.StorageRatesResult{
		{
			{
				ProviderPubkey: "pub-1",
				Ok:             false,
				Details:        "timeout-a",
			},
		},
		{
			{
				ProviderPubkey: "pub-1",
				Ok:             false,
				Details:        "timeout-b",
			},
		},
	}

	merged := mergeStorageRatesRows(pubkeys, responses)
	row, ok := merged["PUB-1"]
	if !ok {
		t.Fatalf("expected merged row for PUB-1")
	}
	if row.OK {
		t.Fatalf("expected ok=false")
	}
	if row.Details != "timeout-a" {
		t.Fatalf("expected first failure details, got %q", row.Details)
	}
}

func TestMergeStorageInfoRows_PrefersOkResult(t *testing.T) {
	queries := []StorageInfoQuery{
		{ProviderPubkey: "Aa", ContractAddress: "EQ1"},
	}
	responses := [][]*providerchecksv1.StorageInfoResult{
		{
			{
				ProviderPubkey:  "Aa",
				ContractAddress: "EQ1",
				Ok:              false,
				Details:         "timeout",
			},
		},
		{
			{
				ProviderPubkey:  "aa",
				ContractAddress: "EQ1",
				Ok:              true,
				Status:          "ok",
				Downloaded:      42,
			},
		},
	}

	merged := mergeStorageInfoRows(queries, responses)
	if len(merged) != 1 {
		t.Fatalf("expected 1 row, got %d", len(merged))
	}
	if !merged[0].OK {
		t.Fatalf("expected ok=true")
	}
	if merged[0].Downloaded != 42 {
		t.Fatalf("unexpected downloaded=%d", merged[0].Downloaded)
	}
}
