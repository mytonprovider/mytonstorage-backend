package agentrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	providerchecksv1 "github.com/rudolfkova/mytonprovider-backend/contracts/gen/go/providerchecks/v1"
)

const (
	defaultRatesQueryMs       = uint32(7_000)
	defaultStorageInfoQueryMs = uint32(10_000)
)

// Config holds gRPC client settings for mytonprovider-agent.
type Config struct {
	Endpoints      []string
	AuthToken      string
	CACertFile     string
	RequestTimeout time.Duration
}

// StorageRatesRow is one provider rates result from RunStorageRates.
type StorageRatesRow struct {
	OK               bool
	Available        bool
	RatePerMBDay     []byte
	MinBounty        []byte
	SpaceAvailableMB uint64
	MinSpan          uint32
	MaxSpan          uint32
	Details          string
}

// StorageInfoQuery is one notify/download check target.
type StorageInfoQuery struct {
	ProviderPubkey  string
	ContractAddress string
	ByteToProof     uint64
}

// StorageInfoRow is one RequestStorageInfo result.
type StorageInfoRow struct {
	ProviderPubkey  string
	ContractAddress string
	OK              bool
	Status          string
	Reason          string
	Downloaded      uint64
	Proof           []byte
	Details         string
}

// Client calls mytonprovider-agent over gRPC.
// Rates and storage-info RPCs fan out to all agents and merge per provider (prefer ok).
type Client struct {
	agents         []agentClient
	authToken      string
	requestTimeout time.Duration
}

type agentClient struct {
	endpoint string
	conn     *grpc.ClientConn
	client   providerchecksv1.ProviderChecksServiceClient
}

type agentCallError struct {
	endpoint string
	err      error
}

// New dials configured agent endpoints. Endpoints may be empty only for tests that inject a mock.
func New(cfg Config) (*Client, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("agent endpoints are required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, fmt.Errorf("agent auth token is required")
	}
	if strings.TrimSpace(cfg.CACertFile) == "" {
		return nil, fmt.Errorf("agent CA cert file is required")
	}

	creds, err := loadTLSCredentials(cfg.CACertFile)
	if err != nil {
		return nil, err
	}

	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}

	clients := make([]agentClient, 0, len(cfg.Endpoints))
	for _, rawEndpoint := range cfg.Endpoints {
		endpoint := strings.TrimSpace(rawEndpoint)
		if endpoint == "" {
			continue
		}

		conn, err := grpc.NewClient(
			endpoint,
			grpc.WithTransportCredentials(creds),
		)
		if err != nil {
			return nil, fmt.Errorf("dial agent %s: %w", endpoint, err)
		}

		clients = append(clients, agentClient{
			endpoint: endpoint,
			conn:     conn,
			client:   providerchecksv1.NewProviderChecksServiceClient(conn),
		})
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("no valid agent endpoints")
	}

	return &Client{
		agents:         clients,
		authToken:      strings.TrimSpace(cfg.AuthToken),
		requestTimeout: requestTimeout,
	}, nil
}

func (c *Client) Close() error {
	var firstErr error
	for _, a := range c.agents {
		if a.conn == nil {
			continue
		}
		if err := a.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GetStorageRates queries rates for all pubkeys via RunStorageRates on every agent in parallel.
// Per pubkey, the first ok result wins; failed rows are kept only when no agent succeeded.
func (c *Client) GetStorageRates(ctx context.Context, pubkeys []string, bagSize uint64) (map[string]StorageRatesRow, error) {
	if len(pubkeys) == 0 {
		return map[string]StorageRatesRow{}, nil
	}

	req := &providerchecksv1.RunStorageRatesRequest{
		JobId:           uuid.NewString(),
		ProviderPubkeys: pubkeys,
		QuerySize:       bagSize,
		Timeouts: &providerchecksv1.StorageRatesTimeouts{
			QueryTimeoutMs: defaultRatesQueryMs,
		},
	}
	if bagSize == 0 {
		req.QuerySize = 1
	}

	type ratesResp struct {
		resp *providerchecksv1.RunStorageRatesResponse
	}

	responses := make([]ratesResp, 0, len(c.agents))
	errs := make([]agentCallError, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, agent := range c.agents {
		wg.Add(1)
		go func(agent agentClient) {
			defer wg.Done()

			raw, err := c.invoke(ctx, agent, func(callCtx context.Context, cl providerchecksv1.ProviderChecksServiceClient) (interface{}, error) {
				return cl.RunStorageRates(callCtx, req)
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, agentCallError{endpoint: agent.endpoint, err: err})
				mu.Unlock()
				return
			}

			mu.Lock()
			responses = append(responses, ratesResp{resp: raw.(*providerchecksv1.RunStorageRatesResponse)})
			mu.Unlock()
		}(agent)
	}
	wg.Wait()

	if len(responses) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("%s: %w", errs[0].endpoint, errs[0].err)
		}
		return nil, fmt.Errorf("all agents unavailable")
	}

	rows := make([][]*providerchecksv1.StorageRatesResult, 0, len(responses))
	for _, r := range responses {
		if r.resp == nil {
			continue
		}
		rows = append(rows, r.resp.GetResults())
	}

	return mergeStorageRatesRows(pubkeys, rows), nil
}

// RequestStorageInfo notifies providers about storage contracts via every agent in parallel.
// Per provider+contract, the first ok result wins.
func (c *Client) RequestStorageInfo(ctx context.Context, queries []StorageInfoQuery) ([]StorageInfoRow, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	protoQueries := make([]*providerchecksv1.StorageInfoQuery, 0, len(queries))
	for _, q := range queries {
		protoQueries = append(protoQueries, &providerchecksv1.StorageInfoQuery{
			ProviderPubkey:  strings.TrimSpace(q.ProviderPubkey),
			ContractAddress: strings.TrimSpace(q.ContractAddress),
			ByteToProof:     q.ByteToProof,
		})
	}

	req := &providerchecksv1.RequestStorageInfoRequest{
		JobId:   uuid.NewString(),
		Queries: protoQueries,
		Timeouts: &providerchecksv1.StorageInfoTimeouts{
			QueryTimeoutMs: defaultStorageInfoQueryMs,
		},
	}

	type infoResp struct {
		resp *providerchecksv1.RequestStorageInfoResponse
	}

	responses := make([]infoResp, 0, len(c.agents))
	errs := make([]agentCallError, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, agent := range c.agents {
		wg.Add(1)
		go func(agent agentClient) {
			defer wg.Done()

			raw, err := c.invoke(ctx, agent, func(callCtx context.Context, cl providerchecksv1.ProviderChecksServiceClient) (interface{}, error) {
				return cl.RequestStorageInfo(callCtx, req)
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, agentCallError{endpoint: agent.endpoint, err: err})
				mu.Unlock()
				return
			}

			mu.Lock()
			responses = append(responses, infoResp{resp: raw.(*providerchecksv1.RequestStorageInfoResponse)})
			mu.Unlock()
		}(agent)
	}
	wg.Wait()

	if len(responses) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("%s: %w", errs[0].endpoint, errs[0].err)
		}
		return nil, fmt.Errorf("all agents unavailable")
	}

	rows := make([][]*providerchecksv1.StorageInfoResult, 0, len(responses))
	for _, r := range responses {
		if r.resp == nil {
			continue
		}
		rows = append(rows, r.resp.GetResults())
	}

	return mergeStorageInfoRows(queries, rows), nil
}

