package utils

import (
	"fmt"
	"math/big"

	builderApiDeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderApiV1 "github.com/attestantio/go-builder-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	consensus "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/prof/profpb"
	"github.com/holiman/uint256"
)

type DenebEnrichBlockRequest struct {
	Uuid                  string
	PayloadBundle         *builderApiDeneb.ExecutionPayloadAndBlobsBundle
	BidTrace              *builderApiV1.BidTrace
	ParentBeaconBlockRoot common.Hash
}

func ExecutableDataToExecutionPayloadV3(data *engine.ExecutableData) (*deneb.ExecutionPayload, error) {
	transactionData := make([]bellatrix.Transaction, len(data.Transactions))
	for i, tx := range data.Transactions {
		transactionData[i] = bellatrix.Transaction(tx)
	}

	withdrawalData := make([]*capella.Withdrawal, len(data.Withdrawals))
	for i, withdrawal := range data.Withdrawals {
		withdrawalData[i] = &capella.Withdrawal{
			Index:          capella.WithdrawalIndex(withdrawal.Index),
			ValidatorIndex: phase0.ValidatorIndex(withdrawal.Validator),
			Address:        bellatrix.ExecutionAddress(withdrawal.Address),
			Amount:         phase0.Gwei(withdrawal.Amount),
		}
	}

	return &deneb.ExecutionPayload{
		ParentHash:    [32]byte(data.ParentHash),
		FeeRecipient:  [20]byte(data.FeeRecipient),
		StateRoot:     [32]byte(data.StateRoot),
		ReceiptsRoot:  [32]byte(data.ReceiptsRoot),
		LogsBloom:     types.BytesToBloom(data.LogsBloom),
		PrevRandao:    [32]byte(data.Random),
		BlockNumber:   data.Number,
		GasLimit:      data.GasLimit,
		GasUsed:       data.GasUsed,
		Timestamp:     data.Timestamp,
		ExtraData:     data.ExtraData,
		BaseFeePerGas: uint256.MustFromBig(data.BaseFeePerGas),
		BlockHash:     [32]byte(data.BlockHash),
		Transactions:  transactionData,
		Withdrawals:   withdrawalData,
		BlobGasUsed:   *data.BlobGasUsed,
		ExcessBlobGas: *data.ExcessBlobGas,
	}, nil
}

// Currently only works for deneb
func DenebRequestToProtoRequest(request *DenebEnrichBlockRequest) (*profpb.EnrichBlockRequest, error) {
	transactions := make([]*profpb.CompressTx, len(request.PayloadBundle.ExecutionPayload.Transactions))
	for i, tx := range request.PayloadBundle.ExecutionPayload.Transactions {
		transactions[i] = &profpb.CompressTx{
			RawData: tx,
			ShortID: 0,
		}
	}

	withdrawals := make([]*profpb.Withdrawal, len(request.PayloadBundle.ExecutionPayload.Withdrawals))
	for i, withdrawal := range request.PayloadBundle.ExecutionPayload.Withdrawals {
		withdrawals[i] = &profpb.Withdrawal{
			ValidatorIndex: uint64(withdrawal.ValidatorIndex),
			Index:          uint64(withdrawal.Index),
			Amount:         uint64(withdrawal.Amount),
			Address:        withdrawal.Address[:],
		}
	}

	return &profpb.EnrichBlockRequest{
		Uuid: request.Uuid,
		PayloadBundle: &profpb.ExecutionPayloadAndBlobsBundle{
			ExecutionPayload: &profpb.ExecutionPayload{
				ParentHash:    request.PayloadBundle.ExecutionPayload.ParentHash[:],
				StateRoot:     request.PayloadBundle.ExecutionPayload.StateRoot[:],
				ReceiptsRoot:  request.PayloadBundle.ExecutionPayload.ReceiptsRoot[:],
				LogsBloom:     request.PayloadBundle.ExecutionPayload.LogsBloom[:],
				PrevRandao:    request.PayloadBundle.ExecutionPayload.PrevRandao[:],
				BaseFeePerGas: uint256ToIntToByteSlice(request.PayloadBundle.ExecutionPayload.BaseFeePerGas),
				FeeRecipient:  request.PayloadBundle.ExecutionPayload.FeeRecipient[:],
				BlockHash:     request.PayloadBundle.ExecutionPayload.BlockHash[:],
				ExtraData:     request.PayloadBundle.ExecutionPayload.ExtraData,
				BlockNumber:   request.PayloadBundle.ExecutionPayload.BlockNumber,
				GasLimit:      request.PayloadBundle.ExecutionPayload.GasLimit,
				Timestamp:     request.PayloadBundle.ExecutionPayload.Timestamp,
				GasUsed:       request.PayloadBundle.ExecutionPayload.GasUsed,
				Transactions:  transactions,
				Withdrawals:   withdrawals,
				BlobGasUsed:   request.PayloadBundle.ExecutionPayload.BlobGasUsed,
				ExcessBlobGas: request.PayloadBundle.ExecutionPayload.ExcessBlobGas,
			},
			BlobsBundle: DenebBlobsBundleToProtoBlobsBundle(request.PayloadBundle.BlobsBundle),
		},
		BidTrace: &profpb.BidTrace{
			Slot:                 request.BidTrace.Slot,
			ParentHash:           request.BidTrace.ParentHash[:],
			BlockHash:            request.BidTrace.BlockHash[:],
			BuilderPubkey:        request.BidTrace.BuilderPubkey[:],
			ProposerPubkey:       request.BidTrace.ProposerPubkey[:],
			ProposerFeeRecipient: request.BidTrace.ProposerFeeRecipient[:],
			GasLimit:             request.BidTrace.GasLimit,
			GasUsed:              request.BidTrace.GasUsed,
			Value:                request.BidTrace.Value.Hex(),
		},
		ParentBeaconBlockRoot: request.ParentBeaconBlockRoot[:],
	}, nil
}

