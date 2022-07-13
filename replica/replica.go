// Package replica provides the required code for starting and running a replica and handling client requests.
package replica

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/relab/gorums"
	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/backend"
	"github.com/relab/hotstuff/consensus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
)

// cmdID is a unique identifier for a command
type cmdID struct {
	clientID    uint32
	sequenceNum uint64
}

// Config configures a replica.
type Config struct {
	// The id of the replica.
	ID hotstuff.ID
	// The private key of the replica.
	PrivateKey consensus.PrivateKey
	// Controls whether TLS is used.
	TLS bool
	// The TLS certificate.
	Certificate *tls.Certificate
	// The root certificates trusted by the replica.
	RootCAs *x509.CertPool
	// The number of client commands that should be batched together in a block.
	BatchSize uint32
	// Options for the client server.
	ClientServerOptions []gorums.ServerOption
	// Options for the replica server.
	ReplicaServerOptions []gorums.ServerOption
	// Options for the replica manager.
	ManagerOptions []gorums.ManagerOption
}

// Replica is a participant in the consensus protocol.
type Replica struct {
	clientSrv *clientSrv
	cfg       *backend.Config
	hsSrv     *backend.Server
	hs        *consensus.Modules
	mulChain  *MultiChain

	execHandlers map[cmdID]func(*emptypb.Empty, error)
	cancel       context.CancelFunc
	done         chan struct{}
}

// New returns a new replica.
func New(conf Config, builder consensus.Builder) (replica *Replica) {
	clientSrvOpts := conf.ClientServerOptions

	if conf.TLS {
		clientSrvOpts = append(clientSrvOpts, gorums.WithGRPCServerOptions(
			grpc.Creds(credentials.NewServerTLSFromCert(conf.Certificate)),
		))
	}

	clientSrv := newClientServer(conf, clientSrvOpts)

	srv := &Replica{
		clientSrv:    clientSrv,
		execHandlers: make(map[cmdID]func(*emptypb.Empty, error)),
		cancel:       func() {},
		done:         make(chan struct{}),
		mulChain:     newMultiChain(conf.ID),
	}

	replicaSrvOpts := conf.ReplicaServerOptions
	if conf.TLS {
		replicaSrvOpts = append(replicaSrvOpts, gorums.WithGRPCServerOptions(
			grpc.Creds(credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{*conf.Certificate},
				ClientCAs:    conf.RootCAs,
				ClientAuth:   tls.RequireAndVerifyClientCert,
			})),
		))
	}

	srv.hsSrv = backend.NewServer(replicaSrvOpts...)

	var creds credentials.TransportCredentials
	managerOpts := conf.ManagerOptions
	if conf.TLS {
		creds = credentials.NewTLS(&tls.Config{
			RootCAs:      conf.RootCAs,
			Certificates: []tls.Certificate{*conf.Certificate},
		})
	}
	srv.cfg = backend.NewConfig(creds, managerOpts...)

	builder.Register(
		srv.cfg,                // configuration
		srv.hsSrv,              // event handling
		srv.clientSrv,          // executor
		srv.clientSrv.cmdCache, // acceptor and command queue
		srv.mulChain,
	)
	srv.hs = builder.Build()

	return srv
}

// StartServers starts the client and replica servers.
func (srv *Replica) StartServers(replicaListen, clientListen net.Listener) {
	srv.hsSrv.StartOnListener(replicaListen)
	srv.clientSrv.StartOnListener(clientListen)
}

// Connect connects to the other replicas.
func (srv *Replica) Connect(replicas []backend.ReplicaInfo) error {
	return srv.cfg.Connect(replicas)
}

