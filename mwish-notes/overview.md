Badger 有一个 managed mode, 是否是 managed mode, 标志是否由外部管理 txn 的 ts 等。

在这节，我们关注 badger 简单的读写流程和相关的对象。Badger 进行了很多很神奇的封装，有的时候，我认为它们是冗余的，但是它们对理解 Badger 内容是必要的。

读链路上，除了 `open()` `close`, 两个比较重要的方法如下：

```go
func (db *DB) View(fn func(txn *Txn) error) error
func (db *DB) Update(fn func(txn *Txn) error) error
```

这两个内部流程大概是：

```go
	txn := db.NewTransaction(/* 是否 update */)
	defer txn.Discard()

	if err := fn(txn); err != nil {
		return err
	}

	return txn.Commit()
```

然后进入事务流程

## 事务和水位

事务的结构体如下（这个只涉及内部实现，不太对外）：

```go

// Txn represents a Badger transaction.
type Txn struct {
  // 读写事务都有的读时间戳
	readTs   uint64
  // 只有写事务才有的时间戳
	commitTs uint64
	// size 和 count 仅对写事务有用
  // 估计的修改的事务的大小(key + value)
	size  int64
  // 修改的事务的个数
	count int64
	// 绑定的 db 对象, 读的起源.
	db *DB

	// 这里的读取不采用 key set, 采用 fingerprint, 这里不会误判, 但是可能有 false positive.
	// 这个是为写事务准备的 read set.
	reads []uint64 // contains fingerprints of keys read.
	// contains fingerprints of keys written. This is used for conflict detection.
	conflictKeys map[uint64]struct{}
	readsLock    sync.Mutex // guards the reads slice. See addReadKey.

	pendingWrites   map[string]*Entry // cache stores any writes done by txn.
	duplicateWrites []*Entry          // Used in managed mode to store duplicate entries.

	numIterators int32
	discarded    bool
	doneRead     bool
	// 是否是更新的事务.
	update bool // update is used to conditionally keep track of reads.
}
```

这里可以看到，有一些用于鉴定冲突的字段和状态字段。

我们要从上面这个 `txn` 的创建讲起，对应的内容在 `DB.NewTransaction`. 忽略 managed mode：

```go
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

这里会：

1. 给事务创建，然后添加 `Fin` 的边界值
2. 如果是写事务，添加 `conflictKeys` 做冲突检测，加 `pendingWrites` 做写入 buffer。
3. 拿到一个 `readTs` 作为读水位

写入的时候，`Set` 接口对应拿到的是 `key-value` pair：

```go
// Entry provides Key, Value, UserMeta and ExpiresAt. This struct can be used by
// the user to set data.
//
// Key, Value 和 TTL. meta 定义在 value.go 中, 比如删除的时候, 外部只会传入 key, meta 为
// del.
// 如果是 `Set` 的话, 就会 `NewEntry` (创建 key/value) -> `modify`(保证合法).
//
// 外部传进来的时候, 只有 Key, Value, ExpiresAt, UserMeta.
type Entry struct {
	Key       []byte
	Value     []byte
	ExpiresAt uint64 // time.Unix
	version   uint64
	offset    uint32 // offset is an internal field.
	UserMeta  byte
	meta      byte

	// Fields maintained internally.
	hlen         int // Length of the header.
	valThreshold int64
}
```

这里正常写入流程中，内容如下：

```go
&Entry{
		Key:  key,
		meta: bitDelete,
}

&Entry{
		Key:   key,
		Value: value,
}
```

我们重点关注 `Key` `Value` `ExpiresAt` `offset` `meta` 等字段。meta 内容很重要，甚至会写进 vLog:

```go
// maxVlogFileSize is the maximum size of the vlog file which can be created. Vlog Offset is of
// uint32, so limiting at max uint32.
var maxVlogFileSize uint32 = math.MaxUint32

// Values have their first byte being byteData or byteDelete. This helps us distinguish between
// a key that has never been seen and a key that has been explicitly deleted.
const (
	bitDelete                 byte = 1 << 0 // Set if the key has been deleted.
	bitValuePointer           byte = 1 << 1 // Set if the value is NOT stored directly next to key.
	BitDiscardEarlierVersions byte = 1 << 2 // Set if earlier versions can be discarded.
	// Set if item shouldn't be discarded via compactions (used by merge operator)
	bitMergeEntry byte = 1 << 3
	// The MSB 2 bits are for transactions.
	bitTxn    byte = 1 << 6 // Set if the entry is part of a txn.
	bitFinTxn byte = 1 << 7 // Set if the entry is to indicate end of txn in value log.

	mi int64 = 1 << 20

	// size of vlog header.
	// +----------------+------------------+
	// | keyID(8 bytes) |  baseIV(12 bytes)|
	// +----------------+------------------+
	vlogHeaderSize = 20
)
```

1. delete 表示是一个删除字段
2. ValuePointer 表示内容在 vLog
3. bitTxn 基本不用管，非 manage mode 的情况下，表示基本的内容写包裹在一个事务中。bitTxnFin 表示一个事务的结束，事务会 wrap 一个这样的字段进去。

然后事务写会记录在 `pendingWrites` 和 `conflictKeys` 里面，我们看看写入提交的时候，这里会发起一个 `Commit()` ，不过很有意思的是，Commit 在 `txn.Update` 的时候会自动处理 `Commit`. 这里的逻辑大概如下（代码很短，我就直接复制了）：

```go
func (txn *Txn) Commit() error {
	// txn.conflictKeys can be zero if conflict detection is turned off. So we
	// should check txn.pendingWrites.
	if len(txn.pendingWrites) == 0 {
		return nil // Nothing to do.
	}
	// Precheck before discarding txn.
	if err := txn.commitPrecheck(); err != nil {
		return err
	}
	defer txn.Discard()

	// 发送到 LSMTree 和 memtable, 来执行写.
	txnCb, err := txn.commitAndSend()
	if err != nil {
		return err
	}
	// If batchSet failed, LSM would not have been updated. So, no need to rollback anything.

	// TODO: What if some of the txns successfully make it to value log, but others fail.
	// Nothing gets updated to LSM, until a restart happens.
	return txnCb()
}
```

1. 检查是否没写入、是否有重复提交
2. `commitAndSend` 是最主要的逻辑，这里会保证写入 `writeCh` 的事务的 commit ts 是顺序的。
3. 调用 `oracle.newCommitTs` , oracle 会维护一个
4. 给事务的 key 编码上版本（以 bigendian u64 的形式），然后加上 FinTxn 的食物
5. 

我们看看 Oracle 维护的水位：

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
	discardTs uint64 // Used by ManagedDB.
	// 相当于读的水位
	readMark *y.WaterMark // Used by DB.

	// committedTxns contains all committed writes (contains fingerprints
	// of keys written and their latest commit counter).
	//
	// 缓存了 committed 的写, 随着读提高而释放部分.
	committedTxns []committedTxn
	lastCleanupTs uint64

	// closer is used to stop watermarks.
	closer *z.Closer
}
```

老样子，managed mode 我们不管。重要的是：

1. `committedTxns`, 一个模糊的事务冲突表。为什么说是模糊的呢？有亮点原因



