package chord

import (
	"crypto/sha1"
	"errors"
	"io"
	"math/big"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

func init() {
	f, _ := os.Create("dht-test.log")
	logrus.SetOutput(f)
}

const M int = 160

type Node struct {
	Addr             string
	online           bool
	innet            bool
	innetLock        sync.RWMutex
	fixfinished      chan int
	stablizefinished chan int

	listener net.Listener
	server   *rpc.Server

	data           map[string]string
	dataLock       sync.RWMutex
	backupdata     map[string]string
	backupdataLock sync.RWMutex

	fingerTable       [M]string
	fingerTableLock   sync.RWMutex
	predecessor       string
	predecessorLock   sync.RWMutex
	successorlist     []string
	successorlistLock sync.RWMutex
}

type Pair struct {
	Key   string
	Value string
}

var size big.Int

func HashString(s string) *big.Int {
	h := sha1.New()
	h.Write([]byte(s))
	return new(big.Int).SetBytes(h.Sum(nil))
}

func HashFile(file *os.File) big.Int {
	h := sha1.New()
	io.Copy(h, file)
	return *new(big.Int).SetBytes(h.Sum(nil))
}

func In(target *big.Int, left *big.Int, right *big.Int, leftflag, rightflag bool) bool {
	if left.Cmp(right) != -1 {
		if ((target.Cmp(left) == 1 || (target.Cmp(left) == 0 && leftflag)) &&
			(target.Cmp(&size) == -1 || (target.Cmp(&size) == 0 && rightflag))) ||
			((target.Cmp(big.NewInt(0)) == 1 || (target.Cmp(big.NewInt(0)) == 0 && leftflag)) &&
				(target.Cmp(right) == -1 || (target.Cmp(right) == 0 && rightflag))) {
			return true
		} else {
			return false
		}
	} else {
		if (target.Cmp(left) == 1 || (target.Cmp(left) == 0 && leftflag)) &&
			(target.Cmp(right) == -1 || (target.Cmp(right) == 0 && rightflag)) {
			return true
		} else {
			return false
		}
	}
}

func (node *Node) Init(addr string) {
	node.Addr = addr
	node.innet = false
	node.dataLock.Lock()
	node.data = make(map[string]string)
	node.dataLock.Unlock()
	node.backupdataLock.Lock()
	node.backupdata = make(map[string]string)
	node.backupdataLock.Unlock()
	size.Exp(big.NewInt(2), big.NewInt(int64(M)), nil)
	node.fingerTableLock.Lock()
	for i := 0; i < M; i++ {
		node.fingerTable[i] = node.Addr
	}
	node.fingerTableLock.Unlock()
	node.stablizefinished = make(chan int)
	node.fixfinished = make(chan int)
	node.successorlistLock.Lock()
	node.successorlist = make([]string, 10)
	node.successorlist[0] = addr
	node.successorlistLock.Unlock()
}

func (node *Node) RunRPCServer() {
	node.server = rpc.NewServer()
	node.server.Register(node)

	var err error
	node.listener, err = net.Listen("tcp", node.Addr)
	if err != nil {
		logrus.Fatal("Listen error:", err)
	}

	for node.online {
		conn, err := node.listener.Accept()

		if err != nil {
			logrus.Error("Accept error:", err)
			return
		}
		go node.server.ServeConn(conn)
	}
}

func (node *Node) StopRPCServer() {
	node.online = false
	node.listener.Close()
}

func (node *Node) RemoteCall(addr string, method string, args interface{}, reply interface{}) error {
	if method != "Node.Ping" {
		logrus.Infof("[%s] RemoteCall %s %s %v", node.Addr, addr, method, args)
	}

	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		logrus.Error("Dialing:", err)
		return err
	}

	client := rpc.NewClient(conn)
	defer client.Close()
	err = client.Call(method, args, reply)
	if err != nil {
		logrus.Error("RemoteCall error:", err)
		return err
	}
	return nil
}

func (node *Node) Ping(_ string, _ *struct{}) error {
	return nil
}