// Start runs the replica in a goroutine.
func (srv *Replica) Start() {
	var ctx context.Context
	ctx, srv.cancel = context.WithCancel(context.Background())
	predisFull := true
	go func() {
		lasttime := time.Now()
		var unicastOrder []int
		unicastOrder = make([]int, srv.mulChain.ReplicaNum)
		for i := 0; i < srv.mulChain.ReplicaNum; i++ {
			unicastOrder[i] = i + 1
		}
		for {
			select {
			case <-ctx.Done():
			default:
				if time.Now().Sub(lasttime) > 3*time.Millisecond {
					lasttime = time.Now()
					batch, ok := srv.mulChain.packBatch(ctx, srv.clientSrv.cmdCache)
					if ok { //|| srv.mulChain.CanUpdateMyTip {
						srv.mulChain.Add(srv.mulChain.NodeID, &batch)
						//srv.hs.Configuration().ProposeBatchMultiCast(&batch)
						if predisFull {
							//println("nodeid:", srv.mulChain.NodeID, "  success pack a batch,batchsize:", batch.BatchID)
							srv.hs.Configuration().ProposeBatchMultiCast(&batch)
							//srv.hs.Configuration().ProposeBatch_unicast(batchmsg)
						} else {
							EcmatrixReplica, reallenReplica := srv.mulChain.Ecencode(batch)
							rand.Seed(time.Now().UnixNano())
							rand.Shuffle(len(unicastOrder), func(i, j int) { unicastOrder[i], unicastOrder[j] = unicastOrder[j], unicastOrder[i] })
							//fmt.Println("--send: Node id: ", srv.mulChain.NodeID, " Batchid: ", batch.BatchID, "reaLen: ", reallenReplica)
							for i := 0; i < srv.mulChain.ReplicaNum; i++ {
								//fmt.Println("--send: Node id: ", srv.mulChain.NodeID, " Batchid: ", batch.BatchID, "order: ", i, " len:", len(EcmatrixReplica[i]))
								//if hotstuff.ID(unicast_order[i]) == srv.mulChain.NodeID {
								//	continue
								//}
								node, ok := srv.hs.Configuration().Replica(hotstuff.ID(unicastOrder[i]))
								if ok {
									Ecbatch := consensus.NewEcBatch(srv.mulChain.NodeID, srv.mulChain.NodeID, batch.BatchID, int32(i), reallenReplica, EcmatrixReplica[i])
									node.ProposeEcBatchUniCast(Ecbatch)
								} else {
									fmt.Println("unicast faild from: ", srv.mulChain.NodeID, " to: ", unicastOrder[i])
								}
							}
						}

					}
				}
			}
		}
	}()

	go func() {
		srv.Run(ctx)
		//fmt.Println("nodeid:", srv.hs.ID(), "packed height:", srv.mulChain.PackagedHeight[1], "total len ", len(srv.mulChain.ChainPool[1]))
		//for _, v := range srv.mulChain.ChainPool[1] {
		//	fmt.Print(v.BatchID, " ")
		//}
		//fmt.Println()
		//fmt.Println("nodeid:", srv.hs.ID(), "packed height:", srv.mulChain.PackagedHeight[2], "total len ", len(srv.mulChain.ChainPool[2]))
		//for _, v := range srv.mulChain.ChainPool[2] {
		//	fmt.Print(v.BatchID, " ")
		//}
		//fmt.Println()
		//fmt.Println("nodeid:", srv.hs.ID(), "packed height:", srv.mulChain.PackagedHeight[3], "total len ", len(srv.mulChain.ChainPool[3]))
		//for _, v := range srv.mulChain.ChainPool[3] {
		//	fmt.Print(v.BatchID, " ")
		//}
		//fmt.Println()
		//fmt.Println("nodeid:", srv.hs.ID(), "packed height:", srv.mulChain.PackagedHeight[4], "total len ", len(srv.mulChain.ChainPool[4]))
		//for _, v := range srv.mulChain.ChainPool[4] {
		//	fmt.Print(v.BatchID, " ")
		//}
		//fmt.Println()
		close(srv.done)
	}()
}

// Stop stops the replica and closes connections.
func (srv *Replica) Stop() {
	srv.cancel()
	<-srv.done
	srv.Close()
}

// Run runs the replica until the context is cancelled.
func (srv *Replica) Run(ctx context.Context) {
	srv.hs.Synchronizer().Start(ctx)
	srv.hs.Run(ctx)
}

// Close closes the connections and stops the servers used by the replica.
func (srv *Replica) Close() {
	srv.clientSrv.Stop()
	srv.cfg.Close()
	srv.hsSrv.Stop()
}

// GetHash returns the hash of all executed commands.
func (srv *Replica) GetHash() (b []byte) {
	return srv.clientSrv.hash.Sum(b)
}
