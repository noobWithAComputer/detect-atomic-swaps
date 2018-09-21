// Author: dominik.lauck@mailbox.tu-dresden.de
// 
// This script searches through the bitcoin (or litecoin or decred or whatever) blockchain and looks for scripts specifying hashed timelock contracts.
// These HTLCs might be part of an atomic swap which is the desired target to find.
// Found HTLCs are saved in a json file depending on the chain to search through (e.g. HTLCsBTC.json).

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/echa/btcutil/hash"
	"github.com/echa/btcutil/log"
	"github.com/echa/btcutil/rpc"
	"github.com/echa/btcutil/txscript"
	"github.com/echa/btcutil/wire"

	// auto-register all available blockchain params
	_ "github.com/echa/btcutil/wire/params"
)

var (
	jrpcLog     = log.NewLogger("JRPC")
	flags       = flag.NewFlagSet("index", flag.ContinueOnError)
	height      int64
	chain       string
	host        string
	port        string
	user        string
	pass        string
	verbose     bool
	concurrency int
)

func init() {
	flags.Usage = func() {}
	flags.Int64Var(&height, "height", 0, "start height")
	flags.StringVar(&chain, "chain", "bitcoin", "blockchain")
	flags.StringVar(&host, "host", "127.0.0.1", "RPC hostname")
	flags.StringVar(&user, "user", "", "RPC username")
	flags.StringVar(&pass, "pass", "", "RPC password")
	flags.StringVar(&port, "port", "", "RPC port")
	flags.IntVar(&concurrency, "c", 1, "RPC Concurrency")
	flags.BoolVar(&verbose, "v", false, "be verbose")
	rpc.UseLogger(jrpcLog)
}

type candidate struct {
	Block       int64    `json:"block"`
	Timestamp   string   `json:"timestamp"`
	Transaction string   `json:"transaction"`
	InputTx     string   `json:"input_tx"`
	InputValue  float64  `json:"input_value"`
	Asm         []string `json:"asm"`
}

