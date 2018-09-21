// Copyright (c) 2018 KIDTSUNAMI
// Author: alex@kidtsunami.com

package main

import (
	"encoding/json"
	"io/ioutil"
	"math"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/echa/btcutil/log"

	// auto-register all available blockchain params
	_ "github.com/echa/btcutil/wire/params"
)

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

type ProcessingHTLC struct {
	ThisHTLC htlc
	Processed bool
}

type AtomicSwap struct {
	Chain1 string `json:"chain1"`
	HTLC1 htlc  `json:"HTLC1"`
	Chain2 string `json:"chain2"`
	HTLC2 htlc  `json:"HTLC2"`
}

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Block and transaction processing can cause bursty allocations.  This
	// limits the garbage collector from excessively overallocating during
	// bursts.  This value was arrived at with the help of profiling live
	// usage.
	debug.SetGCPercent(20)
	
	// BTC
	// read btc matches from file
	rawBTC, err := ioutil.ReadFile("realHTLCsBTC.json")
	if err != nil {
        log.Fatal(err)
    }
	// create matches slice
	var HTLCsBTC []htlc
	// unmarshal json into matches slice
	err = json.Unmarshal(rawBTC, &HTLCsBTC)
	if err != nil {
        log.Fatal(err)
    }
	
	// LTC
	// read ltc matches from file
	rawLTC, err := ioutil.ReadFile("realHTLCsLTC.json")
	if err != nil {
        log.Fatal(err)
    }
	// create matches slice
	var HTLCsLTC []htlc
	// unmarshal json into matches slice
	err = json.Unmarshal(rawLTC, &HTLCsLTC)
	if err != nil {
        log.Fatal(err)
    }
	
	//BCH
	// read bch matches from file
	rawBCH, err := ioutil.ReadFile("realHTLCsBCH.json")
	if err != nil {
        log.Fatal(err)
    }
	// create matches slice
	var HTLCsBCH []htlc
	// unmarshal json into matches slice
	err = json.Unmarshal(rawBCH, &HTLCsBCH)
	if err != nil {
        log.Fatal(err)
    }
	
	// DCR
	// read dcr matches from file
	rawDCR, err := ioutil.ReadFile("realHTLCsDCR.json")
	if err != nil {
        log.Fatal(err)
    }
	// create matches slice
	var HTLCsDCR []htlc
	// unmarshal json into matches slice
	err = json.Unmarshal(rawDCR, &HTLCsDCR)
	if err != nil {
        log.Fatal(err)
    }
	
	thisHTLC := new(ProcessingHTLC)
	var pmBTC []ProcessingHTLC
	
	// initialize processingMatches from loaded matches (BTC)
	for _, currentHTLC := range(HTLCsBTC) {
		*thisHTLC = ProcessingHTLC{
			ThisHTLC: currentHTLC,
			Processed: false,
		}
		
		pmBTC = append(pmBTC, *thisHTLC)
	}
	
	var pmLTC []ProcessingHTLC
	
	// initialize processingMatches from loaded matches (LTC)
	for _, currentHTLC := range(HTLCsLTC) {
		*thisHTLC = ProcessingHTLC{
			ThisHTLC: currentHTLC,
			Processed: false,
		}
		
		pmLTC = append(pmLTC, *thisHTLC)
	}
	
	var pmBCH []ProcessingHTLC
	
	// initialize processingMatches from loaded matches (BCH)
	for _, currentHTLC := range(HTLCsBCH) {
		*thisHTLC = ProcessingHTLC{
			ThisHTLC: currentHTLC,
			Processed: false,
		}
		
		pmBCH = append(pmBCH, *thisHTLC)
	}
	
	var pmDCR []ProcessingHTLC
	
	// initialize processingMatches from loaded matches (DCR)
	for _, currentHTLC := range(HTLCsDCR) {
		*thisHTLC = ProcessingHTLC{
			ThisHTLC: currentHTLC,
			Processed: false,
		}
		
		pmDCR = append(pmDCR, *thisHTLC)
	}
	
	var AS []AtomicSwap
	thisAS := new(AtomicSwap)
	
	for i := 0; i < 6; i++ {
		
		var pmChain1 []ProcessingHTLC
		var pmChain2 []ProcessingHTLC
		chain1 := ""
		chain2 := ""
		
		switch i {
			case 0:
				pmChain1 = pmBTC
				pmChain2 = pmLTC
				chain1 = "BTC"
				chain2 = "LTC"
			case 1:
				pmChain1 = pmBTC
				pmChain2 = pmBCH
				chain1 = "BTC"
				chain2 = "BCH"
			case 2:
				pmChain1 = pmBTC
				pmChain2 = pmDCR
				chain1 = "BTC"
				chain2 = "DCR"
			case 3:
				pmChain1 = pmLTC
				pmChain2 = pmBCH
				chain1 = "LTC"
				chain2 = "BCH"
			case 4:
				pmChain1 = pmLTC
				pmChain2 = pmDCR
				chain1 = "LTC"
				chain2 = "DCR"
			case 5:
				pmChain1 = pmBCH
				pmChain2 = pmDCR
				chain1 = "BCH"
				chain2 = "DCR"
		}
		
		// iterate over all matches in chain1
		for _, currentHTLC1 := range(pmChain1) {
			// if this match has not been processed yet
			if currentHTLC1.Processed == false {
				// iterate over all matches in chain2
				for _, currentHTLC2 := range(pmChain2) {
					matchFound := false
					matchAndTime := false
					
					// if this match has not been processed yet
					if currentHTLC2.Processed == false {
						// check length of the secrethashes slice
						if len(currentHTLC1.ThisHTLC.SecretHashes) == 1 && len(currentHTLC1.ThisHTLC.SecretHashes) == len(currentHTLC2.ThisHTLC.SecretHashes) { // only one secret
							if currentHTLC1.ThisHTLC.SecretHashes[0] == currentHTLC2.ThisHTLC.SecretHashes[0] {
								matchFound = true
							}
						} else { // multiple secrethashes
							for i, thisSecretHash := range(currentHTLC1.ThisHTLC.SecretHashes) {
								if thisSecretHash == currentHTLC2.ThisHTLC.SecretHashes[i] {
									matchFound = true
								} else {
									matchFound = false
									break
								}
							}
						}
						
						// if all secrethashes are matching
						if matchFound {
							// parse the timestamps of the HTLCs
							timelayout := "2006-01-02 15:04:05 -0700 UTC"
							time1, err := time.Parse(timelayout, currentHTLC1.ThisHTLC.Timestamp)
							if err != nil {
								log.Fatal(err)
							}
							time2, err := time.Parse(timelayout, currentHTLC2.ThisHTLC.Timestamp)
							if err != nil {
								log.Fatal(err)
							}
							
							// and check if they are close enough to each other (less then one day)
							if math.Abs(float64(time1.Unix() - time2.Unix())) < float64(86400) {
								matchAndTime = true
							}
						}
						
						// if all secrethashes match and the times are in range
						if matchAndTime {
							*thisAS = AtomicSwap{
								Chain1: chain1,
								HTLC1: currentHTLC1.ThisHTLC,
								Chain2: chain2,
								HTLC2: currentHTLC2.ThisHTLC,
							}
							
							// append this newly found match 
							AS = append(AS, *thisAS)
							
							// mark these matches as already processed
							currentHTLC1.Processed = true
							currentHTLC2.Processed = true
							
						}
					}
				}
			}
		}
	}
	
	ASjson, err := json.MarshalIndent(AS, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	
	err = ioutil.WriteFile("AS.json", ASjson, 0644)
	if err != nil {
		log.Fatal(err)
	}
	
	log.Infof("All done.")
}