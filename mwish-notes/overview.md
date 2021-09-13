读的时候：

```go
// View executes a function creating and managing a read-only transaction for the user. Error
// returned by the function is relayed by the View method.
// If View is used with managed transactions, it would assume a read timestamp of MaxUint64.
func (db *DB) View(fn func(txn *Txn) error) error {
	if db.IsClosed() {
		return ErrDBClosed
	}
	var txn *Txn
	if db.opt.managedTxns {
		txn = db.NewTransactionAt(math.MaxUint64, false)
	} else {
		txn = db.NewTransaction(false)
	}
	defer txn.Discard()

	return fn(txn)
}

// Update executes a function, creating and managing a read-write transaction
// for the user. Error returned by the function is relayed by the Update method.
// Update cannot be used with managed transactions.
func (db *DB) Update(fn func(txn *Txn) error) error {
	if db.IsClosed() {
		return ErrDBClosed
	}
	if db.opt.managedTxns {
		panic("Update can only be used with managedDB=false.")
	}
	txn := db.NewTransaction(true)
	defer txn.Discard()

	if err := fn(txn); err != nil {
		return err
	}

	return txn.Commit()
}
```

这里先不管 `managedTxns`, 走到 `NewTransaction` 中：

```go
// NewTransaction creates a new transaction. Badger supports concurrent execution of transactions,
// providing serializable snapshot isolation, avoiding write skews. Badger achieves this by tracking
// the keys read and at Commit time, ensuring that these read keys weren't concurrently modified by
// another transaction.
//
// For read-only transactions, set update to false. In this mode, we don't track the rows read for
// any changes. Thus, any long running iterations done in this mode wouldn't pay this overhead.
//
// Running transactions concurrently is OK. However, a transaction itself isn't thread safe, and
// should only be run serially. It doesn't matter if a transaction is created by one goroutine and
// passed down to other, as long as the Txn APIs are called serially.
//
// When you create a new transaction, it is absolutely essential to call
// Discard(). This should be done irrespective of what the update param is set
// to. Commit API internally runs Discard, but running it twice wouldn't cause
// any issues.
//
//  txn := db.NewTransaction(false)
//  defer txn.Discard()
//  // Call various APIs.
func (db *DB) NewTransaction(update bool) *Txn {
	return db.newTransaction(update, false)
}

func (db *DB) newTransaction(update, isManaged bool) *Txn {
	if db.opt.ReadOnly && update {
		// DB is read-only, force read-only transaction.
		update = false
	}

	txn := &Txn{
		update: update,
		db:     db,
		count:  1,                       // One extra entry for BitFin.
		size:   int64(len(txnKey) + 10), // Some buffer for the extra entry.
	}
	if update {
		if db.opt.DetectConflicts {
			txn.conflictKeys = make(map[uint64]struct{})
		}
		txn.pendingWrites = make(map[string]*Entry)
	}
	if !isManaged {
		txn.readTs = db.orc.readTs()
	}
	return txn
}
```

txn 结构如下：

```go
// Txn represents a Badger transaction.
type Txn struct {
	readTs   uint64
	commitTs uint64
	size     int64
	count    int64
	db       *DB

	reads []uint64 // contains fingerprints of keys read.
	// contains fingerprints of keys written. This is used for conflict detection.
	conflictKeys map[uint64]struct{}
	readsLock    sync.Mutex // guards the reads slice. See addReadKey.

	pendingWrites   map[string]*Entry // cache stores any writes done by txn.
	duplicateWrites []*Entry          // Used in managed mode to store duplicate entries.

	numIterators int32
	discarded    bool
	doneRead     bool
	update       bool // update is used to conditionally keep track of reads.
}
```

我们可以看到，这里有几个时间戳，这个时间戳可以看 oracle 模块：

```go
type oracle struct {
	isManaged       bool // Does not change value, so no locking required.
	detectConflicts bool // Determines if the txns should be checked for conflicts.

	sync.Mutex // For nextTxnTs and commits.
	// writeChLock lock is for ensuring that transactions go to the write
	// channel in the same order as their commit timestamps.
	writeChLock sync.Mutex
	nextTxnTs   uint64

	// Used to block NewTransaction, so all previous commits are visible to a new read.
	txnMark *y.WaterMark

	// Either of these is used to determine which versions can be permanently
	// discarded during compaction.
	discardTs uint64       // Used by ManagedDB.
	readMark  *y.WaterMark // Used by DB.

	// committedTxns contains all committed writes (contains fingerprints
	// of keys written and their latest commit counter).
	committedTxns []committedTxn
	lastCleanupTs uint64

	// closer is used to stop watermarks.
	closer *z.Closer
}
```

1. Watermark 模块提供了 `Begin` 和 `Wait` 语义，并带上了 index，这个模块会顺序处理里面的逻辑，然后通知前面的 Waiter.
2. Closer 是一个封装过的 WaitGroup, 用来做一些 close 相关的语义。