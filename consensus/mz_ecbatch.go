package consensus

import (
	"github.com/relab/hotstuff"
)

type EcBatch struct {
	NodeID  hotstuff.ID
	FromID  hotstuff.ID
	BatchID int32
	Order   int32
	RealLen []int32
	Data    []byte
}

func NewEcBatch(id hotstuff.ID, fromid hotstuff.ID, Batchid int32, order int32, realLen []int32, data []byte) *EcBatch {
	b := &EcBatch{
		NodeID:  id,
		FromID:  fromid,
		BatchID: Batchid,
		Order:   order,
		RealLen: realLen,
		Data:    data,
	}
	return b
}

//func (b *EcBatch) ToBytes() []byte {
//	buf := b.Parent[:]
//	buf = append(buf, []byte(b.Cmd)...)
//	buf = append(buf, []byte(b.NodeID.ToBytes())...)
//	return buf
//}
