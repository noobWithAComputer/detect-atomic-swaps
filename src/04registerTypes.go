// Author: dominik.lauck@mailbox.tu-dresden.de
// 
// This script searches through the bitcoin (or litecoin or decred or whatever) blockchain and looks for scripts specifying hashed timelock contracts.
// These HTLCs might be part of an atomic swap which is the desired target to find.
// Found HTLCs are saved in a json file depending on the chain to search through (e.g. HTLCsBTC.json).

package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/echa/btcutil/log"
	"github.com/echa/btcutil/rpc"

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

type processedCandidate struct {
	Block       int64    `json:"block"`
	Timestamp   string   `json:"timestamp"`
	Transaction string   `json:"transaction"`
	InputTx     string   `json:"input_tx"`
	InputValue  float64  `json:"input_value"`
	Asm         []string `json:"asm"`
	Ops         []string `json:"ops"`
}

type htlcType struct {
	Name string   `json:"name"`
	Ops  []string `json:"ops"`
}

// detect of which type a PC is
// if a new type was found save it
func registerType(PC processedCandidate, types []htlcType, chain string) ([]htlcType) {
	length := len(PC.Ops)
	typeFound := false
	
	// iterate over all ops to format them properly
	for i, op := range(PC.Ops) {
		// if it is an OP_DATA opcode
		if strings.Contains(op, " ") {
			// find the whitespace
			index := strings.Index(op, " ")
			// and remove everything from the whitespace on
			PC.Ops[i] = op[:index]
		}
		
		// if it is an OP_1 ... OP_16 opcode
		if len(op) == 4 || (len(op) == 5 && strings.HasPrefix(op, "OP_1")) {
			// replace them with OP_
			// as locktimes can be set with OP_1 ... OP_16
			// but HTLCs with different locktimes can nonetheless be part of the same AS
			PC.Ops[i] = "OP_"
		}
		
		// if it is a dcr htlc
		if chain == "dcr" {
			// dcr replaced OP_SHA256 with OP_BLAKE256
			if op == "OP_SHA256" {
				PC.Ops[i] = "OP_BLAKE256"
			}
			// and moved OP_SHA256 to OP_UNKNOWN192
			if op == "OP_UNKNOWN192" {
				PC.Ops[i] = "OP_SHA256"
			}
		}
	}
	
	// iterate over all types found yet
	for _, thisType := range(types) {
		
		typeLength := len(thisType.Ops)
		
		// if the length of the PC ops and the type ops is unequal, skip this type
		if length != typeLength {
			continue
		}
		
		// iterate over all PC ops
		for i, op := range(PC.Ops) {
			
			typeFound = false
			
			// if one opcode is not matching, break the for loop
			if op != thisType.Ops[i] {
				break
			}
			
			if i == (length - 1) {
				typeFound = true
			}
		}
		
		// if this type was already registered
		if typeFound {
			return types
		}
	}
	
	typeCount := len(types)
	
	typeNumber := typeCount + 1
	typeName := "Type " + strconv.Itoa(typeNumber)
	
	newType := new(htlcType)
	
	*newType = htlcType{
		Name: typeName,
		Ops: PC.Ops,
	}
	
	types = append(types, *newType)
	
	return types
}



func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Block and transaction processing can cause bursty allocations.  This
	// limits the garbage collector from excessively overallocating during
	// bursts.  This value was arrived at with the help of profiling live
	// usage.
	debug.SetGCPercent(20)
	
	// read types from file
	raw, err := ioutil.ReadFile("types.json")
	if err != nil {
		log.Fatal(err)
	}
	
	var types []htlcType

	// unmarshal json into types slice
	err = json.Unmarshal(raw, &types)
	if err != nil {
		log.Fatal(err)
	}
	
	// for all blockchains
	for i := 0; i < 4; i++ {
		
		var thisPCs []processedCandidate
		jsonFileName := ""
		chain := ""
		
		// set some stuff
		switch i {
			case 0:
				jsonFileName = "filteredHTLCsBTC.json"
				chain = "btc"
			case 1:
				jsonFileName = "filteredHTLCsLTC.json"
				chain = "ltc"
			case 2:
				jsonFileName = "filteredHTLCsBCH.json"
				chain = "bch"
			case 3:
				jsonFileName = "filteredHTLCsDCR.json"
				chain = "dcr"
		}
		
		// read candidates from file
		raw, err := ioutil.ReadFile(jsonFileName)
		if err != nil {
			log.Fatal(err)
		}
		
		// unmarshal json into candidates slice
		err = json.Unmarshal(raw, &thisPCs)
		if err != nil {
			log.Fatal(err)
		}
		
		// iterate over all found possible HTLCs
		for _, thisPC := range(thisPCs) {
			
			types = registerType(thisPC, types, chain)
			
		}
	}
	
	// format json
	typesJson, err := json.MarshalIndent(types, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	
	// save json file
	err = ioutil.WriteFile("types.json", typesJson, 0644)
	if err != nil {
		log.Fatal(err)
	}
	
	log.Infof("Finished all.")
}