func checkForTimeLock(scriptString string) (bool) {
	
	script, err := hex.DecodeString(scriptString)
	if err != nil {
//		log.Fatalf("error decoding script. %v", err)
		return false
	}
	
	pops, err := txscript.ParseScript([]byte(script))
	if err != nil {
//		log.Fatalf("error getting script. %v", err)
		return false
	}
	
	if txscript.IsPubkey(pops) || txscript.IsPubkeyHash(pops) || txscript.IsMultiSig(pops) || txscript.IsNullData(pops) {
		return false
	}
	
//	log.Infof("  new tx.")
	
	TLfound := false
	HLfound := false
	
	for _, pop := range pops {
//		log.Infof("        OpValue: %s", pop.Opcode.Name)
		if pop.Opcode.Value == txscript.OP_CHECKLOCKTIMEVERIFY || pop.Opcode.Value == txscript.OP_CHECKSEQUENCEVERIFY {
			TLfound = true
		}
		if pop.Opcode.Value == txscript.OP_RIPEMD160 || pop.Opcode.Value == txscript.OP_SHA1 || pop.Opcode.Value == txscript.OP_SHA256 || pop.Opcode.Value == txscript.OP_HASH160 || pop.Opcode.Value == txscript.OP_HASH256 {
			HLfound = true
		}
		
		if TLfound && HLfound {
			return true
		}
	}
	
	return false
}

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Block and transaction processing can cause bursty allocations.  This
	// limits the garbage collector from excessively overallocating during
	// bursts.  This value was arrived at with the help of profiling live
	// usage.
	debug.SetGCPercent(20)

	// parse command line flags
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Println("Blockchain Indexer")
			flags.PrintDefaults()
			os.Exit(0)
		}
		log.Fatalf("Error: %v", err)
	}

	// set log level
	if verbose {
		log.SetLevel(log.LevelTrace)
		jrpcLog.SetLevel(log.LevelTrace)
	} else {
		log.SetLevel(log.LevelInfo)
		jrpcLog.SetLevel(log.LevelInfo)
	}
	
	thisChain := ""
	// set names for files depending on the specified chain
	jsonFileName := "atomicswapsBTC.json"
	blockFileName := "blockBTC.txt"
	dcr := false
	TLSstate := true
	lowestBlock := int64(0)
	
	switch chain {
	case "btc":
		jsonFileName = "HTLCsBTC.json"
		blockFileName = "blockBTC.txt"
		thisChain = "bitcoin"
		lowestBlock = 446033
	case "BTC":
		jsonFileName = "HTLCsBTC.json"
		blockFileName = "blockBTC.txt"
		thisChain = "bitcoin"
		lowestBlock = 446033
	case "ltc":
		jsonFileName = "HTLCsLTC.json"
		blockFileName = "blockLTC.txt"
		thisChain = "litecoin"
		lowestBlock = 1125292
	case "LTC":
		jsonFileName = "HTLCsLTC.json"
		blockFileName = "blockLTC.txt"
		thisChain = "litecoin"
		lowestBlock = 1125292
	case "bch":
		jsonFileName = "HTLCsBCH.json"
		blockFileName = "blockBCH.txt"
		thisChain = "bitcoin"
		lowestBlock = 478461
	case "BCH":
		jsonFileName = "HTLCsBCH.json"
		blockFileName = "blockBCH.txt"
		thisChain = "bitcoin"
		lowestBlock = 478461
	case "dcr":
		jsonFileName = "HTLCsDCR.json"
		blockFileName = "blockDCR.txt"
		thisChain = "decred"
		lowestBlock = 94501
		dcr = true
	case "DCR":
		jsonFileName = "HTLCsDCR.json"
		blockFileName = "blockDCR.txt"
		thisChain = "decred"
		lowestBlock = 94501
		dcr = true
	default:
		log.Fatalf("error: wrong chain specified.")
	}

	// find params for chain
	params, err := wire.GetParams(thisChain)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	if port == "" {
		port = params.DefaultRPCPort
	}
	
	cert := []byte{}
	
	if dcr {
		TLSstate = false
		
		cert, err = ioutil.ReadFile("rpc.cert")
		if err != nil {
			log.Fatal(err)
		}
	}
	
	// create new RPC client instance for other currencies
	c, err := rpc.New(&rpc.ConnConfig{
		Threads:      concurrency,
		DisableTLS:   TLSstate,
		Certificates: cert,
		Host:         net.JoinHostPort(host, port),
		User:         user,
		Pass:         pass,
	}, params)
	if err != nil {
		log.Fatalf("error creating rpc client: %v", err)
	}

	// create a new context for RPC calls
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// read file with current block
	content, err := ioutil.ReadFile(blockFileName)
    if err != nil {
        log.Fatal(err)
    }
	
	// get the height from the file content
	heightString, err := strconv.Atoi(string(content))
	if err != nil {
        log.Fatal(err)
    }
	
	if dcr {
		height = int64(heightString)
	} else {
		info, err := c.GetBlockChainInfo(ctx)
		if err != nil {
			log.Fatalf("error getting info: %v", err)
		} else {
			b, _ := json.MarshalIndent(info, "", "  ")
			log.Infof("%s\n", string(b))
		}
		
		// if the content of the blockfile is lower than 10000, get the current highest block number
		if heightString < 10000 {
			height = int64(info.Blocks)
		} else {
			height = int64(heightString)
		}
	}
	
