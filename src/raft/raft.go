package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"MIT-6.824-2017/src/labrpc"
)

// import "bytes"
// import "encoding/gob"

// define state
const (
	FOLLOWER int = iota
	LEADER
	CANDIDATE
)

const (
	HEARTBEATINTERVAL time.Duration = time.Millisecond * 50
)

//
// LogEntry contains command for state machinem, and term when entry was recieved
// by leader
//
type LogEntry struct {
	Command interface{}
	Term    int
}

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	// persistent state on all servers
	currentTerm int
	votedFor    int //candidatedId that received vote in current term
	logs        []LogEntry

	//volatile state on all servers
	committedIndex int //index of highest log entry known to be committed
	lastApplied    int //index of highest log entry applied to state machine

	//volatile state on leaders
	nextIndex  []int
	matchIndex []int

	//granted  vote number
	//用于统计赞成投票的数量
	grantedVotesCount int

	//state and apply message chan
	state   int
	applyCh chan ApplyMsg

	//用channel来server之间的消息
	chanGrantVote chan bool
	chanLeader    chan bool
	chanHeartbeat chan bool

	//Log and Timer
	// logger *log.Logger
	// 用于节点定时器
	timer *time.Timer
}

//生成指定区间内的随机数
func GenerateRangeNum(min, max int) int {
	rand.Seed(time.Now().Unix()) //选取时间为随机数种子
	randNum := rand.Intn(max-min) + min
	return randNum
}

// func (rf *Raft) resetTimer() {
// 	electionTimeout := GenerateRangeNum(150, 300)
// 	rf.timer = time.NewTimer(time.Millisecond * time.Duration(electionTimeout))
// 	<-rf.timer.C
// 	go rf.handleTimer()
// }

func (rf *Raft) getLastLogIndex() int {
	if len(rf.logs) == 0 {
		return -1
	}
	return len(rf.logs) - 1
}

func (rf *Raft) getLastLogTerm() int {
	if len(rf.logs) == 0 {
		return -1
	}
	return rf.logs[len(rf.logs)-1].Term
}

func (rf *Raft) CandidateAction() {
	rf.mu.Lock()
	//defer rf.mu.Unlock()

	rf.currentTerm++
	rf.votedFor = rf.me
	rf.grantedVotesCount = 1
	rf.persist()

	//初始化requestvote
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.me,
		LastLogIndex: len(rf.logs) - 1,
	}
	if len(rf.logs) > 0 {
		args.LastLogTerm = rf.logs[rf.getLastLogIndex()].Term
	}
	rf.mu.Unlock()

	for server := range rf.peers {
		if server == rf.me {
			continue
		}
		go func(server int, args RequestVoteArgs) {
			var reply RequestVoteReply
			ok := rf.sendRequestVote(server, &args, &reply)
			if ok == 0 || ok == 1 {
				rf.handleVoteResult(reply)
			}
		}(server, args)

	}
	select {
	case <-time.After(time.Duration(GenerateRangeNum(150, 300)) * time.Millisecond):
		rf.state = CANDIDATE
	case <-rf.chanHeartbeat:
		rf.state = FOLLOWER
	case <-rf.chanLeader:
	}
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	term = rf.currentTerm
	isleader = (rf.state == LEADER)
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := gob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}

//
// appendEntry rpc arguments
//
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PreLogIndex  int
	PreLogTerm   int
	Entries      []LogEntry
	LeaderCommit int
}

//
//reply appendEntry  arguments
//
type AppendEntriesReply struct {
	Term    int
	Success bool
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

// example RequestVote RPC handler.
// 投票时，candidate的判断
// 接收到RequestVote RPC的时候，首先
//1、如果vote's term > candidate‘s term vote false
//2、如果vote’s term < candidate's term
//将自己变成follower;如果当前尚未投票，将票投给candidate
//3、如果vote's term = candidate‘s term,比较lastlogindex
// RequestVote
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//vote's term > candidate's term 直接return
	if rf.currentTerm > args.Term {
		reply.Term = rf.currentTerm //return vote's term
		reply.VoteGranted = false
		return
	}

	//candidates newer than votes,此时无论是candidate还是voter，set currentTerm=T,convert to follower
	if rf.currentTerm < args.Term {
		rf.state = FOLLOWER
		rf.currentTerm = args.Term
		//rf.votedFor = -1 //voteFor需要重置，因为当前term它还没有进行投票

		rf.votedFor = args.CandidateID

		//设置返回消息
		reply.Term = args.Term
		reply.VoteGranted = true
		//rf.chanGrantVote <- true
		return
	}

	//candidates equal votes
	if rf.currentTerm == args.Term {
		//compare log entry index
		if rf.getLastLogIndex() <= args.LastLogIndex && rf.getLastLogTerm() <= args.LastLogTerm && rf.votedFor == -1 {
			rf.state = FOLLOWER
			rf.votedFor = args.CandidateID
			rf.currentTerm = args.Term
			reply.Term = args.Term
			reply.VoteGranted = true
		} else {
			reply.Term = rf.currentTerm
			reply.VoteGranted = false
		}
		return
	}
}

