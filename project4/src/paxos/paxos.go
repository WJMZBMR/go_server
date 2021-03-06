package paxos

//
// Paxos library, to be included in an application.
// Multiple applications will run, each including
// a Paxos peer.
//
// Manages a sequence of agreed-on values.
// The set of peers is fixed.
// Copes with network failures (partition, msg loss, &c).
// Does not store anything persistently, so cannot handle crash+restart.
//
// The application interface:
//
// px = paxos.MakeUseTCP(peers []string, me string)
// px = paxos.Make(peers []string, me string, useTCP bool)
// px.Start(seq int, v interface{}) -- start agreement on new instance
// px.Status(seq int) (decided bool, v interface{}) -- get info about an instance
// px.Done(seq int) -- ok to forget all instances <= seq
// px.Max() int -- highest instance seq known, or -1
// px.Min() int -- instances before this seq have been forgotten
//

import "net"
import "net/rpc"
import "log"
import "os"
import "syscall"
import "sync"
import "fmt"
import "math/rand"

type Paxos struct {
	mu         sync.Mutex
	l          net.Listener
	dead       bool
	unreliable bool
	rpcCount   int
	peers      []string
	me         int // index into peers[]

	// Your data here.
	DecideList map[int]interface{}
	DoneList   []int
	agreement map[int]Proposal
	useTCP     bool
}

const (
	PREPARE       = "Prepare"
	PREPAREOK     = "PrepareOK"
	PREPAREREJECT = "PrepareReject"
	ACCEPT        = "Accept"
	ACCEPTOK      = "AcceptOK"
	ACCEPTREJECT  = "AcceptReject"
	DECIDED       = "Decided"
)

type AcceptorArgs struct {
	Type           string
	Seq            int
	Done           []int
	ProposalNumber int
	ProposalValue  interface{}
}

type AcceptorReply struct {
	RType          string
	Np             int
	Done           []int
	AcceptorNumber int
	AcceptorValue  interface{}
}

type Proposal struct {
	n_p            int
	n_a            int
	v_a            interface{}
	proposalNumber int
}

func (px *Paxos) UpdateProposal(seq int, field string, newValue int) {
	//px.mu.Lock()
	//defer px.mu.Unlock()
	switch {
	case field == "n_p":
		px.agreement[seq] = Proposal{
			newValue,
			px.agreement[seq].n_a,
			px.agreement[seq].v_a,
			px.agreement[seq].proposalNumber}
	case field == "n_a":
		px.agreement[seq] = Proposal{
			px.agreement[seq].n_p,
			newValue,
			px.agreement[seq].v_a,
			px.agreement[seq].proposalNumber}
	case field == "proposalNumber":
		px.agreement[seq] = Proposal{
			px.agreement[seq].n_p,
			px.agreement[seq].n_a,
			px.agreement[seq].v_a,
			newValue}
	}
}

func (px *Paxos) UpdateProposalValue(seq int, newValue interface{}) {
	//px.mu.Lock()
	//defer px.mu.Unlock()
	px.agreement[seq] = Proposal{
		px.agreement[seq].n_p,
		px.agreement[seq].n_a,
		newValue,
		px.agreement[seq].proposalNumber}
}

//
// call() sends an RPC to the rpcname handler on server srv
// with arguments args, waits for the reply, and leaves the
// reply in reply. the reply argument should be a pointer
// to a reply structure.
//
// the return value is true if the server responded, and false
// if call() was not able to contact the server. in particular,
// the replys contents are only valid if call() returned true.
//
// you should assume that call() will time out and return an
// error after a while if it does not get a reply from the server.
//
// please use call() to send all RPCs, in client.go and server.go.
// please do not change this function.
//
func call(srv string, name string, args interface{}, reply interface{},useTCP bool) bool {
	conntype := "unix";
	if (useTCP){
		conntype = "tcp";
	}
	c, err := rpc.Dial(conntype, srv)
	if err != nil {
		err1 := err.(*net.OpError)
		if err1.Err != syscall.ENOENT && err1.Err != syscall.ECONNREFUSED {
			fmt.Printf("paxos Dial() failed: %v\n", err1)
		}
		return false
	}
	defer c.Close()

	err = c.Call(name, args, reply)
	if err == nil {
		return true
	}
	return false
}


