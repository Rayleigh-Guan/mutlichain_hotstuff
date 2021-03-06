package replica

import (
	"context"
	"fmt"
	"sync"

	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/consensus"
	"github.com/relab/hotstuff/internal/proto/clientpb"
	"github.com/relab/hotstuff/internal/proto/hotstuffpb"
	"github.com/relab/hotstuff/logging"

	"google.golang.org/protobuf/proto"
)

type MultiChain struct {
	mut sync.Mutex //Mutex

	ChainPool      map[hotstuff.ID][]consensus.Batch //multichain
	tmpChainPool   map[hotstuff.ID]map[int32]consensus.Batch
	ChainPoolTip   map[hotstuff.ID]map[uint32]int32 //Record the latest altitude received by other nodes
	PackagedHeight map[hotstuff.ID]int32            //Record the height to which each batch chain has been packaged

	MyGeneratedHeight int32       //The latest height of the batch generated by the current node
	NodeID            hotstuff.ID //Node id
	ReplicaNum        int         //the number of replica
	CanUpdateMyTip    bool

	EcEg        *EcEngine
	EcBatchPool map[hotstuff.ID]map[int32]map[int32]consensus.EcBatch

	marshaler   proto.MarshalOptions
	unmarshaler proto.UnmarshalOptions
}

func newMultiChain(id hotstuff.ID) (multichain *MultiChain) {
	multichain = &MultiChain{
		ChainPool:      make(map[hotstuff.ID][]consensus.Batch),
		tmpChainPool:   make(map[hotstuff.ID]map[int32]consensus.Batch),
		ChainPoolTip:   make(map[hotstuff.ID]map[uint32]int32),
		PackagedHeight: make(map[hotstuff.ID]int32),

		MyGeneratedHeight: 0,
		NodeID:            id,
		ReplicaNum:        4, //mustdo:when change node number
		CanUpdateMyTip:    false,

		EcBatchPool: make(map[hotstuff.ID]map[int32]map[int32]consensus.EcBatch),

		marshaler:   proto.MarshalOptions{Deterministic: true},
		unmarshaler: proto.UnmarshalOptions{DiscardUnknown: true},
	}

	dataNum := (multichain.ReplicaNum-1)*2/3 + 1
	parityNum := multichain.ReplicaNum - dataNum
	multichain.EcEg = newEcEngine(dataNum, parityNum)

	for i := 1; i <= multichain.ReplicaNum; i++ {
		multichain.tmpChainPool[hotstuff.ID(i)] = make(map[int32]consensus.Batch)
		multichain.EcBatchPool[hotstuff.ID(i)] = make(map[int32]map[int32]consensus.EcBatch)
	}
	multichain.PackagedHeight[id] = 0
	return multichain
}
func (c *MultiChain) CheckTmpToUpdate(id hotstuff.ID, newheight int32) {
	for {
		_, ok := c.tmpChainPool[id][newheight]
		if !ok {
			break
		}
		c.ChainPool[id] = append(c.ChainPool[id], c.tmpChainPool[id][newheight])
		delete(c.tmpChainPool[id], newheight)
		newheight = newheight + 1
	}

}
func (c *MultiChain) AddEcBatch(b *consensus.EcBatch) {
	if b.NodeID == c.NodeID {
		return
	}
	c.mut.Lock()
	batchlen := len(c.ChainPool[b.NodeID])
	if batchlen > 0 && c.ChainPool[b.NodeID][batchlen-1].BatchID >= b.BatchID {
		return
	}
	_, ok := c.tmpChainPool[b.NodeID][b.BatchID]
	if ok {
		return
	}
	if c.EcBatchPool[b.NodeID][b.BatchID] == nil {
		c.EcBatchPool[b.NodeID][b.BatchID] = make(map[int32]consensus.EcBatch)
	}
	c.EcBatchPool[b.NodeID][b.BatchID][b.Order] = *b

	if len(c.EcBatchPool[b.NodeID][b.BatchID]) >= c.EcEg.dataNum {
		var dphas []int
		var needReconst []int
		var ecmatrix [][]byte
		var datalen int
		var realLen []int32
		ecmatrix = make([][]byte, c.ReplicaNum)

		for i := 0; i < c.ReplicaNum; i++ {
			value, ok := c.EcBatchPool[b.NodeID][b.BatchID][int32(i)]
			if ok {
				dphas = append(dphas, i)
				//fmt.Println("--receive value.nodeid: ", value.NodeID, "  value.Order:", value.Order, " value.BatchID:", value.BatchID, " value.len", len(value.Data), "value.realen: ", value.RealLen[i])
				ecmatrix[i] = value.Data
				datalen = len(value.Data)
				realLen = value.RealLen
			} else {
				needReconst = append(needReconst, i)
			}
		}
		c.EcBatchPool[b.NodeID][b.BatchID] = nil
		c.mut.Unlock()

		for _, element := range needReconst {
			ecmatrix[element] = []byte(c.EcEg.randCreator(datalen))
		}
		//for i := 0; i < c.ReplicaNum; i++ {
		//	fmt.Println("i: ", i, " length: ", len(ecmatrix[i]))
		//}
		//fmt.Println(datalen, "----", realLen)
		batch := c.Ecdecode(ecmatrix, dphas, needReconst, realLen)

		c.Add(batch.NodeID, batch)
	} else {
		c.mut.Unlock()
	}

}
func (c *MultiChain) Add(id hotstuff.ID, b *consensus.Batch) {
	c.mut.Lock()

	if _, ok := c.PackagedHeight[id]; !ok {
		c.PackagedHeight[id] = 0
	}

	//if b.Cmd != "" {
	batchLen := len(c.ChainPool[id])
	maxbatchid := 0
	if batchLen == 0 {
		maxbatchid = 0
	} else {
		maxbatchid = int(c.ChainPool[id][batchLen-1].BatchID)
	}
	if maxbatchid+1 == int((*b).BatchID) {
		c.ChainPool[id] = append(c.ChainPool[id], *b)
		c.CheckTmpToUpdate(id, (*b).BatchID+1)
		if id != c.NodeID {
			c.CanUpdateMyTip = true
		}
	} else {
		c.tmpChainPool[id][(*b).BatchID] = *b
	}

	//}
	c.ChainPoolTip[id] = b.ChainPoolTip

	//else {
	// 	c.CanUpdateMyTip = false
	// 	//c.GetCommandsfrombatch(b)
	// }

	c.mut.Unlock()
}

