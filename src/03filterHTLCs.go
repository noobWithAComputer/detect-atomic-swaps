// Copyright (c) 2018 KIDTSUNAMI
// Author: alex@kidtsunami.com

package main

import (
	"encoding/json"
	"io/ioutil"
	"runtime"
	"runtime/debug"

	"github.com/echa/btcutil/log"

	// auto-register all available blockchain params
	_ "github.com/echa/btcutil/wire/params"
)

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
				jsonFileName1 = "pHTLCsBTC.json"
				jsonFileName2 = "filteredHTLCsBTC.json"
			case 1:
				jsonFileName1 = "pHTLCsLTC.json"
				jsonFileName2 = "filteredHTLCsLTC.json"
			case 2:
				jsonFileName1 = "pHTLCsBCH.json"
				jsonFileName2 = "filteredHTLCsBCH.json"
			case 3:
				jsonFileName1 = "pHTLCsDCR.json"
				jsonFileName2 = "filteredHTLCsDCR.json"
		}
		
		// read candidates from file
		raw, err := ioutil.ReadFile(jsonFileName1)
		if err != nil {
			log.Fatal(err)
		}
		
		// create candidates slice
		var thisHTLCs []processedCandidate
		
		// unmarshal json into candidates slice
		err = json.Unmarshal(raw, &thisHTLCs)
		if err != nil {
			log.Fatal(err)
		}
		
		// iterate over all found possible HTLCs
		for _, thisHTLC := range(thisHTLCs) {
			
			ops := thisHTLC.Ops
			
			foundEqual := false
			foundIf := false
			foundSig := false
			
			for _, op := range(ops) {
				if op == "OP_EQUAL" || op == "OP_EQUALVERIFY" {
					foundEqual = true
				}
				
				if op == "OP_IF" {
					foundIf = true
				}
				
				if op == "OP_CHECKSIG" || op == "OP_CHECKSIGVERIFY" {
					foundSig = true
				}
				
			}
			
			// if all requirements are found
			if foundEqual && foundIf && foundSig {
				// add this candidate to the slice
				thisPCs = append(thisPCs, thisHTLC)
			}
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