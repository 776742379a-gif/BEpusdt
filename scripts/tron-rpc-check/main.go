// Read-only probe for the same native TRON gRPC client used by the scanner.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/v03413/bepusdt/app/utils"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
	"google.golang.org/grpc/status"
)

func main() {
	key := os.Getenv("BEPUSDT_TRON_DRPC_KEY")
	if strings.TrimSpace(key) == "" {
		fmt.Fprintln(os.Stderr, "BEPUSDT_TRON_DRPC_KEY is required; do not pass credentials as command arguments")
		os.Exit(1)
	}
	conn, err := utils.NewTronGrpcClientWithConfig(utils.TronGRPCConfig{Endpoint: "grpc.drpc.live:443", DRPCAPIKeys: strings.Split(key, ",")})
	if err != nil {
		fmt.Fprintln(os.Stderr, "TRON gRPC configuration rejected")
		os.Exit(1)
	}
	defer conn.Close()
	wallet := api.NewWalletClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	check := func(method string, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s failed: %s\n", method, status.Code(err))
			os.Exit(1)
		}
	}
	head, err := wallet.GetNowBlock2(ctx, &api.EmptyMessage{})
	check("GetNowBlock2", err)
	number := head.GetBlockHeader().GetRawData().GetNumber()
	if number <= 0 {
		fmt.Fprintln(os.Stderr, "GetNowBlock2 returned no block")
		os.Exit(1)
	}
	block, err := wallet.GetBlockByNum2(ctx, &api.NumberMessage{Num: number})
	check("GetBlockByNum2", err)
	if block.GetBlockHeader().GetRawData().GetNumber() != number {
		fmt.Fprintln(os.Stderr, "GetBlockByNum2 returned a different block")
		os.Exit(1)
	}
	result := map[string]any{"head_block": number, "head_age_seconds": time.Since(time.UnixMilli(head.GetBlockHeader().GetRawData().GetTimestamp())).Seconds(), "block_read_matches": true}
	if value := os.Getenv("BEPUSDT_TRON_CHECK_TXID"); value != "" {
		hash, err := hex.DecodeString(value)
		if err != nil || len(hash) != 32 {
			fmt.Fprintln(os.Stderr, "BEPUSDT_TRON_CHECK_TXID must be a 32-byte hex transaction ID")
			os.Exit(1)
		}
		info, err := wallet.GetTransactionInfoById(ctx, &api.BytesMessage{Value: hash})
		check("GetTransactionInfoById", err)
		if !bytes.Equal(info.GetId(), hash) || info.GetBlockNumber() <= 0 || info.GetReceipt().GetResult() != core.Transaction_Result_SUCCESS {
			fmt.Fprintln(os.Stderr, "transaction receipt was absent or unsuccessful")
			os.Exit(1)
		}
		result["receipt_hash_matches"] = true
		result["receipt_success"] = true
		result["receipt_block"] = info.GetBlockNumber()
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
