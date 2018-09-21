// Copyright (c) 2018 KIDTSUNAMI
// Author: alex@kidtsunami.com

package main

import (
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"runtime"
	"runtime/debug"

	"github.com/echa/btcutil/log"
	"github.com/echa/btcutil/txscript"

	// auto-register all available blockchain params
	_ "github.com/echa/btcutil/wire/params"
)

type candidate struct {
	Block       int64    `json:"block"`
	Timestamp   string   `json:"timestamp"`
	Transaction string   `json:"transaction"`
	InputTx     string   `json:"input_tx"`
	InputValue  float64  `json:"input_value"`
	Asm         []string `json:"asm"`
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

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Block and transaction processing can cause bursty allocations.  This
	// limits the garbage collector from excessively overallocating during
	// bursts.  This value was arrived at with the help of profiling live
	// usage.
	debug.SetGCPercent(20)
	
	// for all blockchains
	for i := 0; i < 4; i++ {
		
		var thisPCs []processedCandidate
		jsonFileName1 := ""
		jsonFileName2 := ""
		
		// set some stuff
		switch i {
			case 0:
				jsonFileName1 = "HTLCsBTC.json"
				jsonFileName2 = "pHTLCsBTC.json"
			case 1:
				jsonFileName1 = "HTLCsLTC.json"
				jsonFileName2 = "pHTLCsLTC.json"
			case 2:
				jsonFileName1 = "HTLCsBCH.json"
				jsonFileName2 = "pHTLCsBCH.json"
			case 3:
				jsonFileName1 = "HTLCsDCR.json"
				jsonFileName2 = "pHTLCsDCR.json"
		}
		
		// read candidates from file
		raw, err := ioutil.ReadFile(jsonFileName1)
		if err != nil {
			log.Fatal(err)
		}
		
		// create candidates slice
		var thisHTLCs []candidate
		
		// unmarshal json into candidates slice
		err = json.Unmarshal(raw, &thisHTLCs)
		if err != nil {
			log.Fatal(err)
		}
		
		// iterate over all found possible HTLCs
		for _, thisHTLC := range(thisHTLCs) {
			
			var ops []string
			
			// extract the asm
			asm := thisHTLC.Asm
			length := len(asm)
			
			script, err := hex.DecodeString(asm[length - 1])
			
			pops, err := txscript.ParseScript(script)
			if err != nil {
				log.Fatal(err)
			}
			
			for _, op := range pops {
				if op.Opcode != nil {
					if len(op.Data) > 0 {
						ops = append(ops, op.Opcode.Name + " " + hex.EncodeToString(op.Data))
					} else {
						ops = append(ops, op.Opcode.Name)
					}
				}
			}
			
			thisPC := new(processedCandidate)
			
			// add the ops string to the candidate
			*thisPC = processedCandidate {
				Block: thisHTLC.Block,
				Timestamp: thisHTLC.Timestamp,
				Transaction: thisHTLC.Transaction,
				InputTx: thisHTLC.InputTx,
				InputValue: thisHTLC.InputValue,
				Asm: thisHTLC.Asm,
				Ops: ops,
			}
			
			// add this candidate to the slice
			thisPCs = append(thisPCs, *thisPC)
		}
		
		// format json
		candidatesJson, err := json.MarshalIndent(thisPCs, "", "\t")
		if err != nil {
			log.Fatal(err)
		}
		
		// save json file
		err = ioutil.WriteFile(jsonFileName2, candidatesJson, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}
	
	log.Infof("All done.")
}