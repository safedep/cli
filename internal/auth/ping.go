package auth

import (
	"context"
	"fmt"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/cloud"
)

const pingTimeout = 10 * time.Second

// pingDataPlane issues a control-tower Ping over the supplied data-plane
// client. A successful round trip authenticates the API key + tenant.
func pingDataPlane(client *cloud.Client) error {
	if client == nil {
		return fmt.Errorf("auth: nil data plane client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	svc := controltowerv1grpc.NewPingServiceClient(client.Connection())
	_, err := svc.Ping(ctx, &controltowerv1.PingRequest{Id: "safedep-cli-verify"})
	return err
}
