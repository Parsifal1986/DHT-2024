# Report For DHT Implementation

## Report for Chord Implementation

### Introduction

The Chord protocol is a distributed hash table (DHT) designed to provide a decentralized method for distributed systems to store and retrieve data efficiently. This report covers the implementation details of a Chord node, focusing on its interfaces and overall operation.

### Implementation Details

#### Node Structure

The `Node` struct represents a Chord node and includes various fields and synchronization mechanisms:

- **Addr**: Node address.
- **online**: Node status.
- **innet**: Whether the node is part of the network.
- **fixfinished, stablizefinished**: Channels to signal the completion of fix and stabilization processes.
- **threadfinished**: WaitGroup to manage concurrent operations.
- **listener**: Network listener for RPC.
- **server**: RPC server.
- **data, backupdata**: Maps to store primary and backup data.
- **fingerTable**: Array for finger table entries.
- **predecessor**: Address of the predecessor node.
- **successorlist**: List of successors.

#### Hashing and ID Space

The implementation uses SHA-1 hashing to convert strings and files to node IDs:

- **HashString**: Hashes a string and returns a big integer.
- **HashFile**: Hashes a file and returns a big integer.
- **In**: Determines if an ID is within a specific range in the identifier space.

#### Node Initialization and Server Management

- **Init**: Initializes a node with a given address.
- **RunRPCServer**: Starts the RPC server for the node.
- **StopRPCServer**: Stops the RPC server.
- **RemoteCall**: Facilitates remote procedure calls to other nodes.

#### Basic Operations

- **Ping**: Checks if a node is reachable.
- **FindSuccessor**: Finds the successor of a given ID.
- **FindPredecessor**: Finds the predecessor of a given ID.
- **ClosestPrecedingFinger**: Finds the closest preceding finger for an ID.
- **ChangeSuccessor, ChangePredecessor**: Updates successor and predecessor.

#### Maintenance Operations

- **Stablize**: Periodically checks and updates successor and predecessor pointers.
- **Notify**: Notifies a node about a potential predecessor.
- **FixFinger**: Periodically refreshes finger table entries.
- **MergeBackupData**: Merges backup data into primary data.
- **UpdateDataFor**: Updates data for a given node.
- **BackupDataFrom**: Backs up data from another node.
- **BackupData**: Stores backup data.
- **GetSuccessorlist**: Retrieves the successor list.

#### Data Operations

- **GetData, PutData, DeleteData**: Standard data retrieval, insertion, and deletion.
- **Put, Get, Delete**: High-level interfaces for data operations.

#### Network Management

- **Run**: Initializes data structures and starts the node.
- **Create**: Creates a new Chord ring.
- **Join**: Joins an existing Chord ring.
- **Quit, ForceQuit**: Gracefully and forcefully leave the network.

### Summary of Chord Protocol

Chord uses consistent hashing to assign keys to nodes. Each node maintains a finger table, a predecessor pointer, and a list of successors. Nodes communicate using RPC to maintain the network structure and handle data requests. The protocol includes mechanisms for stabilization, finger table maintenance, and data backup to ensure robustness and fault tolerance.

## Report on Kademlia-based Distributed Hash Table (DHT) Implementation

### Overview

This report describes the implementation of a Kademlia-based Distributed Hash Table (DHT) system in Go. Kademlia is a peer-to-peer (P2P) protocol that provides efficient storage and retrieval of key-value pairs in a decentralized network. This implementation includes the following key components: node management, data storage, and network communication using Remote Procedure Calls (RPC).

### Components

#### Constants and Variables

- **M**: The bit length of the keys, set to 160.
- **K**: The number of closest nodes to retain in a bucket, set to 20.
- **alpha**: The degree of parallelism in network requests, set to 3.
- **tExpire**: Time interval for data expiration, set to 86400 seconds.
- **tRefresh**: Time interval for refreshing buckets, set to 5 seconds.
- **tReplicate**: Time interval for data replication, set to 20 seconds.
- **tRepublish**: Time interval for data republishing, set to 86400 seconds.
- **timeout**: Time interval for RPC timeout, set to 10 seconds.
- **size**: A big integer representing the size of the keyspace.

