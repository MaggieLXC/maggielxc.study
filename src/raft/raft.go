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
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"sort"
	"sync"
	"time"

	"MIT-6.824-2017/src/labrpc"
	. "github.com/logrusorgru/aurora"
)

func init() {
	// 利用pprof监测进程的运行状况
	go http.ListenAndServe("localhost:6060", nil)

	// 打印日志的时候，精确到时间戳
	log.SetFlags(log.Lmicroseconds)
}

// define state that a servre in the raft can be
const (
	FOLLOWER int = iota
	LEADER
	CANDIDATE
)

// define heartbeat interval
const (
	HEARTBEATINTERVAL time.Duration = time.Millisecond * 30
)

// define log class undo
const (
	LEADERSENDHEARTBEAT int = 10
	LEADERSENDAPPENDENTRY
	CANDIDATESENDVOTEFOR
	FOLLOWERTIMEOUTBECABDIDATE
	FOLLOWERRECEIVEAPPENDENTRY
	FOLLOWERRESPONSEAPPENDENTRY
)

// LogEntry contains command for state machine, and term when entry was recieved
// by leader
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
	chanApply     chan ApplyMsg

	//Log and Timer
	// logger *log.Logger
	// 用于节点定时器
	timer *time.Timer
}

// GenerateRangeNum : 生成指定区间内的随机数
func GenerateRangeNum(min, max int) int {
	//rand.Seed(time.Now().UnixNano()) //选取时间为随机数种子
	randNum := rand.Intn(max-min) + min
	//log.Printf("election-out time: %v\n", randNum)
	return randNum
}

func (rf *Raft) getLastLogIndex() int {

	return len(rf.logs) - 1
}

func (rf *Raft) getLastLogTerm() int {

	//attention first log entry(index=0) is nil
	//client log entry always begin at index=1
	if len(rf.logs) == 1 {
		return 0
	}
	return rf.logs[len(rf.logs)-1].Term
}

// CandidateRequestVote : when a server become cadidate, it will do
func (rf *Raft) CandidateRequestVote(requestargs RequestVoteArgs) {

	//同时发送给除了自己以外的servers,注意考虑消息返回超时的情况
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
		}(server, requestargs)
	}

	//这里要考虑到在发送的过程中出现：
	//1、选举失败，一定时间内没有majority同意选举
	//2、收到来自新leader的heartbeat
	//3、自己选举成功
	select {
	case <-time.After(time.Duration(GenerateRangeNum(450, 600)) * time.Millisecond):
		rf.state = CANDIDATE
	case <-rf.chanHeartbeat:
		rf.state = FOLLOWER
	case <-rf.chanLeader:
	}
}

//
// return currentTerm and whether this server
// believes it is the leader.
//
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
	LeaderID     int
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