func (px *Paxos) Proposer(seq int, v interface{}) {

	for px.dead == false {
		args := &AcceptorArgs{}
		args.Type = PREPARE
		args.Seq = seq

		px.mu.Lock()
		pp := px.agreement[seq]
		args.Done = px.DoneList
		temppp := (((pp.proposalNumber / len(px.peers)) + 1) * len(px.peers)) + px.me
		px.UpdateProposal(seq, "proposalNumber", temppp)
		args.ProposalNumber = temppp
		px.mu.Unlock()

		pcount := 0
		tmp_n_a := 0
		var tmp_v_a interface{}
		for i := 0; i < len(px.peers); i++ {
			var reply AcceptorReply
			ok := false
			if i == px.me {
				err := px.Acceptor(args, &reply)
				if err == nil {
					ok = true
				}
			} else {
				ok = call(px.peers[i], "Paxos.Acceptor", args, &reply,px.useTCP)
			}

			if ok {
				px.mu.Lock()
				for j := 0; j < len(px.peers); j++ {
					if px.DoneList[j] < reply.Done[j] {
						px.DoneList[j] = reply.Done[j]
					}
				}
				px.mu.Unlock()

				switch reply.RType {
				case PREPAREOK:
					pcount++
					if reply.AcceptorNumber > tmp_n_a {
						tmp_n_a = reply.AcceptorNumber
						tmp_v_a = reply.AcceptorValue
					}
				case PREPAREREJECT:
					px.mu.Lock()
					if reply.Np > px.agreement[seq].proposalNumber {
						px.UpdateProposal(seq, "proposalNumber", reply.Np)
					}
					px.mu.Unlock()
				}
			}
		}

		if pcount > len(px.peers)/2 {
			if tmp_v_a == nil {
				args.ProposalValue = v
			} else {
				args.ProposalValue = tmp_v_a
			}
		} else {
			continue
		}
		args.Type = ACCEPT
		acount := 0
		for i := 0; i < len(px.peers); i++ {
			var reply AcceptorReply
			ok := false
			if i == px.me {
				err := px.Acceptor(args, &reply)
				if err == nil {
					ok = true
				}
			} else {
				ok = call(px.peers[i], "Paxos.Acceptor", args, &reply,px.useTCP)
			}

			if ok {
				switch reply.RType {
				case ACCEPTOK:
					acount++
				case ACCEPTREJECT:
					px.mu.Lock()
					if reply.Np > px.agreement[seq].proposalNumber {
						px.UpdateProposal(seq, "proposalNumber", reply.Np)
					}
					px.mu.Unlock()
				}
			}
		}

		if acount <= len(px.peers)/2 {
			continue
		}
		args.Type = DECIDED
		for i := 0; i < len(px.peers); i++ {
			var reply AcceptorReply
			if i == px.me {
				px.Acceptor(args, &reply)
			} else {
				call(px.peers[i], "Paxos.Acceptor", args, &reply,px.useTCP)
			}
		}

		break
	}
}

func (px *Paxos) Acceptor(args *AcceptorArgs, reply *AcceptorReply) error {
	px.mu.Lock()
	pp := px.agreement[args.Seq]

	switch args.Type {
	case PREPARE:
		for i := 0; i < len(px.peers); i++ {
			if px.DoneList[i] < args.Done[i] {
				px.DoneList[i] = args.Done[i]
			}
		}

		if args.ProposalNumber > pp.n_p {
			px.UpdateProposal(args.Seq, "n_p", args.ProposalNumber)
			reply.RType = PREPAREOK
			reply.AcceptorNumber = pp.n_a
			reply.AcceptorValue = pp.v_a
			reply.Done = px.DoneList
		} else {
			reply.RType = PREPAREREJECT
			reply.Np = pp.n_p
			reply.Done = px.DoneList
		}
	case ACCEPT:
		if args.ProposalNumber >= pp.n_p {
			px.UpdateProposal(args.Seq, "n_p", args.ProposalNumber)
			px.UpdateProposal(args.Seq, "n_a", args.ProposalNumber)
			px.UpdateProposalValue(args.Seq, args.ProposalValue)

			reply.RType = ACCEPTOK
			reply.AcceptorNumber = args.ProposalNumber
		} else {
			reply.RType = ACCEPTREJECT
		}
	case DECIDED:
		px.DecideList[args.Seq] = args.ProposalValue
	default:
		panic("Impossible place.")
	}

	px.mu.Unlock()
	min := px.Min()
	px.mu.Lock()
	for i, _ := range px.DecideList {
		if i < min {
			delete(px.DecideList, i)
			delete(px.agreement, i)
		}
	}
	defer px.mu.Unlock()
	return nil
}

//
// the application wants paxos to start agreement on
// instance seq, with proposed value v.
// Start() returns right away; the application will
// call Status() to find out if/when agreement
// is reached.
//
func (px *Paxos) Start(seq int, v interface{}) {
	go px.Proposer(seq, v)
}

//
// the application on this machine is done with
// all instances <= seq.
//
// see the comments for Min() for more explanation.
//
func (px *Paxos) Done(seq int) {
	px.mu.Lock()
	defer px.mu.Unlock()
	px.DoneList[px.me] = seq
}