// Currently only works for deneb
func ProtoRequestToDenebRequest(request *profpb.EnrichBlockRequest) (*DenebEnrichBlockRequest, error) {
	transactions := make([]bellatrix.Transaction, len(request.PayloadBundle.ExecutionPayload.Transactions))
	for index, tx := range request.PayloadBundle.ExecutionPayload.Transactions {
		transactions[index] = tx.RawData
	}

	// Withdrawal is defined in capella spec
	// https://github.com/attestantio/go-eth2-client/blob/21f7dd480fed933d8e0b1c88cee67da721c80eb2/spec/deneb/executionpayload.go#L42
	withdrawals := make([]*capella.Withdrawal, len(request.PayloadBundle.ExecutionPayload.Withdrawals))
	for index, withdrawal := range request.PayloadBundle.ExecutionPayload.Withdrawals {
		withdrawals[index] = &capella.Withdrawal{
			ValidatorIndex: phase0.ValidatorIndex(withdrawal.ValidatorIndex),
			Index:          capella.WithdrawalIndex(withdrawal.Index),
			Amount:         phase0.Gwei(withdrawal.Amount),
			Address:        b20(withdrawal.Address),
		}
	}

	// BlobsBundle
	blobsBundle := &builderApiDeneb.BlobsBundle{
		Commitments: make([]consensus.KZGCommitment, len(request.PayloadBundle.BlobsBundle.Commitments)),
		Proofs:      make([]consensus.KZGProof, len(request.PayloadBundle.BlobsBundle.Proofs)),
		Blobs:       make([]consensus.Blob, len(request.PayloadBundle.BlobsBundle.Blobs)),
	}
	for index, commitment := range request.PayloadBundle.BlobsBundle.Commitments {
		copy(blobsBundle.Commitments[index][:], commitment)
	}

	for index, proof := range request.PayloadBundle.BlobsBundle.Proofs {
		copy(blobsBundle.Proofs[index][:], proof)
	}

	for index, blob := range request.PayloadBundle.BlobsBundle.Blobs {
		copy(blobsBundle.Blobs[index][:], blob)
	}

	value, err := uint256.FromHex(request.BidTrace.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert deneb block value %s to uint256: %s", request.BidTrace.Value, err.Error())
	}

	return &DenebEnrichBlockRequest{
		PayloadBundle: &builderApiDeneb.ExecutionPayloadAndBlobsBundle{
			ExecutionPayload: &deneb.ExecutionPayload{
				ParentHash:    b32(request.PayloadBundle.ExecutionPayload.ParentHash),
				StateRoot:     b32(request.PayloadBundle.ExecutionPayload.StateRoot),
				ReceiptsRoot:  b32(request.PayloadBundle.ExecutionPayload.ReceiptsRoot),
				LogsBloom:     b256(request.PayloadBundle.ExecutionPayload.LogsBloom),
				PrevRandao:    b32(request.PayloadBundle.ExecutionPayload.PrevRandao),
				BaseFeePerGas: byteSliceToUint256Int(request.PayloadBundle.ExecutionPayload.BaseFeePerGas),
				FeeRecipient:  b20(request.PayloadBundle.ExecutionPayload.FeeRecipient),
				BlockHash:     b32(request.PayloadBundle.ExecutionPayload.BlockHash),
				ExtraData:     request.PayloadBundle.ExecutionPayload.ExtraData,
				BlockNumber:   request.PayloadBundle.ExecutionPayload.BlockNumber,
				GasLimit:      request.PayloadBundle.ExecutionPayload.GasLimit,
				Timestamp:     request.PayloadBundle.ExecutionPayload.Timestamp,
				GasUsed:       request.PayloadBundle.ExecutionPayload.GasUsed,
				Transactions:  transactions,
				Withdrawals:   withdrawals,
				BlobGasUsed:   request.PayloadBundle.ExecutionPayload.BlobGasUsed,
				ExcessBlobGas: request.PayloadBundle.ExecutionPayload.ExcessBlobGas,
			},
			BlobsBundle: blobsBundle,
		},
		BidTrace: &builderApiV1.BidTrace{
			Slot:                 request.BidTrace.Slot,
			ParentHash:           b32(request.BidTrace.ParentHash),
			BlockHash:            b32(request.BidTrace.BlockHash),
			BuilderPubkey:        b48(request.BidTrace.BuilderPubkey),
			ProposerPubkey:       b48(request.BidTrace.ProposerPubkey),
			ProposerFeeRecipient: b20(request.BidTrace.ProposerFeeRecipient),
			GasLimit:             request.BidTrace.GasLimit,
			GasUsed:              request.BidTrace.GasUsed,
			Value:                value,
		},
		ParentBeaconBlockRoot: common.BytesToHash(request.ParentBeaconBlockRoot),
	}, nil
}