func (c *MultiChain) deleteByID(nodeid uint32, batchid int32) {
	if batchid < (c.ChainPool[hotstuff.ID(nodeid)][0].BatchID)+2000 {
		return
	}
	for index, value := range c.ChainPool[c.NodeID] {
		if value.BatchID == int32(batchid)-1000 {
			c.ChainPool[hotstuff.ID(nodeid)] = c.ChainPool[hotstuff.ID(nodeid)][index+1:] //Remove old content by slicing
			break
		}
	}
}

func (c *MultiChain) packBatch(ctx context.Context, cache *cmdCache) (batch consensus.Batch, ok bool) {
	cmd, ok := cache.Get(ctx)
	if (!ok) && (!c.CanUpdateMyTip) {
		return batch, false
	}
	var genesisHash [32]byte

	c.mut.Lock()

	//if cmd != "" {
	c.MyGeneratedHeight = c.MyGeneratedHeight + 1
	//}
	c.CanUpdateMyTip = false

	if c.MyGeneratedHeight == 1 {
		b := consensus.NewBatch(genesisHash, c.NodeID, cmd, c.MyGeneratedHeight, c.GetMyChainPoolTip())
		batch = *b
	} else {
		b := consensus.NewBatch(c.ChainPool[c.NodeID][len(c.ChainPool[c.NodeID])-1].Hash, c.NodeID, cmd, c.MyGeneratedHeight, c.GetMyChainPoolTip())
		batch = *b
	}
	c.mut.Unlock()
	return batch, true
}
func (c *MultiChain) GetMyChainPoolTip() map[uint32]int32 {
	mytip := make(map[uint32]int32)
	for i := 1; i <= c.ReplicaNum; i++ {
		poollen := len(c.ChainPool[hotstuff.ID(i)])
		if poollen <= 0 {
			mytip[uint32(i)] = -1
		} else {
			mytip[uint32(i)] = c.ChainPool[hotstuff.ID(i)][len(c.ChainPool[hotstuff.ID(i)])-1].BatchID
		}
	}
	return mytip
}

