package consensus

import (
	"encoding/binary"
	"fmt"
	"github.com/relab/hotstuff"
)

type MzBlock struct {
	// keep a copy of the hash to avoid hashing multiple times
	hash       Hash
	parent     Hash
	proposer   hotstuff.ID
	batchlist  BatchList
	cmd_unsync Command
	cert       QuorumCert
	view       View
}

func NewMzBlock(parent Hash, cert QuorumCert, batchlist BatchList, cmd_unsync Command, view View, proposer hotstuff.ID, hash Hash) *MzBlock {
	b := &MzBlock{
		parent:     parent,
		cert:       cert,
		batchlist:  batchlist,
		cmd_unsync: cmd_unsync,
		view:       view,
		proposer:   proposer,
		hash:       hash,
	}
	// cache the hash immediately because it is too racy to do it in Hash()
	return b
}

func (b *MzBlock) String() string {
	return fmt.Sprintf(
		"Block{ hash: %.6s parent: %.6s, proposer: %d, view: %d , cert: %v }",
		b.Hash().String(),
		b.parent.String(),
		b.proposer,
		b.view,
		b.cert,
	)
}

// Hash returns the hash of the Block
func (b *MzBlock) Hash() Hash {
	return b.hash
}

// Proposer returns the id of the replica who proposed the block.
func (b *MzBlock) Proposer() hotstuff.ID {
	return b.proposer
}

// Parent returns the hash of the parent Block
func (b *MzBlock) Parent() Hash {
	return b.parent
}

// Command returns the command
func (b *MzBlock) BatchList() BatchList {
	return b.batchlist
}

// QuorumCert returns the quorum certificate in the block
func (b *MzBlock) QuorumCert() QuorumCert {
	return b.cert
}

// View returns the view in which the Block was proposed
func (b *MzBlock) View() View {
	return b.view
}
func (b *MzBlock) Cmdunsync() Command {
	return b.cmd_unsync
}

// ToBytes returns the raw byte form of the Block, to be used for hashing, etc.
func (b *MzBlock) ToBytes() []byte {
	buf := b.parent[:]
	var proposerBuf [4]byte
	binary.LittleEndian.PutUint32(proposerBuf[:], uint32(b.proposer))
	buf = append(buf, proposerBuf[:]...)
	var viewBuf [8]byte
	binary.LittleEndian.PutUint64(viewBuf[:], uint64(b.view))
	buf = append(buf, viewBuf[:]...)
	buf = append(buf, []byte(b.batchlist)...)
	buf = append(buf, b.cert.ToBytes()...)
	return buf
}
