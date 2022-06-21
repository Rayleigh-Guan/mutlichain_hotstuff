package consensus

import (
	"crypto/sha256"
	"github.com/relab/hotstuff"
)

type Batch struct {
	Parent       Hash
	NodeID       hotstuff.ID
	Cmd          Command
	Hash         Hash
	BatchID      int32
	ChainPoolTip map[uint32]int32
}

func NewBatch(parent Hash, id hotstuff.ID, cmd Command, batchID int32, chainpooltip map[uint32]int32) *Batch {
	b := &Batch{
		Parent:       parent,
		NodeID:       id,
		Cmd:          cmd,
		BatchID:      batchID,
		ChainPoolTip: chainpooltip,
	}
	b.Hash = sha256.Sum256(b.ToBytes())
	return b
}

func (b *Batch) ToBytes() []byte {
	buf := b.Parent[:]
	buf = append(buf, []byte(b.Cmd)...)
	buf = append(buf, []byte(b.NodeID.ToBytes())...)
	return buf
}
