package api

import (
	"context"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/eth"
	pb "github.com/ethereum/go-ethereum/prof/profpb" // Add this import
	"github.com/ethereum/go-ethereum/prof/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// BundleMergerServer implements the BundleMerger gRPC service
type BundleMergerServer struct {
	pb.UnimplementedBundleMergerServer
	eth *eth.Ethereum
}

// NewBundleMergerServer creates a new BundleMergerServer
func NewBundleMergerServer(ethInstance *eth.Ethereum) *BundleMergerServer {
	return &BundleMergerServer{
		eth: ethInstance,
	}
}

// EnrichBlock implements the EnrichBlock RPC method as a bidirectional streaming RPC
func (s *BundleMergerServer) EnrichBlock(stream pb.BundleMerger_EnrichBlockServer) error {

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil // Client closed the stream
		}
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to receive request: %v", err)
		}

		// Convert Proto Request from gRPC to DenebRequest
		denebRequest, err := utils.ProtoRequestToDenebRequest(req)
		if err != nil {
			return err
		}

		// Convert Deneb Request to Block
		block, err := engine.ExecutionPayloadV3ToBlock(denebRequest.PayloadBundle.ExecutionPayload, denebRequest.PayloadBundle.BlobsBundle, denebRequest.ParentBeaconBlockRoot)
		if err != nil {
			return err
		}

		// Print Block to see that reqconstruction from gRPC works
		fmt.Printf("got ExecutionPayloadV3ToBlock %+v\n", block)

		// TODO - Currently returns empty DUMMY response, need to get tx from prof bundle
		resp := &pb.EnrichBlockResponse{
			Uuid:             req.Uuid,
			EnrichedHeader:   &pb.ExecutionPayloadHeader{},
			Commitments:      []*pb.KZGCommitment{},
			EnrichedBidValue: "0",
		}

		if err := stream.Send(resp); err != nil {
			return status.Errorf(codes.Internal, "Failed to send response: %v", err)
		}
	}
}

// GetEnrichedPayload implements the GetEnrichedPayload RPC method
func (s *BundleMergerServer) GetEnrichedPayload(ctx context.Context, req *pb.GetEnrichedPayloadRequest) (*pb.GetEnrichedPayloadResponse, error) {
	// TODO: Implement the logic to get the enriched payload
	// This logic is currently situated in eth/block-validation
	// For now, we'll return a placeholder response

	// Simulating a case where the payload is not found
	return &pb.GetEnrichedPayloadResponse{
		Uuid: req.Uuid,
		PayloadOrEmpty: &pb.GetEnrichedPayloadResponse_Empty{
			Empty: &emptypb.Empty{},
		},
	}, nil

	// Uncomment and modify the following to return an actual payload
	/*
		return &pb.GetEnrichedPayloadResponse{
			Uuid: req.Uuid,
			PayloadOrEmpty: &pb.GetEnrichedPayloadResponse_PayloadBundle{
				PayloadBundle: &pb.ExecutionPayloadAndBlobsBundle{
					ExecutionPayload: &pb.ExecutionPayload{},
					BlobsBundle:      &pb.BlobsBundle{},
				},
			},
		}, nil
	*/
}