func (c *MultiChain) PackList() (batchlist consensus.BatchList, ok bool) {
	c.mut.Lock()

	//totoal_num := int(0)
	b := new(hotstuffpb.BatchList)
	quorum := c.ReplicaNum
	usebatchhash := false
	for i := 1; i <= c.ReplicaNum; i++ {

		batchLen := len(c.ChainPool[hotstuff.ID(i)])
		if batchLen <= 0 {
			continue
		}
		startHeight := c.PackagedHeight[hotstuff.ID(i)] + 1
		endHeight := c.ChainPool[hotstuff.ID(i)][batchLen-1].BatchID
		usebatchhash = false
		if endHeight >= startHeight {
			nNotFallBehind := 0
			for j := 1; j <= c.ReplicaNum; j++ {
				temptip := c.ChainPoolTip[hotstuff.ID(j)][uint32(i)]
				if temptip <= 0 || temptip < startHeight {
					continue
				}
				nNotFallBehind = nNotFallBehind + 1
				if temptip < endHeight {
					endHeight = temptip
				}
			}
			if nNotFallBehind >= quorum {
				usebatchhash = true
			} else {
				endHeight = startHeight
			}
			temp := hotstuffpb.BatchListitem{StartHeight: startHeight, EndHeight: endHeight, NodeID: uint32(i), UseBatchHash: usebatchhash}
			b.BatchListitems = append(b.BatchListitems, &temp)
		}
	}
	c.mut.Unlock()
	//totoal_num = len(b.BatchListitems) * 16
	//fmt.Println("block commands:", totoal_num)
	if len(b.BatchListitems) <= 0 {
		//fmt.Println(c.NodeID, " No packlist")
		return "", false
	}
	p, err := c.marshaler.Marshal(b)
	if err != nil {
		return "", false
	}
	batchlist = consensus.BatchList(p)
	return batchlist, true
}

//func (c *MultiChain) GetCommands(list consensus.BatchList) (cmds []*clientpb.Command) {
//
//	batchtest := new(hotstuffpb.BatchList)
//	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(list), batchtest)
//	if err == nil {
//		for _, item := range batchtest.GetBatchListitems() {
//			for i := item.StartHeight; i <= item.EndHeight; i++ {
//				cmdtest := new(clientpb.Batch)
//				err1 := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(c.ChainPool[hotstuff.ID(item.NodeID)][i].Cmd), cmdtest)
//				if err1 == nil {
//					for _, cmd := range cmdtest.GetCommands() {
//						cmds = append(cmds, cmd)
//					}
//				}
//			}
//
//		}
//	}
//	return cmds
//}
//
//func (c *MultiChain) GetCommandsfrombatch(batch *consensus.Batch) (cmds []*clientpb.Command) {
//
//	batchtest := new(clientpb.Batch)
//	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(batch.Cmd), batchtest)
//	if err == nil {
//		for _, cmd := range batchtest.GetCommands() {
//			fmt.Println("--Batch Information Nodeid: ", batch.NodeID, " Batchid: ", batch.BatchID, " cmd.clientid: ", cmd.ClientID, " cmd.sequence: ", cmd.SequenceNumber)
//		}
//	}
//	return cmds
//}

//func (c *MultiChain) Findindex(des int32) (myindex int) {
//	for index, value := range c.ChainPool[c.NodeID] {
//		if value.BatchID == int32(des) {
//			return index
//		}
//	}
//	return int(des)
//}