//	// read candidate struct from json file
//	raw, err := ioutil.ReadFile(jsonFileName)
//	if err != nil {
//		log.Fatal(err)
//	}
//	
//	// unmarshal bytes
//	var candidates []candidate
//	err = json.Unmarshal(raw, &candidates)
//	if err != nil {
//		log.Fatal(err)
//	}
		
	// get block hash from height
	h, err := c.GetBlockHash(ctx, height)
	if err != nil {
		log.Fatalf("error getting block hash for height %d: %v", height, err)
	}
	
	// try fetching a block to let the RPC package detect which verbosity mode
	// to use (we ignore the return value and any error here)
	c.GetBlockVerbose(ctx, h)
	
	var (
		ntx   int
	)
	
	w, err := os.OpenFile(jsonFileName, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
	defer io.WriteString(w, "\n]")
	
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	//io.WriteString(w, "[")
	_, err = w.Seek(-2, os.SEEK_END)
	
	// process all available blocks
	for ; height >= lowestBlock; height-- {
		// get a block with all transactions
		block, err := c.GetBlockVerbose(ctx, h)
		if err != nil {
			log.Fatalf("error fetching block: %v", err)
		}
		if block.Height != height {
			log.Infof("warning: block height mismatch exp=%d got=%d\n", height, block.Height)
		}
		// change block.PreviousHash to block.NextHash, when changing search direction
		if h, err = hash.NewHashFromStr(block.PreviousHash); err != nil {
			log.Infof("error getting next block hash from %s: %v", block.PreviousHash, err)
		}

		// skip genesis block transactions
		if height == 0 {
			continue
		}

		// get transactions from id
		if block.Tx == nil {
			txHashes := make([]*hash.Hash, len(block.TxIds))
			for i, v := range block.TxIds {
				txHashes[i], _ = hash.NewHashFromStr(v)
			}
			// fetch all tx in parallel
			block.Tx, err = c.GetRawTransactionsVerbose(ctx, txHashes)
			if err != nil {
				log.Fatalf("error getting txs in block %d: %v", block.Height, err)
			}
		}
		ntx += len(block.Tx)

		// walk all transactions
		for _, tx := range block.Tx {
			
			// check inputs
			// walk all tx inputs
			for _, in := range tx.Vin {
				
				if in.IsCoinBase() {
					continue
				}
				
				// decode to string
				scriptString, err := hex.DecodeString(in.ScriptSig.Hex)
				
				if err != nil {
					log.Fatalf("error decoding script string from tx: %v", err)
				}
				
				// decode string as script
				script, err := c.DecodeScript(ctx, []byte(scriptString))
				
				if err != nil {
					log.Fatalf("error getting script. %v", err)
				}
				
				// decompose the asm of the script (the datapushes)
				asmStrings := strings.Split(script.Asm, " ")
				
				length := len(asmStrings)
				
				if checkForTimeLock(asmStrings[length - 1]) {
					log.Infof("      Found timelock in Tx: %s", tx.Txid)
					
					inputTx := in.Txid
					
					prevTxHash, err := hash.NewHashFromStr(inputTx)
					if err != nil {
						log.Fatal(err)
					}
					
					prevTx, err := c.GetRawTransactionVerbose(ctx, prevTxHash)
					if err != nil {
						log.Fatal(err)
					}
					
					prevOutInd := in.Vout
					prevOut := prevTx.Vout[prevOutInd]
					inputValue := prevOut.Value
					
					thisCandidate := new(candidate)
					
					*thisCandidate = candidate {
						Block: block.Height,
						Timestamp: time.Unix(block.Time, 0).UTC().String(),
						Transaction: tx.Txid,
						InputTx: inputTx,
						InputValue: inputValue,
						Asm: asmStrings,
					}
					
					io.WriteString(w, ",\n\t")
					
					enc.Encode(thisCandidate)
					
					_, err = w.Seek(-1, os.SEEK_END)
					
				}
			}
		}

		log.Infof("Block %6d: %s (%d)\tsize=%d\tn_tx=%d\n",
			height,
			time.Unix(block.Time, 0).UTC().String(),
			block.Time,
			block.Size,
			len(block.TxIds),
		)
		
		err = ioutil.WriteFile(blockFileName, []byte(strconv.FormatInt(height, 10)), 0644)
		if err != nil {
			log.Fatal(err)
		}
	}
}