//
// example RequestVote RPC handler.
// 给candidate投票时的判断
//1、如果voter's term > candidate‘s term vote false
//2、如果vote’s term < candidate's term
//   将自己变成follower
//3、compare if the candidate's entry is at least as latest as voters
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	log.Printf("server%v begin RequestVote, it's logentry is :%v, and term is%v, commitIndex is:%v", rf.me, rf.logs, rf.currentTerm, rf.committedIndex)
	//vote's term > candidate's term 直接return
	if rf.currentTerm > args.Term {
		reply.Term = rf.currentTerm //return vote's term
		reply.VoteGranted = false
		return
	}
	if rf.currentTerm < args.Term {
		rf.currentTerm = args.Term
		rf.state = FOLLOWER
		rf.votedFor = -1
	}

	log.Printf("voter%v's getLastLogTerm:%v getLastLogTerm:%v commitIndex:%v, votedFor:%v, cadidate%v's LastLogTerm: %v, getLastLogIndex: %v, ",
		rf.me, rf.getLastLogTerm(), rf.getLastLogIndex(), rf.committedIndex, rf.votedFor,
		args.CandidateID, args.LastLogTerm, args.LastLogIndex)

	// if the log is at least as latest as voter's, then vote
	if (rf.getLastLogTerm() < args.LastLogTerm || rf.getLastLogIndex() <= args.LastLogIndex) && (rf.votedFor == -1) {
		rf.state = FOLLOWER
		rf.votedFor = args.CandidateID
		rf.currentTerm = args.Term
		reply.Term = args.Term
		reply.VoteGranted = true
		rf.chanGrantVote <- true
		log.Printf("server%v agree cadidate%v,and update it's term to %v", rf.me, args.CandidateID, rf.currentTerm)
	} else {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		//rf.chanGrantVote <- false
		log.Printf("server%v disagree cadidate%v,and it's term is %v", rf.me, args.CandidateID, rf.currentTerm)
	}
	return
	//}
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
			//become leader
			//first it should intialize its nextIndex and matchIndex
			//and send rpcs
			rf.state = LEADER
			//log.Printf("Candidate%v become Leader", rf.me)
			rf.chanLeader <- true
			for server := range rf.peers {
				// 这里matchIndex同时也更新自己，然后用中位数的方式判断哪个应该提交
				rf.nextIndex[server] = rf.getLastLogIndex() + 1
				rf.matchIndex[server] = 0
			}
		}
		//log.Printf("server%v become leader,and initial the nextIndex: %v, and matchIndex: %v\n", rf.me, rf.nextIndex, rf.matchIndex)
		//log.Printf("server%v's log length is: %v", rf.me, len(rf.logs))
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
	//log.Printf("candidate%v send requestvote rpc to server%v,current term is %v\n", rf.me, server, rf.currentTerm)

	ok := make(chan bool, 1)
	var re bool

	// 启用一个协程发送请求，使用channel防止超时
	go func(ok chan bool) {
		ok <- rf.peers[server].Call("Raft.RequestVote", args, reply)
	}(ok)

	// 要对发送超时进行处理
	select {
	case <-time.After(time.Millisecond * 60):
		//log.Printf("server%v can't recieve from server%v,current term is %v\n", rf.me, server, rf.currentTerm)
		return -1
	case re = <-ok:
		//log.Printf("server%v recieve vote from server%v, is %v,current result term is %v\n", rf.me, server, re, rf.currentTerm)
		if re == false {
			return 0
		}
		return 1
	}
}

//SendAppendEntriedToFollower appendentries and heartbeats to a follower
func (rf *Raft) SendAppendEntriedToFollower(server int, args AppendEntriesArgs, reply *AppendEntriesReply) int {
	//返回0和1都表示rpc正常
	//返回-1表示出现超时

	//log.Printf("leader%v send appendentry rpc to followers,current term is %v\n", rf.me, rf.currentTerm)
	ok := make(chan bool, 1)
	//ok <- rf.peers[server].Call("Raft.AppendEntries", args, reply)

	var re bool
	go func(ok chan bool) {
		ok <- rf.peers[server].Call("Raft.AppendEntries", args, reply)
	}(ok)

	select {
	case <-time.After(time.Millisecond * 60):
		//log.Printf("Leader%v can't recieve from server%v,current term is %v\n", rf.me, server, rf.currentTerm)
		return -1
	case re = <-ok:
		//log.Printf("Leader%v recieve appendentriesReply from server%v,result is %v,current term is %v\n", rf.me, server, re, rf.currentTerm)
		if re == false {
			return 0
		}
		return 1
	}
}

//SendAppnedEntriesToAllFollower ：
func (rf *Raft) SendAppnedEntriesToAllFollower() {
	for server := range rf.peers {
		if server == rf.me {
			continue
		}
		//log.Printf("send heartbeat to %d", server)

		rf.mu.Lock()
		args := AppendEntriesArgs{
			Term:     rf.currentTerm,
			LeaderID: rf.me,
		}

		args.PreLogIndex = rf.nextIndex[server] - 1
		//log.Printf("nextIndex:%v\n", rf.nextIndex)
		args.PreLogTerm = rf.logs[args.PreLogIndex].Term
		args.LeaderCommit = rf.committedIndex
		args.Entries = rf.logs[args.PreLogIndex+1:]
		rf.mu.Unlock()

		var reply AppendEntriesReply
		go func(server int, args AppendEntriesArgs, reply *AppendEntriesReply) {
			ok := rf.SendAppendEntriedToFollower(server, args, reply)
			if ok == 0 || ok == 1 {
				rf.handleAppendEntriesReply(server, args, reply)

			}
		}(server, args, &reply)
	}

	go func() {
		rf.doCommitCheck()
		rf.doCommit()
		//log.Printf("leader commit:%v", rf.committedIndex)
	}()

	//要对发送超时进行处理
	select {
	case <-time.After(time.Millisecond * 60):
		return
	}
}