func (node *Node) FindSuccessor(id big.Int, reply *string) error {
	// node.innetLock.RLock()
	// defer node.innetLock.RUnlock()
	if !node.innet {
		return errors.New("hasn't create a net yet")
	}

	if id.Cmp(HashString(node.Addr)) == 0 {
		node.fingerTableLock.RLock()
		err := node.RemoteCall(node.fingerTable[0], "Node.Ping", new(string), nil)
		node.fingerTableLock.RUnlock()
		if err != nil {
			logrus.Error(err)
			flag := false
			for i := 0; i < 10; i++ {
				node.successorlistLock.RLock()
				possible_successor := node.successorlist[0]
				node.successorlistLock.RUnlock()
				if possible_successor == "" {
					node.successorlistLock.Lock()
					copy(node.successorlist, node.successorlist[1:])
					node.successorlistLock.Unlock()
					continue
				}
				ok := node.RemoteCall(possible_successor, "Node.Ping", new(string), nil)
				if ok == nil {
					node.fingerTableLock.Lock()
					node.fingerTable[0] = possible_successor
					node.fingerTableLock.Unlock()
					flag = true
					node.RemoteCall(possible_successor, "Node.ChangePredecessor", node.Addr, nil)
					node.RemoteCall(possible_successor, "Node.MergeBackupData", new(string), nil)
					node.dataLock.RLock()
					node.RemoteCall(possible_successor, "Node.BackupDataFrom", node.data, nil)
					node.dataLock.RUnlock()
					break
				}
				node.successorlistLock.Lock()
				copy(node.successorlist, node.successorlist[1:])
				node.successorlistLock.Unlock()
			}
			if !flag {
				return errors.New("lost successor connect")
			}
		}
		node.fingerTableLock.RLock()
		*reply = node.fingerTable[0]
		node.fingerTableLock.RUnlock()
		return nil
	}

	prime := new(string)
	*prime = node.Addr
	node.FindPredecessor(id, prime)
	if err := node.RemoteCall(*prime, "Node.FindSuccessor", HashString(*prime), reply); err != nil {
		return err
	}
	return nil
}

func (node *Node) FindPredecessor(id big.Int, reply *string) error {
	logrus.Infof("%s FindPredecessor %v", node.Addr, &id)

	node.innetLock.RLock()
	defer node.innetLock.RUnlock()
	if !node.innet {
		return errors.New("hasn't create a net yet")
	}

	if id.Cmp(HashString(node.Addr)) == 0 {
		node.predecessorLock.RLock()
		*reply = node.predecessor
		node.predecessorLock.RUnlock()
		return nil
	}

	ret, successor := new(string), new(string)
	*ret = node.Addr
	node.fingerTableLock.RLock()
	*successor = node.fingerTable[0]
	node.fingerTableLock.RUnlock()

	targetHash, retHash, successHash := new(big.Int), HashString(node.Addr), HashString(*successor)
	*targetHash = id

	for !In(targetHash, retHash, successHash, false, true) {
		err := node.RemoteCall(*ret, "Node.ClosestPrecedingFinger", targetHash, ret)
		if err != nil {
			logrus.Error(err)
			*ret = node.Addr
			continue
		}
		retHash = HashString(*ret)
		node.RemoteCall(*ret, "Node.FindSuccessor", retHash, successor)
		successHash = HashString(*successor)
	}

	*reply = *ret
	return nil
}

func (node *Node) ClosestPrecedingFinger(id big.Int, reply *string) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()
	var cur, next, target *big.Int
	cur, target = HashString(node.Addr), &id

	for i := M - 1; i >= 0; i-- {
		node.fingerTableLock.RLock()
		if node.fingerTable[i] == "" {
			node.fingerTableLock.RUnlock()
			continue
		}

		next = HashString(node.fingerTable[i])
		node.fingerTableLock.RUnlock()

		if In(next, cur, target, false, false) {
			node.fingerTableLock.RLock()
			err := node.RemoteCall(node.fingerTable[i], "Node.Ping", new(string), nil)
			if err != nil {
				logrus.Error(err)
				node.fingerTableLock.RUnlock()
				continue
			}
			*reply = node.fingerTable[i]
			node.fingerTableLock.RUnlock()
			return nil
		}
	}

	*reply = node.Addr
	return nil
}

func (node *Node) ChangeSuccessor(addr string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.fingerTableLock.Lock()
	node.fingerTable[0] = addr
	node.fingerTableLock.Unlock()
	return nil
}

func (node *Node) ChangePredecessor(addr string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.predecessorLock.Lock()
	node.predecessor = addr
	node.predecessorLock.Unlock()
	return nil
}

