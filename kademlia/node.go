package kademlia

import (
	"container/heap"
	"container/list"
	"crypto/sha1"
	"errors"
	"math/big"
	"net"
	"net/rpc"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	M          int = 160
	K          int = 20
	alpha      int = 3
	tExpire        = 86400 * time.Second
	tRefresh       = 5 * time.Second
	tReplicate     = 8 * time.Second
	tRepublish     = 86400 * time.Second
	timeout        = 10 * time.Second
)

var size big.Int

type Pair struct {
	Key  string
	Data string
}

type DataExpire struct {
	Data       string
	Expiretime int
}

type Node struct {
	Addr   string
	online bool

	listener net.Listener
	server   *rpc.Server

	data     map[string]DataExpire
	dataLock sync.RWMutex

	nbucket     [M]list.List
	nbucketLock sync.RWMutex

	timer int
}

type PQElement struct {
	key   big.Int
	value string
}

type PriorityQueue []PQElement

func (priority_queue PriorityQueue) Len() int { return len(priority_queue) }

func (priority_queue PriorityQueue) Less(i, j int) bool {
	return priority_queue[i].key.Cmp(&priority_queue[j].key) < 0
}

func (priority_queue PriorityQueue) Swap(i, j int) {
	priority_queue[i], priority_queue[j] = priority_queue[j], priority_queue[i]
}

func (priority_queue *PriorityQueue) Pop() interface{} {
	old := *priority_queue
	n := len(old)
	x := old[n-1]
	*priority_queue = old[0 : n-1]
	return x
}

func (priority_queue *PriorityQueue) Push(x interface{}) {
	*priority_queue = append(*priority_queue, x.(PQElement))
}

func init() {
	f, _ := os.Create("dht-test.log")
	logrus.SetOutput(f)
}

func HashString(s string) *big.Int {
	h := sha1.New()
	h.Write([]byte(s))
	return new(big.Int).SetBytes(h.Sum(nil))
}

func Distance(x *big.Int, y *big.Int) *big.Int {
	return new(big.Int).Xor(x, y)
}

func In(target string, bucket list.List) *list.Element {
	for it := bucket.Back(); it != nil; it = it.Prev() {
		if it.Value.(string) == target {
			return it
		}
	}
	return nil
}

func GetHighBit(num *big.Int) int {
	cnt := 0
	for num.Cmp(big.NewInt(0)) > 0 {
		num.Rsh(num, 1)
		cnt++
	}
	return cnt - 1
}