// AppendEntries ： folllower handle appendenttry reply
func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// leader‘s term < follower’s term，reply false
	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}

	log.Printf("Append entries: follower%v's infomation: logs:%v, commitIndex:%v, getLastLogIndex:%v getLastLogTerm:%v",
		rf.me, rf.logs, rf.committedIndex, rf.getLastLogIndex(), rf.getLastLogTerm())
	log.Printf("Append entries: leader%v's infomation: entries:%v, LeaderCommit:%v, PrelogIndex:%v, PreLogterm:%v",
		args.LeaderID, args.Entries, args.LeaderCommit, args.PreLogIndex, args.PreLogTerm)

	// this also act as heartbeat
	rf.chanHeartbeat <- true
	// if rpc args's term > current term:set currentTerm and convert to follower
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = FOLLOWER
		rf.votedFor = -1
	}
	reply.Term = args.Term

	// there is something to handle
	// judge if prelogindex and prelogterm 是否匹配
	// （1）index不匹配，args log length > rf log length
	// （2）args log length <= rf log length but same index but different term

	if args.PreLogIndex > rf.getLastLogIndex() || (args.PreLogIndex != 0 && args.PreLogTerm != rf.logs[args.PreLogIndex].Term) {
		reply.Success = false
		return
	}

	// as heartbeat or new leader check if the last log is match
	if len(args.Entries) == 0 {
		reply.Success = true

		if args.LeaderCommit > rf.committedIndex {
			rf.committedIndex = minority(args.LeaderCommit, rf.getLastLogIndex())
		}
		go rf.doCommit()
		return
	}

	// same index and term
	//remove the log entries follow the match point
	rf.logs = rf.logs[:args.PreLogIndex+1]
	rf.logs = append(rf.logs, args.Entries...)

	// if leaderCommit<commitIndex,set commitIndex=min(leaderCommit,index of last new entry)
	if args.LeaderCommit > rf.committedIndex {
		rf.committedIndex = minority(args.LeaderCommit, rf.getLastLogIndex())
	}
	reply.Success = true
	go rf.doCommit()
	return
}

func minority(a int, b int) int {
	if a > b {
		return b
	}
	return a
}

// handle the appemndrequest result
func (rf *Raft) handleAppendEntriesReply(server int, args AppendEntriesArgs, reply *AppendEntriesReply) {
	//log.Printf("handler appendEntires reply %d, reply: %+v, args: %+v", server, reply, args)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	log.Printf("leader%v get reply from server%v,result is:%v ,and logentry: %v", rf.me, server, reply.Success, args.Entries)
	//only leader handle the reply
	if rf.state != LEADER {
		return
	}

	// when get a reply,server with any role should check the term
	// once term larger,it should be follower
	//leader shoule degenerate to follower
	if reply.Term > rf.currentTerm {
		rf.state = FOLLOWER
		rf.currentTerm = reply.Term
		rf.votedFor = -1
		return
	}

	if reply.Success {
		// if it is heatbeat or the new leader send message to try the matchIndex
		if args.Entries == nil {
			return
		}

		// if entries is not empty, and get successs reply
		// should do some update
		// update nextindx and match index
		rf.nextIndex[server] = args.PreLogIndex + len(args.Entries) + 1
		rf.matchIndex[server] = rf.nextIndex[server] - 1
		//log.Print(Magenta(fmt.Sprintf("leader%v update server%v's nextIndex: %v and matchIndex: %v", rf.me, server, rf.nextIndex[server], rf.matchIndex[server])))
		log.Printf("leader%v update server%v's nextIndex: %v and matchIndex: %v to nextIndex: %v, matchIndex: %v", rf.me, server, rf.nextIndex, rf.matchIndex, rf.nextIndex[server], rf.matchIndex[server])

	} else {
		// reply failure,if it's heartbeat, only when the follower's term larger which has handled
		// so here only handle the append entry failure which hanppens the mis match

		if rf.nextIndex[server] != 1 {
			rf.nextIndex[server]-- //must revert
		}

	}
}

// check if there is an N
// a majority of matchIndex[i]>=N &&
// log[N].term == currentTerm：
// set commitIndex =N
func (rf *Raft) doCommitCheck() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// leader change
	if rf.state != LEADER {
		return
	}

	//取中位数作为commit的点
	//log.Printf("matchIndex:%v", rf.matchIndex)
	//indice := rf.matchIndex[:]
	indice := make([]int, len(rf.matchIndex))
	for k, v := range rf.matchIndex {
		indice[k] = v
	}
	sort.Ints(indice)

	tmpCommitIndex := indice[len(rf.matchIndex)/2]
	//log.Printf("matchIndex: %v,tmpCommitIndex: %v, isLeader:%v", rf.matchIndex, tmpCommitIndex, rf.state == LEADER)
	log.Printf("leader%v's indice: %v and tmpcommitIndex: %v, leadercommit: %v", rf.me, indice, tmpCommitIndex, rf.committedIndex)

	if rf.committedIndex < tmpCommitIndex {
		rf.committedIndex = tmpCommitIndex
		//log.Printf("indice: %v, leader commitIndex changed: %v", indice, rf.committedIndex)
		log.Printf("Leader%v  doCommitChcek, and change it's commitIndex: %v", rf.me, rf.committedIndex)
	}

	return
}