func (rf *Raft) handleVoteResult(reply RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//old term ignore
	if reply.Term < rf.currentTerm {
		return
	}

	//newer term push current peer to follower
	if reply.Term > rf.currentTerm {
		rf.currentTerm = reply.Term
		rf.state = FOLLOWER
		rf.votedFor = -1
		return
	}

	//is candidate and receive true
	if rf.state == CANDIDATE && reply.VoteGranted {
		rf.grantedVotesCount++
		if rf.grantedVotesCount >= len(rf.peers)/2+1 { //receive majority of votes
			//become leader and send rpcs
			rf.state = LEADER
			rf.chanLeader <- true
			for server := range rf.peers {
				if server == rf.me {
					continue
				}
				rf.nextIndex[server] = rf.getLastLogIndex() + 1
				rf.matchIndex[server] = 0
			}
			//rf.resetTimer()
		}
		return
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
// 该函数用于调用rpc请求
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) int {
	fmt.Printf("candidate%v send requestvote rpc to server%v,current term is %v\n", rf.me, server, rf.currentTerm)
	ok := make(chan bool, 1)
	var re bool
	go func(ok chan bool) {
		ok <- rf.peers[server].Call("Raft.RequestVote", args, reply)
	}(ok)
	select {
	case <-time.After(time.Millisecond * 60):
		fmt.Printf("server%v can't recieve from server%v,current term is %v\n", rf.me, server, rf.currentTerm)
		return -1
	case re = <-ok:
		fmt.Printf("server%v recieve vote from server%v, is %v,currresultent term is %v\n", rf.me, server, re, rf.currentTerm)
		if re == false {
			return 0
		} else {
			return 1
		}
	}
}

//SendAppendEntriedToFollower appendentries and heartbeats to a follower
func (rf *Raft) SendAppendEntriedToFollower(server int, args AppendEntriesArgs, reply *AppendEntriesReply) int {
	//返回0和1都表示rpc正常
	//返回-1表示出现超时
	fmt.Printf("leader%v send appendentry rpc to followers,current term is %v\n", rf.me, rf.currentTerm)
	ok := make(chan bool, 1)
	//ok <- rf.peers[server].Call("Raft.AppendEntries", args, reply)
	var re bool
	go func(ok chan bool) {
		ok <- rf.peers[server].Call("Raft.AppendEntries", args, reply)
	}(ok)
	select {
	case <-time.After(time.Millisecond * 60):
		fmt.Printf("Leader%v can't recieve from server%v,current term is %v\n", rf.me, server, rf.currentTerm)
		return -1
	case re = <-ok:
		fmt.Printf("Leader%v recieve vote from server%v,result is %v,current term is %v\n", rf.me, server, re, rf.currentTerm)
		if re == false {
			return 0
		} else {
			return 1
		}
	}
}

func (rf *Raft) SendAppnedEntriesToAllFollower() {
	for server := range rf.peers {
		if server == rf.me {
			continue
		}

		args := AppendEntriesArgs{
			Term:        rf.currentTerm,
			LeaderId:    rf.me,
			PreLogIndex: rf.getLastLogIndex(),
			PreLogTerm:  rf.getLastLogTerm(),
		}
		if rf.nextIndex[server] < rf.getLastLogIndex() {
			args.Entries = rf.logs[rf.nextIndex[server]:]
		}
		args.LeaderCommit = rf.committedIndex

		var reply AppendEntriesReply
		//for {
		ok := rf.SendAppendEntriedToFollower(server, args, &reply)
		if ok == 0 || ok == 1 {
			rf.handleAppendEntriesReply(server, reply)
			//break
		}
		//}

	}
}

// folllower handle appendenttry reply
func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}

	rf.chanHeartbeat <- true
	//heartbeat
	if args.Entries == nil {
		reply.Success = true
	}
	return
}

// handle the appemndrequest result
func (rf *Raft) handleAppendEntriesReply(server int, reply AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != LEADER {
		return
	}

	//leader shoule degenerate to follower

	if reply.Term > rf.currentTerm {
		rf.state = FOLLOWER
		rf.currentTerm = reply.Term
		rf.votedFor = -1
		//rf.resetTimer()
		return
	}

	if reply.Success {
		return
	}
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.logs = make([]LogEntry, 0)

	rf.committedIndex = -1
	rf.lastApplied = -1

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.state = FOLLOWER
	rf.applyCh = applyCh

	rf.chanGrantVote = make(chan bool, 100)
	rf.chanHeartbeat = make(chan bool, 100)
	rf.chanLeader = make(chan bool, 100)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.persist()

	go func() {
		for {
			switch rf.state {
			case FOLLOWER:
				select {
				case <-rf.chanHeartbeat:
					fmt.Printf("Follower%v has got heartbeat,current term is %v\n", rf.me, rf.currentTerm) //如果接收到heartbeat
				case <-rf.chanGrantVote:
					fmt.Printf("Follower%v has votefor candidate%v,current term is %v\n", rf.me, rf.votedFor, rf.currentTerm)
				case <-time.After(time.Millisecond * time.Duration(GenerateRangeNum(150, 300))):
					rf.state = CANDIDATE
					fmt.Printf("Follower%v find time out and become candidate%v,current term is %v\n", rf.me, rf.me, rf.currentTerm)
				}
			case LEADER:
				rf.SendAppnedEntriesToAllFollower()
				fmt.Printf("Leader%v send heartbeat,current term is %v\n", rf.me, rf.currentTerm)
				time.Sleep(HEARTBEATINTERVAL)
			case CANDIDATE:
				rf.CandidateAction()
				fmt.Printf("Become candidate%v,current term is %v\n", rf.me, rf.currentTerm)
			}
		}
	}()
	//rf.resetTimer()
	return rf
}