//
// the application wants to know the
// highest instance sequence known to
// this peer.
//
func (px *Paxos) Max() int {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	result := -1
	for k, _ := range px.DecideList {
		if k > result {
			result = k
		}
	}
	return result
}

//
// Min() should return one more than the minimum among z_i,
// where z_i is the highest number ever passed
// to Done() on peer i. A peers z_i is -1 if it has
// never called Done().
//
// Paxos is required to have forgotten all information
// about any instances it knows that are < Min().
// The point is to free up memory in long-running
// Paxos-based servers.
//
// Paxos peers need to exchange their highest Done()
// arguments in order to implement Min(). These
// exchanges can be piggybacked on ordinary Paxos
// agreement protocol messages, so it is OK if one
// peers Min does not reflect another Peers Done()
// until after the next instance is agreed to.
//
// The fact that Min() is defined as a minimum over
// *all* Paxos peers means that Min() cannot increase until
// all peers have been heard from. So if a peer is dead
// or unreachable, other peers Min()s will not increase
// even if all reachable peers call Done. The reason for
// this is that when the unreachable peer comes back to
// life, it will need to catch up on instances that it
// missed -- the other peers therefor cannot forget these
// instances.
//
func (px *Paxos) Min() int {
	// You code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	min := px.DoneList[px.me]
	for i := 0; i < len(px.peers); i++ {
		if px.DoneList[i] < min {
			min = px.DoneList[i]
		}
	}
	return min + 1
}

//
// the application wants to know whether this
// peer thinks an instance has been decided,
// and if so what the agreed value is. Status()
// should just inspect the local peer state;
// it should not contact other Paxos peers.
//
func (px *Paxos) Status(seq int) (bool, interface{}) {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	value := px.DecideList[seq]
	flag := false
	if value == nil {
		flag = false
	} else {
		flag = true
	}
	return flag, value
}

//
// tell the peer to shut itself down.
// for testing.
// please do not change this function.
//
func (px *Paxos) Kill() {
	px.dead = true
	if px.l != nil {
		px.l.Close()
	}
}

//
// the application wants to create a paxos peer.
// the ports of all the paxos peers (including this one)
// are in peers[]. this servers port is peers[me].
//
func Make(peers []string, me int, rpcs *rpc.Server) *Paxos{
	//default to unix socket
	return MakeUseTCP(peers, me, rpcs, false);
}

func MakeUseTCP(peers []string, me int, rpcs *rpc.Server, useTCP bool) *Paxos {
	px := &Paxos{}
	px.useTCP=useTCP;
	px.peers = peers
	px.me = me
	px.DoneList = make([]int, len(px.peers))
	for i := 0; i < len(px.peers); i++ {
		px.DoneList[i] = -1
	}
	px.DecideList = make(map[int]interface{})
	px.agreement = make(map[int]Proposal)

	if rpcs != nil {
		// caller will create socket &c
		rpcs.Register(px)
	} else {
		rpcs = rpc.NewServer()
		rpcs.Register(px)

		// prepare to receive connections from clients.
		// change "unix" to "tcp" to use over a network.
		if (px.useTCP){
			os.Remove(peers[me]) // only needed for "unix"
		}
		conntype := "unix";
		if (px.useTCP){
			conntype = "tcp";
		}
		l, e := net.Listen(conntype, peers[me])
		if e != nil {
			log.Fatal("listen error: ", e)
		}
		px.l = l

		// please do not change any of the following code,
		// or do anything to subvert it.

		// create a thread to accept RPC connections
		go func() {
			for px.dead == false {
				conn, err := px.l.Accept()
				if err == nil && px.dead == false {
					if px.unreliable && (rand.Int63()%1000) < 100 {
						// discard the request.
						conn.Close()
					} else if px.unreliable && (rand.Int63()%1000) < 200 {
						// process the request but force discard of reply.
						if (px.useTCP){
							c1 := conn.(*net.TCPConn)
							f, _ := c1.File()
							err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
							if err != nil {
								fmt.Printf("shutdown: %v\n", err)
							}
						}else{
							c1 := conn.(*net.UnixConn)
							f, _ := c1.File()
							err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
							if err != nil {
								fmt.Printf("shutdown: %v\n", err)
							}
						}
						px.rpcCount++
						go rpcs.ServeConn(conn)
					} else {
						px.rpcCount++
						go rpcs.ServeConn(conn)
					}
				} else if err == nil {
					conn.Close()
				}
				if err != nil && px.dead == false {
					fmt.Printf("Paxos(%v) accept: %v\n", me, err.Error())
				}
			}
		}()
	}

	return px
}