func DenebBlobsBundleToProtoBlobsBundle(blobBundle *builderApiDeneb.BlobsBundle) *profpb.BlobsBundle {
	protoBlobsBundle := &profpb.BlobsBundle{
		Commitments: make([][]byte, len(blobBundle.Commitments)),
		Proofs:      make([][]byte, len(blobBundle.Proofs)),
		Blobs:       make([][]byte, len(blobBundle.Blobs)),
	}

	for i := range blobBundle.Commitments {
		protoBlobsBundle.Commitments[i] = blobBundle.Commitments[i][:]
	}

	for i := range blobBundle.Proofs {
		protoBlobsBundle.Proofs[i] = blobBundle.Proofs[i][:]
	}

	for i := range blobBundle.Blobs {
		protoBlobsBundle.Blobs[i] = blobBundle.Blobs[i][:]
	}

	return protoBlobsBundle
}

// b20 converts a byte slice to a [20]byte.
func b20(b []byte) [20]byte {
	out := [20]byte{}
	copy(out[:], b)
	return out
}

// b32 converts a byte slice to a [32]byte.
func b32(b []byte) [32]byte {
	out := [32]byte{}
	copy(out[:], b)
	return out
}

// b48 converts a byte slice to a [48]byte.
func b48(b []byte) [48]byte {
	out := [48]byte{}
	copy(out[:], b)
	return out
}

// b96 converts a byte slice to a [96]byte.
func b96(b []byte) [96]byte {
	out := [96]byte{}
	copy(out[:], b)
	return out
}

// b256 converts a byte slice to a [256]byte.
func b256(b []byte) [256]byte {
	out := [256]byte{}
	copy(out[:], b)
	return out
}

// uint256ToIntToByteSlice converts a *uint256.Int to a byte slice.
func uint256ToIntToByteSlice(u *uint256.Int) []byte {
	if u == nil {
		return nil
	}
	// Convert the uint256.Int to a byte slice.
	// The Bytes method returns the absolute value as a big-endian byte slice.
	return u.Bytes()
}

// byteSliceToUint256Int converts a byte slice to a *uint256.Int.
func byteSliceToUint256Int(b []byte) *uint256.Int {
	u256, _ := uint256.FromBig(new(big.Int).SetBytes(b))
	return u256
}