func mergeStorageRatesRows(pubkeys []string, responses [][]*providerchecksv1.StorageRatesResult) map[string]StorageRatesRow {
	type selected struct {
		row   StorageRatesRow
		valid bool
	}

	byPubkey := make(map[string]selected, len(pubkeys))
	for _, results := range responses {
		for _, r := range results {
			if r == nil {
				continue
			}
			pk := strings.ToUpper(strings.TrimSpace(r.GetProviderPubkey()))
			if pk == "" {
				continue
			}

			row := StorageRatesRow{
				OK:               r.GetOk(),
				Available:        r.GetAvailable(),
				RatePerMBDay:     append([]byte(nil), r.GetRatePerMbDay()...),
				MinBounty:        append([]byte(nil), r.GetMinBounty()...),
				SpaceAvailableMB: r.GetSpaceAvailableMb(),
				MinSpan:          r.GetMinSpan(),
				MaxSpan:          r.GetMaxSpan(),
				Details:          r.GetDetails(),
			}

			current, ok := byPubkey[pk]
			if !ok {
				byPubkey[pk] = selected{row: row, valid: row.OK}
				continue
			}
			if current.valid {
				continue
			}
			if row.OK {
				byPubkey[pk] = selected{row: row, valid: true}
			}
		}
	}

	out := make(map[string]StorageRatesRow, len(pubkeys))
	for _, pk := range pubkeys {
		key := strings.ToUpper(strings.TrimSpace(pk))
		if selected, ok := byPubkey[key]; ok {
			out[key] = selected.row
		}
	}
	return out
}

func storageInfoKey(pubkey, contract string) string {
	return strings.ToLower(strings.TrimSpace(pubkey)) + "|" + strings.TrimSpace(contract)
}

func mergeStorageInfoRows(queries []StorageInfoQuery, responses [][]*providerchecksv1.StorageInfoResult) []StorageInfoRow {
	type selected struct {
		row   StorageInfoRow
		valid bool
	}

	byKey := make(map[string]selected, len(queries))
	for _, results := range responses {
		for _, r := range results {
			if r == nil {
				continue
			}
			pk := strings.TrimSpace(r.GetProviderPubkey())
			contract := strings.TrimSpace(r.GetContractAddress())
			if pk == "" || contract == "" {
				continue
			}

			row := StorageInfoRow{
				ProviderPubkey:  pk,
				ContractAddress: contract,
				OK:              r.GetOk(),
				Status:          r.GetStatus(),
				Reason:          r.GetReason(),
				Downloaded:      r.GetDownloaded(),
				Proof:           append([]byte(nil), r.GetProof()...),
				Details:         r.GetDetails(),
			}

			key := storageInfoKey(pk, contract)
			current, ok := byKey[key]
			if !ok {
				byKey[key] = selected{row: row, valid: row.OK}
				continue
			}
			if current.valid {
				continue
			}
			if row.OK {
				byKey[key] = selected{row: row, valid: true}
			}
		}
	}

	out := make([]StorageInfoRow, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, q := range queries {
		key := storageInfoKey(q.ProviderPubkey, q.ContractAddress)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if selected, ok := byKey[key]; ok {
			out = append(out, selected.row)
		}
	}
	return out
}

func (c *Client) invoke(
	ctx context.Context,
	agent agentClient,
	call func(context.Context, providerchecksv1.ProviderChecksServiceClient) (interface{}, error),
) (interface{}, error) {
	callCtx := ctx
	cancel := func() {}
	if c.requestTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
	}
	defer cancel()
	callCtx = metadata.AppendToOutgoingContext(callCtx, "authorization", "Bearer "+c.authToken)
	return call(callCtx, agent.client)
}

// ParseEndpointsCSV splits a comma-separated agent endpoint list.
func ParseEndpointsCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func loadTLSCredentials(caCertFile string) (credentials.TransportCredentials, error) {
	caPEM, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert file: %w", err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("append CA cert to pool: invalid PEM")
	}

	return credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}), nil
}
