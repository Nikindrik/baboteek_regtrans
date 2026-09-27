package mlclient

import (
	"context"
	"fmt"
	"time"

	pb "baboteek_regtrans/pkg/api/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.MLInferenceClient
}

func New(host string, port int) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize gRPC client for %s: %w", addr, err)
	}

	c := &Client{
		conn:   conn,
		client: pb.NewMLInferenceClient(conn),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := c.Health(ctx); err != nil {
		return c, fmt.Errorf("ML service not ready at %s: %w", addr, err)
	}

	return c, nil
}

func (c *Client) Health(ctx context.Context) (*pb.HealthResponse, error) {
	return c.client.Health(ctx, &pb.HealthRequest{})
}

// Ingest использует таймаут, переданный в ctx из app.go
func (c *Client) Ingest(ctx context.Context, req *pb.IngestRequest) (*pb.IngestResponse, error) {
	return c.client.Ingest(ctx, req)
}

// Predict использует таймаут, переданный в ctx из app.go
func (c *Client) Predict(ctx context.Context, req *pb.PredictRequest) (*pb.PredictionResponse, error) {
	return c.client.Predict(ctx, req)
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
