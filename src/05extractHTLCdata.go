// Author: dominik.lauck@mailbox.tu-dresden.de
// 
// This script searches through the bitcoin (or litecoin or decred or whatever) blockchain and looks for scripts specifying hashed timelock contracts.
// These HTLCs might be part of an atomic swap which is the desired target to find.
// Found HTLCs are saved in a json file depending on the chain to search through (e.g. HTLCsBTC.json).

package main

import (
//	"crypto/sha256"
//	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"runtime"
	"runtime/debug"
	"strings"
//	"golang.org/x/crypto/ripemd160"

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

type htlc struct {
	Chain        string   `json:"chain"`
	Block        int64    `json:"block"`
	Timestamp    string   `json:"timestamp"`
	Transaction  string   `json:"transaction"`
	InputTx      string   `json:"input_tx"`
	InputValue   float64  `json:"input_value"`
	Type         string   `json:"type"`
	Timelock     string   `json:"timelock"`
	PubKeys1     []string `json:"pub_key_hashes1"`
	PubKey2      string   `json:"pub_key_hash2"`
	Secrets      []string `json:"secrets"`
	SecretHashes []string `json:"secret_hashes"`
}

type filteredHTLCType struct {
	Name           string   `json:"name"`
	Length         int      `json:"length"`
	Hash           string   `json:"hash"`
	SecrethashPos  []int    `json:"secrethash_pos"`
	LocktimePos    int      `json:"locktime_pos"`
	PublicKeys1Pos []int    `json:"public_keys1_pos"`
	PublicKey2Pos  int      `json:"public_key2_pos"`
	Ops            []string `json:"ops"`
}

// detect of which type a PC is
// if a new type was found save it
func extractData(PC processedCandidate, types []filteredHTLCType, chain string) (*htlc, error) {
	length := len(PC.Ops)
	typeFound := false
	matchingType := ""
	matchingTypeNumber := -1
	
	if chain == "dcr" {
		for i, op := range(PC.Ops) {
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
	
	// iterate over all types
	for i, thisType := range(types) {
		
		// if length is not matching, continue
		if length != thisType.Length {
			continue
		}
		
		// iterate over all ops
		for j, op := range(PC.Ops) {
			
			// if it is an OP_DATA opcode
			if strings.Contains(op, " ") {
				index := strings.Index(op, " ")
				op = op[:index]
			}
			
			// if it is an OP_1 ... OP_16 opcode
			if len(op) == 4 || (len(op) == 5 && strings.HasPrefix(op, "OP_1")) {
				// replace them with OP_
				// as locktimes can be set with OP_1 ... OP_16
				// but HTLCs with different locktimes can nonetheless be part of the same AS
				op = "OP_"
			}
			
			// if they are not matching, break the search
			if op != thisType.Ops[j] {
				break
			}
			
			// if it is the last round and it did not break yet
			if j == (length - 1) {
				typeFound = true
				matchingType = thisType.Name
				matchingTypeNumber = i
			}
		}
		
		// if the matching type was found, break
		if typeFound {
			break
		}
	}
	
	// if no matching type found, return an error
	if !typeFound {
		return nil, fmt.Errorf("No matching type found!")
	}
	
	var thisType filteredHTLCType
	thisType = types[matchingTypeNumber]
	index := 0
	
	// get the timelock
	thisOp := PC.Ops[thisType.LocktimePos]
	timelock := ""
	if strings.Contains(thisOp, " ") {
		index = strings.Index(thisOp, " ") + 1
		timelock = thisOp[index:]
	} else {
		timelock = thisOp[3:]
	}
	
	
	pubKeys1 := []string{}
	// get the public keys 1
	for _, pkPos := range(thisType.PublicKeys1Pos) {
		thisOp = PC.Ops[pkPos]
		index = strings.Index(thisOp, " ") + 1
		pubKeys1 = append(pubKeys1, thisOp[index:])
	}
	
	// get the public key 2
	thisOp = PC.Ops[thisType.PublicKey2Pos]
	index = strings.Index(thisOp, " ") + 1
	pubKey2 := thisOp[index:]
	
	secretHashes := []string{}
	// get the secret hashes
	for _, shPos := range(thisType.SecrethashPos) {
		thisOp = PC.Ops[shPos]
		index = strings.Index(thisOp, " ") + 1
		secretHashes = append(secretHashes, thisOp[index:])
	}
	
	secrets := []string{}
	asmLength := len(PC.Asm)
//	hashType := 0
	
	// Das Nachfolgende ist ein ziemliches Wirr-Warr...
	// Hier werden abhängig vom identifizierten HTLC-Typen die Secrets identifiziert.
	// Da die meisten HTLCs mit einem IF beginnen, muss geprüft werden, in welchen Zwieg es geht (== "0" oder != "0").
	// Abhängig vom Typ befindet sich das Secret an unterschiedlichen Stellen im ASM.
	switch matchingType {
	case "Type1a":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 1
	case "Type1b":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 1
	case "Type2":
		secrets = append(secrets, PC.Asm[asmLength - 2])
//		hashType = 1
	case "Type3a":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 1
	case "Type3b":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 1
	case "Type3c":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 1
	case "Type4":
		secrets = append(secrets, PC.Asm[asmLength - 2])
//		hashType = 2
	case "Type5a":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 2
	case "Type5b":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 2
	case "Type6a":
		secrets = append(secrets, PC.Asm[asmLength - 2])
//		hashType = 2
	case "Type6b":
		secrets = append(secrets, PC.Asm[asmLength - 2])
//		hashType = 2
	case "Type7":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 3
	case "Type8a":
		if PC.Asm[asmLength - 2] != "0" {
			for i := 15; i > 0; i-- {
				secrets = append(secrets, PC.Asm[i])
			}
		} else {
			for i := 15; i > 0; i-- {
				secrets = append(secrets, "none")
			}
		}
//		hashType = 2
	case "Type8b":
		if PC.Asm[asmLength - 2] != "0" {
			for i := 15; i > 0; i-- {
				secrets = append(secrets, PC.Asm[i])
			}
		} else {
			for i := 15; i > 0; i-- {
				secrets = append(secrets, "none")
			}
		}
//		hashType = 2
	case "Type9a":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type9b":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type10a":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 4])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type10b":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 4])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type11":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type12":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 5])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type13":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type14":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 5])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type15":
		if PC.Asm[asmLength - 2] == "0" {
			secrets = append(secrets, PC.Asm[asmLength - 5])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type16":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type17":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type18":
		secrets = append(secrets, PC.Asm[asmLength - 3])
//		hashType = 4
	case "Type19a":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type19b":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type19c":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	case "Type20":
		if PC.Asm[asmLength - 2] != "0" {
			secrets = append(secrets, PC.Asm[asmLength - 3])
		} else {
			secrets = append(secrets, "none")
		}
//		hashType = 4
	}
	
//	foundCount := 0
//	
//	if secrets[0] != "none" {
//		for i, thisSecret := range(secrets) {
//			newHash := []byte{}
//			
//			switch hashType {
//				case 1:
//					thisHash := sha256.New()
//					hexByte, err := hex.DecodeString(thisSecret)
//					if err != nil {
//						log.Fatal(err)
//					}
//					
//					thisHash.Write(hexByte)
//					newHash = thisHash.Sum(nil)
//				case 2:
//					thisHash := ripemd160.New()
//					hexByte, err := hex.DecodeString(thisSecret)
//					if err != nil {
//						log.Fatal(err)
//					}
//					
//					thisHash.Write(hexByte)
//					newHash = thisHash.Sum(nil)
//				case 3:
//					thisHash := ripemd160.New()
//					hexByte, err := hex.DecodeString(thisSecret)
//					if err != nil {
//						log.Fatal(err)
//					}
//					
//					thisHash.Write(hexByte)
//					newHash = thisHash.Sum(nil)
//					
//					thisHash.Write(newHash)
//					newHash = thisHash.Sum(nil)
//					
//					thisHash.Write(newHash)
//					newHash = thisHash.Sum(nil)
//				case 4:
//					thisHash1 := sha256.New()
//					hexByte, err := hex.DecodeString(thisSecret)
//					if err != nil {
//						log.Fatal(err)
//					}
//					
//					thisHash1.Write(hexByte)
//					shaHash := thisHash1.Sum(nil)
//					
//					thisHash2 := ripemd160.New()
//					thisHash2.Write(shaHash)
//					newHash = thisHash2.Sum(nil)
//			}
//			
//			thisSecretHash := secretHashes[i]
//			
//			if hex.EncodeToString(newHash) == thisSecretHash {
//				foundCount++
//			}
//		}
//		
//		if foundCount != len(secrets) && !(foundCount == 1 && matchingType == "Type18") {
//			return nil, fmt.Errorf("Not all Secrets are matching.")
//		}
//	}
	
	newHTLC := new(htlc)
	
	*newHTLC = htlc{
		Chain: chain,
		Block: PC.Block,
		Timestamp: PC.Timestamp,
		Transaction: PC.Transaction,
		InputTx: PC.InputTx,
		InputValue: PC.InputValue,
		Type: matchingType,
		Timelock: timelock,
		PubKeys1: pubKeys1,
		PubKey2: pubKey2,
		Secrets: secrets,
		SecretHashes: secretHashes,
	}
	
	return newHTLC, nil
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
	raw, err := ioutil.ReadFile("filteredTypes.json")
	if err != nil {
		log.Fatal(err)
	}
	
	var types []filteredHTLCType

	// unmarshal json into types slice
	err = json.Unmarshal(raw, &types)
	if err != nil {
		log.Fatal(err)
	}
	
	// for all blockchains
	for i := 0; i < 4; i++ {
		
		var thisPCs []processedCandidate
		var htlcs []htlc
		jsonFileName1 := ""
		jsonFileName2 := ""
		chain := ""
		
		// set some stuff
		switch i {
			case 0:
				jsonFileName1 = "filteredHTLCsBTC.json"
				jsonFileName2 = "realHTLCsBTC.json"
				chain = "btc"
			case 1:
				jsonFileName1 = "filteredHTLCsLTC.json"
				jsonFileName2 = "realHTLCsLTC.json"
				chain = "ltc"
			case 2:
				jsonFileName1 = "filteredHTLCsBCH.json"
				jsonFileName2 = "realHTLCsBCH.json"
				chain = "bch"
			case 3:
				jsonFileName1 = "filteredHTLCsDCR.json"
				jsonFileName2 = "realHTLCsDCR.json"
				chain = "dcr"
		}
		
		// read candidates from file
		raw, err := ioutil.ReadFile(jsonFileName1)
		if err != nil {
			log.Fatal(err)
		}
		
		// unmarshal json into candidates slice
		err = json.Unmarshal(raw, &thisPCs)
		if err != nil {
			log.Fatal(err)
		}
		
		var newHTLC *htlc
		
		// iterate over all found possible HTLCs
		for _, thisPC := range(thisPCs) {
			
			newHTLC, err = extractData(thisPC, types, chain)
			if err != nil {
//				log.Fatal(err)
			} else {
				htlcs = append(htlcs, *newHTLC)
			}
		}
		
		// format json
		htlcsJson, err := json.MarshalIndent(htlcs, "", "\t")
		if err != nil {
			log.Fatal(err)
		}
		
		// save json file
		err = ioutil.WriteFile(jsonFileName2, htlcsJson, 0644)
		if err != nil {
			log.Fatal(err)
		}
	
	}
	
	log.Infof("Finished all.")
}