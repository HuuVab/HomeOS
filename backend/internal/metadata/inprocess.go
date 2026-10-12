package metadata

import (
	"context"
	"net"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// NewInProcessClient keeps the internal metadata RPC off all network interfaces.
// The HTTP gateway authenticates users before providing an owner ID.
func NewInProcessClient(db *store.Store, files *drive.Store) (metadatapb.MetadataServiceClient, func(), error) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	metadatapb.RegisterMetadataServiceServer(server, &Service{DB: db, Files: files})
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		listener.Close()
		return nil, nil, err
	}
	close := func() {
		connection.Close()
		server.Stop()
		listener.Close()
	}
	return metadatapb.NewMetadataServiceClient(connection), close, nil
}
