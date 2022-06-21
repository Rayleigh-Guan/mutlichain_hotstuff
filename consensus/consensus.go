package consensus

import (
	"fmt"
	"sync"
	"time"
)

// Rules is the minimum interface that a consensus implementations must implement.
// Implementations of this interface can be wrapped in the ConsensusBase struct.
// Together, these provide an implementation of the main Consensus interface.
// Implementors do not need to verify certificates or interact with other modules,
// as this is handled by the ConsensusBase struct.
type Rules interface {
	// VoteRule decides whether to vote for the block.
	VoteRule(proposal ProposeMsg) bool
	// CommitRule decides whether any ancestor of the block can be committed.
	// Returns the youngest ancestor of the block that can be committed.
	CommitRule(*Block) *Block
	// ChainLength returns the number of blocks that need to be chained together in order to commit.
	ChainLength() int
}

// ProposeRuler is an optional interface that adds a ProposeRule method.
// This allows implementors to specify how new blocks are created.
type ProposeRuler interface {
	// ProposeRule creates a new proposal.
	ProposeRule(cert SyncInfo, cmd Command) (proposal ProposeMsg, ok bool)
}

// consensusBase provides a default implementation of the Consensus interface
// for implementations of the ConsensusImpl interface.
type consensusBase struct {
	impl             Rules
	mods             *Modules
	lastVote         View
	mut              sync.Mutex
	bExec            *Block
	batchlistinblock map[Hash]*BatchList
}

// New returns a new Consensus instance based on the given Rules implementation.
func New(impl Rules) Consensus {
	return &consensusBase{
		impl:             impl,
		lastVote:         0,
		bExec:            GetGenesis(),
		batchlistinblock: make(map[Hash]*BatchList),
	}
}

func (cs *consensusBase) CommittedBlock() *Block {
	cs.mut.Lock()
	defer cs.mut.Unlock()
	return cs.bExec
}

func (cs *consensusBase) InitConsensusModule(mods *Modules, opts *OptionsBuilder) {
	cs.mods = mods
	if mod, ok := cs.impl.(Module); ok {
		mod.InitConsensusModule(mods, opts)
	}
	//cs.mods.EventLoop().RegisterHandler(ProposeMsg{}, func(event interface{}) {
	//	cs.OnPropose(event.(ProposeMsg))
	//})
	cs.mods.EventLoop().RegisterHandler(Batch{}, func(event interface{}) {
		cs.OnProposeBatch(event.(Batch))
	})
	cs.mods.EventLoop().RegisterHandler(MzProposeMsg{}, func(event interface{}) {
		cs.MzOnPropose(event.(MzProposeMsg))
	})
}

func (cs *consensusBase) OnProposeBatch(batch Batch) {
	//cs.mods.MultichainList().Add(batch.NodeID, &batch)
	cs.mods.multichainlist.Add(batch.NodeID, &batch)
	//cs.mods.Logger().Info(batch.BatchID, batch.NodeID)
}

// StopVoting ensures that no voting happens in a view earlier than `view`.
func (cs *consensusBase) StopVoting(view View) {
	if cs.lastVote < view {
		cs.lastVote = view
	}
}

