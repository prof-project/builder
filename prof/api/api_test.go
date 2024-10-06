package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	builderApiDeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderApiV1 "github.com/attestantio/go-builder-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	beaconConsensus "github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	pb "github.com/ethereum/go-ethereum/prof/profpb"
	"github.com/ethereum/go-ethereum/prof/utils"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

var (
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr   = crypto.PubkeyToAddress(testKey.PublicKey)

	// validator
	testValidatorKey, _ = crypto.HexToECDSA("28c3cd61b687fdd03488e167a5d84f50269df2a4c29a2cfb1390903aa775c5d0")
	testValidatorAddr   = crypto.PubkeyToAddress(testValidatorKey.PublicKey)

	// builder
	testBuilderKeyHex = "0bfbbbc68fefd990e61ba645efb84e0a62e94d5fff02c9b1da8eb45fea32b4e0"
	testBuilderKey, _ = crypto.HexToECDSA(testBuilderKeyHex)
	testBuilderAddr   = crypto.PubkeyToAddress(testBuilderKey.PublicKey)

	// balance
	testBalance = big.NewInt(2e18)

	// This EVM code generates a log when the contract is created.
	logCode = common.Hex2Bytes("60606040525b7f24ec1d3ff24c2f6ff210738839dbc339cd45a5294d85c79361016243157aae7b60405180905060405180910390a15b600a8060416000396000f360606040526008565b00")
)

func TestEnrichBlock(t *testing.T) {
	// Set up a simulated backend
	genesis, blocks := generateMergeChain(10, true)

	// Set cancun time to last block + 5 seconds
	cancunTime := blocks[len(blocks)-1].Time() + 5
	genesis.Config.ShanghaiTime = &cancunTime
	genesis.Config.CancunTime = &cancunTime
	os.Setenv("BUILDER_TX_SIGNING_KEY", testBuilderKeyHex)

	n, ethservice := startEthService(t, genesis, blocks)
	ethservice.Merger().ReachTTD()
	defer n.Close()

	// Create a new BundleMergerServer
	// Instead of block-validation API, bootstrap grpc server
	server := NewBundleMergerServer(ethservice)

	// Set up a buffer connection for gRPC
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pb.RegisterBundleMergerServer(s, server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Errorf("Server exited with error: %v", err)
		}
	}()

	// Set up a client connection to the server
	ctx := context.Background()
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewBundleMergerClient(conn)

	// Start the EnrichBlock stream
	stream, err := client.EnrichBlock(ctx)
	require.NoError(t, err)

	// Create a sample EnrichBlockRequest
	parent := ethservice.BlockChain().CurrentHeader()

	server.eth.APIBackend.Miner().SetEtherbase(testBuilderAddr)

	statedb, _ := ethservice.BlockChain().StateAt(parent.Root)
	nonce := statedb.GetNonce(testAddr)

	tx1, _ := types.SignTx(types.NewTransaction(nonce, common.Address{0x16}, big.NewInt(10), 21000, big.NewInt(2*params.InitialBaseFee), nil), types.LatestSigner(ethservice.BlockChain().Config()), testKey)
	ethservice.TxPool().Add([]*types.Transaction{tx1}, true, true, false)

	cc, _ := types.SignTx(types.NewContractCreation(nonce+1, new(big.Int), 1000000, big.NewInt(2*params.InitialBaseFee), logCode), types.LatestSigner(ethservice.BlockChain().Config()), testKey)
	ethservice.TxPool().Add([]*types.Transaction{cc}, true, true, false)

	baseFee := eip1559.CalcBaseFee(params.AllEthashProtocolChanges, parent)
	tx2, _ := types.SignTx(types.NewTransaction(nonce+2, testAddr, big.NewInt(10), 21000, baseFee, nil), types.LatestSigner(ethservice.BlockChain().Config()), testKey)

	ethservice.TxPool().Add([]*types.Transaction{tx2}, true, true, false)

	withdrawals := []*types.Withdrawal{
		{
			Index:     0,
			Validator: 1,
			Amount:    100,
			Address:   testAddr,
		},
		{
			Index:     1,
			Validator: 1,
			Amount:    100,
			Address:   testAddr,
		},
	}

	execData, err := assembleBlock(server, parent.Hash(), &engine.PayloadAttributes{
		Timestamp:             parent.Time + 5,
		Withdrawals:           withdrawals,
		SuggestedFeeRecipient: testValidatorAddr,
		BeaconRoot:            &common.Hash{42},
	})
	require.NoError(t, err)
	require.EqualValues(t, len(execData.Withdrawals), 2)
	require.EqualValues(t, len(execData.Transactions), 4)

	payload, err := utils.ExecutableDataToExecutionPayloadV3(execData)
	require.NoError(t, err)

	proposerAddr := bellatrix.ExecutionAddress{}
	copy(proposerAddr[:], testValidatorAddr.Bytes())

	denebRequest := &utils.DenebEnrichBlockRequest{
		Uuid: "test-uuid",
		PayloadBundle: &builderApiDeneb.ExecutionPayloadAndBlobsBundle{
			ExecutionPayload: payload,
			BlobsBundle: &builderApiDeneb.BlobsBundle{
				Commitments: make([]deneb.KZGCommitment, 0),
				Proofs:      make([]deneb.KZGProof, 0),
				Blobs:       make([]deneb.Blob, 0),
			},
		},
		BidTrace: &builderApiV1.BidTrace{ // Use BidTrace instead of ProfBundle
			ParentHash:           phase0.Hash32(execData.ParentHash),
			BlockHash:            phase0.Hash32(execData.BlockHash),
			ProposerFeeRecipient: proposerAddr,
			GasLimit:             execData.GasLimit,
			GasUsed:              execData.GasUsed,
			// This value is actual profit + 1, validation should fail
			Value: uint256.NewInt(132912184722469),
		},
		ParentBeaconBlockRoot: common.Hash{42},
	}

	// Convert to gRPC compatible request
	protoRequest, err := utils.DenebRequestToProtoRequest(denebRequest)
	require.NoError(t, err)

	req := protoRequest

	// Send the request
	err = stream.Send(req)
	require.NoError(t, err)

	// Receive the response
	resp, err := stream.Recv()

	// Print the response
	fmt.Printf("Response: %+v\n", resp)
	require.NoError(t, err)

	// Verify the response
	require.Equal(t, req.Uuid, resp.Uuid)

	// TODO: check enriched header, only once it is implemented
	// require.NotNil(t, resp.EnrichedHeader)
	// require.NotEmpty(t, resp.Commitments)
	// require.NotEmpty(t, resp.EnrichedBidValue)

	// Close the stream
	err = stream.CloseSend()
	require.NoError(t, err)
}

