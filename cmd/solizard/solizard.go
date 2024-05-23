package solizard

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/zsystm/solizard/internal/abi"
	"github.com/zsystm/solizard/internal/config"
	"github.com/zsystm/solizard/internal/ctx"
	"github.com/zsystm/solizard/internal/prompt"
	"github.com/zsystm/solizard/internal/step"
	"github.com/zsystm/solizard/internal/validation"
)

const Binary = "solizard"

var (
	// AbiDir is the directory where all abi files are stored
	// default is $HOME/solizard/abis
	AbiDir      = "./docs/sample"
	ZeroAddr    = common.Address{}
	ConfigExist = false
	conf        *config.Config
)

func init() {
	// get user's home directory
	// homeDir, err := os.UserHomeDir()
	// if err != nil {
	// 	fmt.Printf("failed to get user's home directory (reason: %v)\n", err)
	// 	os.Exit(1)
	// }
	// AbiDir = homeDir + "/" + Binary + "/" + AbiDir
	// if err := validation.DirContainsFiles(AbiDir); err != nil {
	// 	fmt.Println(err)
	// 	os.Exit(1)
	// }
	// ConfigPath := homeDir + "/" + Binary + "/config.toml"

	var err error
	// It is based on finding the configuration file where the executable is located.
	conf, err = config.ReadConfig("./config.toml")
	if err != nil {
		fmt.Printf("failed to read config file (reason: %v)\n", err)
		ConfigExist = false
	}
	ConfigExist = true
}

func Run() error {
	mAbi, err := abi.LoadABIs(AbiDir)
	if err != nil {
		return err
	}

	sctx := new(ctx.Context)
	if ConfigExist {
		if prompt.MustSelectApplyConfig() {
			sctx = ctx.NewCtx(conf)
		}
	}

	// start the main loop
	for {
	STEP_SELECT_CONTRACT:
		selectedAbi := prompt.MustSelectContractABI(mAbi)
	INPUT_RPC_URL:
		if sctx.EthClient() == nil {
			rpcURL := prompt.MustInputRpcUrl()
			client, err := ethclient.Dial(rpcURL)
			if err != nil {
				fmt.Printf("failed to connect to given rpc url: %v, please input valid one\n", err)
				goto INPUT_RPC_URL
			}
			sctx.SetEthClient(client)
		}
	INPUT_CONTRACT_ADDRESS:
		contractAddress := prompt.MustInputContractAddress()
		if err := validation.ValidateContractAddress(sctx, contractAddress); err != nil {
			fmt.Printf("Invalid contract address (reason: %v)\n", err)
			goto INPUT_RPC_URL
		}
	SELECT_METHOD:
		rw := prompt.MustSelectReadOrWrite()
		if rw == abi.WriteMethod {
			// input private key
			if sctx.PrivateKey() == nil {
				pk := prompt.MustInputPrivateKey()
				sctx.SetPrivateKey(pk)
			}
			// input chainId
			if sctx.ChainId().Sign() == 0 {
				chainID := prompt.MustInputChainID()
				sctx.SetChainId(&chainID)
			}
		}
		methodName, method := prompt.MustSelectMethod(selectedAbi, rw)
		//var input []byte
		input := prompt.MustCreateInputDataForMethod(method)
		if rw == abi.ReadMethod {
			callMsg := ethereum.CallMsg{From: ZeroAddr, To: sctx.ContractAddress(), Data: input}
			output, err := sctx.EthClient().CallContract(context.TODO(), callMsg, nil)
			if err != nil {
				return err
			}
			res, err := selectedAbi.Unpack(methodName, output)
			if err != nil {
				return err
			}
			fmt.Printf("output: %v\n", res)
		} else {
			nonce, err := sctx.EthClient().NonceAt(context.TODO(), crypto.PubkeyToAddress(sctx.PrivateKey().PublicKey), nil)
			if err != nil {
				fmt.Printf("failed to get nonce (reason: %v), maybe rpc is not working.\n", err)
				goto INPUT_RPC_URL
			}
			gasPrice, err := sctx.EthClient().SuggestGasPrice(context.TODO())
			if err != nil {
				fmt.Printf("failed to get gas price (reason: %v), maybe rpc is not working.\n", err)
				goto INPUT_RPC_URL
			}
			///@todo Change to EthClient().EstimateGas() call.
			sufficientGasLimit := uint64(3000000)
			unsignedTx := types.NewTx(&types.LegacyTx{
				To:       sctx.ContractAddress(),
				Nonce:    nonce,
				Value:    common.Big0,
				Gas:      sufficientGasLimit,
				GasPrice: gasPrice,
				Data:     input,
			})
			signedTx, err := types.SignTx(unsignedTx, types.NewEIP155Signer(sctx.ChainId()), sctx.PrivateKey())
			if err != nil {
				fmt.Printf("failed to SignTx (reason: %v), maybe rpc is not working.\n", err)
				return err
			}
			if err = sctx.EthClient().SendTransaction(context.TODO(), signedTx); err != nil {
				fmt.Printf("failed to send transaction (reason: %v), maybe rpc is not working.\n", err)
				return err
			}
			fmt.Printf("transaction sent (txHash %v).\n", signedTx.Hash().Hex())

			// sleep for 5 seconds to wait for transaction to be mined
			fmt.Printf("waiting for transaction to be mined... (sleep %.2f sec)", conf.WateTime.Seconds())
			time.Sleep(conf.WateTime)

			receipt, err := sctx.EthClient().TransactionReceipt(context.TODO(), signedTx.Hash())
			if err != nil {
				fmt.Printf("failed to get transaction receipt (reason: %v).\n", err)
			} else {
				jsonReceipt, _ := receipt.MarshalJSON()
				fmt.Printf("transaction receipt: %s\n", string(jsonReceipt))
			}
		}
		st := prompt.MustSelectStep()
		switch st {
		case step.StepSelectMethod:
			goto SELECT_METHOD
		case step.StepChangeContract:
			goto STEP_SELECT_CONTRACT
		case step.StepChangeContractAddress:
			goto INPUT_CONTRACT_ADDRESS
		case step.StepExit:
			panic("exit")
		}
	}
}