func (c *MultiChain) GetCmd(list consensus.BatchList, cmdType bool, cslogger logging.Logger) (cmd consensus.Command, ok bool) {

	if list == "" {
		return "", false
	}
	batchtest := new(hotstuffpb.BatchList)
	cmds := new(clientpb.Batch)
	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(list), batchtest)

	if err == nil {
		c.mut.Lock()
		for _, item := range batchtest.GetBatchListitems() {
			if item.UseBatchHash != cmdType {
				continue
			}
			low := c.ChainPool[hotstuff.ID(item.NodeID)][0].BatchID
			//low:=int32(c.Findindex(item.StartHeight))
			//batchlen := len(c.ChainPool[hotstuff.ID(item.NodeID)])
			//cslogger.Info("chainid: ",item.NodeID," batchlen: ",batchlen," maxbatchid: ",c.ChainPool[hotstuff.ID(item.NodeID)][batchlen-1].BatchID," endheight: ",item.EndHeight)
			for i := item.StartHeight; i <= item.EndHeight; i++ {
				// if (i-item.StartHeight+low)<c.ChainPool[hotstuff.ID(item.NodeID)][i-item.StartHeight+low].BatchID {
				// 	break
				// }
				//if int(i-low) >= batchlen {
				//	break
				//}
				cmdtest := new(clientpb.Batch)
				err1 := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(c.ChainPool[hotstuff.ID(item.NodeID)][i-low].Cmd), cmdtest)
				if err1 == nil {
					for _, cmd := range cmdtest.GetCommands() {
						cmds.Commands = append(cmds.Commands, cmd)
					}
				}
			}
		}
		c.mut.Unlock()
	} else {
		return "", false
	}

	b, err := c.marshaler.Marshal(cmds)
	cmd = consensus.Command(b)
	return cmd, true
}

func (c *MultiChain) UpdatePackagedHeight(list consensus.BatchList) {

	batchtest := new(hotstuffpb.BatchList)
	c.mut.Lock()
	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(list), batchtest)
	if err == nil {
		for _, item := range batchtest.GetBatchListitems() {
			if item.EndHeight > c.PackagedHeight[hotstuff.ID(item.NodeID)] {
				c.PackagedHeight[hotstuff.ID(item.NodeID)] = item.EndHeight
				//c.deleteByID(item.NodeID, item.EndHeight)
			}
		}
	}
	c.mut.Unlock()
}

//func (c *MultiChain) GetBatchItem(list consensus.BatchList) {
//
//	batchtest := new(hotstuffpb.BatchList)
//	//flag := true
//	//last := int32(-1)
//	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(list), batchtest)
//	if err == nil {
//		for _, item := range batchtest.GetBatchListitems() {
//			fmt.Println("Node id:", item.NodeID, " --StartHeight:", item.StartHeight, " --EndHeight:", item.EndHeight, " --usebatchhash:", item.UseBatchHash)
//			//cmdtest := new(clientpb.Batch)
//			for i := item.StartHeight - 1; i < item.EndHeight; i++ {
//				if i+1 != c.ChainPool[hotstuff.ID(item.NodeID)][i].BatchID {
//					fmt.Println("should be:", i+1, " real value:", c.ChainPool[hotstuff.ID(item.NodeID)][i].BatchID)
//				}
//				//fmt.Println("should be:", i+1, " real value:", c.ChainPool[hotstuff.ID(item.NodeID)][i].BatchID)
//				//err1 := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(c.ChainPool[hotstuff.ID(item.NodeID)][i].Cmd), cmdtest)
//				//if err1 == nil {
//				//	for _, cmd := range cmdtest.GetCommands() {
//				//		fmt.Println("-- client id:", cmd.ClientID, " --sequence number:", cmd.SequenceNumber)
//				//	}
//				//}
//				//if last == -1 {
//				//	last = item.EndHeight
//				//} else {
//				//	if last != item.EndHeight {
//				//		flag = false
//				//	}
//				//}
//			}
//		}
//	}
//	//if !flag {
//	//	fmt.Println("has old command", last)
//	//}
//}
func (c *MultiChain) Ecencode(batch consensus.Batch) (ecMatrix [][]byte, realLen []int32) {
	originbatch := hotstuffpb.BatchToProto(&batch)
	batchinbyte, err := c.marshaler.Marshal(originbatch)
	if err != nil {
		return
	}
	ecMatrix, realLen = c.EcEg.CreateEcMatrix(batchinbyte)

	return ecMatrix, realLen
}
func (c *MultiChain) Ecdecode(EcMatrix [][]byte, dphas, needReconst []int, realLen []int32) (batch *consensus.Batch) {
	batchinbytpe := c.EcEg.EcDecode(EcMatrix, dphas, needReconst, realLen)
	batchinProto := new(hotstuffpb.Batch)
	err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal([]byte(batchinbytpe), batchinProto)
	if err != nil {
		fmt.Println(err)
	}
	batchTmp := hotstuffpb.BatchFromProto(batchinProto)
	batch = &batchTmp
	return batch
}