// Propose creates a new proposal.
func (cs *consensusBase) MzPropose(cert SyncInfo) {
	//fmt.Println("Node id: ", cs.mods.ID(), "--try to Mzpropose View", cs.mods.Synchronizer().View())
	cs.mods.Logger().Debug("NewPropose")
	qc, ok := cert.QC()
	if ok {
		// tell the acceptor that the previous proposal succeeded.
		qcBlock, ok := cs.mods.BlockChain().Get(qc.BlockHash())
		if !ok {
			cs.mods.Logger().Errorf("Could not find block for QC: %s", qc)
			fmt.Println("Node id: ", cs.mods.ID(), "--faild to Mzpropose because can not find qc.block，qc.block hash is:", qc.BlockHash().String(), " --view:", cs.mods.Synchronizer().View())
			return
		}
		cs.mods.Acceptor().Proposed(qcBlock.Command())
	}

	batchlist, ok := cs.mods.multichainlist.PackList()
	if !ok {
		fmt.Println("Node id: ", cs.mods.ID(), "--faild to Mzpropose because can not get batchlist --view:", cs.mods.Synchronizer().View())
		cs.mods.Logger().Debug("Propose: No command")
		return
	}

	cmd_sync, ok_sync := cs.mods.multichainlist.GetCmd(batchlist, true)
	cmd_unsync, ok_unsync := cs.mods.multichainlist.GetCmd(batchlist, false)

	cmd := cmd_sync + cmd_unsync
	if !ok_sync && !ok_unsync {
		if batchlist == "" {
			fmt.Println("Node id: ", cs.mods.ID(), "--Mzpropose error: GetCmd --error reason batchlist is null  --parent hash:", cs.mods.Synchronizer().LeafBlock().Hash().String(), " ---View", cs.mods.Synchronizer().View())
		} else {
			fmt.Println("Node id: ", cs.mods.ID(), "--Mzpropose error: GetCmd --error reason Unknown  --parent hash:", cs.mods.Synchronizer().LeafBlock().Hash().String(), " ---View", cs.mods.Synchronizer().View())
		}
	}

	block := NewBlock(cs.mods.Synchronizer().LeafBlock().Hash(), qc, cmd, cs.mods.Synchronizer().View(), cs.mods.ID())
	var proposal MzProposeMsg
	proposal = MzProposeMsg{
		ID: cs.mods.ID(),
		Block: NewMzBlock(
			cs.mods.Synchronizer().LeafBlock().Hash(),
			qc,
			batchlist,
			cmd_unsync,
			cs.mods.Synchronizer().View(),
			cs.mods.ID(),
			block.Hash(),
		),
	}
	fmt.Println("Node id: ", cs.mods.ID(), " success create a mzpropose blockhash :", block.Hash().String(), "at time: ", time.Now().UnixMilli())
	if aggQC, ok := cert.AggQC(); ok && cs.mods.Options().ShouldUseAggQC() {
		proposal.AggregateQC = &aggQC
	}

	cs.mods.BlockChain().Store(block)
	cs.mods.Configuration().MzPropose(proposal)
	//cs.mods.multichainlist.GetBatchItem(proposal.Block.batchlist)
	cs.MzOnPropose(proposal)

}
func (cs *consensusBase) MzOnPropose(mzproposemsg MzProposeMsg) {

	fmt.Println("Node id: ", cs.mods.ID(), " success received a mzpropose  --proposer:", mzproposemsg.Block.proposer,
		" --block view: ", mzproposemsg.Block.View(), " --now view", cs.mods.Synchronizer().View(), " --block hash:", mzproposemsg.Block.hash.String(), " --last vote: ", cs.lastVote)
	if cs.mods.ID() == mzproposemsg.Block.proposer {
		fmt.Println("Node id: ", cs.mods.ID(), " success received a mzpropose  --proposer:", mzproposemsg.Block.proposer,
			" --parenthash ", mzproposemsg.Block.parent.String(), " --block hash:", mzproposemsg.Block.hash.String(), " cmd_unsync size: ", len(mzproposemsg.Block.cmd_unsync))
		cs.mods.multichainlist.GetBatchItem(mzproposemsg.Block.batchlist)
	}

	cmd_sync, ok_sync := cs.mods.multichainlist.GetCmd(mzproposemsg.Block.batchlist, true)

	if !ok_sync {
		return
	}
	cmd_unsync := mzproposemsg.Block.cmd_unsync
	cmd := cmd_sync + cmd_unsync
	var proposemsg ProposeMsg
	proposemsg = ProposeMsg{
		ID: mzproposemsg.ID,
		Block: NewBlock(
			mzproposemsg.Block.parent,
			mzproposemsg.Block.cert,
			cmd,
			mzproposemsg.Block.view,
			mzproposemsg.Block.proposer,
		),
	}
	//fmt.Println("Node: ", cs.mods.ID(), " recovery block ", proposemsg.Block.hash.String())
	//cs.mods.multichainlist.GetBatchItem(mzproposemsg.Block.batchlist)
	proposemsg.AggregateQC = mzproposemsg.AggregateQC

	cs.OnPropose(proposemsg, mzproposemsg.Block.batchlist)

}
func (cs *consensusBase) Propose(cert SyncInfo) {
	cs.mods.Logger().Debug("Propose")

	qc, ok := cert.QC()
	if ok {
		// tell the acceptor that the previous proposal succeeded.
		qcBlock, ok := cs.mods.BlockChain().Get(qc.BlockHash())
		if !ok {
			cs.mods.Logger().Errorf("Could not find block for QC: %s", qc)
			return
		}
		cs.mods.Acceptor().Proposed(qcBlock.Command())
	}

	cmd, ok := cs.mods.CommandQueue().Get(cs.mods.Synchronizer().ViewContext())
	if !ok {
		cs.mods.Logger().Debug("Propose: No command")
		return
	}
	var proposal ProposeMsg
	if proposer, ok := cs.impl.(ProposeRuler); ok {
		proposal, ok = proposer.ProposeRule(cert, cmd)
		if !ok {
			cs.mods.Logger().Debug("Propose: No block")
			return
		}
	} else {
		proposal = ProposeMsg{
			ID: cs.mods.ID(),
			Block: NewBlock(
				cs.mods.Synchronizer().LeafBlock().Hash(),
				qc,
				cmd,
				cs.mods.Synchronizer().View(),
				cs.mods.ID(),
			),
		}

		if aggQC, ok := cert.AggQC(); ok && cs.mods.Options().ShouldUseAggQC() {
			proposal.AggregateQC = &aggQC
		}
	}

	cs.mods.BlockChain().Store(proposal.Block)

	cs.mods.Configuration().Propose(proposal)
	// self vote
	//cs.OnPropose(proposal)
}