// Helper functions from block-validation/api_test.go

func generateMergeChain(n int, merged bool) (*core.Genesis, []*types.Block) {
	config := *params.AllEthashProtocolChanges
	engine := consensus.Engine(beaconConsensus.New(ethash.NewFaker()))
	if merged {
		config.TerminalTotalDifficulty = common.Big0
		config.TerminalTotalDifficultyPassed = true
		engine = beaconConsensus.NewFaker()
	}
	genesis := &core.Genesis{
		Config: &config,
		Alloc: types.GenesisAlloc{
			testAddr:                         {Balance: testBalance},
			params.BeaconRootsStorageAddress: {Balance: common.Big0, Code: common.Hex2Bytes("3373fffffffffffffffffffffffffffffffffffffffe14604457602036146024575f5ffd5b620180005f350680545f35146037575f5ffd5b6201800001545f5260205ff35b6201800042064281555f359062018000015500")},
		},
		ExtraData:  []byte("test genesis"),
		Timestamp:  9000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(0),
	}
	testNonce := uint64(0)
	generate := func(_ int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		tx, _ := types.SignTx(types.NewTransaction(testNonce, common.HexToAddress("0x9a9070028361F7AAbeB3f2F2Dc07F82C4a98A02a"), big.NewInt(1), params.TxGas, big.NewInt(params.InitialBaseFee*2), nil), types.LatestSigner(&config), testKey)
		g.AddTx(tx)
		testNonce++
	}
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, engine, n, generate)

	if !merged {
		totalDifficulty := big.NewInt(0)
		for _, b := range blocks {
			totalDifficulty.Add(totalDifficulty, b.Difficulty())
		}
		config.TerminalTotalDifficulty = totalDifficulty
	}

	return genesis, blocks
}

// startEthService creates a full node instance for testing.
func startEthService(t *testing.T, genesis *core.Genesis, blocks []*types.Block) (*node.Node, *eth.Ethereum) {
	t.Helper()

	n, err := node.New(&node.Config{
		P2P: p2p.Config{
			ListenAddr:  "0.0.0.0:0",
			NoDiscovery: true,
			MaxPeers:    25,
		},
	})
	if err != nil {
		t.Fatal("can't create node:", err)
	}

	ethcfg := &ethconfig.Config{Genesis: genesis, SyncMode: downloader.FullSync, TrieTimeout: time.Minute, TrieDirtyCache: 256, TrieCleanCache: 256}
	ethservice, err := eth.New(n, ethcfg)
	if err != nil {
		t.Fatal("can't create eth service:", err)
	}
	if err := n.Start(); err != nil {
		t.Fatal("can't start node:", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks); err != nil {
		n.Close()
		t.Fatal("can't import test blocks:", err)
	}
	time.Sleep(500 * time.Millisecond) // give txpool enough time to consume head event

	ethservice.SetEtherbase(testAddr)
	ethservice.SetSynced()
	return n, ethservice
}

func assembleBlock(api *BundleMergerServer, parentHash common.Hash, params *engine.PayloadAttributes) (*engine.ExecutableData, error) {
	args := &miner.BuildPayloadArgs{
		Parent:       parentHash,
		Timestamp:    params.Timestamp,
		FeeRecipient: params.SuggestedFeeRecipient,
		GasLimit:     params.GasLimit,
		Random:       params.Random,
		Withdrawals:  params.Withdrawals,
		BeaconRoot:   params.BeaconRoot,
	}

	fmt.Printf("BuildPayloadArgs: %+v\n", args) // Add this line to log the arguments

	payload, err := api.eth.Miner().BuildPayload(args)
	if err != nil {
		return nil, err
	}

	fmt.Printf("BuildPayloadArgs: %+v\n", payload) // Add this line to log the arguments

	if payload := payload.ResolveFull(); payload != nil {
		return payload.ExecutionPayload, nil
	}

	fmt.Printf("BuildPayloadArgs: %+v\n", payload) // Add this line to log the arguments

	return nil, errors.New("payload did not resolve")
}