func (rf *Raft) doCommit() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	for i := rf.lastApplied + 1; i <= rf.committedIndex; i++ {
		//log.Printf("server%v docommit,isleader:%v,i： %d, lastApplied is: %v, commitIndex is: %v", rf.me, rf.state == LEADER, i, rf.lastApplied, rf.committedIndex)

		rf.applyCh <- ApplyMsg{i, rf.logs[i].Command, false, nil}
		rf.lastApplied = i
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
	rf.mu.Lock()
	rf.mu.Unlock()

	index := 0
	term := 0
	isLeader := rf.state == LEADER
	nlog := LogEntry{command, rf.currentTerm}
	// Your code here (2B).

	if isLeader {
		rf.logs = append(rf.logs, nlog)
		index = len(rf.logs) - 1
		term = rf.currentTerm
		rf.matchIndex[rf.me] = index
		log.Print(Magenta(fmt.Sprintf("Leader%v is write data,matchindex change to: %v", rf.me, rf.matchIndex)))
		rf.persist()
	}

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
	rf.logs = make([]LogEntry, 1)

	rf.committedIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.state = FOLLOWER
	rf.applyCh = applyCh

	rf.chanGrantVote = make(chan bool, 100)
	rf.chanHeartbeat = make(chan bool, 100)
	rf.chanLeader = make(chan bool, 100)
	rf.chanApply = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.persist()

	rand.New(rand.NewSource(time.Now().UnixNano()))
	go func() {
		for {
			switch rf.state {
			case FOLLOWER:
				rf.Follower()
			case LEADER:
				rf.Leader()
			case CANDIDATE:
				rf.Candidate()
			}
		}
	}()
	return rf
}

// Leader : 成为Leader之后，常规要做的事情就是给Followers发送心跳和appendentries的消息
func (rf *Raft) Leader() {
	rf.SendAppnedEntriesToAllFollower()
	//log.Printf("Leader%v send heartbeat,current term is %v\n", rf.me, rf.currentTerm)
	time.Sleep(HEARTBEATINTERVAL)
}

// Candidate : 成为Candidate，说明新一轮的选举开始了，需要进行一些操作
func (rf *Raft) Candidate() {
	rf.mu.Lock()
	//defer rf.mu.Unlock()

	rf.currentTerm++
	log.Printf("server%v become candidate, and increase the term, %v,log is: %v", rf.me, rf.currentTerm, rf.logs)
	rf.votedFor = rf.me
	rf.grantedVotesCount = 1
	rf.persist()

	//初始化requestvote
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.me,
		LastLogIndex: rf.getLastLogIndex(),
		LastLogTerm:  rf.getLastLogTerm(),
	}
	rf.mu.Unlock()
	rf.CandidateRequestVote(args)
}

// Follower ：Follwer在正常情况下要做的
// 1、接受心跳rpc并回复
// 2、接受vote rpc并回复
// 3、election time out，开始新一轮的选举
func (rf *Raft) Follower() {
	select {
	case <-rf.chanHeartbeat:
		//log.Printf("Follower%v has got heartbeat,current term is %v\n", rf.me, rf.currentTerm) //如果接收到heartbeat
	case <-rf.chanGrantVote:
		//log.Printf("chanGrantVote Follower%v has votefor candidate%v,current term is %v\n", rf.me, rf.votedFor, rf.currentTerm)
	case <-time.After(time.Millisecond * time.Duration(GenerateRangeNum(450, 600))):
		rf.state = CANDIDATE
		//log.Printf("Follower%v find time out and become candidate%v,current term is %v\n", rf.me, rf.me, rf.currentTerm)
	}

}

//日志打印 undo
func (rf *Raft) logPrint(server int, role int, kind int) {
	switch role {
	case LEADER:
		if kind == LEADERSENDHEARTBEAT {
		}
	case FOLLOWER:
	case CANDIDATE:
	}
}
