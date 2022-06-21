package consensus

import (
	"github.com/relab/hotstuff"
)

type BatchListitem struct {
	StartHeight  int32
	EndHeight    int32
	NodeID       hotstuff.ID
	UseBatchHash bool
}

func NewBatchListitem(startheight int32, endheight int32, nodeid hotstuff.ID, usebatchhash bool) *BatchListitem {
	b := &BatchListitem{
		StartHeight:  startheight,
		EndHeight:    endheight,
		NodeID:       nodeid,
		UseBatchHash: usebatchhash,
	}

	return b
}

type BatchList string