func (node *Node) Init(addr string) {
	node.Addr = addr
	node.data = make(map[string]DataExpire)
	size.Exp(big.NewInt(2), big.NewInt(int64(M)), nil)
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

	conn, err := net.DialTimeout("tcp", addr, timeout)
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

func (node *Node) TryUpdateBucket(addr string) {
	dis := Distance(HashString(node.Addr), HashString(addr))
	k := GetHighBit(dis)
	node.nbucketLock.RLock()
	pos := In(addr, node.nbucket[k])
	node.nbucketLock.RUnlock()
	if pos != nil {
		node.nbucketLock.Lock()
		tmp := (*pos).Value.(string)
		node.nbucket[k].Remove(pos)
		node.nbucket[k].PushBack(tmp)
		node.nbucketLock.Unlock()
		return
	}
	if node.nbucket[k].Len() < M {
		node.nbucketLock.Lock()
		node.nbucket[k].PushBack(addr)
		node.nbucketLock.Unlock()
	} else {
		flag := node.PingOther(node.nbucket[k].Front().Value.(string))
		if !flag {
			node.nbucketLock.Lock()
			node.nbucket[k].Remove(node.nbucket[k].Front())
			node.nbucket[k].PushBack(addr)
			node.nbucketLock.Unlock()
		}
	}
}

func (node *Node) PingOther(addr string) bool {
	if addr == node.Addr {
		return true
	}
	err := node.RemoteCall(addr, "Node.Ping", node.Addr, nil)
	if err == nil {
		node.TryUpdateBucket(addr)
		return true
	} else {
		dis := Distance(HashString(node.Addr), HashString(addr))
		k := GetHighBit(dis)
		node.nbucketLock.RLock()
		pos := In(addr, node.nbucket[k])
		node.nbucketLock.RUnlock()
		if pos != nil {
			node.nbucketLock.Lock()
			node.nbucket[k].Remove(pos)
			node.nbucketLock.Unlock()
		}
		return false
	}
}

func (node *Node) Ping(addr string, _ *struct{}) error {
	node.TryUpdateBucket(addr)
	return nil
}

func (node *Node) FindNClose(id big.Int, n int) []string {
	priority_queue := new(PriorityQueue)
	heap.Init(priority_queue)
	dis := Distance(&id, HashString(node.Addr))
	k := GetHighBit(dis)

	for i := k; priority_queue.Len() < n && i != M; {
		if i >= 0 {
			node.nbucketLock.RLock()
			for it := node.nbucket[i].Front(); it != nil; it = it.Next() {
				heap.Push(priority_queue, PQElement{*Distance(&id, HashString(it.Value.(string))), it.Value.(string)})
			}
			node.nbucketLock.RUnlock()
		}
		if i > 0 && i <= k {
			i--
		} else if i <= 0 && k >= 0 {
			i = k + 1
		} else {
			i++
		}
	}

	var closenode []string
	for i := 0; i < n && priority_queue.Len() > 0; i++ {
		closenode = append(closenode, heap.Pop(priority_queue).(PQElement).value)
	}

	return closenode
}

func (node *Node) FindNode(id big.Int, reply *[]string) error {
	*reply = node.FindNClose(id, K)
	return nil
}

func (node *Node) Store(data Pair, _ *struct{}) error {
	node.dataLock.Lock()
	value, ok := node.data[data.Key]
	if !ok {
		node.data[data.Key] = DataExpire{data.Data, 1}
	} else {
		node.data[data.Key] = DataExpire{value.Data, min(node.timer, value.Expiretime) + 1}
	}
	node.dataLock.Unlock()
	return nil
}

func (node *Node) FindValue(key string, reply *string) error {
	node.dataLock.RLock()
	val, ok := node.data[key]
	node.dataLock.RUnlock()
	if ok {
		*reply = val.Data
		return nil
	} else {
		return errors.New("no such data")
	}
}

func (node *Node) IterativeStore(data Pair) {
	list := node.IterativeFindNode(*HashString(data.Key))

	var wg sync.WaitGroup
	for _, val := range list {
		val := val
		wg.Add(1)
		go func() {
			node.RemoteCall(val, "Node.Store", &data, nil)
			wg.Done()
		}()
	}
	wg.Wait()
}

func (node *Node) IterativeFindNode(id big.Int) []string {
	waitgroup := make(chan bool, alpha)
	defer close(waitgroup)
	candidate := make(PriorityQueue, 0)
	queue := make(PriorityQueue, 0)
	var queueLock sync.RWMutex
	book := make(map[string]bool)
	var bookLock sync.RWMutex
	heap.Init(&queue)
	book[node.Addr] = true

	for _, val := range node.FindNClose(id, K) {
		flag := node.PingOther(val)
		if queue.Len() < alpha {
			if flag {
				Element := PQElement{*Distance(HashString(val), &id), val}
				candidate = append(candidate, Element)
				heap.Push(&queue, Element)
				book[val] = true
			}
		} else {
			break
		}
	}

	for {
		queueLock.RLock()
		queuelen := queue.Len()
		queueLock.RUnlock()
		if queuelen <= 0 && len(waitgroup) <= 0 {
			break
		}
		if queuelen > 0 {
			waitgroup <- true

			queueLock.Lock()
			target := heap.Pop(&queue).(PQElement).value
			queueLock.Unlock()

			bookLock.Lock()
			book[target] = true
			bookLock.Unlock()

			go func() {
				newcontact := make([]string, 0)
				err := node.RemoteCall(target, "Node.FindNode", &id, &newcontact)

				if err != nil {
					<-waitgroup
					return
				}

				node.PingOther(target)
				candidate = append(candidate, PQElement{*Distance(HashString(target), &id), target})

				for _, val := range newcontact {
					bookLock.RLock()
					_, ok := book[val]
					bookLock.RUnlock()
					if !ok {
						Element := PQElement{*Distance(HashString(val), &id), val}
						queueLock.Lock()
						heap.Push(&queue, Element)
						queueLock.Unlock()
						bookLock.Lock()
						book[val] = true
						bookLock.Unlock()
					}
				}

				<-waitgroup
			}()
		}
	}

	sort.Sort(candidate)

	ret := make([]string, 0)
	for i := 0; i < min(K, candidate.Len()); i++ {
		ret = append(ret, candidate[i].value)
	}

	return ret
}

func (node *Node) IterativeFindValue(key string) (bool, string) {
	id := *HashString(key)
	list := node.IterativeFindNode(id)

	logrus.Info(list)
	for _, val := range list {
		reply := new(string)
		err := node.RemoteCall(val, "Node.FindValue", &key, reply)
		node.PingOther(val)
		if err == nil {
			node.RemoteCall(list[0], "Node.Store", Pair{key, *reply}, nil)
			node.PingOther(list[0])
			return true, *reply
		}
	}

	return false, ""
}

func (node *Node) Timer() {
	for node.online {
		node.timer++
		time.Sleep(tReplicate)
	}
}

func (node *Node) BroadcastData() {
	for node.online {
		slice := make([]Pair, 0)
		node.dataLock.Lock()
		for key, value := range node.data {
			if value.Expiretime <= node.timer {
				slice = append(slice, Pair{key, value.Data})
				node.data[key] = DataExpire{value.Data, value.Expiretime + 1}
			}
		}
		node.dataLock.Unlock()

		for _, val := range slice {
			node.IterativeStore(val)
		}
		time.Sleep(tReplicate)
	}
}

func (node *Node) Run() {
	node.online = true
	go node.RunRPCServer()
	// go node.BroadcastData()
	go node.Timer()
}

func (node *Node) Create() {
	logrus.Info("Create")
}

func (node *Node) Join(addr string) bool {
	logrus.Infof("%s Join %s", node.Addr, addr)

	dis := Distance(HashString(addr), HashString(node.Addr))
	k := GetHighBit(dis)

	node.nbucketLock.Lock()
	node.nbucket[k].PushBack(addr)
	node.nbucketLock.Unlock()
	node.IterativeFindNode(*HashString(node.Addr))
	return true
}

func (node *Node) Quit() {
	logrus.Infof("Quit %s", node.Addr)
	if !node.online {
		return
	}

	slice := make([]Pair, 0)
	node.dataLock.RLock()
	for key, value := range node.data {
		if value.Expiretime <= node.timer {
			slice = append(slice, Pair{key, value.Data})
		}
	}
	node.dataLock.RUnlock()

	thread := make(chan bool, 3)
	defer close(thread)
	for _, val := range slice {
		thread <- true
		val := val
		go func() {
			node.IterativeStore(val)
			<-thread
		}()
	}

	for len(thread) > 0 {

	}

	node.StopRPCServer()
}

func (node *Node) ForceQuit() {
	logrus.Info("ForceQuit ", node.Addr)

	if !node.online {
		return
	}

	node.StopRPCServer()
}

func (node *Node) Put(key string, value string) bool {
	logrus.Infof("Put %s %s", key, value)
	node.IterativeStore(Pair{key, value})
	return true
}

func (node *Node) Get(key string) (bool, string) {
	logrus.Infof("%s Get %s", node.Addr, key)

	node.dataLock.RLock()
	value, ok := node.data[key]
	node.dataLock.RUnlock()

	if ok {
		return ok, value.Data
	}

	flag, ret := node.IterativeFindValue(key)
	logrus.Info("Get ", key, " ", flag)
	return flag, ret
}

func (node *Node) Delete(key string) bool {
	logrus.Infof("Delete %s", key)
	return true
}