func (node *Node) Stablize() {
	defer close(node.stablizefinished)
	for {
		logrus.Info("stablize ", node.Addr)
		node.innetLock.RLock()
		if !node.innet {
			node.innetLock.RUnlock()
			return
		}
		node.innetLock.RUnlock()

		possible_successor := new(string)
		node.fingerTableLock.RLock()
		err := node.RemoteCall(node.fingerTable[0], "Node.FindPredecessor", HashString(node.fingerTable[0]), possible_successor)
		node.fingerTableLock.RUnlock()
		if err != nil {
			logrus.Error(err)
			node.FindSuccessor(*HashString(node.Addr), new(string))
		}

		node.fingerTableLock.Lock()
		if In(HashString(*possible_successor), HashString(node.Addr), HashString(node.fingerTable[0]), false, false) {
			node.fingerTable[0] = *possible_successor

			node.dataLock.RLock()
			node.RemoteCall(node.fingerTable[0], "Node.BackupDataFrom", node.data, nil)
			node.dataLock.RUnlock()
		}
		node.RemoteCall(node.fingerTable[0], "Node.Notify", node.Addr, nil)
		NextSuccessorlist := make([]string, 10)
		err = node.RemoteCall(node.fingerTable[0], "Node.GetSuccessorlist", new(string), &NextSuccessorlist)
		if err == nil {
			node.successorlistLock.Lock()
			node.successorlist[0] = node.fingerTable[0]
			copy(node.successorlist[1:], NextSuccessorlist[:9])
			node.successorlistLock.Unlock()
		}
		node.fingerTableLock.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

func (node *Node) Notify(addr string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()
	node.predecessorLock.Lock()
	if node.predecessor == "" || In(HashString(addr), HashString(node.predecessor), HashString(node.Addr), false, false) {
		node.predecessor = addr
	}
	node.predecessorLock.Unlock()
	return nil
}

func (node *Node) FixFinger() {
	pos := 0
	defer close(node.fixfinished)
	for {
		logrus.Info(node.Addr, "fix fingertable before innnet lock ok")
		node.innetLock.RLock()
		logrus.Info(node.Addr, "fix fingertable after innet lock ok")
		if !node.innet {
			node.innetLock.RUnlock()
			return
		}
		node.innetLock.RUnlock()

		pos = (pos + 3) % 160

		logrus.Info(node.Addr, "fix fingertable", pos)
		new_finger := new(string)
		node.FindSuccessor(*new(big.Int).Mod(new(big.Int).Add(HashString(node.Addr), new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(pos)), nil)), &size), new_finger)
		logrus.Info(node.Addr, "fix fingertable before fingertablelock ok")
		node.fingerTableLock.Lock()
		logrus.Info(node.Addr, "fix fingertable after fingertablelock ok")
		node.fingerTable[pos] = *new_finger
		node.fingerTableLock.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}

func (node *Node) test() {
	for {
		node.innetLock.RLock()
		if !node.innet {
			node.innetLock.RUnlock()
			return
		}
		node.innetLock.RUnlock()

		node.fingerTableLock.RLock()
		node.predecessorLock.RLock()
		logrus.Infof("Node Data: Addr: %v Successor %v Predecessor %v Hashnum %v", node.Addr, node.fingerTable[0], node.predecessor, HashString(node.Addr))
		node.successorlistLock.RLock()
		logrus.Info("successorlist", node.successorlist)
		node.successorlistLock.RUnlock()
		node.fingerTableLock.RUnlock()
		node.predecessorLock.RUnlock()
		time.Sleep(100 * time.Millisecond)
	}
}

func (node *Node) MergeBackupData(_ string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.dataLock.Lock()
	node.backupdataLock.RLock()
	for key, value := range node.backupdata {
		node.data[key] = value
	}
	node.backupdataLock.RUnlock()
	node.dataLock.Unlock()

	node.dataLock.RLock()
	node.RemoteCall(node.fingerTable[0], "Node.BackupDataFrom", node.data, nil)
	node.dataLock.RUnlock()

	return nil
}

func (node *Node) UpdateDataFor(addr string, reply *map[string]string) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.dataLock.RLock()
	for key, value := range node.data {
		if !In(HashString(key), HashString(addr), HashString(node.Addr), false, true) {
			(*reply)[key] = value
			delete(node.data, key)
		}
	}
	node.dataLock.RUnlock()

	return nil
}

func (node *Node) BackupDataFrom(backupdata map[string]string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.backupdataLock.Lock()
	node.backupdata = backupdata
	node.backupdataLock.Unlock()

	return nil
}

func (node *Node) BackupData(data Pair, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.backupdataLock.Lock()
	node.backupdata[data.Key] = data.Value
	node.backupdataLock.Unlock()

	return nil
}

func (node *Node) GetSuccessorlist(_ string, reply *[]string) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.successorlistLock.RLock()
	*reply = node.successorlist
	node.successorlistLock.RUnlock()

	return nil
}

func (node *Node) GetData(key string, reply *string) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.dataLock.RLock()
	value, ok := node.data[key]
	node.dataLock.RUnlock()
	if ok {
		*reply = value
		return nil
	}

	return errors.New("no such data")
}

func (node *Node) PutData(data Pair, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.dataLock.Lock()
	node.data[data.Key] = data.Value
	node.dataLock.Unlock()

	node.fingerTableLock.RLock()
	node.RemoteCall(node.fingerTable[0], "Node.BackupData", data, nil)
	node.fingerTableLock.RUnlock()

	return nil
}

