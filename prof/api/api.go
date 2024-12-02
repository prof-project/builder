package api

import (
	"context"
	"fmt"
	"io"

	builderApi "github.com/attestantio/go-builder-client/api"
	builderApiDeneb "github.com/attestantio/go-builder-client/api/deneb"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	pb "github.com/ethereum/go-ethereum/prof/profpb" // Add this import
	"github.com/ethereum/go-ethereum/prof/utils"
	fbutils "github.com/flashbots/go-boost-utils/utils"
	"github.com/holiman/uint256"
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
// TODO: check all the returned errors and status codes
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

		pbsBlock, _ := engine.ExecutionPayloadV3ToBlock(denebRequest.PayloadBundle.ExecutionPayload, denebRequest.PayloadBundle.BlobsBundle, denebRequest.ParentBeaconBlockRoot)

		// Print Block to see that reconstruction from gRPC works
		fmt.Printf("got ExecutionPayloadV3ToBlock %+v\n", pbsBlock)

		profBundle, err := s.getProfBundle()
		if err != nil {
			return status.Errorf(codes.Internal, "Error retrieving PROF bundle: %v", err)
		}

		// Convert Deneb Request and Prof transactions to Block
		block, err := engine.ExecutionPayloadV3ToBlockProf(denebRequest.PayloadBundle.ExecutionPayload, profBundle, denebRequest.PayloadBundle.BlobsBundle, denebRequest.ParentBeaconBlockRoot)
		if err != nil {
			return err
		}

		fmt.Printf("PROF block before execution %+v\n", block)

		profValidationResp, err := s.validateProfBlock(block, denebRequest.BidTrace.ProposerFeeRecipient, 0 /* TODO: suitable gaslimit?*/)
		if err != nil {
			return err
		}

		enrichedPayload := profValidationResp.ExecutionPayloadAndBlobsBundle
		// TODO: save the execution payload.

		enrichedPayloadHeader, err := fbutils.PayloadToPayloadHeader(
			&builderApi.VersionedExecutionPayload{ //nolint:exhaustivestruct
				Version: spec.DataVersionDeneb,
				Deneb:   enrichedPayload.ExecutionPayload,
			},
		)
		if err != nil {
			return err
		}

		// TODO - Currently returns empty DUMMY response, need to get tx from prof bundle
		resp := &pb.EnrichBlockResponse{
			Uuid:             req.Uuid,
			EnrichedHeader:   utils.HeaderToProtoHeader(enrichedPayloadHeader.Deneb),
			Commitments:      utils.CommitmentsToProtoCommitments(enrichedPayload.BlobsBundle.Commitments),
			EnrichedBidValue: profValidationResp.Value.String(),
		}

		if err := stream.Send(resp); err != nil {
			return status.Errorf(codes.Internal, "Failed to send response: %v", err)
		}
	}
}

func (s *BundleMergerServer) getProfBundle() ([][]byte, error) {
	// TODO : will need to come from the sequencer
	return make([][]byte, 0), nil
}

type ProfSimResp struct {
	Value                          *uint256.Int
	ExecutionPayloadAndBlobsBundle *builderApiDeneb.ExecutionPayloadAndBlobsBundle
}

