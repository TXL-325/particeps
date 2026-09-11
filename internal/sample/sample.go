package sample

import "math"

type Point struct {
	CPUNs uint64
	Rx    uint64
	Tx    uint64
	OK    bool
}

type Rate struct {
	Cores   float64
	RxBps   float64
	TxBps   float64
	Quality string // ok | first | reset | missing
}

func Delta(prev, cur Point, seconds float64, prevOK bool) Rate {
	if seconds <= 0 || !cur.OK {
		return Rate{Quality: "missing"}
	}
	if !prevOK || !prev.OK {
		return Rate{Quality: "first"}
	}
	if cur.CPUNs < prev.CPUNs || cur.Rx < prev.Rx || cur.Tx < prev.Tx {
		return Rate{Quality: "reset"}
	}
	cores := float64(cur.CPUNs-prev.CPUNs) / (seconds * 1e9)
	if cores < 0 {
		cores = 0
	}
	rx := float64(cur.Rx-prev.Rx) / seconds
	tx := float64(cur.Tx-prev.Tx) / seconds
	return Rate{Cores: cores, RxBps: math.Max(0, rx), TxBps: math.Max(0, tx), Quality: "ok"}
}

func QuotaPercent(usedCores, quotaCores float64) float64 {
	if quotaCores <= 0 {
		return 0
	}
	return usedCores / quotaCores * 100
}