#### Data Structures

- **Pair**: A struct representing a key-value pair.
- **DataExpire**: A struct representing data with an expiration time.
- **Node**: The main struct representing a Kademlia node, containing its address, status, RPC server, data store, and bucket lists.
- **PQElement**: A struct representing an element in the priority queue.
- **PriorityQueue**: A type representing a priority queue of `PQElement` elements.

#### Node Initialization and Management

- **Node.Init(addr string)**: Initializes a node with the given address and sets up its data structures.
- **Node.RunRPCServer()**: Starts the RPC server for the node, allowing it to handle incoming RPC requests.
- **Node.StopRPCServer()**: Stops the RPC server and sets the node status to offline.
- **Node.Create()**: Creates a new Kademlia network.
- **Node.Join(addr string) bool**: Allows a node to join an existing Kademlia network by contacting another node.

#### Data Storage and Retrieval

- **Node.Store(data Pair, _ *struct{}) error**: Stores a key-value pair in the node's data store.
- **Node.FindValue(key string, reply *string) error**: Retrieves the value associated with a given key from the node's data store.
- **Node.IterativeStore(data Pair)**: Stores a key-value pair iteratively across multiple nodes.
- **Node.IterativeFindValue(key string) (bool, string)**: Finds a value iteratively across multiple nodes.

#### Network Communication

- **Node.RemoteCall(addr string, method string, args interface{}, reply interface{}) error**: Makes an RPC call to another node.
- **Node.Ping(addr string, _ *struct{}) error**: Pings another node to check its availability.
- **Node.FindNode(id big.Int, reply *[]string) error**: Finds nodes closest to a given ID.
- **Node.PingOther(addr string) bool**: Pings another node and updates the bucket list based on the response.

#### Utility Functions

- **HashString(s string) *big.Int**: Computes the SHA-1 hash of a string and returns it as a big integer.
- **Distance(x *big.Int, y *big.Int) *big.Int**: Computes the XOR distance between two big integers.
- **In(target string, bucket list.List) *list.Element**: Checks if a target string is present in a given bucket.
- **GetHighBit(num *big.Int) int**: Returns the position of the highest set bit in a big integer.

### Functionality

#### Bucket Management

The implementation maintains a routing table organized into buckets. Each bucket stores node addresses that fall within a certain distance range from the current node. The routing table is updated based on the Kademlia protocol rules:

- **Node.TryUpdateBucket(addr string)**: Attempts to update the appropriate bucket with a new node address.
- **Node.FindNClose(id big.Int, n int) []string**: Finds the n closest nodes to a given ID using the routing table.
- **Node.IterativeFindNode(id big.Int) []string**: Iteratively finds nodes closest to a given ID across the network.

#### Data Replication and Refresh

The implementation includes mechanisms for data replication and refreshing to ensure data availability and consistency:

- **Node.Timer()**: Increments a timer for managing data expiration.
- **Node.BroadcastData()**: Broadcasts data that is close to expiration to other nodes for replication.

#### Node Operations

The implementation supports various node operations, including putting, getting, and deleting data:

- **Node.Put(key string, value string) bool**: Stores a key-value pair in the DHT.
- **Node.Get(key string) (bool, string)**: Retrieves a value associated with a key from the DHT.
- **Node.Delete(key string) bool**: Deletes a key-value pair from the DHT.

### Logging and Debugging

The implementation uses the **logrus** library for logging various events and errors, which helps in debugging and monitoring the system.

### Conclusion

This Kademlia-based DHT implementation in Go provides a robust and efficient mechanism for decentralized storage and retrieval of key-value pairs. The use of buckets, iterative operations, and RPC communication ensures scalability and reliability in a distributed network. The implementation includes essential functionalities such as node initialization, data storage and retrieval, bucket management, and data replication, making it a comprehensive solution for P2P systems.