// TODO : invalid profTransactions are not being filtered out currently, change the validateProfBlock method to pluck out the invalid transactions, blockhash would also change in that case
func (s *BundleMergerServer) validateProfBlock(profBlock *types.Block, proposerFeeRecipient bellatrix.ExecutionAddress, registeredGasLimit uint64) (*ProfSimResp, error) {

	feeRecipient := common.BytesToAddress(proposerFeeRecipient[:])

	var vmconfig vm.Config

	value, header, err := s.eth.BlockChain().SimulateProfBlock(profBlock, feeRecipient, registeredGasLimit, vmconfig, true /* prof uses balance diff*/, true /* exclude withdrawals */)

	if err != nil {
		return nil, err
	}
	profBlockFinal := profBlock.WithSeal(header)
	// log.Info("validated prof block", "number", profBlockFinal.NumberU64(), "parentHash", profBlockFinal.ParentHash())

	valueBig := value.ToBig()

	executableData := engine.BlockToExecutableData(profBlockFinal, valueBig, []*types.BlobTxSidecar{})

	payload, err := getDenebPayload(executableData)
	if err != nil {
		// log.Error("could not format execution payload", "err", err)
		return nil, err
	}

	return &ProfSimResp{value, payload}, nil
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

func getDenebPayload(
	data *engine.ExecutionPayloadEnvelope,
) (*builderApiDeneb.ExecutionPayloadAndBlobsBundle, error) {
	payload := data.ExecutionPayload
	blobsBundle := data.BlobsBundle
	baseFeePerGas, overflow := uint256.FromBig(payload.BaseFeePerGas)
	if overflow {
		return nil, fmt.Errorf("base fee per gas overflow")
	}
	transactions := make([]bellatrix.Transaction, len(payload.Transactions))
	for i, tx := range payload.Transactions {
		transactions[i] = bellatrix.Transaction(tx)
	}
	withdrawals := make([]*capella.Withdrawal, len(payload.Withdrawals))
	for i, wd := range payload.Withdrawals {
		withdrawals[i] = &capella.Withdrawal{
			Index:          capella.WithdrawalIndex(wd.Index),
			ValidatorIndex: phase0.ValidatorIndex(wd.Validator),
			Address:        bellatrix.ExecutionAddress(wd.Address),
			Amount:         phase0.Gwei(wd.Amount),
		}
	}
	return &builderApiDeneb.ExecutionPayloadAndBlobsBundle{
		ExecutionPayload: &deneb.ExecutionPayload{
			ParentHash:    [32]byte(payload.ParentHash),
			FeeRecipient:  [20]byte(payload.FeeRecipient),
			StateRoot:     [32]byte(payload.StateRoot),
			ReceiptsRoot:  [32]byte(payload.ReceiptsRoot),
			LogsBloom:     types.BytesToBloom(payload.LogsBloom),
			PrevRandao:    [32]byte(payload.Random),
			BlockNumber:   payload.Number,
			GasLimit:      payload.GasLimit,
			GasUsed:       payload.GasUsed,
			Timestamp:     payload.Timestamp,
			ExtraData:     payload.ExtraData,
			BaseFeePerGas: baseFeePerGas,
			BlockHash:     [32]byte(payload.BlockHash),
			Transactions:  transactions,
			Withdrawals:   withdrawals,
			BlobGasUsed:   *payload.BlobGasUsed,
			ExcessBlobGas: *payload.ExcessBlobGas,
		},
		BlobsBundle: getBlobsBundle(blobsBundle),
	}, nil

}

func getBlobsBundle(blobsBundle *engine.BlobsBundleV1) *builderApiDeneb.BlobsBundle {
	commitments := make([]deneb.KZGCommitment, len(blobsBundle.Commitments))
	proofs := make([]deneb.KZGProof, len(blobsBundle.Proofs))
	blobs := make([]deneb.Blob, len(blobsBundle.Blobs))

	// we assume the lengths for blobs bundle is validated beforehand to be the same
	for i := range blobsBundle.Blobs {
		var commitment deneb.KZGCommitment
		copy(commitment[:], blobsBundle.Commitments[i][:])
		commitments[i] = commitment

		var proof deneb.KZGProof
		copy(proof[:], blobsBundle.Proofs[i][:])
		proofs[i] = proof

		var blob deneb.Blob
		copy(blob[:], blobsBundle.Blobs[i][:])
		blobs[i] = blob
	}
	return &builderApiDeneb.BlobsBundle{
		Commitments: commitments,
		Proofs:      proofs,
		Blobs:       blobs,
	}
}