func (cs *consensusBase) OnPropose(proposal ProposeMsg, list BatchList) {
	cs.mods.Logger().Debugf("OnPropose: %v", proposal.Block)
	block := proposal.Block

	if cs.mods.Options().ShouldUseAggQC() && proposal.AggregateQC != nil {
		ok, highQC := cs.mods.Crypto().VerifyAggregateQC(*proposal.AggregateQC)
		if !ok {
			cs.mods.Logger().Warn("OnPropose: failed to verify aggregate QC")
			return
		}
		// NOTE: for simplicity, we require that the highQC found in the AggregateQC equals the QC embedded in the block.
		if !block.QuorumCert().Equals(highQC) {
			cs.mods.Logger().Warn("OnPropose: block QC does not equal highQC")
			return
		}
	}

	if !cs.mods.Crypto().VerifyQuorumCert(block.QuorumCert()) {
		cs.mods.Logger().Info("OnPropose: invalid QC")
		return
	}
	cs.mods.synchronizer.UpdateHighQC(block.QuorumCert())
	//更新节点packagedheight
	// ensure the block came from the leader.
	if proposal.ID != cs.mods.LeaderRotation().GetLeader(block.View()) {
		cs.mods.Logger().Info("OnPropose: block was not proposed by the expected leader")
		return
	}

	if !cs.impl.VoteRule(proposal) {
		cs.mods.Logger().Info("OnPropose: Block not voted for")
		return
	}

	if qcBlock, ok := cs.mods.BlockChain().Get(block.QuorumCert().BlockHash()); ok {
		cs.mods.Acceptor().Proposed(qcBlock.Command())
	} else {
		cs.mods.Logger().Info("OnPropose: Failed to fetch qcBlock")
	}

	if !cs.mods.Acceptor().Accept(block.Command()) {
		cs.mods.Logger().Info("OnPropose: command not accepted ", block.hash.String())
		return
	}

	// block is safe and was accepted
	cs.mods.multichainlist.UpdatePackagedHeight(list)
	cs.mods.BlockChain().Store(block)
	//fmt.Println("Node: ", cs.mods.ID(), " stores block ", block.hash.String())
	//block.cert.hash = block.Hash()
	//block.cert.view = block.view
	// we defer the following in order to speed up voting
	defer func() {
		if b := cs.impl.CommitRule(block); b != nil {
			cs.commit(b)
		}
		cs.mods.Synchronizer().AdvanceView(NewSyncInfo().WithQC(block.QuorumCert()))
	}()

	if block.View() <= cs.lastVote {
		cs.mods.Logger().Info("OnPropose: block view too old ", block.String())
		//fmt.Println("Node: ", cs.mods.ID(), "OnPropose: block view too old ", block.hash.String())
		return
	}

	pc, err := cs.mods.Crypto().CreatePartialCert(block)
	if err != nil {
		cs.mods.Logger().Error("OnPropose: failed to sign vote: ", err)
		//fmt.Println("Node: ", cs.mods.ID(), " OnPropose: failed to sign vote: ", err)
		return
	}

	cs.lastVote = block.View()

	leaderID := cs.mods.LeaderRotation().GetLeader(cs.lastVote + 1)
	if leaderID == cs.mods.ID() {
		cs.mods.EventLoop().AddEvent(VoteMsg{ID: cs.mods.ID(), PartialCert: pc})
		return
	}

	leader, ok := cs.mods.Configuration().Replica(leaderID)
	if !ok {
		cs.mods.Logger().Warnf("Replica with ID %d was not found!", leaderID)
		return
	}

	leader.Vote(pc)
	fmt.Println("Node ", cs.mods.ID(), "vote to block ", block.hash.String(), "at time: ", time.Now().UnixMilli())
}

func (cs *consensusBase) commit(block *Block) {
	cs.mut.Lock()
	// can't recurse due to requiring the mutex, so we use a helper instead.
	cs.commitInner(block)
	cs.mut.Unlock()

	// prune the blockchain and handle forked blocks
	forkedBlocks := cs.mods.BlockChain().PruneToHeight(block.View())
	for _, block := range forkedBlocks {
		cs.mods.ForkHandler().Fork(block)
	}
}

// recursive helper for commit
func (cs *consensusBase) commitInner(block *Block) {
	if cs.bExec.View() < block.View() {
		if parent, ok := cs.mods.BlockChain().Get(block.Parent()); ok {
			cs.commitInner(parent)
		} else {
			cs.mods.Logger().Warn("Refusing to commit because parent block could not be retrieved.")
			return
		}
		fmt.Println("Node id：", cs.mods.ID(), " Start commit block:", block.Hash().String(), " at time: ", time.Now().UnixMilli())
		cs.mods.Logger().Debug("EXEC: ", block)
		cs.mods.Executor().Exec(block)
		cs.bExec = block
	}
}

// ChainLength returns the number of blocks that need to be chained together in order to commit.
func (cs *consensusBase) ChainLength() int {
	return cs.impl.ChainLength()
}