func (node *Node) DeleteData(key string, _ *struct{}) error {
	node.innetLock.RLock()
	defer node.innetLock.RUnlock()

	node.dataLock.RLock()
	_, ok := node.data[key]
	node.dataLock.RUnlock()
	if ok {
		node.dataLock.Lock()
		delete(node.data, key)
		node.dataLock.Unlock()
		return nil
	}

	return errors.New("no such data")
}

func (node *Node) Run() {
	node.online = true
	go node.RunRPCServer()
}

func (node *Node) Create() {
	logrus.Info("Create")
	node.dataLock.Lock()
	node.data = make(map[string]string)
	node.dataLock.Unlock()
	node.predecessorLock.Lock()
	node.predecessor = node.Addr
	node.predecessorLock.Unlock()
	node.innetLock.Lock()
	node.innet = true
	node.innetLock.Unlock()
	go node.Stablize()
	go node.FixFinger()
	go node.test()
}

func (node *Node) Join(addr string) bool {
	logrus.Infof("Join %s", addr)
	if addr != "" {
		node.fingerTableLock.Lock()
		err := node.RemoteCall(addr, "Node.FindSuccessor", HashString(node.Addr), &node.fingerTable[0])
		node.fingerTableLock.Unlock()

		if err != nil {
			logrus.Error(err)
			return false
		}

		node.fingerTableLock.RLock()
		node.dataLock.Lock()
		node.RemoteCall(node.fingerTable[0], "Node.UpdateDataFor", node.Addr, &node.data)
		node.dataLock.Unlock()

		node.dataLock.RLock()
		node.RemoteCall(node.fingerTable[0], "Node.BackupDataFrom", node.data, nil)
		node.dataLock.RUnlock()
		node.fingerTableLock.RUnlock()

		node.predecessorLock.Lock()
		node.predecessor = ""
		node.predecessorLock.Unlock()

		node.innetLock.Lock()
		node.innet = true
		node.innetLock.Unlock()

		go node.Stablize()
		go node.FixFinger()
		go node.test()

		return true
	} else {
		return false
	}
}

func (node *Node) Quit() {
	logrus.Infof("Quit %s", node.Addr)
	if !node.online {
		return
	}

	node.innetLock.Lock()
	node.innet = false
	node.innetLock.Unlock()
	<-node.stablizefinished
	<-node.fixfinished

	node.fingerTableLock.RLock()
	successor := node.fingerTable[0]
	node.fingerTableLock.RUnlock()

	node.predecessorLock.RLock()
	predecessor := node.predecessor
	node.predecessorLock.RUnlock()

	if successor != node.Addr {
		node.RemoteCall(successor, "Node.MergeBackupData", new(string), nil)

		node.backupdataLock.RLock()
		node.RemoteCall(successor, "Node.BackupDataFrom", node.backupdata, nil)
		node.backupdataLock.RUnlock()

		if predecessor != "" {
			node.RemoteCall(successor, "Node.ChangePredecessor", predecessor, nil)
			node.RemoteCall(predecessor, "Node.ChangeSuccessor", successor, nil)
		}
	}

	node.StopRPCServer()
}

func (node *Node) ForceQuit() {
	logrus.Info("ForceQuit ", node.Addr)
	if !node.online {
		return
	}

	node.innetLock.Lock()
	logrus.Info("ForceQuit Step 1 good ", node.Addr)
	node.innet = false
	node.innetLock.Unlock()

	node.StopRPCServer()
}

func (node *Node) Put(key string, value string) bool {
	logrus.Infof("Put %s %s", key, value)
	hashvalue := HashString(key)
	successor := new(string)
	node.FindSuccessor(*hashvalue, successor)
	node.RemoteCall(*successor, "Node.PutData", Pair{key, value}, nil)
	return true
}

func (node *Node) Get(key string) (bool, string) {
	logrus.Infof("%s Get %s", node.Addr, key)
	targetnode, data := new(string), new(string)

	err := node.FindSuccessor(*HashString(key), targetnode)
	if err != nil {
		logrus.Error(err)
		return false, ""
	}

	err = node.RemoteCall(*targetnode, "Node.GetData", key, data)
	if err != nil {
		logrus.Error(err)
		return false, ""
	}

	return true, *data
}

func (node *Node) Delete(key string) bool {
	logrus.Infof("Delete %s", key)
	targetnode := new(string)

	err := node.FindSuccessor(*HashString(key), targetnode)
	if err != nil {
		logrus.Error(err)
		return false
	}

	err = node.RemoteCall(*targetnode, "Node.DeleteData", key, nil)

	return err == nil
}
